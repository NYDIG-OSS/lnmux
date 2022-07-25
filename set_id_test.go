package lnmux

import (
	"encoding/hex"
	"testing"

	"github.com/bottlepay/lnmux/types"
	"github.com/stretchr/testify/require"
)

func TestSetID(t *testing.T) {
	keys := []types.CircuitKey{
		{ChanID: 1, HtlcID: 3},
		{ChanID: 2, HtlcID: 4},
	}

	expectedHash, _ := hex.DecodeString("567123ef075dbb9e4d08093a9a52221f3e26ebe728310961d05232796083f1f7")

	hash := newSetID(keys)
	require.Equal(t, expectedHash, hash[:])

	keys[0], keys[1] = keys[1], keys[0]
	hash = newSetID(keys)
	require.Equal(t, expectedHash, hash[:])

	keys[0].HtlcID = 5
	hash = newSetID(keys)
	require.NotEqual(t, expectedHash, hash[:])
}
