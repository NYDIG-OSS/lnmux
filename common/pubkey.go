package common

import (
	"encoding/hex"
	"fmt"
)

const PubKeySize = 33

type PubKey [PubKeySize]byte

// NewPubKeyFromBytes returns a new PubKey based on a serialized pubkey in a
// byte slice.
func NewPubKeyFromBytes(b []byte) (PubKey, error) {
	PubKeyLen := len(b)
	if PubKeyLen != PubKeySize {
		return PubKey{}, fmt.Errorf("invalid PubKey length of %v, "+
			"want %v", PubKeyLen, PubKeySize)
	}

	var v PubKey
	copy(v[:], b)
	return v, nil
}

// NewPubKeyFromStr returns a new PubKey given its hex-encoded string format.
func NewPubKeyFromStr(v string) (PubKey, error) {
	// Return error if hex string is of incorrect length.
	if len(v) != PubKeySize*2 {
		return PubKey{}, fmt.Errorf("invalid PubKey string length of "+
			"%v, want %v", len(v), PubKeySize*2)
	}

	pubKey, err := hex.DecodeString(v)
	if err != nil {
		return PubKey{}, err
	}

	return NewPubKeyFromBytes(pubKey)
}

// String returns a human readable version of the PubKey which is the
// hex-encoding of the serialized compressed public key.
func (v PubKey) String() string {
	return fmt.Sprintf("%x", v[:])
}
