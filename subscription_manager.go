package lnmux

import (
	"sync/atomic"

	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

type subscriptionManager struct {
	subscriptionHash    map[int]lntypes.Hash
	acceptSubscriptions map[int]AcceptCallback
	logger              *zap.SugaredLogger

	nextSubscriberId uint64
}

func newSubscriptionManager(logger *zap.SugaredLogger) *subscriptionManager {
	return &subscriptionManager{
		subscriptionHash:    make(map[int]lntypes.Hash),
		acceptSubscriptions: make(map[int]AcceptCallback),
		logger:              logger,
	}
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

func (s *subscriptionManager) notifySubscribers(hash lntypes.Hash, setID [32]byte) {
	for _, subscriber := range s.acceptSubscriptions {
		subscriber(hash, setID)
	}
}
