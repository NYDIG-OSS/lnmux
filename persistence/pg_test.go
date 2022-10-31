package persistence

import (
	"context"
	"testing"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/persistence/test"
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestDB(t *testing.T) (*PostgresPersister, func()) {
	opts := test.CreatePGTestDB(t, &test.TestDBSettings{
		MigrationsPath: "./migrations",
	})

	log := zap.NewNop().Sugar()
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

	invoiceSettled, err := persister.MarkHtlcSettled(context.Background(), types.HtlcKey{
		Node:   nodeKey,
		ChanID: 10,
		HtlcID: 11,
	})
	require.NoError(t, err)
	require.False(t, invoiceSettled)

	invoiceSettled, err = persister.MarkHtlcSettled(context.Background(), types.HtlcKey{
		Node:   nodeKey,
		ChanID: 10,
		HtlcID: 11,
	})
	require.NoError(t, err)
	require.False(t, invoiceSettled)

	invoice, _, err := persister.Get(context.Background(), hash)
	require.NoError(t, err)
	require.False(t, invoice.Settled)

	invoiceSettled, err = persister.MarkHtlcSettled(context.Background(), types.HtlcKey{
		Node:   nodeKey,
		ChanID: 11,
		HtlcID: 12,
	})
	require.NoError(t, err)
	require.True(t, invoiceSettled)

	invoice, htlcs, err = persister.Get(context.Background(), hash)
	require.NoError(t, err)
	require.Len(t, htlcs, 2)
	require.True(t, invoice.Settled)
}
