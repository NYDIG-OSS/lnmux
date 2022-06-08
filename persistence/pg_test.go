package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bottlepay/lnmux/persistence/test"
	"github.com/bottlepay/lnmux/types"
)

func setupTestDB(t *testing.T) (*pg.DB, *PostgresPersister) {
	conn, dsn := test.ResetPGTestDB(t, &test.TestDBSettings{
		MigrationsPath: "./migrations",
	})

	log := zap.NewNop().Sugar()
	db, err := NewPostgresPersisterFromDSN(dsn, log)
	require.NoError(t, err)

	return conn, db
}

func TestSettleInvoice(t *testing.T) {
	pg, persister := setupTestDB(t)
	defer pg.Close()

	preimage := lntypes.Preimage{1}
	hash := preimage.Hash()

	// Initially no invoices are expected.
	_, _, err := persister.Get(context.Background(), hash)
	require.ErrorIs(t, err, types.ErrInvoiceNotFound)

	require.NoError(t, persister.Add(context.Background(), &InvoiceCreationData{
		CreatedAt:      time.Unix(100, 0),
		PaymentRequest: "ln...",
		InvoiceCreationData: types.InvoiceCreationData{
			FinalCltvDelta:  40,
			PaymentPreimage: preimage,
			Value:           100,
			PaymentAddr:     [32]byte{2},
		},
		ID:        123,
		ExpiresAt: time.Now().Add(time.Hour),
	}))

	invoice, htlcs, err := persister.Get(context.Background(), hash)
	require.NoError(t, err)
	require.Empty(t, htlcs, 0)
	require.Equal(t, invoice.PaymentRequest, "ln...")
	require.Equal(t, InvoiceStateOpen, invoice.State)

	htlcs = map[types.CircuitKey]int64{
		{
			ChanID: 10,
			HtlcID: 11,
		}: 70,
		{
			ChanID: 11,
			HtlcID: 12,
		}: 30,
	}
	require.NoError(t, persister.RequestSettle(context.Background(), hash, htlcs))
	require.NoError(t, persister.Settle(context.Background(), hash))
}
