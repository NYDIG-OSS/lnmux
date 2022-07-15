package lnmux

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"time"

	"github.com/lightningnetwork/lnd/lntypes"
)

func encodeStatelessData(privKey []byte, amtMsat int64, expiry time.Time) (
	[32]byte, lntypes.Preimage, error) {

	// We'll use part of the payment address to store the stateless invoice
	// properties.
	var addr [32]byte

	// Start payment address with 16 random bytes. The payment address was
	// introduced to thwart probing attacks and 16 bytes still provide plenty of
	// protection.
	if _, err := rand.Read(addr[:16]); err != nil {
		return [32]byte{}, lntypes.Preimage{}, err
	}

	// Store the expiry time in the payment address.
	byteOrder.PutUint64(addr[16:24], uint64(expiry.Unix()))

	// Store amount in the payment address.
	byteOrder.PutUint64(addr[24:32], uint64(amtMsat))

	// Create hmac using private key.
	mac := hmac.New(sha256.New, privKey)
	mac.Write(addr[:])

	// The hmac can only be derived with knowledge of the private key. We'll use
	// it as the preimage for this invoice. When we later receive a payment, the
	// preimage can be re-derived.
	preimageBytes := mac.Sum(nil)

	paymentPreimage, err := lntypes.MakePreimage(preimageBytes)
	if err != nil {
		return [32]byte{}, lntypes.Preimage{}, err
	}

	return addr, paymentPreimage, nil
}

type statelessData struct {
	amtMsat  int64
	expiry   time.Time
	preimage lntypes.Preimage
}

func decodeStatelessData(privKey []byte, paymentAddr [32]byte) (
	*statelessData, error) {

	// Re-derive preimage from payment address.
	mac := hmac.New(sha256.New, privKey[:])

	mac.Write(paymentAddr[:])
	paymentPreimage, err := lntypes.MakePreimage(mac.Sum(nil))
	if err != nil {
		return nil, err
	}

	// Extract payment parameters.
	expiry := time.Unix(int64(byteOrder.Uint64(paymentAddr[16:24])), 0)
	amtMsat := byteOrder.Uint64(paymentAddr[24:32])

	return &statelessData{
		amtMsat:  int64(amtMsat),
		expiry:   expiry,
		preimage: paymentPreimage,
	}, nil
}
