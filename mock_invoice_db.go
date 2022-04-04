package lnmux

import (
	"time"

	"github.com/lightningnetwork/lnd/lntypes"
)

type MockInvoiceDb struct {
	Invoice   *InvoiceCreationData
	Htlcs     map[CircuitKey]int64
	Hash      lntypes.Hash
	SettleErr error
}

func (m *MockInvoiceDb) Get(hash lntypes.Hash) (*InvoiceCreationData, map[CircuitKey]int64,
	error) {

	return m.Invoice, m.Htlcs, nil
}

func (m *MockInvoiceDb) Settle(hash lntypes.Hash,
	htlcs map[CircuitKey]int64) error {

	time.Sleep(1 * time.Second)

	if m.SettleErr != nil {
		return m.SettleErr
	}

	m.Htlcs = htlcs

	return nil
}
