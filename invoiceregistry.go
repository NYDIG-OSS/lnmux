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

	// ErrInvoiceNotFound is returned when the invoice is unknown to the invoice
	// registry.
	ErrInvoiceNotFound = errors.New("invoice not found")
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
}

type InvoiceCallback func(update InvoiceUpdate)

type invoiceState struct {
	invoice       *types.InvoiceCreationData
	acceptedHtlcs map[types.CircuitKey]*InvoiceHTLC
	autoSettle    bool
}

func (i *invoiceState) totalSetAmt() int {
	total := 0
	for _, htlc := range i.acceptedHtlcs {
		total += int(htlc.Amt)
	}
	return total
}

type invoiceRequest struct {
	hash    lntypes.Hash
	errChan chan error
}

type invoiceSubscription struct {
	hash     lntypes.Hash
	callback InvoiceCallback
	id       int
}

type invoiceSubscriptionCancelRequest struct {
	hash lntypes.Hash
	id   int
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

	invoices       map[lntypes.Hash]*invoiceState
	htlcChan       chan *registryHtlc
	newInvoiceChan chan *persistence.InvoiceCreationData
	settleChan     chan *invoiceRequest
	cancelChan     chan *invoiceRequest

	autoReleaseHeap *queue.PriorityQueue
	logger          *zap.SugaredLogger

	newInvoiceSubscription    chan invoiceSubscription
	cancelInvoiceSubscription chan invoiceSubscriptionCancelRequest

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
		cdb:                       cdb,
		hodlSubscriptions:         make(map[types.CircuitKey][]func(HtlcResolution)),
		cfg:                       cfg,
		invoices:                  make(map[lntypes.Hash]*invoiceState),
		htlcChan:                  make(chan *registryHtlc),
		newInvoiceChan:            make(chan *persistence.InvoiceCreationData),
		newInvoiceSubscription:    make(chan invoiceSubscription),
		cancelInvoiceSubscription: make(chan invoiceSubscriptionCancelRequest),
		subscriptionManager:       newSubscriptionManager(cfg.Logger),
		settleChan:                make(chan *invoiceRequest),
		cancelChan:                make(chan *invoiceRequest),
		quit:                      make(chan struct{}),

		logger:          cfg.Logger,
		autoReleaseHeap: &queue.PriorityQueue{},
	}
}

// Start starts the registry and all goroutines it needs to carry out its task.
func (i *InvoiceRegistry) Run(ctx context.Context) error {
	i.logger.Info("InvoiceRegistry starting")

	pendingInvoices, err := i.cdb.GetOpen(ctx)
	if err != nil {
		return err
	}

	i.logger.Infow("Open invoices", "count", len(pendingInvoices))

	for _, invoice := range pendingInvoices {
		// Immediately fail invoices that expired while we were not running.
		if time.Now().After(invoice.ExpiresAt) {
			hash := invoice.PaymentPreimage.Hash()

			i.logger.Debugw("Invoice expired",
				"hash", hash, "expiresAt", invoice.ExpiresAt)

			err := i.cdb.Fail(ctx, hash, persistence.CancelledReasonExpired)
			if err != nil {
				return err
			}

			continue
		}

		state := &invoiceState{
			invoice:       &invoice.InvoiceCreationData.InvoiceCreationData,
			acceptedHtlcs: make(map[types.CircuitKey]*InvoiceHTLC),
			autoSettle:    invoice.AutoSettle,
		}

		hash := invoice.PaymentPreimage.Hash()

		i.invoices[hash] = state
		i.startInvoiceExpireTimer(hash, invoice.ExpiresAt)

		i.logger.Debugw("Pending invoice",
			"hash", hash, "expiresAt", invoice.ExpiresAt)
	}

	err = i.invoiceEventLoop(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		i.logger.Errorw("InvoiceRegistry error", "err", err)

		return err
	}

	i.logger.Info("InvoiceRegistry shutting down")
	close(i.quit)

	return nil
}

func (i *InvoiceRegistry) Subscribe(hash lntypes.Hash,
	callback InvoiceCallback) (func(), error) {

	subscriberId := i.subscriptionManager.generateSubscriptionId()
	logger := i.logger.With("id", subscriberId, "hash", hash)

	logger.Debugw("Adding subscriber")

	select {
	case i.newInvoiceSubscription <- invoiceSubscription{
		callback: callback,
		hash:     hash,
		id:       subscriberId,
	}:

	case <-i.quit:
		return nil, ErrShuttingDown
	}

	return func() {
		logger.Debugw("Removing subscriber")

		select {
		case i.cancelInvoiceSubscription <- invoiceSubscriptionCancelRequest{
			id:   subscriberId,
			hash: hash,
		}:
		case <-i.quit:
		}
	}, nil
}

func (i *InvoiceRegistry) NewInvoice(invoice *persistence.InvoiceCreationData) error {
	select {
	case i.newInvoiceChan <- invoice:
	case <-i.quit:
		return ErrShuttingDown
	}

	return nil
}

func (i *InvoiceRegistry) RequestSettle(hash lntypes.Hash) error {
	i.logger.Debugw("New settle request received", "hash", hash)

	request := &invoiceRequest{
		hash:    hash,
		errChan: make(chan error),
	}

	select {
	case i.settleChan <- request:

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
	hash lntypes.Hash) error {

	i.logger.Debugw("New cancel request received", "hash", hash)

	request := &invoiceRequest{
		hash:    hash,
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
				err := i.cancelSingleHtlc(
					event.hash, event.key, ResultMppTimeout,
				)
				if err != nil {
					i.logger.Errorf("HTLC timer: %v", err)
				}

			case *invoiceExpiredEvent:
				err := i.failInvoice(
					ctx, event.hash, persistence.CancelledReasonExpired,
				)
				if err != nil {
					return err
				}

			case *acceptTimeoutEvent:
				err := i.failInvoice(
					ctx, event.hash, persistence.CancelledReasonAcceptTimeout,
				)
				if err != nil {
					return err
				}
			}

		case htlc := <-i.htlcChan:
			err := i.process(ctx, htlc)
			if err != nil {
				i.logger.Errorf("Process: %v", err)
			}

		case invoice := <-i.newInvoiceChan:
			err := i.cdb.Add(ctx, invoice)
			if err != nil {
				return err
			}

			hash := invoice.PaymentPreimage.Hash()

			i.logger.Debugw("New invoice",
				"hash", hash, "amt", invoice.Value)

			state := &invoiceState{
				invoice:       &invoice.InvoiceCreationData,
				acceptedHtlcs: make(map[types.CircuitKey]*InvoiceHTLC),
				autoSettle:    invoice.AutoSettle,
			}

			i.invoices[hash] = state

			// Notify subscriber of new invoice.
			i.subscriptionManager.notifySubscribers(hash, InvoiceUpdate{
				State: persistence.InvoiceStateOpen,
			})

			i.startInvoiceExpireTimer(hash, invoice.ExpiresAt)

		case newSubscription := <-i.newInvoiceSubscription:
			if err := i.addSubscriber(ctx, newSubscription); err != nil {
				return err
			}

		case request := <-i.cancelInvoiceSubscription:
			i.subscriptionManager.deleteSubscription(request.hash, request.id)

		case req := <-i.settleChan:
			sendResponse := func(err error) error {
				return i.sendResponse(req.errChan, err)
			}

			// Retrieve invoice.
			state, ok := i.invoices[req.hash]
			if !ok {
				if err := sendResponse(ErrInvoiceNotFound); err != nil {
					return err
				}

				break
			}

			// Don't allow external settles on auto-settling invoices.
			if state.autoSettle {
				err := sendResponse(errors.New("invoice is auto-settling"))
				if err != nil {
					return err
				}

				break
			}

			// Store settle request in database.
			err := i.markSettleRequested(ctx, state)

			// In both error and success cases, notify request thread of
			// outcome.
			if sendErr := sendResponse(err); sendErr != nil {
				return sendErr
			}

			if err != nil {
				i.logger.Debugw("Settle request error", "err", err)

				break
			}

			// Send settle signal to lnd node(s).
			err = i.requestSettle(ctx, state)
			if err != nil {
				return err
			}

		case req := <-i.cancelChan:
			// Retrieve invoice.
			state, ok := i.invoices[req.hash]
			if !ok {
				err := i.sendResponse(req.errChan, ErrInvoiceNotFound)
				if err != nil {
					return err
				}

				break
			}

			// Mark invoice as failed.
			if err := i.cdb.Fail(ctx, req.hash, persistence.CancelledReasonExternal); err != nil {
				return errors.New("cannot fail invoice in database")
			}

			// Delete in-memory record for this invoice. Only open invoices are
			// kept in memory.
			delete(i.invoices, req.hash)

			// Notify subscribers that the htlcs should be settled
			// with our peer.
			for key := range state.acceptedHtlcs {
				resolution := NewFailResolution(
					ResultInvoiceNotOpen,
				)
				i.notifyHodlSubscribers(key, resolution)
			}

			// Notify subscriber of settled invoice.
			i.subscriptionManager.notifySubscribers(
				req.hash,
				InvoiceUpdate{
					State:           persistence.InvoiceStateCancelled,
					CancelledReason: persistence.CancelledReasonExternal,
				},
			)

			// Send success response.
			err := i.sendResponse(req.errChan, nil)
			if err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()

		case <-i.quit:
			return nil
		}
	}
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

func (i *InvoiceRegistry) addSubscriber(ctx context.Context,
	newSubscription invoiceSubscription) error {

	hash := newSubscription.hash

	// Store subscriber for future updates.
	i.subscriptionManager.addSubscription(
		hash, newSubscription.id, newSubscription.callback,
	)

	// Send open or accepted invoice from memory.
	var update InvoiceUpdate

	invoiceState, ok := i.invoices[hash]
	if ok {
		update.State = persistence.InvoiceStateOpen
		if len(invoiceState.acceptedHtlcs) > 0 {
			update.State = persistence.InvoiceStateAccepted
		}
	} else {
		// Send other states from database.
		invoice, _, err := i.cdb.Get(ctx, hash)
		switch {
		case err == types.ErrInvoiceNotFound:
			i.logger.Debugw("No initial state to send",
				"hash", hash, "id", newSubscription.id)

			return nil

		case err != nil:
			return err
		}

		update.State = invoice.State
		update.CancelledReason = invoice.CancelledReason
	}

	newSubscription.callback(update)

	return nil
}

func (i *InvoiceRegistry) failInvoice(ctx context.Context,
	hash lntypes.Hash, reason persistence.CancelledReason) error {

	logger := i.logger.With("hash", hash)

	// Retrieve invoice.
	state, ok := i.invoices[hash]
	if !ok {
		logger.Debugw("Invoice to fail no longer open/accepted")

		return nil
	}

	// Don't expire invoices that are already accepted.
	setComplete := state.totalSetAmt() == int(state.invoice.Value)
	if reason == persistence.CancelledReasonExpired && setComplete {
		return nil
	}

	// Cancel all accepted htlcs.
	for key := range state.acceptedHtlcs {
		i.notifyHodlSubscribers(key, NewFailResolution(ResultInvoiceExpired))
	}

	// Mark invoice as expired in the database.
	err := i.cdb.Fail(ctx, hash, reason)
	if err != nil {
		return err
	}

	// Remove from memory because invoice is no longer open.
	delete(i.invoices, hash)

	// Notify subscriber.
	i.subscriptionManager.notifySubscribers(hash, InvoiceUpdate{
		State:           persistence.InvoiceStateCancelled,
		CancelledReason: reason,
	})

	logger.Infow("Failed invoice")

	return nil
}

func (i *InvoiceRegistry) startInvoiceExpireTimer(hash lntypes.Hash,
	releaseTime time.Time) {

	event := &invoiceExpiredEvent{
		eventBase: eventBase{
			hash:        hash,
			releaseTime: releaseTime,
		},
	}

	i.logger.Debugw("Scheduling auto-release for invoice",
		"hash", hash, "releaseTime", releaseTime)

	i.autoReleaseHeap.Push(event)
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
	key types.CircuitKey, acceptTime time.Time) {

	releaseTime := acceptTime.Add(i.cfg.HtlcHoldDuration)
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
	key types.CircuitKey, result FailResolutionResult) error {

	// Do nothing if the invoice has already been settled.
	invoice, ok := i.invoices[hash]
	if !ok {
		return nil
	}

	// Do nothing if the set is already complete.
	setComplete := invoice.totalSetAmt() == int(invoice.invoice.Value)
	if setComplete {
		return nil
	}

	_, ok = invoice.acceptedHtlcs[key]
	if !ok {
		return fmt.Errorf("cancelSingleHtlc: htlc %v on invoice %v "+
			"not accepted", key, hash)
	}

	i.logger.Debugf("cancelSingleHtlc: cancelling htlc %v on invoice %v",
		key, hash)

	delete(invoice.acceptedHtlcs, key)

	i.notifyHodlSubscribers(key, NewFailResolution(result))

	return nil
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
	// Always require an mpp record.
	mpp := h.payload.MultiPath()
	if mpp == nil {
		h.resolve(NewFailResolution(
			ResultHtlcInvoiceTypeMismatch,
		))

		return nil
	}

	state, ok := i.invoices[h.rHash]
	if !ok {
		// Invoice is not present in memory. Do a db lookup to see if this
		// happens to be a previously settled invoice and resolve the htlc if
		// possible.
		resolved, err := i.resolveViaDb(ctx, h)
		if err != nil {
			return err
		}
		if !resolved {
			// If the invoice was not found, return a failure
			// resolution with an invoice not found result.
			h.resolve(NewFailResolution(ResultInvoiceNotFound))
		}

		return nil
	}

	if _, ok := state.acceptedHtlcs[h.circuitKey]; ok {
		i.logger.Debugf("Htlc re-accepted: hash=%v, amt=%v, expiry=%v, circuit=%v, mpp=%v",
			h.rHash, h.amtPaid, h.expiry, h.circuitKey, mpp)

		i.hodlSubscribe(h.resolve, h.circuitKey)

		return nil
	}

	inv := state.invoice

	// Check the payment address that authorizes the payment.
	if mpp.PaymentAddr() != inv.PaymentAddr {
		h.resolve(NewFailResolution(
			ResultAddressMismatch,
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

	// Check that the total amt of the htlc set is matching the invoice
	// amount. We don't accept overpayment.
	if mpp.TotalMsat() != inv.Value {
		h.resolve(NewFailResolution(
			ResultHtlcSetOverpayment,
		))

		return nil
	}

	// Check whether total amt matches other htlcs in the set.
	var newSetTotal lnwire.MilliSatoshi
	for _, htlc := range state.acceptedHtlcs {
		if mpp.TotalMsat() != htlc.MppTotalAmt {
			h.resolve(NewFailResolution(ResultHtlcSetTotalMismatch))

			return nil
		}

		newSetTotal += htlc.Amt
	}

	// Add amount of new htlc.
	newSetTotal += h.amtPaid

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

	if h.expiry < uint32(h.currentHeight+inv.FinalCltvDelta) {
		h.resolve(NewFailResolution(
			ResultExpiryTooSoon,
		))

		return nil
	}

	state.acceptedHtlcs[h.circuitKey] = &InvoiceHTLC{
		Amt:         h.amtPaid,
		MppTotalAmt: mpp.TotalMsat(),
	}

	i.hodlSubscribe(h.resolve, h.circuitKey)

	i.logger.Debugf("Htlc accepted: hash=%v, amt=%v, expiry=%v, circuit=%v, mpp=%v",
		h.rHash, h.amtPaid, h.expiry, h.circuitKey, mpp)

	// If the invoice cannot be settled yet, only record the htlc.
	setComplete := newSetTotal == mpp.TotalMsat()
	if !setComplete {
		i.startHtlcTimer(
			h.rHash, h.circuitKey, time.Now(),
		)

		return nil
	}

	i.startAcceptTimer(h.rHash)

	// Notify subscriber of accepted invoice.
	i.subscriptionManager.notifySubscribers(h.rHash, InvoiceUpdate{
		State: persistence.InvoiceStateAccepted,
	})

	// Auto-settle invoice if specified.
	if state.autoSettle {
		i.logger.Debugw("Auto-settling", "hash", h.rHash)

		if err := i.markSettleRequested(ctx, state); err != nil {
			return err
		}

		if err := i.requestSettle(ctx, state); err != nil {
			return err
		}
	}

	return nil
}

type InvoiceUpdate struct {
	State           persistence.InvoiceState
	CancelledReason persistence.CancelledReason
}

func (i *InvoiceRegistry) requestSettle(ctx context.Context,
	state *invoiceState) error {

	hash := state.invoice.PaymentPreimage.Hash()

	// Delete in-memory record for this invoice. Only open invoices are
	// kept in memory.
	delete(i.invoices, hash)

	// Notify subscribers that the htlcs should be settled
	// with our peer.
	for key := range state.acceptedHtlcs {
		htlcSettleResolution := NewSettleResolution(
			state.invoice.PaymentPreimage, ResultSettled,
		)
		i.notifyHodlSubscribers(key, htlcSettleResolution)
	}

	// TODO: Wait for final settle event from lnd. Unfortunately this
	// event is not yet implemented.

	// Mark invoice as settled.
	if err := i.cdb.Settle(ctx, hash); err != nil {
		return fmt.Errorf("cannot settle invoice in database: %w", err)
	}

	// Notify subscriber of settled invoice.
	i.subscriptionManager.notifySubscribers(
		hash,
		InvoiceUpdate{
			State: persistence.InvoiceStateSettled,
		},
	)

	return nil
}

func (i *InvoiceRegistry) markSettleRequested(ctx context.Context,
	state *invoiceState) error {

	hash := state.invoice.PaymentPreimage.Hash()

	// Check whether the set is still complete.
	var setTotal lnwire.MilliSatoshi
	for _, htlc := range state.acceptedHtlcs {
		setTotal += htlc.Amt
	}
	if setTotal != state.invoice.Value {
		i.logger.Infow("Set no longer complete",
			"setTotal", setTotal,
			"invoiceValue", state.invoice.Value)

		return errors.New("set no longer complete")
	}

	// Store settle request in database. This is important to prevent partial
	// settles after a restart.
	htlcMap := make(map[types.CircuitKey]int64)
	for key, htlc := range state.acceptedHtlcs {
		htlcMap[key] = int64(htlc.Amt)
	}

	err := i.cdb.RequestSettle(
		ctx, hash, htlcMap,
	)
	if err != nil {
		return errors.New("cannot settle invoice in database")
	}

	// Notify subscriber of settle request.
	i.subscriptionManager.notifySubscribers(hash, InvoiceUpdate{
		State: persistence.InvoiceStateSettleRequested,
	})

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
