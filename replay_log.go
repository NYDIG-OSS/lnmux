package lnmux

import (
	sphinx "github.com/lightningnetwork/lightning-onion"
)

type replayLog struct {
	sphinx.ReplayLog
}

func (r *replayLog) Put(hash *sphinx.HashPrefix, cltv uint32) error {
	return nil
}
