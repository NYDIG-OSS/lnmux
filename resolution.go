package lnmux

import (
	"github.com/lightningnetwork/lnd/lntypes"
)

// HtlcResolution describes how an htlc should be resolved.
type HtlcResolution interface {
}

// HtlcFailResolution is an implementation of the HtlcResolution interface
// which is returned when a htlc is failed.
type HtlcFailResolution struct {
	// Outcome indicates the outcome of the invoice registry update.
	Outcome FailResolutionResult
}

// NewFailResolution returns a htlc failure resolution.
func NewFailResolution(outcome FailResolutionResult) *HtlcFailResolution {
	return &HtlcFailResolution{
		Outcome: outcome,
	}
}

// HtlcSettleResolution is an implementation of the HtlcResolution interface
// which is returned when a htlc is settled.
type HtlcSettleResolution struct {
	// Preimage is the htlc preimage. Its value is nil in case of a cancel.
	Preimage lntypes.Preimage

	// Outcome indicates the outcome of the invoice registry update.
	Outcome SettleResolutionResult
}

// NewSettleResolution returns a htlc resolution which is associated with a
// settle.
func NewSettleResolution(preimage lntypes.Preimage,
	outcome SettleResolutionResult) *HtlcSettleResolution {

	return &HtlcSettleResolution{
		Preimage: preimage,
		Outcome:  outcome,
	}
}
