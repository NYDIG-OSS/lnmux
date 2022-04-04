package lnmux

// FailResolutionResult provides metadata about a htlc that was failed by
// the registry. It can be used to take custom actions on resolution of the
// htlc.
type FailResolutionResult uint8

const (
	resultInvalidFailure FailResolutionResult = iota

	// ResultExpiryTooSoon is returned when we do not accept an invoice
	// payment because it expires too soon.
	ResultExpiryTooSoon

	// ResultInvoiceNotOpen is returned when a mpp invoice is not open.
	ResultInvoiceNotOpen

	// ResultMppTimeout is returned when an invoice paid with multiple
	// partial payments times out before it is fully paid.
	ResultMppTimeout

	// ResultAddressMismatch is returned when the payment address for a mpp
	// invoice does not match.
	ResultAddressMismatch

	// ResultHtlcSetTotalMismatch is returned when the amount paid by a
	// htlc does not match its set total.
	ResultHtlcSetTotalMismatch

	// ResultHtlcSetTotalTooLow is returned when a mpp set total is too low
	// for an invoice.
	ResultHtlcSetTotalTooLow

	// ResultHtlcSetOverpayment is returned when a mpp set is overpaid.
	ResultHtlcSetOverpayment

	// ResultInvoiceNotFound is returned when an attempt is made to pay an
	// invoice that is unknown to us.
	ResultInvoiceNotFound

	// ResultHtlcInvoiceTypeMismatch is returned when an AMP HTLC targets a
	// non-AMP invoice and vice versa.
	ResultHtlcInvoiceTypeMismatch

	// ResultCannotSettle is returned when the invoice cannot be settled in
	// the database.
	ResultCannotSettle
)

// String returns a string representation of the result.
func (f FailResolutionResult) String() string {
	return f.FailureString()
}

// FailureString returns a string representation of the result.
//
// Note: it is part of the FailureDetail interface.
func (f FailResolutionResult) FailureString() string {
	switch f {
	case resultInvalidFailure:
		return "invalid failure result"

	case ResultExpiryTooSoon:
		return "expiry too soon"

	case ResultInvoiceNotOpen:
		return "invoice no longer open"

	case ResultMppTimeout:
		return "mpp timeout"

	case ResultAddressMismatch:
		return "payment address mismatch"

	case ResultHtlcSetTotalMismatch:
		return "htlc total amt doesn't match set total"

	case ResultHtlcSetTotalTooLow:
		return "set total too low for invoice"

	case ResultHtlcSetOverpayment:
		return "mpp is overpaying set total"

	case ResultInvoiceNotFound:
		return "invoice not found"

	case ResultHtlcInvoiceTypeMismatch:
		return "htlc invoice type mismatch"

	case ResultCannotSettle:
		return "cannot settle"

	default:
		return "unknown failure resolution result"
	}
}

// SettleResolutionResult provides metadata which about a htlc that was failed
// by the registry. It can be used to take custom actions on resolution of the
// htlc.
type SettleResolutionResult uint8

const (
	resultInvalidSettle SettleResolutionResult = iota

	// ResultSettled is returned when we settle an invoice.
	ResultSettled

	// ResultReplayToSettled is returned when we replay a settled invoice.
	ResultReplayToSettled
)

// String returns a string representation of the result.
func (s SettleResolutionResult) String() string {
	switch s {
	case resultInvalidSettle:
		return "invalid settle result"

	case ResultSettled:
		return "settled"

	case ResultReplayToSettled:
		return "replayed htlc to settled invoice"

	default:
		return "unknown settle resolution result"
	}
}
