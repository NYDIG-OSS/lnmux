package lnmux

import (
	"context"
	"testing"
	"time"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/bottlepay/lnmux/test"
	"github.com/bottlepay/lnmux/types"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/go-pg/pg/v10"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
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

	creator *InvoiceCreator
}

func newRegistryTestContext(t *testing.T, autoSettle bool) *registryTestContext {
	logger, _ := zap.NewDevelopment()

	pg, db := setupTestDB(t)

	keyRing := NewKeyRing(testKey)

	creator, err := NewInvoiceCreator(
		&InvoiceCreatorConfig{
			KeyRing:         keyRing,
			GwPubKeys:       []common.PubKey{testPubKey1, testPubKey2},
			ActiveNetParams: &chaincfg.RegressionNetParams,
		},
	)
	require.NoError(t, err)

	cfg := &RegistryConfig{
		Clock:                clock.NewDefaultClock(),
		FinalCltvRejectDelta: 10,
		HtlcHoldDuration:     time.Second,
		AcceptTimeout:        time.Second * 2,
		Logger:               logger.Sugar(),
		PrivKey:              testKey,
		AutoSettle:           autoSettle,
	}

	c := &registryTestContext{
		t:       t,
		pg:      pg,
		cfg:     cfg,
		db:      db,
		logger:  cfg.Logger,
		testAmt: 10000,
		creator: creator,
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

func (r *registryTestContext) createInvoice(id int, expiry time.Duration) ( // nolint:unparam
	*Invoice, lntypes.Preimage) {

	invoice, preimage, err := r.creator.Create(r.testAmt, expiry, "test", nil, 40)
	require.NoError(r.t, err)

	return invoice, preimage
}

func (r *registryTestContext) subscribe(hash lntypes.Hash) (chan InvoiceUpdate, func()) {
	updateChan := make(chan InvoiceUpdate, 2)
	cancel, err := r.registry.Subscribe(hash, func(update InvoiceUpdate) {
		updateChan <- update
	})
	require.NoError(r.t, err)

	return updateChan, cancel
}

func (r *registryTestContext) subscribeAccept() (chan lntypes.Hash, func()) {
	updateChan := make(chan lntypes.Hash)
	cancel, err := r.registry.SubscribeAccept(func(hash lntypes.Hash) {
		updateChan <- hash
	})
	require.NoError(r.t, err)

	return updateChan, cancel
}

func TestInvoiceExpiry(t *testing.T) {
	defer test.Timeout()()

	c := newRegistryTestContext(t, false)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, 100*time.Millisecond)

	// Wait for the invoice to expire.
	time.Sleep(200 * time.Millisecond)

	// Send htlc.
	resolved := make(chan HtlcResolution)
	c.registry.NotifyExitHopHtlc(&registryHtlc{
		rHash:         preimage.Hash(),
		amtPaid:       lnwire.MilliSatoshi(c.testAmt),
		expiry:        100,
		currentHeight: 0,
		resolve: func(r HtlcResolution) {
			resolved <- r
		},
		payload: &testPayload{
			amt:     lnwire.MilliSatoshi(c.testAmt),
			payAddr: invoice.PaymentAddr,
		},
	})

	resolution := <-resolved
	require.IsType(t, &HtlcFailResolution{}, resolution)
	require.Equal(t, ResultInvoiceExpired, resolution.(*HtlcFailResolution).Outcome)
}

func TestAutoSettle(t *testing.T) {
	defer test.Timeout()()

	c := newRegistryTestContext(t, true)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)

	// Subscribe to updates for invoice.
	updateChan, cancelUpdates := c.subscribe(preimage.Hash())
	acceptChan, cancelAcceptUpdates := c.subscribeAccept()

	resolved := make(chan struct{})
	c.registry.NotifyExitHopHtlc(&registryHtlc{
		rHash:         preimage.Hash(),
		amtPaid:       lnwire.MilliSatoshi(c.testAmt),
		expiry:        100,
		currentHeight: 0,
		resolve: func(r HtlcResolution) {
			close(resolved)
		},
		payload: &testPayload{
			amt:     lnwire.MilliSatoshi(c.testAmt),
			payAddr: invoice.PaymentAddr,
		},
	})

	acceptedInvoice := <-acceptChan
	require.Equal(t, preimage.Hash(), acceptedInvoice)

	update := <-updateChan
	require.Equal(t, persistence.InvoiceStateAccepted, update.State)

	update = <-updateChan
	require.Equal(t, persistence.InvoiceStateSettleRequested, update.State)

	update = <-updateChan
	require.Equal(t, persistence.InvoiceStateSettled, update.State)

	<-resolved

	cancelUpdates()
	cancelAcceptUpdates()
}

func TestSettle(t *testing.T) {
	defer test.Timeout()()

	c := newRegistryTestContext(t, false)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	// Test cancel/settle on invoices that are not yet accepted.
	require.ErrorIs(t, c.registry.RequestSettle(hash), types.ErrInvoiceNotFound)
	require.ErrorIs(t, c.registry.CancelInvoice(hash), types.ErrInvoiceNotFound)

	// Subscribe to updates for invoice.
	updateChan, cancelUpdates := c.subscribe(hash)

	resolved := make(chan struct{})
	c.registry.NotifyExitHopHtlc(&registryHtlc{
		rHash:         hash,
		amtPaid:       lnwire.MilliSatoshi(c.testAmt),
		expiry:        100,
		currentHeight: 0,
		resolve: func(r HtlcResolution) {
			close(resolved)
		},
		payload: &testPayload{
			amt:     lnwire.MilliSatoshi(c.testAmt),
			payAddr: invoice.PaymentAddr,
		},
	})

	update := <-updateChan
	require.Equal(t, persistence.InvoiceStateAccepted, update.State)

	require.NoError(t, c.registry.RequestSettle(hash))

	update = <-updateChan
	require.Equal(t, persistence.InvoiceStateSettleRequested, update.State)

	update = <-updateChan
	require.Equal(t, persistence.InvoiceStateSettled, update.State)

	<-resolved

	// Test idempotency.
	require.NoError(t, c.registry.RequestSettle(hash))

	require.ErrorIs(t, c.registry.CancelInvoice(hash), ErrInvoiceAlreadySettled)

	cancelUpdates()
}

func TestCancel(t *testing.T) {
	defer test.Timeout()()

	c := newRegistryTestContext(t, false)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	// Subscribe to updates for invoice.
	updateChan, cancelUpdates := c.subscribe(hash)

	resolved := make(chan struct{})
	c.registry.NotifyExitHopHtlc(&registryHtlc{
		rHash:         hash,
		amtPaid:       lnwire.MilliSatoshi(c.testAmt),
		expiry:        100,
		currentHeight: 0,
		resolve: func(r HtlcResolution) {
			close(resolved)
		},
		payload: &testPayload{
			amt:     lnwire.MilliSatoshi(c.testAmt),
			payAddr: invoice.PaymentAddr,
		},
	})

	update := <-updateChan
	require.Equal(t, persistence.InvoiceStateAccepted, update.State)

	require.NoError(t, c.registry.CancelInvoice(hash))

	<-resolved

	require.ErrorIs(t, c.registry.CancelInvoice(hash), types.ErrInvoiceNotFound)

	cancelUpdates()
}

type testPayload struct {
	amt     lnwire.MilliSatoshi
	payAddr [32]byte
}

func (t *testPayload) MultiPath() *record.MPP {
	return record.NewMPP(t.amt, t.payAddr)
}
