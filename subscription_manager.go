package lnmux

import (
	"sync/atomic"
	"time"

	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

// DefaultGracePeriodWithoutSubscribers is the default period during which the subscriberManager is waiting for a subscriber
// to reconnect.
const DefaultGracePeriodWithoutSubscribers = 30 * time.Second

type subscriptionManager struct {
	subscriptionHash    map[int]lntypes.Hash
	acceptSubscriptions map[int]AcceptCallback
	logger              *zap.SugaredLogger

	nextSubscriberId              uint64
	lastSubscriberRemovedAt       time.Time
	gracePeriodWithoutSubscribers time.Duration
}

func newSubscriptionManager(logger *zap.SugaredLogger, gracePeriodWithoutSubscribers time.Duration) *subscriptionManager {
	return &subscriptionManager{
		subscriptionHash:              make(map[int]lntypes.Hash),
		acceptSubscriptions:           make(map[int]AcceptCallback),
		logger:                        logger,
		gracePeriodWithoutSubscribers: gracePeriodWithoutSubscribers,
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

	s.lastSubscriberRemovedAt = time.Now()
}

func (s *subscriptionManager) generateSubscriptionId() int {
	return int(atomic.AddUint64(&s.nextSubscriberId, 1))
}

func (s *subscriptionManager) notifySubscribers(hash lntypes.Hash, setID [32]byte) {
	for _, subscriber := range s.acceptSubscriptions {
		subscriber(hash, setID)
	}
}

func (s *subscriptionManager) hasRecentSubscribers() bool {
	return len(s.acceptSubscriptions) > 0 || time.Since(s.lastSubscriberRemovedAt) <= s.gracePeriodWithoutSubscribers
}
