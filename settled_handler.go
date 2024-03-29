package lnmux

import (
	"context"
	"sync"

	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

type SettledHandlerConfig struct {
	Persister *persistence.PostgresPersister
	Logger    *zap.SugaredLogger
	Lnds      []lnd.LndClient
}

type SettledHandler struct {
	persister *persistence.PostgresPersister
	logger    *zap.SugaredLogger
	lnds      []lnd.LndClient

	waiters     map[lntypes.Hash][]chan bool
	waitersLock sync.Mutex
}

func NewSettledHandler(cfg *SettledHandlerConfig) *SettledHandler {
	return &SettledHandler{
		logger:    cfg.Logger,
		persister: cfg.Persister,
		lnds:      cfg.Lnds,
		waiters:   make(map[lntypes.Hash][]chan bool),
	}
}

func (p *SettledHandler) InvoiceSettled(hash lntypes.Hash, settled bool) {
	p.waitersLock.Lock()

	waiters := p.waiters[hash]

	p.logger.Infow("Invoice settled", "hash", hash, "waiters", len(waiters))

	for _, waiter := range waiters {
		waiter <- settled
		close(waiter)
	}
	p.waiters[hash] = nil
	p.waitersLock.Unlock()
}

func (p *SettledHandler) WaitForInvoiceFinalStatus(ctx context.Context,
	hash lntypes.Hash) (bool, error) {

	logger := p.logger.With("hash", hash)

	waitChan := make(chan bool, 1)

	// First subscribe to the settled event. Otherwise a race condition could
	// occur.
	p.waitersLock.Lock()
	p.waiters[hash] = append(p.waiters[hash], waitChan)
	p.waitersLock.Unlock()

	// Check database to see if invoice was already settled.
	invoice, _, err := p.persister.Get(ctx, hash)
	if err != nil {
		return false, err
	}

	// If invoice has status 'SETTLED' or 'FAILED' in DB,
	// then return.
	if invoice.Status.IsFinal() {
		logger.Debugw("Wait for invoice settled completed via db")

		return invoice.Status == types.InvoiceStatusSettled, nil
	}

	// Not settled yet. Wait for the event.
	select {
	case settled := <-waitChan:
		logger.Debugw("Wait for invoice settled completed via wait channel")

		return settled, nil

	case <-ctx.Done():
		return false, ctx.Err()
	}
}
