package lnmux

import (
	"time"

	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/queue"
)

type releaseEvent interface {
	getReleaseTime() time.Time
}
type eventBase struct {
	hash lntypes.Hash

	// releaseTime is the time at which to release the htlc.
	releaseTime time.Time
}

func (e *eventBase) getReleaseTime() time.Time {
	return e.releaseTime
}

// htlcReleaseEvent describes an htlc auto-release event. It is used to release
// mpp htlcs for which the complete set didn't arrive in time.
type htlcReleaseEvent struct {
	eventBase

	// key is the circuit key of the htlc to release.
	key types.HtlcKey
}

// Less is used to order PriorityQueueItem's by their release time such that
// items with the older release time are at the top of the queue.
//
// NOTE: Part of the queue.PriorityQueueItem interface.
func (r *eventBase) Less(other queue.PriorityQueueItem) bool {
	return r.getReleaseTime().Before(other.(releaseEvent).getReleaseTime())
}

type acceptTimeoutEvent struct {
	eventBase
}
