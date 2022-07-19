package lnmux

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/types"
	"github.com/btcsuite/btcd/chaincfg"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/htlcswitch/hop"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"go.uber.org/zap"
)

type Mux struct {
	registry *InvoiceRegistry
	sphinx   *hop.OnionProcessor

	lnd    []lnd.LndClient
	logger *zap.SugaredLogger
}

type MuxConfig struct {
	KeyRing         keychain.SecretKeyRing
	ActiveNetParams *chaincfg.Params

	Lnd      []lnd.LndClient
	Logger   *zap.SugaredLogger
	Registry *InvoiceRegistry
}

func New(cfg *MuxConfig) (*Mux,
	error) {

	idKeyDesc, err := cfg.KeyRing.DeriveKey(
		keychain.KeyLocator{
			Family: keychain.KeyFamilyNodeKey,
			Index:  0,
		},
	)
	if err != nil {
		return nil, err
	}

	nodeKeyECDH := keychain.NewPubKeyECDH(idKeyDesc, cfg.KeyRing)

	replayLog := &replayLog{}

	sphinxRouter := sphinx.NewRouter(
		nodeKeyECDH, cfg.ActiveNetParams, replayLog,
	)

	sphinx := hop.NewOnionProcessor(sphinxRouter)

	return &Mux{
		registry: cfg.Registry,
		sphinx:   sphinx,
		lnd:      cfg.Lnd,
		logger:   cfg.Logger,
	}, nil
}

func (p *Mux) Run(ctx context.Context) error {
	registryErrChan := make(chan error)
	go func() {
		registryErrChan <- p.registry.Run(ctx)
	}()

	// Start multiplexer main loop.
	err := p.run(ctx)
	if err != nil {
		return err
	}

	// Await registry termination.
	err = <-registryErrChan
	if err != nil {
		return err
	}

	p.logger.Infof("Multiplexer stopped")

	return nil
}

type interceptedHtlc struct {
	source         common.PubKey
	circuitKey     types.CircuitKey
	hash           lntypes.Hash
	onionBlob      []byte
	amountMsat     int64
	expiry         uint32
	outgoingChanID uint64

	reply func(*interceptedHtlcResponse) error
}

type interceptedHtlcResponse struct {
	action         routerrpc.ResolveHoldForwardAction
	preimage       lntypes.Preimage
	failureMessage []byte
	failureCode    lnrpc.Failure_FailureCode
}

func (p *Mux) run(mainCtx context.Context) error {
	ctx, cancel := context.WithCancel(mainCtx)
	defer cancel()

	var wg sync.WaitGroup
	defer wg.Wait()

	// Register for htlc interception and block events.
	htlcChan := make(chan *interceptedHtlc)
	heightChan := make(chan int)

	for _, lnd := range p.lnd {
		interceptor := newInterceptor(lnd, p.logger, htlcChan, heightChan)

		wg.Add(1)
		go func(ctx context.Context) {
			defer wg.Done()

			interceptor.run(ctx)
		}(ctx)
	}

	// All connected lnd nodes will immediately send the current block height.
	// Pick up the first height received to initialize our local height.
	var height int
	select {
	case height = <-heightChan:
	case <-ctx.Done():
		return nil
	}

	p.logger.Debugw("Starting main event loop")
	for {
		select {
		case receivedHeight := <-heightChan:
			// Keep track of the highest height only. Perhaps this can be made
			// more sophisticated in the future.
			if receivedHeight > height {
				height = receivedHeight
			}

		case htlc := <-htlcChan:
			// Only intercept htlcs for the virtual channel.
			if htlc.outgoingChanID != virtualChannel {
				err := htlc.reply(&interceptedHtlcResponse{
					action: routerrpc.ResolveHoldForwardAction_RESUME,
				})

				if err != nil {
					p.logger.Errorw("htlc reply error", "err", err)
				}

				break
			}

			err := p.ProcessHtlc(htlc, height)
			if err != nil {
				return err
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func marshallFailureCode(code lnwire.FailCode) (
	lnrpc.Failure_FailureCode, error) {

	switch code {
	case lnwire.CodeInvalidOnionHmac:
		return lnrpc.Failure_INVALID_ONION_HMAC, nil

	case lnwire.CodeInvalidOnionVersion:
		return lnrpc.Failure_INVALID_ONION_VERSION, nil

	case lnwire.CodeInvalidOnionKey:
		return lnrpc.Failure_INVALID_ONION_KEY, nil

	default:
		return 0, fmt.Errorf("unsupported code %v", code)
	}
}

func (p *Mux) ProcessHtlc(
	htlc *interceptedHtlc, height int) error {

	logger := p.logger.With(
		"hash", htlc.hash,
		"source", htlc.source,
		"circuitKey", htlc.circuitKey,
	)

	logger.Infow("Htlc received")

	fail := func(code lnwire.FailCode) error {
		logger.Debugw("Failing htlc", "code", code)

		rpcCode, err := marshallFailureCode(code)
		if err != nil {
			return err
		}

		return htlc.reply(&interceptedHtlcResponse{
			action:      routerrpc.ResolveHoldForwardAction_FAIL,
			failureCode: rpcCode,
		})
	}

	// Try decode final hop onion. Expiry can be set to zero, because the
	// replay log is disabled.
	onionReader := bytes.NewReader(htlc.onionBlob)
	iterator, failCode := p.sphinx.DecodeHopIterator(
		onionReader, htlc.hash[:], uint32(height),
	)
	if failCode != lnwire.CodeNone {
		logger.Debugw("Cannot decode hop iterator")

		return fail(failCode)
	}

	payload, err := iterator.HopPayload()
	if err != nil {
		return err
	}

	obfuscator, failCode := iterator.ExtractErrorEncrypter(
		p.sphinx.ExtractErrorEncrypter,
	)
	if failCode != lnwire.CodeNone {
		logger.Debugw("Cannot extract error encryptor")

		return fail(failCode)
	}

	resolve := func(resolution HtlcResolution) error {
		// Determine required action for the resolution based on the type of
		// resolution we have received.
		switch res := resolution.(type) {

		case *HtlcSettleResolution:
			logger.Debugw("Sending settle resolution",
				"outcome", res.Outcome)

			return htlc.reply(&interceptedHtlcResponse{
				action:   routerrpc.ResolveHoldForwardAction_SETTLE,
				preimage: res.Preimage,
			})

		case *HtlcFailResolution:
			logger.Debugw("Sending failed resolution",
				"outcome", res.Outcome)

			var failureMessage lnwire.FailureMessage
			if res.Outcome == ResultMppTimeout {
				failureMessage = &lnwire.FailMPPTimeout{}
			} else {
				failureMessage = lnwire.NewFailIncorrectDetails(
					lnwire.MilliSatoshi(htlc.amountMsat), 0,
				)
			}

			reason, err := obfuscator.EncryptFirstHop(failureMessage)
			if err != nil {
				return err
			}

			// Here we need more control over htlc
			// interception so that we can send back an
			// encrypted failure message to the sender.
			return htlc.reply(&interceptedHtlcResponse{
				action:         routerrpc.ResolveHoldForwardAction_FAIL,
				failureMessage: reason,
			})

		// Fail if we do not get a settle of fail resolution, since we
		// are only expecting to handle settles and fails.
		default:
			return fmt.Errorf("unknown htlc resolution type: %T",
				resolution)
		}
	}

	// Notify the invoice registry of the intercepted htlc.
	p.registry.NotifyExitHopHtlc(
		&registryHtlc{
			rHash:         htlc.hash,
			amtPaid:       lnwire.MilliSatoshi(htlc.amountMsat),
			expiry:        htlc.expiry,
			currentHeight: int32(height),
			circuitKey:    htlc.circuitKey,
			payload:       payload,
			resolve: func(res HtlcResolution) {
				err := resolve(res)
				if err != nil {
					logger.Errorf("resolve error", "err", err)
				}
			},
		},
	)

	return nil
}
