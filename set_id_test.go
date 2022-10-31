package lnmux

import (
	"encoding/hex"
	"testing"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/types"
	"github.com/stretchr/testify/require"
)

func TestSetID(t *testing.T) {
	testKeys := func() []types.HtlcKey {
		return []types.HtlcKey{
			{ChanID: 1, HtlcID: 3},
			{ChanID: 2, HtlcID: 4},
		}
	}

	keys := testKeys()
	expectedHash, _ := hex.DecodeString("ae4aae558656bd09cba89b3857513eff4de4b0f10daaffd40f1807edc965b17f")
	hash := newSetID(keys)
	require.Equal(t, expectedHash, hash[:])

	keys = testKeys()
	keys[0], keys[1] = keys[1], keys[0]
	hash = newSetID(keys)
	require.Equal(t, expectedHash, hash[:])

	keys = testKeys()
	keys[0].Node = common.PubKey{1}
	hash = newSetID(keys)
	require.NotEqual(t, expectedHash, hash[:])

	keys = testKeys()
	keys[0].ChanID = 2
	hash = newSetID(keys)
	require.NotEqual(t, expectedHash, hash[:])

	keys = testKeys()
	keys[0].HtlcID = 5
	hash = newSetID(keys)
	require.NotEqual(t, expectedHash, hash[:])
}

// TestSetIDSelfPayments tests that two payments in different directions between
// our own nodes result in a different set id. Htlc ids are counted per
// direction separately and do not unique identify an htlc on the channel in any
// direction.
func TestSetIDSelfPayments(t *testing.T) {
	payment1 := []types.HtlcKey{
		{Node: common.PubKey{1}, ChanID: 1, HtlcID: 3},
	}
	hash1 := newSetID(payment1)

	payment2 := []types.HtlcKey{
		{Node: common.PubKey{2}, ChanID: 1, HtlcID: 3},
	}
	hash2 := newSetID(payment2)

	require.NotEqual(t, hash1, hash2)
}
