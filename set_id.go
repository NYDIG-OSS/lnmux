package lnmux

import (
	"bytes"
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
func newSetID(keys []types.HtlcKey) SetID {
	sortedKeys := make([]types.HtlcKey, len(keys))
	copy(sortedKeys, keys)

	sort.Slice(sortedKeys, func(a, b int) bool {
		if sortedKeys[a].Node != sortedKeys[b].Node {
			return bytes.Compare(sortedKeys[a].Node[:], sortedKeys[b].Node[:]) == -1
		}

		if sortedKeys[a].ChanID != sortedKeys[b].ChanID {
			return sortedKeys[a].ChanID < sortedKeys[b].ChanID
		}

		return sortedKeys[a].HtlcID < sortedKeys[b].HtlcID
	})

	digest := sha256.New()
	scratch := make([]byte, 8)

	for _, key := range sortedKeys {
		// Panic on error because this cannot happen in the current
		// implementation of digest.
		if _, err := digest.Write(key.Node[:]); err != nil {
			panic(err)
		}

		byteOrder.PutUint64(scratch, key.ChanID)
		if _, err := digest.Write(scratch); err != nil {
			panic(err)
		}

		byteOrder.PutUint64(scratch, key.HtlcID)
		if _, err := digest.Write(scratch); err != nil {
			panic(err)
		}
	}

	var setID SetID
	copy(setID[:], digest.Sum(nil))

	return setID
}
