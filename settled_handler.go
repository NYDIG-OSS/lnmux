package lnmux

import (
	"context"
	"sync"

	"github.com/bottlepay/lnmux/persistence"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

type SettledHandlerConfig struct {
	Persister *persistence.PostgresPersister
	Logger    *zap.SugaredLogger
}

type SettledHandler struct {
	persister *persistence.PostgresPersister
	logger    *zap.SugaredLogger

	waiters     map[lntypes.Hash][]chan struct{}
	waitersLock sync.Mutex
}

func NewSettledHandler(cfg *SettledHandlerConfig) *SettledHandler {
	return &SettledHandler{
		logger:    cfg.Logger,
		persister: cfg.Persister,
		waiters:   make(map[lntypes.Hash][]chan struct{}),
	}
}

func (p *SettledHandler) preSendHandler(ctx context.Context, item queuedReply) error {
	if item.resp.action != routerrpc.ResolveHoldForwardAction_SETTLE {
		return nil
	}

	invoiceSettled, err := p.persister.MarkHtlcSettled(ctx, item.hash, item.incomingKey)
	if err != nil {
		return err
	}

	if invoiceSettled {
		p.waitersLock.Lock()

		waiters := p.waiters[item.hash]

		p.logger.Infow("Invoice settled",
			"hash", item.hash, "waiters", len(waiters))

		for _, waiter := range waiters {
			close(waiter)
		}
		p.waiters[item.hash] = nil
		p.waitersLock.Unlock()

	}

	return nil
}

func (p *SettledHandler) WaitForInvoiceSettled(ctx context.Context,
	hash lntypes.Hash) error {

	waitChan := make(chan struct{}, 1)

	// First subscribe to the settled event. Otherwise a race condition could
	// occur.
	p.waitersLock.Lock()
	p.waiters[hash] = append(p.waiters[hash], waitChan)
	p.waitersLock.Unlock()

	// Check database to see if invoice was already settled.
	invoice, _, err := p.persister.Get(ctx, hash)
	if err != nil {
		return err
	}
	if invoice.Settled {
		return nil
	}

	// Not settled yet. Wait for the event.
	select {
	case <-waitChan:
		return nil

	case <-ctx.Done():
		return ctx.Err()
	}
}
