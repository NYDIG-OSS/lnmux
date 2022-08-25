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
	cfg             *RegistryConfig
	db              *persistence.PostgresPersister
	dropDB          func()
	cancelRegistry  func()
	registryErrChan chan error
	logger          *zap.SugaredLogger

	testAmt int64

	creator *InvoiceCreator
}

func newRegistryTestContext(t *testing.T, autoSettle bool) *registryTestContext {
	logger, _ := zap.NewDevelopment()

	db, dropDB := setupTestDB(t)

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
		cfg:     cfg,
		db:      db,
		dropDB:  dropDB,
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

	r.dropDB()
}

func (r *registryTestContext) createInvoice(id int, expiry time.Duration) ( // nolint:unparam
	*Invoice, lntypes.Preimage) {

	invoice, preimage, err := r.creator.Create(r.testAmt, expiry, "test", nil, 40)
	require.NoError(r.t, err)

	return invoice, preimage
}

type acceptEvent struct {
	hash  lntypes.Hash
	setID SetID
}

func (r *registryTestContext) subscribeAccept() (chan acceptEvent, func()) {
	updateChan := make(chan acceptEvent)
	cancel, err := r.registry.SubscribeAccept(func(hash lntypes.Hash,
		setID SetID) {

		updateChan <- acceptEvent{hash: hash, setID: setID}
	})
	require.NoError(r.t, err)

	return updateChan, cancel
}

func (r *registryTestContext) checkHtlcFailed(resolution HtlcResolution,
	result FailResolutionResult) {

	require.IsType(r.t, &HtlcFailResolution{}, resolution)
	require.Equal(r.t, result, resolution.(*HtlcFailResolution).Outcome)
}

func (r *registryTestContext) checkHtlcSettled(resolution HtlcResolution) {
	require.IsType(r.t, &HtlcSettleResolution{}, resolution)
}

func TestInvoiceExpiry(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

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

	c.checkHtlcFailed(<-resolved, ResultInvoiceExpired)
}

func TestAutoSettle(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, true)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)

	// Subscribe to updates for invoice.
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
	require.Equal(t, preimage.Hash(), acceptedInvoice.hash)

	<-resolved

	cancelAcceptUpdates()
}

// TestNoSubscriberFail asserts that no new set is started when there's no
// application subscribed to accept events.
func TestNoSubscriberFail(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, false)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)

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

	c.checkHtlcFailed(<-resolved, ResultNoAcceptSubscriber)
}

func TestSettle(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, false)

	acceptChan, acceptCancel := c.subscribeAccept()
	defer acceptCancel()

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	// Test cancel/settle on invoices that are not yet accepted.
	require.ErrorIs(t, c.registry.RequestSettle(hash, SetID{}), types.ErrInvoiceNotFound)
	require.ErrorIs(t, c.registry.CancelInvoice(hash, SetID{}), types.ErrInvoiceNotFound)

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

	accept := <-acceptChan
	require.Equal(t, hash, accept.hash)

	// Settling with a different set id should fail.
	require.ErrorIs(t, c.registry.RequestSettle(accept.hash, SetID{}), types.ErrInvoiceNotFound)
	require.ErrorIs(t, c.registry.CancelInvoice(hash, SetID{}), types.ErrInvoiceNotFound)

	require.NoError(t, c.registry.RequestSettle(accept.hash, accept.setID))

	// After settling, re-settling with a different set id should still fail.
	require.ErrorIs(t, c.registry.RequestSettle(accept.hash, SetID{}), types.ErrInvoiceNotFound)

	<-resolved

	// Test idempotency.
	require.NoError(t, c.registry.RequestSettle(accept.hash, accept.setID))

	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, accept.setID), ErrSettleRequested)
	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, SetID{}), types.ErrInvoiceNotFound)
}

func TestCancel(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, false)

	acceptChan, acceptCancel := c.subscribeAccept()
	defer acceptCancel()

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

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

	accept := <-acceptChan

	require.NoError(t, c.registry.CancelInvoice(accept.hash, accept.setID))

	<-resolved

	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, accept.setID), types.ErrInvoiceNotFound)
}

func TestAcceptTimeout(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, false)

	acceptChan, acceptCancel := c.subscribeAccept()
	defer acceptCancel()

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	resolved := make(chan HtlcResolution)
	c.registry.NotifyExitHopHtlc(&registryHtlc{
		rHash:         hash,
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

	accept := <-acceptChan

	c.checkHtlcFailed(<-resolved, ResultAcceptTimeout)

	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, accept.setID), types.ErrInvoiceNotFound)
}

func TestOverpayment(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, true)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	rHash := preimage.Hash()

	// Subscribe to updates for invoice.
	acceptChan, cancelAcceptUpdates := c.subscribeAccept()

	resolved := make(chan HtlcResolution)

	notifyHtlc := func(id uint64, amt int64) {
		c.registry.NotifyExitHopHtlc(&registryHtlc{
			rHash:         rHash,
			amtPaid:       lnwire.MilliSatoshi(amt),
			expiry:        100,
			currentHeight: 0,
			circuitKey: types.CircuitKey{
				HtlcID: id,
			},
			resolve: func(r HtlcResolution) {
				resolved <- r
			},
			payload: &testPayload{
				amt:     lnwire.MilliSatoshi(c.testAmt),
				payAddr: invoice.PaymentAddr,
			},
		})
	}

	// Test with full overpayment in a single HTLC.
	notifyHtlc(1, c.testAmt+1)

	c.checkHtlcFailed(<-resolved, ResultHtlcSetOverpayment)

	// We should not have a second resolution, an update, or an accept here.
	select {

	case <-acceptChan:
		require.FailNow(t, "Invoice accepted incorrectly")

	case <-resolved:
		require.FailNow(t, "Resolution generated incorrectly")

	case <-time.After(100 * time.Millisecond):
		// We want enough time here to check for any messages but not
		// long enough for any HTLCs to time out.
	}

	// Test with half of the amount in one HTLC and a slight overpayment
	// in a second HTLC.
	first := c.testAmt / 2
	rest := c.testAmt - first

	notifyHtlc(2, first)
	notifyHtlc(3, rest+1000)

	c.checkHtlcFailed(<-resolved, ResultHtlcSetOverpayment)

	// We should not have a second resolution, an update, or an accept here.
	select {

	case <-acceptChan:
		require.FailNow(t, "Invoice accepted incorrectly")

	case <-resolved:
		require.FailNow(t, "Resolution generated incorrectly")

	case <-time.After(100 * time.Millisecond):
		// We want enough time here to check for any messages but not
		// long enough for any HTLCs to time out.
	}

	// Send another HTLC that should complete the set and auto-settle
	notifyHtlc(4, rest)

	acceptedInvoice := <-acceptChan
	require.Equal(t, preimage.Hash(), acceptedInvoice.hash)

	// Both of the HTLCs should now resolve as settled.
	c.checkHtlcSettled(<-resolved)
	c.checkHtlcSettled(<-resolved)

	cancelAcceptUpdates()
}

type testPayload struct {
	amt     lnwire.MilliSatoshi
	payAddr [32]byte
}

func (t *testPayload) MultiPath() *record.MPP {
	return record.NewMPP(t.amt, t.payAddr)
}
