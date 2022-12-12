package lnmux

import (
	"encoding/binary"

	"github.com/bottlepay/lnmux/common"
)

var byteOrder = binary.BigEndian

// virtualChannelFromNode deterministically derives a short channel id for
// multiplexer for a node pubkey.
func virtualChannelFromNode(key common.PubKey) uint64 {
	var b [8]byte

	// Take the final bytes of the key.
	copy(b[:], key[common.PubKeySize-8:])

	// Force the high bit to make sure there are no collisions with real
	// channels.
	b[0] |= 1 << 7

	return byteOrder.Uint64(b[:])
}
