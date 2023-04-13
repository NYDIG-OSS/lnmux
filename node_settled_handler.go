package lnmux

import (
	"context"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

const subscribeEventsRetryDelay = time.Second

type NodeSettledHandlerConfig struct {
	Persister     *persistence.PostgresPersister
	Logger        *zap.SugaredLogger
	Lnd           lnd.LndClient
	FinalCallback func(lntypes.Hash, bool)
}

type NodeSettledHandler struct {
	persister *persistence.PostgresPersister
	logger    *zap.SugaredLogger
	lnd       lnd.LndClient

	finalCallback func(lntypes.Hash, bool)
}

func NewNodeSettledHandler(cfg *NodeSettledHandlerConfig) *NodeSettledHandler {
	logger := cfg.Logger.With("node", cfg.Lnd.PubKey())

	return &NodeSettledHandler{
		logger:        logger,
		persister:     cfg.Persister,
		lnd:           cfg.Lnd,
		finalCallback: cfg.FinalCallback,
	}
}

func (p *NodeSettledHandler) Run(ctx context.Context) {
	p.logger.Infow("Starting node settled handler")

	for {
		err := p.subscribeEvents(ctx)
		switch {
		case err == context.DeadlineExceeded:
			continue

		case err == context.Canceled:
			return

		case err != nil:
			p.logger.Infow("Htlc notifier error", "err", err)
		}

		select {
		case <-time.After(subscribeEventsRetryDelay):
		case <-ctx.Done():
			return
		}
	}
}

func (p *NodeSettledHandler) subscribeEvents(ctx context.Context) error {
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
		switch {
		// No final resolution yet.
		case err == lnd.ErrHtlcNotFound:
			continue

		case err != nil:
			return err
		}

		if err := p.handleFinalHtlc(ctx, key, settled); err != nil {
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

		key := types.CircuitKey{
			ChanID: event.IncomingChannelId,
			HtlcID: event.IncomingHtlcId,
		}

		err = p.handleFinalHtlc(
			ctx, key, finalEvent.FinalHtlcEvent.Settled,
		)
		if err != nil {
			return err
		}
	}
}

func (p *NodeSettledHandler) handleFinalHtlc(ctx context.Context,
	key types.CircuitKey, settled bool) error {

	htlcKey := types.HtlcKey{
		ChanID: key.ChanID,
		HtlcID: key.HtlcID,
		Node:   p.lnd.PubKey(),
	}

	invoiceHash, err := p.persister.MarkHtlcFinal(ctx, htlcKey, settled)
	switch {
	case err == persistence.ErrHtlcAlreadyFinal:
		return nil

	// If the htlc is not found, the final resolution was for an htlc that
	// isn't managed by lnmux.
	case err == types.ErrHtlcNotFound:
		return nil

	case err != nil:
		return fmt.Errorf("unable to finalize htlc %v: %w",
			key, err)
	}

	p.logger.Infow("Htlc final resolution received",
		"chanID", key.ChanID,
		"htlcID", key.HtlcID,
		"hash", invoiceHash,
		"settled", settled,
	)

	if invoiceHash != nil {
		p.finalCallback(*invoiceHash, settled)
	}

	return nil
}
