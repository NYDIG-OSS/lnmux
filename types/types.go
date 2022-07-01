package types

import (
	"fmt"

	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
)

var (
	// ErrInvoiceNotFound is returned when a targeted invoice can't be
	// found.
	ErrInvoiceNotFound = fmt.Errorf("unable to locate invoice")
)

type InvoiceCreationData struct {
	// PaymentPreimage is the preimage which is to be revealed in the
	// occasion that an HTLC paying to the hash of this preimage is
	// extended. Set to nil if the preimage isn't known yet.
	PaymentPreimage lntypes.Preimage

	// Value is the expected amount of milli-satoshis to be paid to an HTLC
	// which can be satisfied by the above preimage.
	Value lnwire.MilliSatoshi

	// PaymentAddr is a randomly generated value include in the MPP record
	// by the sender to prevent probing of the receiver.
	PaymentAddr [32]byte
}

// CircuitKey is used by a channel to uniquely identify the HTLCs it receives
// from the switch, and is used to purge our in-memory state of HTLCs that have
// already been processed by a link. Two list of CircuitKeys are included in
// each CommitDiff to allow a link to determine which in-memory htlcs directed
// the opening and closing of circuits in the switch's circuit map.
type CircuitKey struct {
	// ChanID is the short chanid indicating the HTLC's origin.
	//
	// NOTE: It is fine for this value to be blank, as this indicates a
	// locally-sourced payment.
	ChanID uint64

	// HtlcID is the unique htlc index predominately assigned by links,
	// though can also be assigned by switch in the case of locally-sourced
	// payments.
	HtlcID uint64
}

// String returns a string representation of the CircuitKey.
func (k CircuitKey) String() string {
	return fmt.Sprintf("%d:%d", k.ChanID, k.HtlcID)
}
