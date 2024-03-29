package lnmux

import (
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lntypes"
)

type htlcSet interface {
	addHtlc(key types.HtlcKey, amt int64)

	getHtlcMap() map[types.HtlcKey]int64
	accepted(key types.HtlcKey) bool
	totalSetAmt() int64
	isComplete() *SetID

	deleteAll(cb func(types.HtlcKey))
	deleteHtlc(key types.HtlcKey)

	hash() lntypes.Hash
	paymentAddr() [32]byte
	value() int64
	preimage() lntypes.Preimage
}

type htlcSetImpl struct {
	params htlcSetParameters
	htlcs  map[types.HtlcKey]int64

	parent *htlcSetsImpl
}

func newHtlcSetImpl(parent *htlcSetsImpl,
	params htlcSetParameters) *htlcSetImpl {

	return &htlcSetImpl{
		params: params,
		htlcs:  make(map[types.HtlcKey]int64),
		parent: parent,
	}
}

func (h *htlcSetImpl) deleteAll(cb func(types.HtlcKey)) {
	for key := range h.htlcs {
		cb(key)
	}

	h.parent.delete(h.params.preimage.Hash())
}

func (h *htlcSetImpl) totalSetAmt() int64 {
	var total int64
	for _, amt := range h.htlcs {
		total += amt
	}

	return total
}

func (h *htlcSetImpl) isComplete() *SetID {
	if h.totalSetAmt() != h.params.value {
		return nil
	}

	var keys []types.HtlcKey
	for htlc := range h.htlcs {
		keys = append(keys, htlc)
	}

	hash := newSetID(keys)

	return &hash
}

func (h *htlcSetImpl) hash() lntypes.Hash {
	return h.params.preimage.Hash()
}

func (h *htlcSetImpl) paymentAddr() [32]byte {
	return h.params.paymentAddr
}

func (h *htlcSetImpl) value() int64 {
	return h.params.value
}

func (h *htlcSetImpl) preimage() lntypes.Preimage {
	return h.params.preimage
}

func (h *htlcSetImpl) getHtlcMap() map[types.HtlcKey]int64 {
	htlcMap := make(map[types.HtlcKey]int64)
	for key, amt := range h.htlcs {
		htlcMap[key] = amt
	}

	return htlcMap
}

func (h *htlcSetImpl) accepted(key types.HtlcKey) bool {
	_, ok := h.htlcs[key]

	return ok
}

func (h *htlcSetImpl) deleteHtlc(key types.HtlcKey) {
	_, ok := h.htlcs[key]
	if !ok {
		panic("htlc not found")
	}

	delete(h.htlcs, key)

	if len(h.htlcs) == 0 {
		h.parent.delete(h.params.preimage.Hash())
	}
}

func (h *htlcSetImpl) addHtlc(key types.HtlcKey, amt int64) {
	if _, ok := h.htlcs[key]; ok {
		panic("htlc already exists")
	}

	h.htlcs[key] = amt
}
