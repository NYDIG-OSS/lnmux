// Copyright (C) 2015-2022 Lightning Labs and The Lightning Network Developers

package lnmux

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bottlepay/lnmux/persistence"
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/queue"
	"go.uber.org/zap"
)

var (
	// ErrInvoiceExpiryTooSoon is returned when an invoice is attempted to be
	// accepted or settled with not enough blocks remaining.
	ErrInvoiceExpiryTooSoon = errors.New("invoice expiry too soon")

	// ErrInvoiceAmountTooLow is returned  when an invoice is attempted to be
	// accepted or settled with an amount that is too low.
	ErrInvoiceAmountTooLow = errors.New("paid amount less than invoice amount")

	// ErrShuttingDown is returned when an operation failed because the
	// invoice registry is shutting down.
	ErrShuttingDown = errors.New("invoice registry shutting down")

	// ErrAutoSettling is returned when a settle request is made in
	// auto-settle mode.
	ErrAutoSettling = errors.New("lnmux is in auto-settle mode")

	ErrSettleRequested = errors.New("invoice settle already requested")
)

const (
	// DefaultHtlcHoldDuration defines the default for how long mpp htlcs
	// are held while waiting for the other set members to arrive.
	DefaultHtlcHoldDuration = 120 * time.Second
)

// RegistryConfig contains the configuration parameters for invoice registry.
type RegistryConfig struct {
	// FinalCltvRejectDelta defines the number of blocks before the expiry
	// of the htlc where we no longer settle it as an exit hop and instead
	// cancel it back. Normally this value should be lower than the cltv
	// expiry of any invoice we create and the code effectuating this should
	// not be hit.
	FinalCltvRejectDelta int32

	// HtlcHoldDuration defines for how long mpp htlcs are held while
	// waiting for the other set members to arrive.
	HtlcHoldDuration time.Duration

	// AcceptTimeout defines for how long a complete htlc set is held before the
	// invoice is cancelled.
	AcceptTimeout time.Duration

	// Clock holds the clock implementation that is used to provide
	// Now() and TickAfter() and is useful to stub out the clock functions
	// during testing.
	Clock clock.Clock

	Logger *zap.SugaredLogger

	// PrivKey is the private key that is used for calculating payment metadata
	// hmacs, which serve as the payment preimage.
	PrivKey [32]byte

	AutoSettle bool
}

type InvoiceCallback func(update InvoiceUpdate)
type AcceptCallback func(lntypes.Hash, SetID)

type invoiceRequest struct {
	hash    lntypes.Hash
	setID   SetID
	errChan chan error
}

type invoiceAcceptSubscription struct {
	callback AcceptCallback
	id       int
}

// InvoiceRegistry is a central registry of all the outstanding invoices
// created by the daemon. The registry is a thin wrapper around a map in order
// to ensure that all updates/reads are thread safe.
type InvoiceRegistry struct {
	sync.RWMutex

	cdb *persistence.PostgresPersister

	// cfg contains the registry's configuration parameters.
	cfg *RegistryConfig

	// subscriptions is a map from a circuit key to a list of subscribers.
	// It is used for efficient notification of links.
	hodlSubscriptions map[types.CircuitKey][]func(HtlcResolution)

	sets              htlcSets
	htlcChan          chan *registryHtlc
	requestSettleChan chan *invoiceRequest
	cancelChan        chan *invoiceRequest

	autoReleaseHeap *queue.PriorityQueue
	logger          *zap.SugaredLogger

	newInvoiceAcceptSubscription    chan invoiceAcceptSubscription
	cancelInvoiceAcceptSubscription chan int

	subscriptionManager *subscriptionManager

	quit chan struct{}
}

// NewRegistry creates a new invoice registry. The invoice registry
// wraps the persistent on-disk invoice storage with an additional in-memory
// layer. The in-memory layer is in place such that debug invoices can be added
// which are volatile yet available system wide within the daemon.
func NewRegistry(cdb *persistence.PostgresPersister,
	cfg *RegistryConfig) *InvoiceRegistry {

	return &InvoiceRegistry{
		cdb:                             cdb,
		hodlSubscriptions:               make(map[types.CircuitKey][]func(HtlcResolution)),
		cfg:                             cfg,
		sets:                            newHtlcSets(),
		htlcChan:                        make(chan *registryHtlc),
		newInvoiceAcceptSubscription:    make(chan invoiceAcceptSubscription),
		cancelInvoiceAcceptSubscription: make(chan int),
		subscriptionManager:             newSubscriptionManager(cfg.Logger),
		requestSettleChan:               make(chan *invoiceRequest),
		cancelChan:                      make(chan *invoiceRequest),
		quit:                            make(chan struct{}),

		logger:          cfg.Logger,
		autoReleaseHeap: &queue.PriorityQueue{},
	}
}

// Start starts the registry and all goroutines it needs to carry out its task.
func (i *InvoiceRegistry) Run(ctx context.Context) error {
	i.logger.Info("InvoiceRegistry starting")

	err := i.invoiceEventLoop(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		i.logger.Errorw("InvoiceRegistry error", "err", err)

		return err
	}

	i.logger.Info("InvoiceRegistry shutting down")
	close(i.quit)

	return nil
}

func (i *InvoiceRegistry) AutoSettle() bool {
	return i.cfg.AutoSettle
}

func (i *InvoiceRegistry) SubscribeAccept(callback AcceptCallback) (
	func(), error) {

	subscriberId := i.subscriptionManager.generateSubscriptionId()
	logger := i.logger.With("id", subscriberId)

	logger.Debugw("Adding subscriber")

	select {
	case i.newInvoiceAcceptSubscription <- invoiceAcceptSubscription{
		callback: callback,
		id:       subscriberId,
	}:

	case <-i.quit:
		return nil, ErrShuttingDown
	}

	return func() {
		logger.Debugw("Removing subscriber")

		select {
		case i.cancelInvoiceAcceptSubscription <- subscriberId:
		case <-i.quit:
		}
	}, nil
}

func (i *InvoiceRegistry) RequestSettle(hash lntypes.Hash,
	setID SetID) error {

	i.logger.Debugw("New settle request received", "hash", hash, "setID", setID)

	request := &invoiceRequest{
		hash:    hash,
		setID:   setID,
		errChan: make(chan error),
	}

	select {
	case i.requestSettleChan <- request:

	case <-i.quit:
		return ErrShuttingDown
	}

	select {
	case err := <-request.errChan:
		return err

	case <-i.quit:
		return ErrShuttingDown
	}
}

func (i *InvoiceRegistry) CancelInvoice(
	hash lntypes.Hash, setID SetID) error {

	i.logger.Debugw("New cancel request received", "hash", hash)

	request := &invoiceRequest{
		hash:    hash,
		setID:   setID,
		errChan: make(chan error),
	}

	select {
	case i.cancelChan <- request:

	case <-i.quit:
		return ErrShuttingDown
	}

	select {
	case err := <-request.errChan:
		return err

	case <-i.quit:
		return ErrShuttingDown
	}
}

// tickAt returns a channel that ticks at the specified time. If the time has
// already passed, it will tick immediately.
func (i *InvoiceRegistry) tickAt(t time.Time) <-chan time.Time {
	now := i.cfg.Clock.Now()

	return i.cfg.Clock.TickAfter(t.Sub(now))
}

// invoiceEventLoop is the dedicated goroutine responsible for accepting
// new notification subscriptions, cancelling old subscriptions, and
// dispatching new invoice events.
func (i *InvoiceRegistry) invoiceEventLoop(ctx context.Context) error {
	for {
		// If there is something to release, set up a release tick
		// channel.
		var nextHtlcReleaseTick <-chan time.Time
		if i.autoReleaseHeap.Len() > 0 {
			head := i.autoReleaseHeap.Top().(releaseEvent)
			nextHtlcReleaseTick = i.tickAt(head.getReleaseTime())

			i.logger.Debugw("Next release", "time", head.getReleaseTime())
		}

		select {

		// The htlc at the top of the heap needs to be auto-released.
		case <-nextHtlcReleaseTick:
			head := i.autoReleaseHeap.Pop()

			i.logger.Debugw("Release tick",
				"time", head.(releaseEvent).getReleaseTime())

			switch event := head.(type) {
			case *htlcReleaseEvent:
				i.cancelSingleHtlc(
					event.hash, event.key, ResultMppTimeout,
				)

			case *acceptTimeoutEvent:
				err := i.failInvoice(event.hash)
				if err != nil {
					return err
				}
			}

		case htlc := <-i.htlcChan:
			err := i.process(ctx, htlc)
			if err != nil {
				i.logger.Errorf("Process: %v", err)
			}

		case newAcceptSubscription := <-i.newInvoiceAcceptSubscription:
			err := i.addAcceptSubscriber(newAcceptSubscription)
			if err != nil {
				return err
			}

		case id := <-i.cancelInvoiceAcceptSubscription:
			i.subscriptionManager.deleteAcceptSubscription(id)

		case req := <-i.requestSettleChan:
			requestErr, err := i.handleSettleRequest(ctx, req.hash, req.setID)
			if err != nil {
				return err
			}

			if err := i.sendResponse(req.errChan, requestErr); err != nil {
				return err
			}

		case req := <-i.cancelChan:
			requestErr, err := i.handleCancelRequest(ctx, req.hash, req.setID)
			if err != nil {
				return err
			}

			if err := i.sendResponse(req.errChan, requestErr); err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()

		case <-i.quit:
			return nil
		}
	}
}

func (i *InvoiceRegistry) handleCancelRequest(ctx context.Context,
	hash lntypes.Hash, setID SetID) (error, error) {

	set, requestErr, err := i.getAcceptedSet(ctx, hash, setID)
	if err != nil {
		return nil, err
	}
	if requestErr != nil {
		return requestErr, nil
	}

	// Delete in-memory record for this invoice. Only open invoices are
	// kept in memory.
	set.deleteAll(func(key types.CircuitKey) {
		// Notify subscribers that the htlcs should be settled
		// with our peer.
		resolution := NewFailResolution(
			ResultInvoiceNotOpen,
		)
		i.notifyHodlSubscribers(key, resolution)
	})

	return nil, nil
}

func (i *InvoiceRegistry) handleSettleRequest(ctx context.Context,
	hash lntypes.Hash, setID [32]byte) (error, error) {

	set, requestErr, err := i.getAcceptedSet(ctx, hash, setID)
	if err != nil {
		return nil, err
	}

	switch {
	// Invoice already requested to be settled. This operation is implemented
	// idempotently, so return success.
	case requestErr == ErrSettleRequested:
		return nil, nil

	case requestErr != nil:
		return requestErr, nil
	}

	// Don't allow external settles on auto-settling invoices.
	if i.cfg.AutoSettle {
		return ErrAutoSettling, nil
	}

	// Store settle request in database.
	err = i.markSettleRequested(ctx, set)
	if err != nil {
		i.logger.Debugw("Settle request error", "err", err)

		return nil, err
	}

	err = i.requestSettle(set)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func (i *InvoiceRegistry) getAcceptedSet(ctx context.Context, hash lntypes.Hash,
	setID SetID) (htlcSet, error, error) {

	// Retrieve invoice.
	set, ok := i.sets.get(hash)
	if !ok {
		// The invoice is not in the accepted state. Check the database
		// to see if it was already settled or requested to be settled.
		_, htlcs, err := i.cdb.Get(ctx, hash)
		switch {
		case err == types.ErrInvoiceNotFound:
			return nil, types.ErrInvoiceNotFound, nil

		case err != nil:
			return nil, nil, err
		}

		// Invoice is in the database. Check if the set id matches.
		var keys []types.CircuitKey
		for key := range htlcs {
			keys = append(keys, key)
		}
		dbSetID := newSetID(keys)
		if setID != dbSetID {
			return nil, types.ErrInvoiceNotFound, nil
		}

		return nil, ErrSettleRequested, nil
	}

	// Check whether the set is still complete.
	setHash := set.isComplete()
	if setHash == nil {
		i.logger.Infow("Set incomplete",
			"setTotal", set.totalSetAmt(),
			"invoiceValue", set.value())

		return nil, types.ErrInvoiceNotFound, nil
	}
	if *setHash != setID {
		i.logger.Infow("Set mismatch",
			"expectedSet", *setHash, "actualSet", setID)

		return nil, types.ErrInvoiceNotFound, nil
	}

	return set, nil, nil
}

// sendResponse sends a result on a response channel.
func (i *InvoiceRegistry) sendResponse(ch chan error, err error) error {
	select {
	case ch <- err:
		return nil

	case <-i.quit:
		return ErrShuttingDown
	}
}

func (i *InvoiceRegistry) addAcceptSubscriber(
	newSubscription invoiceAcceptSubscription) error {

	// Store subscriber for future updates.
	i.subscriptionManager.addAcceptSubscription(
		newSubscription.id, newSubscription.callback,
	)

	// Send accepted invoices from memory.
	i.sets.forEach(func(set htlcSet) {
		// No event for partially accepted invoices.
		setID := set.isComplete()
		if setID == nil {
			return
		}

		newSubscription.callback(set.hash(), *setID)
	})

	return nil
}

func (i *InvoiceRegistry) failInvoice(hash lntypes.Hash) error {
	logger := i.logger.With("hash", hash)

	// Retrieve invoice.
	set, ok := i.sets.get(hash)
	if !ok {
		// Invoice has already been settled or cancelled
		// We don't log here because it is a lazy clean-up.
		return nil
	}

	// Cancel all accepted htlcs.
	set.deleteAll(func(key types.CircuitKey) {
		i.notifyHodlSubscribers(key, NewFailResolution(ResultAcceptTimeout))
	})

	logger.Infow("Failed invoice")

	return nil
}

func (i *InvoiceRegistry) startAcceptTimer(hash lntypes.Hash) {
	releaseTime := time.Now().Add(i.cfg.AcceptTimeout)
	event := &acceptTimeoutEvent{
		eventBase: eventBase{
			hash:        hash,
			releaseTime: releaseTime,
		},
	}

	i.logger.Debugw("Scheduling auto-release for invoice accept",
		"hash", hash, "releaseTime", releaseTime)

	i.autoReleaseHeap.Push(event)
}

// startHtlcTimer starts a new timer via the invoice registry main loop that
// cancels a single htlc on an invoice when the htlc hold duration has passed.
func (i *InvoiceRegistry) startHtlcTimer(hash lntypes.Hash,
	key types.CircuitKey, releaseTime time.Time) {

	event := &htlcReleaseEvent{
		eventBase: eventBase{
			hash:        hash,
			releaseTime: releaseTime,
		},
		key: key,
	}

	i.logger.Debugf("Scheduling auto-release for htlc: "+
		"ref=%v, key=%v at %v", hash, key, releaseTime)

	// We use an independent timer for every htlc rather
	// than a set timer that is reset with every htlc coming
	// in. Otherwise the sender could keep resetting the
	// timer until the broadcast window is entered and our
	// channel is force closed.
	i.autoReleaseHeap.Push(event)
}

// cancelSingleHtlc cancels a single accepted htlc on an invoice. It takes
// a resolution result which will be used to notify subscribed links and
// resolvers of the details of the htlc cancellation.
func (i *InvoiceRegistry) cancelSingleHtlc(hash lntypes.Hash,
	key types.CircuitKey, result FailResolutionResult) {

	// Do nothing if the set has already been settled.
	set, ok := i.sets.get(hash)
	if !ok {
		return
	}

	// Do nothing if the set is already complete.
	if set.isComplete() != nil {
		return
	}

	i.logger.Debugf("cancelSingleHtlc: cancelling htlc %v on invoice %v",
		key, hash)

	set.deleteHtlc(key)

	i.notifyHodlSubscribers(key, NewFailResolution(result))
}

// NotifyExitHopHtlc attempts to mark an invoice as settled. The return value
// describes how the htlc should be resolved.
//
// When the preimage of the invoice is not yet known (hodl invoice), this
// function moves the invoice to the accepted state. When SettleHoldInvoice is
// called later, a resolution message will be send back to the caller via the
// provided hodlChan. Invoice registry sends on this channel what action needs
// to be taken on the htlc (settle or cancel). The caller needs to ensure that
// the channel is either buffered or received on from another goroutine to
// prevent deadlock.
//
// In the case that the htlc is part of a larger set of htlcs that pay to the
// same invoice (multi-path payment), the htlc is held until the set is
// complete. If the set doesn't fully arrive in time, a timer will cancel the
// held htlc.
func (i *InvoiceRegistry) NotifyExitHopHtlc(h *registryHtlc) {
	select {
	case i.htlcChan <- h:
	case <-i.quit:
	}
}

type registryHtlc struct {
	rHash         lntypes.Hash
	amtPaid       lnwire.MilliSatoshi
	expiry        uint32
	currentHeight int32
	circuitKey    types.CircuitKey
	payload       Payload
	resolve       func(HtlcResolution)
}

func (i *InvoiceRegistry) resolveViaDb(ctx context.Context,
	h *registryHtlc) (bool, error) {

	dbInvoice, htlcs, err := i.cdb.Get(ctx, h.rHash)
	switch {
	case err == types.ErrInvoiceNotFound:
		return false, nil

	case err != nil:
		return false, err
	}

	i.logger.Debugw("Loaded settled invoice from db", "hash", h.rHash)

	// Handle replays to a settled invoice.
	if len(htlcs) == 0 {
		return false, errors.New("unexpected unsettled invoice")
	}

	// If this htlc was used for settling the invoice,
	// resolve to settled again.
	if _, ok := htlcs[h.circuitKey]; ok {
		h.resolve(NewSettleResolution(
			dbInvoice.PaymentPreimage,
			ResultReplayToSettled,
		))

		return true, nil
	}

	// Otherwise fail the htlc.
	h.resolve(NewFailResolution(ResultInvoiceNotOpen))

	return true, nil
}

func (i *InvoiceRegistry) process(ctx context.Context, h *registryHtlc) error {
	logger := i.logger.With("hash", h.rHash)

	// First try to resolve via the database, in case this is a replay.
	resolved, err := i.resolveViaDb(ctx, h)
	if err != nil {
		return err
	}
	if resolved {
		return nil
	}

	// Always require an mpp record.
	mpp := h.payload.MultiPath()
	if mpp == nil {
		h.resolve(NewFailResolution(
			ResultHtlcInvoiceTypeMismatch,
		))

		return nil
	}

	// Don't accept zero-valued sets.
	if mpp.TotalMsat() == 0 {
		h.resolve(NewFailResolution(
			ResultHtlcSetTotalTooLow,
		))

		return nil
	}

	statelessData, err := decodeStatelessData(
		i.cfg.PrivKey[:], mpp.PaymentAddr(),
	)
	if err != nil {
		return err
	}

	// If the preimage doesn't match the payment hash, fail this htlc. Someone
	// must have tampered with our payment parameters.
	if statelessData.preimage.Hash() != h.rHash {
		logger.Debugw("Hash mismatch")

		h.resolve(NewFailResolution(ResultAddressMismatch))

		return nil
	}

	logger = logger.With(
		"amtMsat", statelessData.amtMsat, "expiry", statelessData.expiry,
	)

	// Check that the total amt of the htlc set is matching the invoice
	// amount. We don't accept overpayment.
	if mpp.TotalMsat() != lnwire.MilliSatoshi(statelessData.amtMsat) {
		h.resolve(NewFailResolution(
			ResultHtlcSetTotalMismatch,
		))

		return nil
	}

	logger.Infow("Stateless invoice payment received")

	// Look up this invoice in memory. If it is present, we have already
	// received other shards of the payment.
	var setTotal int64

	set, setExists := i.sets.get(h.rHash)
	if setExists {
		// Handle re-accepts.
		if set.accepted(h.circuitKey) {
			i.logger.Debugf("Htlc re-accepted: hash=%v, amt=%v, expiry=%v, circuit=%v, mpp=%v",
				h.rHash, h.amtPaid, h.expiry, h.circuitKey, mpp)

			i.hodlSubscribe(h.resolve, h.circuitKey)

			return nil
		}

		setTotal = set.totalSetAmt()
	}

	// Check expiry. Do this after handling re-accepts. We only want to apply
	// this check to new htlcs.
	if statelessData.expiry.Before(time.Now()) {
		logger.Infow("Stateless invoice payment to expired invoice")

		h.resolve(NewFailResolution(ResultInvoiceExpired))

		return nil
	}

	// Add amount of new htlc.
	newSetTotal := lnwire.MilliSatoshi(setTotal) + h.amtPaid

	// Make sure the communicated set total isn't overpaid.
	if newSetTotal > mpp.TotalMsat() {
		h.resolve(NewFailResolution(
			ResultHtlcSetOverpayment,
		))

		return nil
	}

	// The invoice is still open. Check the expiry.
	if h.expiry < uint32(h.currentHeight+i.cfg.FinalCltvRejectDelta) {
		h.resolve(NewFailResolution(
			ResultExpiryTooSoon,
		))

		return nil
	}

	// We are going to accept this htlc. Create in-memory state if that didn't
	// exist yet.
	if !setExists {
		// Don't start a new set if there is currently no application listening
		// for accept events. This is to prevent htlcs being held on to until
		// the mpp timeout when no one is listening.
		//
		// When we are adding to an existing set, we keep going and assume that
		// the application will be back online soon.
		//
		// This restriction does not apply in auto-settle mode.
		if !i.cfg.AutoSettle && !i.subscriptionManager.hasSubscribers() {
			h.resolve(NewFailResolution(
				ResultNoAcceptSubscriber,
			))

			return nil
		}

		set = i.sets.add(
			&htlcSetParameters{
				preimage:    statelessData.preimage,
				value:       statelessData.amtMsat,
				paymentAddr: mpp.PaymentAddr(),
			},
			h.circuitKey, int64(h.amtPaid),
		)
	} else {
		set.addHtlc(h.circuitKey, int64(h.amtPaid))
	}

	i.hodlSubscribe(h.resolve, h.circuitKey)

	i.logger.Debugf("Htlc accepted: hash=%v, amt=%v, expiry=%v, circuit=%v, mpp=%v",
		h.rHash, h.amtPaid, h.expiry, h.circuitKey, mpp)

	// If the invoice cannot be settled yet, only record the htlc.
	setID := set.isComplete()
	if setID == nil {
		// Start a release timer for this htlc. We release either after the hold
		// duration has passed or the invoice expires - whichever comes first.
		releaseTime := time.Now().Add(i.cfg.HtlcHoldDuration)
		if releaseTime.After(statelessData.expiry) {
			releaseTime = statelessData.expiry
		}

		i.startHtlcTimer(h.rHash, h.circuitKey, releaseTime)

		return nil
	}

	i.logger.Debugw("Set complete", "setID", *setID)

	// The set is complete and we start the accept timer.
	i.startAcceptTimer(h.rHash)

	// Notify subscriber of accepted invoice.
	i.subscriptionManager.notifySubscribers(h.rHash, *setID)

	// Auto-settle invoice if specified.
	if i.cfg.AutoSettle {
		i.logger.Debugw("Auto-settling", "hash", h.rHash)

		// The set is complete and we go straight to markSettleRequested.
		if err := i.markSettleRequested(ctx, set); err != nil {
			return err
		}

		if err := i.requestSettle(set); err != nil {
			return err
		}
	}

	return nil
}

type InvoiceUpdate struct {
	State persistence.InvoiceState
}

func (i *InvoiceRegistry) requestSettle(set htlcSet) error {
	// Delete in-memory record for this invoice. Only open invoices are
	// kept in memory.
	set.deleteAll(func(key types.CircuitKey) {
		// Notify subscribers that the htlcs should be settled
		// with our peer.
		htlcSettleResolution := NewSettleResolution(
			set.preimage(), ResultSettled,
		)
		i.notifyHodlSubscribers(key, htlcSettleResolution)
	})

	// TODO: Wait for final settle event from lnd. Unfortunately this
	// event is not yet implemented.

	return nil
}

func (i *InvoiceRegistry) markSettleRequested(ctx context.Context,
	set htlcSet) error {

	hash := set.hash()

	i.logger.Infow("Stateless invoice JIT insertion",
		"hash", hash)

	// Store settle request in database. This is important to prevent partial
	// settles after a restart.
	htlcMap := set.getHtlcMap()

	invoice := &persistence.InvoiceCreationData{
		InvoiceCreationData: types.InvoiceCreationData{
			Value:           lnwire.MilliSatoshi(set.value()),
			PaymentPreimage: set.preimage(),
			PaymentAddr:     set.paymentAddr(),
		},
	}

	err := i.cdb.RequestSettle(ctx, invoice, htlcMap)
	if err != nil {
		return fmt.Errorf("cannot request settle in database: %w", err)
	}

	return nil
}

// notifyHodlSubscribers sends out the htlc resolution to all current
// subscribers.
func (i *InvoiceRegistry) notifyHodlSubscribers(key types.CircuitKey,
	htlcResolution HtlcResolution) {

	subscribers, ok := i.hodlSubscriptions[key]
	if !ok {
		return
	}

	// Notify all interested subscribers and remove subscription from both
	// maps. The subscription can be removed as there only ever will be a
	// single resolution for each hash.
	for _, subscriber := range subscribers {
		subscriber(htlcResolution)
	}

	delete(i.hodlSubscriptions, key)
}

// hodlSubscribe adds a new invoice subscription.
func (i *InvoiceRegistry) hodlSubscribe(subscriber func(HtlcResolution),
	circuitKey types.CircuitKey) {

	i.logger.Debugf("Hodl subscribe for %v", circuitKey)

	subscriptions := i.hodlSubscriptions[circuitKey]
	subscriptions = append(subscriptions, subscriber)
	i.hodlSubscriptions[circuitKey] = subscriptions
}
