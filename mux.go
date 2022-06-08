package lnmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/htlcswitch/hop"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"go.uber.org/zap"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/types"
)

const (
	resolutionQueueSize = 100
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

func (p *Mux) interceptHtlcs(ctx context.Context,
	lnd lnd.LndClient, htlcChan chan *interceptedHtlc, heightChan chan int) {

	var pubKey = lnd.PubKey()

	logger := p.logger.With("node", pubKey)

	start := func(ctx context.Context) error {
		streamCtx, streamCancel := context.WithCancel(ctx)
		defer streamCancel()

		send, recv, err := lnd.HtlcInterceptor(streamCtx)
		if err != nil {
			return err
		}

		logger.Debugw("Starting htlc interception")

		// Register for block notifications.
		blockChan, blockErrChan, err := lnd.RegisterBlockEpochNtfn(streamCtx)
		if err != nil {
			return err
		}

		// The block stream immediately sends the current block. Read that to
		// set our initial height.
		select {
		case block := <-blockChan:
			logger.Debugw("Initial block height", "height", block.Height)
			heightChan <- int(block.Height)

		case <-streamCtx.Done():
			return streamCtx.Err()
		}

		var (
			errChan   = make(chan error, 1)
			replyChan = make(
				chan *routerrpc.ForwardHtlcInterceptResponse,
				resolutionQueueSize,
			)
			replyChanClosed bool
		)

		var wg sync.WaitGroup
		defer wg.Wait()
		wg.Add(1)

		go func(ctx context.Context) {
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

				reply := func(resp *interceptedHtlcResponse) error {
					// Don't try to write if the channel is closed. This
					// callback does not need to be thread-safe.
					if replyChanClosed {
						return errors.New("reply channel closed")
					}

					rpcResp := &routerrpc.ForwardHtlcInterceptResponse{
						IncomingCircuitKey: htlc.IncomingCircuitKey,
						Action:             resp.action,
						Preimage:           resp.preimage[:],
						FailureMessage:     resp.failureMessage,
						FailureCode:        resp.failureCode,
					}

					select {
					case replyChan <- rpcResp:
						return nil

					// When the context is cancelled, close the reply channel.
					// We don't want to skip this reply and on the next one send
					// into the channel again.
					case <-ctx.Done():
						close(replyChan)
						replyChanClosed = true

						return ctx.Err()

					// When the update channel is full, terminate the subscriber
					// to prevent blocking multiplexer.
					default:
						close(replyChan)
						replyChanClosed = true

						return errors.New("reply channel full")
					}
				}

				circuitKey := newCircuitKeyFromRPC(htlc.IncomingCircuitKey)

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
		}(ctx)

		for {
			select {
			case err := <-errChan:
				return fmt.Errorf("stream error: %w", err)

			case block := <-blockChan:
				select {
				case heightChan <- int(block.Height):

				case <-streamCtx.Done():
					return streamCtx.Err()
				}

			case rpcResp, ok := <-replyChan:
				if !ok {
					return errors.New("reply channel full")
				}

				err := send(rpcResp)
				if err != nil {
					return fmt.Errorf(
						"cannot send: %w", err)
				}

			case err := <-blockErrChan:
				return fmt.Errorf("block error: %w", err)

			case <-streamCtx.Done():
				return streamCtx.Err()
			}
		}
	}

	go func(ctx context.Context) {
		defer logger.Debugw("Exiting interceptor loop")

		for {
			err := start(ctx)
			if err == nil || err == context.Canceled {
				return
			}

			logger.Infow("Htlc interceptor error",
				"err", err)

			select {
			// Retry delay.
			case <-time.After(time.Second):

			case <-ctx.Done():
				return
			}
		}
	}(ctx)
}

func (p *Mux) run(ctx context.Context) error {
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
