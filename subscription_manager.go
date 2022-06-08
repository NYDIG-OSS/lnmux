package lnmux

import (
	"sync/atomic"

	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

type subscriptionManager struct {
	subscriptions map[lntypes.Hash]map[int]InvoiceCallback
	logger        *zap.SugaredLogger

	nextSubscriberId uint64
}

func newSubscriptionManager(logger *zap.SugaredLogger) *subscriptionManager {
	return &subscriptionManager{
		subscriptions: make(map[lntypes.Hash]map[int]InvoiceCallback),
		logger:        logger,
	}
}

func (s *subscriptionManager) deleteSubscription(hash lntypes.Hash, id int) {
	subs, ok := s.subscriptions[hash]
	if !ok {
		s.logger.Debugw("no subscriptions for hash",
			"hash", hash)

		return
	}

	if _, ok := subs[id]; !ok {
		s.logger.Debugw("subscription not found",
			"id", id, "hash", hash)

		return
	}

	delete(subs, id)
}

func (s *subscriptionManager) addSubscription(hash lntypes.Hash, id int,
	callback InvoiceCallback) {

	subs := s.subscriptions
	if subs[hash] == nil {
		subs[hash] = make(map[int]InvoiceCallback)
	}
	subs[hash][id] = callback
}

func (s *subscriptionManager) generateSubscriptionId() int {
	return int(atomic.AddUint64(&s.nextSubscriberId, 1))
}

func (s *subscriptionManager) notifySubscribers(hash lntypes.Hash,
	update InvoiceUpdate) {

	for _, subscriber := range s.subscriptions[hash] {
		subscriber(update)
	}
}
