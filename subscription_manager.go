package lnmux

import (
	"sync/atomic"

	"github.com/bottlepay/lnmux/persistence"
	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

type subscriptionManager struct {
	subscriptions       map[lntypes.Hash]map[int]InvoiceCallback
	subscriptionHash    map[int]lntypes.Hash
	acceptSubscriptions map[int]AcceptCallback
	logger              *zap.SugaredLogger

	nextSubscriberId uint64
}

func newSubscriptionManager(logger *zap.SugaredLogger) *subscriptionManager {
	return &subscriptionManager{
		subscriptions:       make(map[lntypes.Hash]map[int]InvoiceCallback),
		subscriptionHash:    make(map[int]lntypes.Hash),
		acceptSubscriptions: make(map[int]AcceptCallback),
		logger:              logger,
	}
}

func (s *subscriptionManager) deleteSubscription(id int) {
	hash, ok := s.subscriptionHash[id]
	if !ok {
		s.logger.Debugw("no subscriptions for hash",
			"hash", hash)

		return
	}

	subs, ok := s.subscriptions[hash]
	if !ok {
		panic("inconsistent subscription")
	}

	if _, ok := subs[id]; !ok {
		panic("inconsistent subscription")
	}

	delete(subs, id)
	if len(subs) == 0 {
		delete(s.subscriptions, hash)
	}

	delete(s.subscriptionHash, id)
}

func (s *subscriptionManager) addSubscription(hash lntypes.Hash, id int,
	callback InvoiceCallback) {

	subs := s.subscriptions
	if subs[hash] == nil {
		subs[hash] = make(map[int]InvoiceCallback)
	}
	subs[hash][id] = callback

	s.subscriptionHash[id] = hash
}

func (s *subscriptionManager) addAcceptSubscription(id int,
	callback AcceptCallback) {

	s.acceptSubscriptions[id] = callback
}

func (s *subscriptionManager) deleteAcceptSubscription(id int) {
	_, ok := s.acceptSubscriptions[id]
	if !ok {
		s.logger.Debugw("no accept subscription", "id", id)

		return
	}

	delete(s.acceptSubscriptions, id)
}

func (s *subscriptionManager) generateSubscriptionId() int {
	return int(atomic.AddUint64(&s.nextSubscriberId, 1))
}

func (s *subscriptionManager) notifySubscribers(hash lntypes.Hash,
	update InvoiceUpdate) {

	if update.State == persistence.InvoiceStateAccepted {
		for _, subscriber := range s.acceptSubscriptions {
			subscriber(hash)
		}
	}

	for _, subscriber := range s.subscriptions[hash] {
		subscriber(update)
	}
}
