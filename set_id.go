package lnmux

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/bottlepay/lnmux/types"
)

type SetID [32]byte

func (s SetID) String() string {
	return hex.EncodeToString(s[:])
}

// newSetID calculates a deterministic hash over a set of htlc circuit
// keys.
func newSetID(keys []types.CircuitKey) SetID {
	sortedKeys := make([]types.CircuitKey, len(keys))
	copy(sortedKeys, keys)

	sort.Slice(sortedKeys, func(a, b int) bool {
		if sortedKeys[a].ChanID == sortedKeys[b].ChanID {
			return sortedKeys[a].HtlcID < sortedKeys[b].HtlcID
		}

		return sortedKeys[a].ChanID < sortedKeys[b].ChanID
	})

	keysBytes := make([]byte, len(sortedKeys)*16)
	idx := 0
	for _, key := range sortedKeys {
		byteOrder.PutUint64(keysBytes[idx:], key.ChanID)
		byteOrder.PutUint64(keysBytes[idx+8:], key.HtlcID)
		idx += 8
	}

	hash := sha256.Sum256(keysBytes)

	return hash
}
