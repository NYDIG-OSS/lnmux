package persistence

import (
	"context"
	"testing"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/persistence/test"
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func setupTestDB(t *testing.T) (*PostgresPersister, func()) {
	opts := test.CreatePGTestDB(t, &test.TestDBSettings{
		MigrationsPath: "./migrations",
	})

	log := zaptest.NewLogger(t).Sugar()
	db := NewPostgresPersisterFromOptions(opts, log)

	drop := func() {
		db.Close()
		test.DropTestDB(t, *opts)
	}

	return db, drop
}

func TestSettleInvoice(t *testing.T) {
	t.Parallel()

	persister, dropDB := setupTestDB(t)
	defer dropDB()

	preimage := lntypes.Preimage{1}
	hash := preimage.Hash()

	// Initially no invoices are expected.
	_, _, err := persister.Get(context.Background(), hash)
	require.ErrorIs(t, err, types.ErrInvoiceNotFound)

	nodeKey := common.PubKey{1}
	htlcs := map[types.HtlcKey]int64{
		{
			Node:   nodeKey,
			ChanID: 10,
			HtlcID: 11,
		}: 70,
		{
			Node:   nodeKey,
			ChanID: 11,
			HtlcID: 12,
		}: 30,
	}
	require.NoError(t, persister.RequestSettle(context.Background(), &InvoiceCreationData{
		InvoiceCreationData: types.InvoiceCreationData{
			PaymentPreimage: preimage,
			Value:           100,
			PaymentAddr:     [32]byte{2},
		},
	}, htlcs))

	_, err = persister.MarkHtlcSettled(context.Background(), types.HtlcKey{
		Node:   nodeKey,
		ChanID: 99,
		HtlcID: 99,
	})
	require.ErrorIs(t, err, types.ErrHtlcNotFound)

	settledHash, err := persister.MarkHtlcSettled(context.Background(), types.HtlcKey{
		Node:   nodeKey,
		ChanID: 10,
		HtlcID: 11,
	})
	require.NoError(t, err)
	require.Nil(t, settledHash)

	_, err = persister.MarkHtlcSettled(context.Background(), types.HtlcKey{
		Node:   nodeKey,
		ChanID: 10,
		HtlcID: 11,
	})
	require.Error(t, err, ErrHtlcAlreadySettled)

	invoice, _, err := persister.Get(context.Background(), hash)
	require.NoError(t, err)
	require.False(t, invoice.Settled)

	settledHash, err = persister.MarkHtlcSettled(context.Background(), types.HtlcKey{
		Node:   nodeKey,
		ChanID: 11,
		HtlcID: 12,
	})
	require.NoError(t, err)
	require.Equal(t, hash, *settledHash)

	invoice, htlcs, err = persister.Get(context.Background(), hash)
	require.NoError(t, err)
	require.Len(t, htlcs, 2)
	require.True(t, invoice.Settled)
}

func TestConcurrentHtlcSettle(t *testing.T) {
	t.Parallel()

	persister, dropDB := setupTestDB(t)
	defer dropDB()

	preimage := lntypes.Preimage{1}

	nodeKey := common.PubKey{1}
	htlcs := map[types.HtlcKey]int64{
		{
			Node:   nodeKey,
			ChanID: 10,
			HtlcID: 11,
		}: 70,
		{
			Node:   nodeKey,
			ChanID: 11,
			HtlcID: 12,
		}: 30,
	}
	require.NoError(t, persister.RequestSettle(context.Background(), &InvoiceCreationData{
		InvoiceCreationData: types.InvoiceCreationData{
			PaymentPreimage: preimage,
			Value:           100,
			PaymentAddr:     [32]byte{2},
		},
	}, htlcs))

	settledChan := make(chan bool)

	// Mark both htlcs as settled concurrently.
	for htlc := range htlcs {
		htlc := htlc

		go func() {
			invoiceSettled, err := persister.MarkHtlcSettled(context.Background(), htlc)
			require.NoError(t, err)

			settledChan <- invoiceSettled != nil
		}()
	}

	// We expect at least one of those operations to signal that the invoice is
	// now settled.
	settled1 := <-settledChan
	settled2 := <-settledChan

	require.True(t, settled1 || settled2)
}

func TestGetSettledInvoices(t *testing.T) {
	t.Parallel()

	persister, dropDB := setupTestDB(t)
	defer dropDB()

	type invoiceInfo struct {
		types.InvoiceCreationData
		htlcs map[types.HtlcKey]int64
	}

	nodeKey := common.PubKey{1}

	invoices := []*invoiceInfo{
		{
			InvoiceCreationData: types.InvoiceCreationData{
				PaymentPreimage: lntypes.Preimage{1},
				Value:           100,
				PaymentAddr:     [32]byte{2},
			},

			htlcs: map[types.HtlcKey]int64{{Node: nodeKey, ChanID: 10, HtlcID: 11}: 70},
		},
		{
			InvoiceCreationData: types.InvoiceCreationData{
				PaymentPreimage: lntypes.Preimage{2},
				Value:           100,
				PaymentAddr:     [32]byte{2},
			},
			htlcs: map[types.HtlcKey]int64{{Node: nodeKey, ChanID: 10, HtlcID: 12}: 70},
		},
		{
			InvoiceCreationData: types.InvoiceCreationData{
				PaymentPreimage: lntypes.Preimage{3},
				Value:           100,
				PaymentAddr:     [32]byte{2},
			},
			htlcs: map[types.HtlcKey]int64{{Node: nodeKey, ChanID: 10, HtlcID: 13}: 70},
		},
	}

	for _, invoice := range invoices {
		require.NoError(t, persister.RequestSettle(context.Background(),
			&InvoiceCreationData{invoice.InvoiceCreationData},
			invoice.htlcs,
		))
	}

	// Settled the two first invoices but not the last
	for i := 0; i < 2; i++ {
		var invoiceSettled *lntypes.Hash
		for htlc := range invoices[i].htlcs {
			var err error
			invoiceSettled, err = persister.MarkHtlcSettled(context.Background(), htlc)
			require.NoError(t, err)
		}
		require.NotNil(t, invoiceSettled)
	}

	// Ask only for the first settled or to be settled invoice
	dbInvoices, err := persister.GetInvoices(context.Background(), 1, 0)
	require.NoError(t, err)
	require.Len(t, dbInvoices, 1)
	require.Equal(t, dbInvoices[0].SequenceNum, uint64(1))
	require.Equal(t, dbInvoices[0].PaymentPreimage, invoices[0].PaymentPreimage)
	require.True(t, dbInvoices[0].Settled)

	// Ask for invoices with a sequence number greater than 2 (max 1 invoice): only the second invoice should be returned
	dbInvoices, err = persister.GetInvoices(context.Background(), 1, 2)
	require.NoError(t, err)
	require.Len(t, dbInvoices, 1)
	require.Equal(t, dbInvoices[0].SequenceNum, uint64(2))
	require.Equal(t, dbInvoices[0].PaymentPreimage, invoices[1].PaymentPreimage)
	require.True(t, dbInvoices[0].Settled)

	// Ask for all settled or to be settled invoices
	// Now we should have 3 invoices (2 settled and one not settled yet)
	dbInvoices, err = persister.GetInvoices(context.Background(), 3, 0)
	require.NoError(t, err)
	require.Len(t, dbInvoices, 3)
	require.Equal(t, dbInvoices[0].SequenceNum, uint64(1))
	require.Equal(t, dbInvoices[0].PaymentPreimage, invoices[0].PaymentPreimage)
	require.True(t, dbInvoices[0].Settled)
	require.Equal(t, dbInvoices[1].SequenceNum, uint64(2))
	require.Equal(t, dbInvoices[1].PaymentPreimage, invoices[1].PaymentPreimage)
	require.True(t, dbInvoices[1].Settled)
	require.Equal(t, dbInvoices[2].SequenceNum, uint64(3))
	require.Equal(t, dbInvoices[2].PaymentPreimage, invoices[2].PaymentPreimage)
	require.False(t, dbInvoices[2].Settled)
}
