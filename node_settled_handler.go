package lnmux

import (
	"context"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

type NodeSettledHandlerConfig struct {
	Logger          *zap.SugaredLogger
	Lnd             lnd.LndClient
	SettledCallback func(lntypes.Hash)
}

type NodeSettledHandler struct {
	logger *zap.SugaredLogger
	lnd    lnd.LndClient

	finalHtlcsChan chan types.CircuitKey
	newWatchChan   chan types.CircuitKey

	settledCallback func(lntypes.Hash)
}

func NewNodeSettledHandler(cfg *NodeSettledHandlerConfig) *NodeSettledHandler {
	logger := cfg.Logger.With("node", cfg.Lnd.PubKey())

	return &NodeSettledHandler{
		logger:          logger,
		lnd:             cfg.Lnd,
		finalHtlcsChan:  make(chan types.CircuitKey),
		newWatchChan:    make(chan types.CircuitKey),
		settledCallback: cfg.SettledCallback,
	}
}

func (p *NodeSettledHandler) Run(ctx context.Context) {
	p.logger.Infow("Starting node settled handler")

	for {
		err := p.subscribeEvents(ctx)
		switch {
		// TODO: Re-enable for no logging.
		// case err == context.DeadlineExceeded:

		case err == context.Canceled:
			return

		case err != nil:
			p.logger.Infow("Htlc notifier error", "err", err)
		}

		// TODO: Configure retry delay.
		time.Sleep(time.Second)
	}
}

func (p *NodeSettledHandler) subscribeEvents(ctx context.Context) error {
	// Force stream timeout.
	//
	// TODO: Config timeout.
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	// First subscribe to the htlc notification stream to prevent missing
	// updates.
	recv, err := p.lnd.HtlcNotifier(ctx)
	if err != nil {
		return err
	}

	// Retrieve all htlcs that are not yet settled.
	htlcs, err := p.persister.GetPendingHtlcs(ctx, p.lnd.PubKey())
	if err != nil {
		return err
	}

	// Look up each htlc to see if it has been settled in the mean time.
	for key := range htlcs {
		settled, err := p.lnd.LookupHtlc(ctx, key)
		if err != nil {
			return err
		}
		if !settled {
			continue
		}

		if err := p.handleFinalHtlc(ctx, key); err != nil {
			return err
		}
	}

	// Start processing newly settled htlcs.
	for {
		event, err := recv()
		if err != nil {
			return err
		}

		finalEvent, ok := event.Event.(*routerrpc.HtlcEvent_FinalHtlcEvent)
		if !ok {
			continue
		}
		if !finalEvent.FinalHtlcEvent.Settled {
			// TODO: Handle final fail?

			continue
		}

		key := types.CircuitKey{
			ChanID: event.IncomingChannelId,
			HtlcID: event.IncomingHtlcId,
		}

		if err := p.handleFinalHtlc(ctx, key); err != nil {
			return err
		}
	}
}

func (p *NodeSettledHandler) handleFinalHtlc(ctx context.Context,
	key types.CircuitKey) error {

	htlcKey := types.HtlcKey{
		ChanID: key.ChanID,
		HtlcID: key.HtlcID,
		Node:   p.lnd.PubKey(),
	}

	settledHash, err := p.persister.MarkHtlcSettled(ctx, htlcKey)
	switch {
	// case err == persistence.ErrHtlcAlreadySettled:
	// 	return nil

	// If the htlc is not found, the final resolution was for an htlc that isn't
	// managed by lnmux.
	case err == types.ErrHtlcNotFound:
		return nil

	case err != nil:
		return fmt.Errorf("unable to mark htlc %v settled: %w", key, err)
	}

	p.logger.Infow("Htlc final settled received",
		"chanID", key.ChanID, "htlcID", key.HtlcID, "hash", settledHash)

	if settledHash != nil {
		p.settledCallback(*settledHash)
	}

	return nil
}
