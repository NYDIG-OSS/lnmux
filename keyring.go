package lnmux

import (
	"crypto/sha256"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightningnetwork/lnd/keychain"
)

// KeyRing is an implementation of both the KeyRing and SecretKeyRing
// interfaces backed by btcwallet's internal root waddrmgr. Internally, we'll
// be using a ScopedKeyManager to do all of our derivations, using the key
// scope and scope addr scehma defined above. Re-using the existing key scope
// construction means that all key derivation will be protected under the root
// seed of the wallet, making each derived key fully deterministic.
type KeyRing struct {
	keychain.SecretKeyRing

	pubKey  *btcec.PublicKey
	privKey *btcec.PrivateKey
}

// NewKeyRing creates a new implementation of the
// keychain.SecretKeyRing interface backed by btcwallet.
//
// NOTE: The passed waddrmgr.Manager MUST be unlocked in order for the keychain
// to function.
func NewKeyRing(key [32]byte) *KeyRing {
	privKey, pubKey := btcec.PrivKeyFromBytes(key[:])

	return &KeyRing{
		pubKey:  pubKey,
		privKey: privKey,
	}
}

func (b *KeyRing) DeriveNextKey(keyFam keychain.KeyFamily) (
	keychain.KeyDescriptor, error) {

	panic("not implemented")
}

// DeriveKey attempts to derive an arbitrary key specified by the passed
// KeyLocator. This may be used in several recovery scenarios, or when manually
// rotating something like our current default node key.
//
// NOTE: This is part of the keychain.KeyRing interface.
func (b *KeyRing) DeriveKey(keyLoc keychain.KeyLocator) (keychain.KeyDescriptor, error) {
	var keyDesc keychain.KeyDescriptor

	keyDesc.KeyLocator = keyLoc
	keyDesc.PubKey = b.pubKey

	return keyDesc, nil
}

// DerivePrivKey attempts to derive the private key that corresponds to the
// passed key descriptor.
//
// NOTE: This is part of the keychain.SecretKeyRing interface.
func (b *KeyRing) DerivePrivKey(keyDesc keychain.KeyDescriptor) (
	*btcec.PrivateKey, error) {

	return b.privKey, nil
}

// ECDH performs a scalar multiplication (ECDH-like operation) between the
// target key descriptor and remote public key. The output returned will be
// the sha256 of the resulting shared point serialized in compressed format. If
// k is our private key, and P is the public key, we perform the following
// operation:
//
// sx := k*P s := sha256(sx.SerializeCompressed())
//
// NOTE: This is part of the keychain.ECDHRing interface.
func (b *KeyRing) ECDH(keyDesc keychain.KeyDescriptor,
	pub *btcec.PublicKey) ([32]byte, error) {

	var (
		pubJacobian btcec.JacobianPoint
		s           btcec.JacobianPoint
	)
	pub.AsJacobian(&pubJacobian)

	btcec.ScalarMultNonConst(&b.privKey.Key, &pubJacobian, &s)
	s.ToAffine()
	sPubKey := btcec.NewPublicKey(&s.X, &s.Y)
	h := sha256.Sum256(sPubKey.SerializeCompressed())

	return h, nil
}

// SignMessage signs the given message, single or double SHA256 hashing it
// first, with the private key described in the key locator.
//
// NOTE: This is part of the keychain.MessageSignerRing interface.
func (b *KeyRing) SignMessage(keyLoc keychain.KeyLocator,
	msg []byte, doubleHash bool) (*ecdsa.Signature, error) {

	privKey, err := b.DerivePrivKey(keychain.KeyDescriptor{
		KeyLocator: keyLoc,
	})
	if err != nil {
		return nil, err
	}

	var digest []byte
	if doubleHash {
		digest = chainhash.DoubleHashB(msg)
	} else {
		digest = chainhash.HashB(msg)
	}

	return ecdsa.Sign(privKey, digest), nil
}

// SignMessageCompact signs the given message, single or double SHA256 hashing
// it first, with the private key described in the key locator and returns
// the signature in the compact, public key recoverable format.
//
// NOTE: This is part of the keychain.MessageSignerRing interface.
func (b *KeyRing) SignMessageCompact(keyLoc keychain.KeyLocator,
	msg []byte, doubleHash bool) ([]byte, error) {

	privKey, err := b.DerivePrivKey(keychain.KeyDescriptor{
		KeyLocator: keyLoc,
	})
	if err != nil {
		return nil, err
	}

	var digest []byte
	if doubleHash {
		digest = chainhash.DoubleHashB(msg)
	} else {
		digest = chainhash.HashB(msg)
	}

	return ecdsa.SignCompact(privKey, digest, true)
}
