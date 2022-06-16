package lnmux

import (
	"context"
	"testing"
	"time"

	"github.com/bottlepay/lnmux/persistence"
	"github.com/bottlepay/lnmux/test"
	"github.com/bottlepay/lnmux/types"
	"github.com/go-pg/pg/v10"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type registryTestContext struct {
	t               *testing.T
	registry        *InvoiceRegistry
	pg              *pg.DB
	cfg             *RegistryConfig
	db              *persistence.PostgresPersister
	cancelRegistry  func()
	registryErrChan chan error
	logger          *zap.SugaredLogger

	testAmt int64
}

func newRegistryTestContext(t *testing.T) *registryTestContext {
	logger, _ := zap.NewDevelopment()

	pg, db := setupTestDB(t)

	cfg := &RegistryConfig{
		Clock:                clock.NewDefaultClock(),
		FinalCltvRejectDelta: 10,
		HtlcHoldDuration:     time.Second,
		AcceptTimeout:        time.Second * 2,
		Logger:               logger.Sugar(),
	}

	c := &registryTestContext{
		t:       t,
		pg:      pg,
		cfg:     cfg,
		db:      db,
		logger:  cfg.Logger,
		testAmt: 10000,
	}

	c.start()

	t.Cleanup(c.close)

	return c
}

func (r *registryTestContext) start() {
	r.registryErrChan = make(chan error)
	r.registry = NewRegistry(r.db, r.cfg)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		r.registryErrChan <- r.registry.Run(ctx)
	}()

	r.cancelRegistry = cancel
}

func (r *registryTestContext) stop() {
	r.cancelRegistry()
	err := <-r.registryErrChan

	require.NoError(r.t, err)
}

func (r *registryTestContext) close() {
	r.stop()

	r.pg.Close()
}

func (r *registryTestContext) preimage(id int) lntypes.Preimage {
	return lntypes.Preimage{byte(id)}
}

func (r *registryTestContext) payAddr(id int) lntypes.Preimage {
	return [32]byte{0, byte(id)}
}

func (r *registryTestContext) addInvoice(id int, expiry time.Duration) {
	preimage := r.preimage(id)
	payAddr := r.payAddr(id)

	require.NoError(r.t, r.registry.NewInvoice(&persistence.InvoiceCreationData{
		ExpiresAt: time.Now().Add(expiry),
		InvoiceCreationData: types.InvoiceCreationData{
			FinalCltvDelta:  40,
			PaymentPreimage: preimage,
			Value:           lnwire.MilliSatoshi(r.testAmt),
			PaymentAddr:     payAddr,
		},
		CreatedAt:      time.Now(),
		PaymentRequest: "payreq",
		ID:             int64(id),
	}))
}

func (r *registryTestContext) subscribe(id int) (chan InvoiceUpdate, func()) {
	preimage := r.preimage(id)

	updateChan := make(chan InvoiceUpdate)
	cancel, err := r.registry.Subscribe(preimage.Hash(), func(update InvoiceUpdate) {
		updateChan <- update
	})
	require.NoError(r.t, err)

	return updateChan, cancel
}

func TestInvoiceExpiry(t *testing.T) {
	defer test.Timeout()()

	c := newRegistryTestContext(t)

	// Subscribe to updates for invoice 1.
	updateChan1, cancel1 := c.subscribe(1)

	// Add invoice.
	c.addInvoice(1, time.Second)

	// Expect an open notification.
	update := <-updateChan1
	require.Equal(t, persistence.InvoiceStateOpen, update.State)

	// Expected an expired notification.
	update = <-updateChan1
	require.Equal(t, persistence.InvoiceStateCancelled, update.State)
	require.Equal(t, persistence.CancelledReasonExpired, update.CancelledReason)

	cancel1()

	// Add another invoice.
	c.addInvoice(2, time.Second)

	// Expect the open update.
	updateChan2, cancel2 := c.subscribe(2)
	update = <-updateChan2
	require.Equal(t, persistence.InvoiceStateOpen, update.State)
	cancel2()

	// Stop the registry.
	c.stop()

	// Wait for the invoice to expire.
	time.Sleep(2 * time.Second)

	// Restart the registry.
	c.start()

	// This should result in an immediate expiry of the invoice.
	updateChan3, cancel3 := c.subscribe(2)

	select {
	case update := <-updateChan3:
		require.Equal(t, persistence.InvoiceStateCancelled, update.State)
		require.Equal(t, persistence.CancelledReasonExpired, update.CancelledReason)

	case <-time.After(200 * time.Millisecond):
	}
	cancel3()
}
