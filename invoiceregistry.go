package lnmux

import (
	"errors"
	"fmt"
	"sync"
	"time"

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

	// Clock holds the clock implementation that is used to provide
	// Now() and TickAfter() and is useful to stub out the clock functions
	// during testing.
	Clock clock.Clock

	Logger *zap.SugaredLogger
}

// htlcReleaseEvent describes an htlc auto-release event. It is used to release
// mpp htlcs for which the complete set didn't arrive in time.
type htlcReleaseEvent struct {
	hash lntypes.Hash

	// key is the circuit key of the htlc to release.
	key CircuitKey

	// releaseTime is the time at which to release the htlc.
	releaseTime time.Time
}

// Less is used to order PriorityQueueItem's by their release time such that
// items with the older release time are at the top of the queue.
//
// NOTE: Part of the queue.PriorityQueueItem interface.
func (r *htlcReleaseEvent) Less(other queue.PriorityQueueItem) bool {
	return r.releaseTime.Before(other.(*htlcReleaseEvent).releaseTime)
}

type InvoiceDb interface {
	Get(hash lntypes.Hash) (*InvoiceCreationData, map[CircuitKey]int64, error)
	Settle(hash lntypes.Hash, htlcs map[CircuitKey]int64) error
}

type invoiceState struct {
	invoice       *InvoiceCreationData
	acceptedHtlcs map[CircuitKey]*InvoiceHTLC
}

// InvoiceRegistry is a central registry of all the outstanding invoices
// created by the daemon. The registry is a thin wrapper around a map in order
// to ensure that all updates/reads are thread safe.
type InvoiceRegistry struct {
	sync.RWMutex

	cdb InvoiceDb

	// cfg contains the registry's configuration parameters.
	cfg *RegistryConfig

	// subscriptions is a map from a circuit key to a list of subscribers.
	// It is used for efficient notification of links.
	hodlSubscriptions map[CircuitKey][]func(HtlcResolution)

	invoices map[lntypes.Hash]*invoiceState
	htlcChan chan *registryHtlc

	autoReleaseHeap *queue.PriorityQueue
	logger          *zap.SugaredLogger

	wg   sync.WaitGroup
	quit chan struct{}
}

// NewRegistry creates a new invoice registry. The invoice registry
// wraps the persistent on-disk invoice storage with an additional in-memory
// layer. The in-memory layer is in place such that debug invoices can be added
// which are volatile yet available system wide within the daemon.
func NewRegistry(cdb InvoiceDb,
	cfg *RegistryConfig) *InvoiceRegistry {

	return &InvoiceRegistry{
		cdb:               cdb,
		hodlSubscriptions: make(map[CircuitKey][]func(HtlcResolution)),
		cfg:               cfg,
		invoices:          make(map[lntypes.Hash]*invoiceState),
		htlcChan:          make(chan *registryHtlc),
		quit:              make(chan struct{}),
		logger:            cfg.Logger,
		autoReleaseHeap:   &queue.PriorityQueue{},
	}
}

// Start starts the registry and all goroutines it needs to carry out its task.
func (i *InvoiceRegistry) Start() error {
	i.logger.Info("InvoiceRegistry starting")

	i.wg.Add(1)
	go i.invoiceEventLoop()

	return nil
}

// Stop signals the registry for a graceful shutdown.
func (i *InvoiceRegistry) Stop() error {
	i.logger.Info("InvoiceRegistry shutting down")

	close(i.quit)

	i.wg.Wait()

	return nil
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
func (i *InvoiceRegistry) invoiceEventLoop() {
	defer i.wg.Done()

	for {
		// If there is something to release, set up a release tick
		// channel.
		var nextReleaseTick <-chan time.Time
		if i.autoReleaseHeap.Len() > 0 {
			head := i.autoReleaseHeap.Top().(*htlcReleaseEvent)
			nextReleaseTick = i.tickAt(head.releaseTime)
		}

		select {

		// The htlc at the top of the heap needs to be auto-released.
		case <-nextReleaseTick:
			event := i.autoReleaseHeap.Pop().(*htlcReleaseEvent)
			err := i.cancelSingleHtlc(
				event.hash, event.key, ResultMppTimeout,
			)
			if err != nil {
				i.logger.Errorf("HTLC timer: %v", err)
			}

		case htlc := <-i.htlcChan:
			err := i.process(htlc)
			if err != nil {
				i.logger.Errorf("Process: %v", err)
			}

		case <-i.quit:
			return
		}
	}
}

// startHtlcTimer starts a new timer via the invoice registry main loop that
// cancels a single htlc on an invoice when the htlc hold duration has passed.
func (i *InvoiceRegistry) startHtlcTimer(hash lntypes.Hash,
	key CircuitKey, acceptTime time.Time) {

	releaseTime := acceptTime.Add(i.cfg.HtlcHoldDuration)
	event := &htlcReleaseEvent{
		hash:        hash,
		key:         key,
		releaseTime: releaseTime,
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
	key CircuitKey, result FailResolutionResult) error {

	// Do nothing if the invoice has already been settled.
	invoice, ok := i.invoices[hash]
	if !ok {
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

	// Purge invoice from memory if there a no more pending htlcs.
	if len(invoice.acceptedHtlcs) == 0 {
		delete(i.invoices, hash)
	}

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
	circuitKey    CircuitKey
	payload       Payload
	resolve       func(HtlcResolution)
}

func (i *InvoiceRegistry) process(h *registryHtlc) error {
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
		dbInvoice, htlcs, err := i.cdb.Get(h.rHash)
		switch {
		case err == ErrInvoiceNotFound:
			// If the invoice was not found, return a failure
			// resolution with an invoice not found result.
			h.resolve(NewFailResolution(ResultInvoiceNotFound))

			return nil

		case err != nil:
			return err
		}

		i.logger.Debugw("Loaded invoice from db", "hash", h.rHash)

		// Handle replays to a settled invoice.
		if len(htlcs) > 0 {
			// If this htlc was used for settling the invoice,
			// resolve to settled again.
			if _, ok := htlcs[h.circuitKey]; ok {
				h.resolve(NewSettleResolution(
					dbInvoice.PaymentPreimage,
					ResultReplayToSettled,
				))

				return nil
			}

			// Otherwise fail the htlc.
			h.resolve(NewFailResolution(ResultInvoiceNotOpen))

			return nil
		}

		state = &invoiceState{
			invoice:       dbInvoice,
			acceptedHtlcs: make(map[CircuitKey]*InvoiceHTLC),
		}
	} else {
		if _, ok := state.acceptedHtlcs[h.circuitKey]; ok {
			i.logger.Debugf("Htlc re-accepted: hash=%v, amt=%v, expiry=%v, circuit=%v, mpp=%v",
				h.rHash, h.amtPaid, h.expiry, h.circuitKey, mpp)

			i.hodlSubscribe(h.resolve, h.circuitKey)

			return nil
		}
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
		// Store invoice in-memory until settled. Don't do this sooner,
		// because otherwise a memory leak may occur.
		i.invoices[h.rHash] = state

		i.startHtlcTimer(
			h.rHash, h.circuitKey, time.Now(),
		)

		return nil
	}

	htlcMap := make(map[CircuitKey]int64)
	for key, htlc := range state.acceptedHtlcs {
		htlcMap[key] = int64(htlc.Amt)
	}

	err := i.cdb.Settle(h.rHash, htlcMap)
	if err != nil {
		i.logger.Debugw("Cannot settle", "err", err)

		// Cancel all htlcs.
		for key := range state.acceptedHtlcs {
			i.notifyHodlSubscribers(
				key, NewFailResolution(ResultCannotSettle),
			)
		}

		delete(i.invoices, h.rHash)

		return nil
	}

	// Also settle any previously accepted htlcs. If a htlc is
	// marked as settled, we should follow now and settle the htlc
	// with our peer.
	for key := range state.acceptedHtlcs {
		// Notify subscribers that the htlcs should be settled
		// with our peer. Note that the outcome of the
		// resolution is set based on the outcome of the single
		// htlc that we just settled, so may not be accurate
		// for all htlcs.
		htlcSettleResolution := NewSettleResolution(
			inv.PaymentPreimage, ResultSettled,
		)

		// Notify subscribers that the htlc should be settled
		// with our peer.
		i.notifyHodlSubscribers(key, htlcSettleResolution)
	}

	// Delete in-memory record for this invoice.
	delete(i.invoices, h.rHash)

	return nil
}

// notifyHodlSubscribers sends out the htlc resolution to all current
// subscribers.
func (i *InvoiceRegistry) notifyHodlSubscribers(key CircuitKey,
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
	circuitKey CircuitKey) {

	i.logger.Debugf("Hodl subscribe for %v", circuitKey)

	subscriptions := i.hodlSubscriptions[circuitKey]
	subscriptions = append(subscriptions, subscriber)
	i.hodlSubscriptions[circuitKey] = subscriptions
}
