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

	expectedHash, _ := hex.DecodeString("cc70fcada0615d5f945db224c5d261d895f381f27298e9e1f96568c32825e3bd")

	hash := newSetID(keys)
	require.Equal(t, expectedHash, hash[:])

	keys[0], keys[1] = keys[1], keys[0]
	hash = newSetID(keys)
	require.Equal(t, expectedHash, hash[:])

	keys[0].HtlcID = 5
	hash = newSetID(keys)
	require.NotEqual(t, expectedHash, hash[:])
}
