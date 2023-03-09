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
	"go.uber.org/zap/zaptest"
)

// registryTestContext contains all the elements needed to spin up a registry
// for testing.
type registryTestContext struct {
	t               *testing.T
	registry        *InvoiceRegistry
	cfg             *RegistryConfig
	db              *persistence.PostgresPersister
	dropDB          func()
	cancelRegistry  func()
	registryErrChan chan error
	resolved        chan HtlcResolution
	logger          *zap.SugaredLogger

	testAmt int64

	creator *InvoiceCreator
}

// regTestContextOption is a functional argument to newRegistryTestContext()
// that operates on a registryTestContext which hasn't yet been started.
type regTestContextOption func(c *registryTestContext)

// optAutoSettle returns a functional argument to newRegistryTestContext()
// that modifies the AutoSettle configuration in the RegistryConfig.
func optAutoSettle(autoSettle bool) regTestContextOption {
	return func(c *registryTestContext) {
		c.cfg.AutoSettle = autoSettle
	}
}

// optHtlcHoldDuration returns a functional argument to newRegistryTestContext()
// that modifies the HtlcHoldDuration configuration in the RegistryConfig.
func optHtlcHoldDuration(d time.Duration) regTestContextOption { // nolint:unused
	return func(c *registryTestContext) {
		c.cfg.HtlcHoldDuration = d
	}
}

// optAcceptTimeout returns a functional argument to newRegistryTestContext()
// that modifies the AcceptTimeout configuration in the RegistryConfig.
func optAcceptTimeout(d time.Duration) regTestContextOption {
	return func(c *registryTestContext) {
		c.cfg.AcceptTimeout = d
	}
}

// optGracePeriodWithoutSubscribers returns a functional argument to newRegistryTestContext()
// that modifies the gracePeriodWithoutSubscribers configuration in the RegistryConfig.
func optGracePeriodWithoutSubscribers(d time.Duration) regTestContextOption {
	return func(c *registryTestContext) {
		c.cfg.GracePeriodWithoutSubscribers = d
	}
}

// optTestAmt returns a functional argument to newRegistryTestContext()
// that modifies the testAmt configuration.
func optTestAmt(amt int64) regTestContextOption { // nolint:unused
	return func(c *registryTestContext) {
		c.testAmt = amt
	}
}

// newRegistryTestContext creates a new registryTestContext, assigns convenient
// defaults, and applies functional options to modify default values if needed.
// Then, it starts the registry and associated context/handling and returns it.
func newRegistryTestContext(t *testing.T,
	opts ...regTestContextOption) *registryTestContext {

	logger := zaptest.NewLogger(t)

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

	// Create configuration with convenient defaults.
	cfg := &RegistryConfig{
		Clock:                clock.NewDefaultClock(),
		FinalCltvRejectDelta: 10,
		HtlcHoldDuration:     time.Second,
		AcceptTimeout:        time.Second * 2,
		Logger:               logger.Sugar(),
		PrivKey:              testKey,
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

	// Apply options to change configuration elements from defaults.
	for _, opt := range opts {
		opt(c)
	}

	c.start()

	t.Cleanup(c.close)

	return c
}

func (r *registryTestContext) start() {
	r.resolved = make(chan HtlcResolution)

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
	select {
	case <-r.resolved:
		require.FailNow(r.t, "Resolution generated incorrectly")
	default:
	}

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

func (r *registryTestContext) checkHtlcFailed(result FailResolutionResult) {
	resolution := <-r.resolved

	require.IsType(r.t, &HtlcFailResolution{}, resolution)
	require.Equal(r.t, result, resolution.(*HtlcFailResolution).Outcome)
}

func (r *registryTestContext) checkHtlcSettled() {
	require.IsType(r.t, &HtlcSettleResolution{}, <-r.resolved)
}

// optNotifyHtlc is a functional option to notifyHtlc.
type optNotifyHtlc func(h *registryHtlc)

// optAmtPaid changes the amtPaid field of the HTLC.
func optAmtPaid(amtPaid int64) optNotifyHtlc {
	return func(h *registryHtlc) {
		h.amtPaid = msat(amtPaid)
	}
}

// optHtlcID sets the HTLC ID for the HTLC's circuit key.
func optHtlcID(id uint64) optNotifyHtlc {
	return func(h *registryHtlc) {
		h.circuitKey.HtlcID = id
	}
}

// optCurrentHeight sets the HTLC's current block height.
func optCurrentHeight(height int32) optNotifyHtlc {
	return func(h *registryHtlc) {
		h.currentHeight = height
	}
}

// optPayloadAmt sets the amt in the MPP payload. Unspecified results occur
// when used with optNullPayload.
func optPayloadAmt(amt int64) optNotifyHtlc {
	return func(h *registryHtlc) {
		h.payload.(*testPayload).amt = msat(amt)
	}
}

// optNullPayload sets the HTLC payload to nil for testing that the registry
// rejects the HTLC. Unspecified results occur when used with optPayloadAmt.
func optNullPayload() optNotifyHtlc {
	return func(h *registryHtlc) {
		h.payload = (*nullPayload)(nil)
	}
}

// notifyHtlc notifies the registry of an incoming HTLC. It builds the HTLC
// with convenient defaults.
func (r *registryTestContext) notifyHtlc(rHash lntypes.Hash, payAddr [32]byte,
	opts ...optNotifyHtlc) {

	h := &registryHtlc{
		rHash:         rHash,
		amtPaid:       msat(r.testAmt),
		expiry:        100,
		currentHeight: 0,
		payload: &testPayload{
			amt:     msat(r.testAmt),
			payAddr: payAddr,
		},
		resolve: func(hr HtlcResolution) {
			go func() {
				r.resolved <- hr
			}()
		},
	}

	for _, opt := range opts {
		opt(h)
	}

	r.registry.NotifyExitHopHtlc(h)
}

func TestInvoiceExpiry(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t)

	// Add invoice.
	invoice, preimage := c.createInvoice(1, 100*time.Millisecond)

	// Wait for the invoice to expire.
	time.Sleep(200 * time.Millisecond)

	// Send htlc.
	c.notifyHtlc(preimage.Hash(), invoice.PaymentAddr)

	c.checkHtlcFailed(ResultInvoiceExpired)
}

func TestAutoSettle(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, optAutoSettle(true))

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	// Subscribe to updates for invoice.
	acceptChan, cancelAcceptUpdates := c.subscribeAccept()

	c.notifyHtlc(hash, invoice.PaymentAddr)

	acceptedInvoice := <-acceptChan
	require.Equal(t, hash, acceptedInvoice.hash)

	c.checkHtlcSettled()

	cancelAcceptUpdates()
}

// TestHasRecentSubscriber asserts that a new set is started when there was
// a recent subscriber.
// After the grace period, we should not accept any new sets.
func TestHasRecentSubscriber(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t,
		optAcceptTimeout(200*time.Millisecond),
		optGracePeriodWithoutSubscribers(200*time.Millisecond),
	)

	// There has never been a subscriber, don't accept any new set
	invoice, preimage := c.createInvoice(1, time.Hour)

	c.notifyHtlc(preimage.Hash(), invoice.PaymentAddr)

	c.checkHtlcFailed(ResultNoAcceptSubscriber)

	// A subscriber connects and disconnects
	_, acceptCancel := c.subscribeAccept()
	acceptCancel()

	// Now check that we accept new set_id if there was a recent subscriber
	c.notifyHtlc(preimage.Hash(), invoice.PaymentAddr)

	// The set was accepted but we reach accept timeout
	c.checkHtlcFailed(ResultAcceptTimeout)

	// Add invoice.
	invoice, preimage = c.createInvoice(1, time.Hour)

	c.notifyHtlc(preimage.Hash(), invoice.PaymentAddr)

	// The grace period is finished, we don't accept new sets
	c.checkHtlcFailed(ResultNoAcceptSubscriber)
}

func TestSettle(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t)

	acceptChan, acceptCancel := c.subscribeAccept()
	defer acceptCancel()

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	// Test cancel/settle on invoices that are not yet accepted.
	require.ErrorIs(t, c.registry.RequestSettle(hash, SetID{}), types.ErrInvoiceNotFound)
	require.ErrorIs(t, c.registry.CancelInvoice(hash, SetID{}), types.ErrInvoiceNotFound)

	c.notifyHtlc(hash, invoice.PaymentAddr)

	accept := <-acceptChan
	require.Equal(t, hash, accept.hash)

	// Settling with a different set id should fail.
	require.ErrorIs(t, c.registry.RequestSettle(accept.hash, SetID{}), types.ErrInvoiceNotFound)
	require.ErrorIs(t, c.registry.CancelInvoice(hash, SetID{}), types.ErrInvoiceNotFound)

	// Settling with the correct hash and set ID should work.
	require.NoError(t, c.registry.RequestSettle(accept.hash, accept.setID))

	c.checkHtlcSettled()

	// After settling, re-settling with a different set id should still fail.
	require.ErrorIs(t, c.registry.RequestSettle(accept.hash, SetID{}), types.ErrInvoiceNotFound)

	// Test idempotency.
	require.NoError(t, c.registry.RequestSettle(accept.hash, accept.setID))

	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, accept.setID), ErrSettleRequested)
	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, SetID{}), types.ErrInvoiceNotFound)
}

func TestCancel(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t)

	acceptChan, acceptCancel := c.subscribeAccept()
	defer acceptCancel()

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	c.notifyHtlc(hash, invoice.PaymentAddr)

	accept := <-acceptChan

	require.NoError(t, c.registry.CancelInvoice(accept.hash, accept.setID))

	c.checkHtlcFailed(ResultInvoiceNotOpen)

	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, accept.setID), types.ErrInvoiceNotFound)
}

func TestAcceptTimeout(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, optAcceptTimeout(200*time.Millisecond))

	acceptChan, acceptCancel := c.subscribeAccept()
	defer acceptCancel()

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	hash := preimage.Hash()

	c.notifyHtlc(hash, invoice.PaymentAddr)

	accept := <-acceptChan

	c.checkHtlcFailed(ResultAcceptTimeout)

	require.ErrorIs(t, c.registry.CancelInvoice(accept.hash, accept.setID), types.ErrInvoiceNotFound)
}

func TestOverpayment(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, optAutoSettle(true))

	// Add invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	rHash := preimage.Hash()

	// Subscribe to updates for invoice.
	acceptChan, cancelAcceptUpdates := c.subscribeAccept()

	// Test with full overpayment in a single HTLC.
	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(1),
		optAmtPaid(c.testAmt+1))

	c.checkHtlcFailed(ResultHtlcSetOverpayment)

	// Test with half of the amount in one HTLC and a slight overpayment
	// in a second HTLC.
	first := c.testAmt / 2
	rest := c.testAmt - first

	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(2),
		optAmtPaid(first))

	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(3),
		optAmtPaid(rest+1000))

	c.checkHtlcFailed(ResultHtlcSetOverpayment)

	// Send another HTLC that should complete the set and auto-settle
	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(4), optAmtPaid(rest))

	acceptedInvoice := <-acceptChan
	require.Equal(t, preimage.Hash(), acceptedInvoice.hash)

	// Both of the HTLCs should now resolve as settled.
	c.checkHtlcSettled()
	c.checkHtlcSettled()

	cancelAcceptUpdates()
}

func TestMPPValidity(t *testing.T) {
	defer test.Timeout()()
	t.Parallel()

	c := newRegistryTestContext(t, optAutoSettle(true))

	// Create invoice.
	invoice, preimage := c.createInvoice(1, time.Hour)
	rHash := preimage.Hash()

	// Test HTLC with no MPP record.
	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(1), optNullPayload())

	c.checkHtlcFailed(ResultHtlcInvoiceTypeMismatch)

	// Test MPP with total too low.
	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(2), optPayloadAmt(0))

	c.checkHtlcFailed(ResultHtlcSetTotalTooLow)

	// Test MPP with hash mismatching the invoice.
	c.notifyHtlc(lntypes.ZeroHash, invoice.PaymentAddr, optHtlcID(3))

	c.checkHtlcFailed(ResultAddressMismatch)

	// Test MPP with set total mismatching the invoice.
	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(4),
		optPayloadAmt(c.testAmt-1))

	c.checkHtlcFailed(ResultHtlcSetTotalMismatch)

	// Test MPP with HTLC that expires too soon.
	c.notifyHtlc(rHash, invoice.PaymentAddr, optHtlcID(5),
		optCurrentHeight(100))

	c.checkHtlcFailed(ResultExpiryTooSoon)
}

type testPayload struct {
	amt     lnwire.MilliSatoshi
	payAddr [32]byte
}

func (t *testPayload) MultiPath() *record.MPP {
	return record.NewMPP(t.amt, t.payAddr)
}

type nullPayload struct{}

func (t *nullPayload) MultiPath() *record.MPP {
	return nil
}

func msat(n int64) lnwire.MilliSatoshi {
	return lnwire.MilliSatoshi(n)
}
