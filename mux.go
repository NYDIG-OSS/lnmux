package lnmux

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/htlcswitch/hop"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"go.uber.org/zap"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
)

const (
	htlcHoldDuration = time.Second
)

type Mux struct {
	registry *InvoiceRegistry
	sphinx   *hop.OnionProcessor

	lnd    []lnd.LndClient
	logger *zap.SugaredLogger

	wg   sync.WaitGroup
	quit chan struct{}
}

type MuxConfig struct {
	KeyRing         keychain.SecretKeyRing
	ActiveNetParams *chaincfg.Params

	ChannelDb InvoiceDb
	Lnd       []lnd.LndClient
	Logger    *zap.SugaredLogger
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

	registry := NewRegistry(
		cfg.ChannelDb,
		&RegistryConfig{
			Clock:                clock.NewDefaultClock(),
			FinalCltvRejectDelta: 10,
			HtlcHoldDuration:     htlcHoldDuration,
			Logger:               cfg.Logger,
		},
	)

	nodeKeyECDH := keychain.NewPubKeyECDH(idKeyDesc, cfg.KeyRing)

	replayLog := &replayLog{}

	sphinxRouter := sphinx.NewRouter(
		nodeKeyECDH, cfg.ActiveNetParams, replayLog,
	)

	sphinx := hop.NewOnionProcessor(sphinxRouter)

	return &Mux{
		registry: registry,
		sphinx:   sphinx,
		lnd:      cfg.Lnd,
		logger:   cfg.Logger,

		quit: make(chan struct{}),
	}, nil
}

func (p *Mux) Start() error {
	if err := p.registry.Start(); err != nil {
		return err
	}

	// Start multiplexer main loop.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		err := p.run()
		if err != nil {
			p.logger.Errorf("Invoice multiplexer error: %v", err)
		}
	}()

	return nil
}

func (p *Mux) Stop() error {
	close(p.quit)
	p.wg.Wait()

	if err := p.registry.Stop(); err != nil {
		return err
	}

	p.logger.Infof("Multiplexer stopped")

	return nil
}

type interceptedHtlc struct {
	source         common.PubKey
	circuitKey     CircuitKey
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

type interceptedHtlcResponseWithKey struct {
	*interceptedHtlcResponse

	incomingCircuitKey CircuitKey
}

func (p *Mux) interceptHtlcs(ctx context.Context,
	lnd lnd.LndClient, htlcChan chan *interceptedHtlc, heightChan chan int) {

	var (
		respChan = make(chan *interceptedHtlcResponseWithKey)
		pubKey   = lnd.PubKey()
	)

	start := func() error {
		p.logger.Debugw("Starting htlc interception", "node", pubKey)

		// DEBUG TIMEOUT.
		// streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
		streamCtx, streamCancel := context.WithCancel(ctx)
		defer streamCancel()

		send, recv, err := lnd.HtlcInterceptor(streamCtx)
		if err != nil {
			return fmt.Errorf("cannot subscribe: %w", err)
		}

		// Register for block notifications.
		blockChan, blockErrChan, err := lnd.RegisterBlockEpochNtfn(streamCtx)
		if err != nil {
			return err
		}

		// The block stream immediately sends the current block. Read that to
		// set our initial height.
		select {
		case block := <-blockChan:
			heightChan <- int(block.Height)

		case <-streamCtx.Done():
			return streamCtx.Err()
		}

		errChan := make(chan error, 1)

		var wg sync.WaitGroup
		defer wg.Wait()
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				htlc, err := recv()
				if err != nil {
					errChan <- err

					return
				}

				hash, err := lntypes.MakeHash(htlc.PaymentHash)
				if err != nil {
					errChan <- err

					return
				}

				circuitKey := newCircuitKeyFromRPC(htlc.IncomingCircuitKey)

				reply := func(resp *interceptedHtlcResponse) error {
					select {
					case respChan <- &interceptedHtlcResponseWithKey{
						interceptedHtlcResponse: resp,
						incomingCircuitKey:      circuitKey,
					}:
					case <-ctx.Done():
						return ctx.Err()
					}

					return nil
				}

				select {
				case htlcChan <- &interceptedHtlc{
					source:         pubKey,
					circuitKey:     circuitKey,
					hash:           hash,
					onionBlob:      htlc.OnionBlob,
					amountMsat:     int64(htlc.OutgoingAmountMsat),
					expiry:         htlc.OutgoingExpiry,
					outgoingChanID: htlc.OutgoingRequestedChanId,
					reply:          reply,
				}:

				case <-streamCtx.Done():
					errChan <- streamCtx.Err()

					return
				}
			}
		}()

		for {
			select {
			case resp := <-respChan:
				circuitKey := &routerrpc.CircuitKey{
					ChanId: resp.incomingCircuitKey.ChanID,
					HtlcId: resp.incomingCircuitKey.HtlcID,
				}
				rpcResp := &routerrpc.ForwardHtlcInterceptResponse{
					IncomingCircuitKey: circuitKey,
					Action:             resp.action,
					Preimage:           resp.preimage[:],
					FailureMessage:     resp.failureMessage,
					FailureCode:        resp.failureCode,
				}
				err := send(rpcResp)
				if err != nil {
					return fmt.Errorf(
						"cannot send: %w", err)
				}

			case err := <-errChan:
				return fmt.Errorf("stream error: %w", err)

			case block := <-blockChan:
				select {
				case heightChan <- int(block.Height):

				case <-streamCtx.Done():
					return streamCtx.Err()
				}

			case err := <-blockErrChan:
				return fmt.Errorf("block error: %w", err)

			case <-streamCtx.Done():
				return streamCtx.Err()
			}
		}
	}

	go func() {
		for {
			err := start()
			if err == nil {
				return
			}

			if err != context.Canceled {
				p.logger.Debugw("Htlc interceptor error",
					"err", err)
			}

			select {
			// Retry delay.
			case <-time.After(time.Second):

			case <-ctx.Done():
				return
			}
		}
	}()
}

func (p *Mux) run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register for htlc interception and block events.
	htlcChan := make(chan *interceptedHtlc)
	heightChan := make(chan int)
	for _, lnd := range p.lnd {
		p.interceptHtlcs(ctx, lnd, htlcChan, heightChan)
	}

	// All connected lnd nodes will immediately send the current block height.
	// Pick up the first height received to initialize our local height.
	var height int
	select {
	case height = <-heightChan:
	case <-p.quit:
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

		case <-p.quit:
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
				// TODO: Goroutine to not block registry?
				err := resolve(res)
				if err != nil {
					logger.Errorf("resolve error", "err", err)
				}
			},
		},
	)

	return nil
}
