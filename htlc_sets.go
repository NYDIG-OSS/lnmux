package lnmux

import (
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lntypes"
)

type htlcSets interface {
	get(hash lntypes.Hash) (htlcSet, bool)
	forEach(cb func(htlcSet))
	add(set *htlcSetParameters, htlcKey types.CircuitKey, htlcAmt int64) htlcSet
}

type htlcSetsImpl struct {
	sets map[lntypes.Hash]*htlcSetImpl
}

func newHtlcSets() *htlcSetsImpl {
	return &htlcSetsImpl{
		sets: make(map[lntypes.Hash]*htlcSetImpl),
	}
}

type htlcSetParameters struct {
	preimage    lntypes.Preimage
	value       int64
	paymentAddr [32]byte
}

// add adds a new htlc set and the first accepted htlc for that set.
func (h *htlcSetsImpl) add(set *htlcSetParameters, htlcKey types.CircuitKey,
	htlcAmt int64) htlcSet {

	hash := set.preimage.Hash()
	if _, ok := h.sets[hash]; ok {
		panic("set already exists")
	}

	htlcSet := newHtlcSetImpl(h, *set)
	htlcSet.addHtlc(htlcKey, htlcAmt)

	h.sets[hash] = htlcSet

	return htlcSet
}

func (h *htlcSetsImpl) get(hash lntypes.Hash) (htlcSet, bool) {
	set, ok := h.sets[hash]
	if !ok {
		return nil, false
	}

	return set, ok
}

func (h *htlcSetsImpl) forEach(cb func(htlcSet)) {
	for _, set := range h.sets {
		cb(set)
	}
}

func (h *htlcSetsImpl) delete(hash lntypes.Hash) {
	if _, ok := h.sets[hash]; !ok {
		panic("set not found")
	}
	delete(h.sets, hash)
}
