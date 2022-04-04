package persistence

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-pg/pg/v9"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/persistence/migrations"
)

func setupTestDB(t *testing.T) (*pg.DB, *PostgresPersister) {
	conn, dsn := ResetPGTestDB(t, &TestDBSettings{
		MigrationsPath: "./migrations",
	})

	log, _ := zap.NewDevelopment()

	db, err := NewPostgresPersister(&PostgresOptions{
		DSN:    dsn,
		Logger: log.Sugar(),
	})
	require.NoError(t, err)

	return conn, db
}

const PGExpectedSchemaVersion = 1

type TestDBSettings struct {
	DSN                   string
	Schema                string
	MigrationsPath        string
	ExpectedSchemaVersion int64
}

func PGTestDSN() string {
	dsn, ok := os.LookupEnv("MULTIPLEXER_TEST_DB_DSN")
	if !ok {
		dsn = "postgres://bottle:bottle@localhost:45432/bottle_test?sslmode=disable"
	}

	return dsn
}

func ResetPGTestDB(t *testing.T, settings *TestDBSettings) (conn *pg.DB,
	dsn string) {

	if settings.DSN == "" {
		settings.DSN = PGTestDSN()
	}
	dsn = settings.DSN

	if settings.Schema == "" {
		settings.Schema = "multiplexer"
	}

	if settings.ExpectedSchemaVersion == 0 {
		settings.ExpectedSchemaVersion = PGExpectedSchemaVersion
	}

	dbSettings, err := pg.ParseURL(settings.DSN)
	require.NoError(t, err)
	require.NoError(t, err)

	db := pg.Connect(dbSettings)

	_, err = db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", settings.Schema))
	require.NoError(t, err)

	_, err = db.Exec(fmt.Sprintf("CREATE SCHEMA %s", settings.Schema))
	require.NoError(t, err)

	err = migrations.DiscoverSQLMigrations(settings.MigrationsPath)
	require.NoError(t, err)

	_, _, err = migrations.Run(db, "init")
	require.NoError(t, err)

	_, newVersion, err := migrations.Run(db, "up")
	require.NoError(t, err)

	require.Equal(t, settings.ExpectedSchemaVersion, newVersion)

	return db, dsn
}

func TestSettleInvoice(t *testing.T) {
	pg, persister := setupTestDB(t)
	defer pg.Close()

	preimage := lntypes.Preimage{1}
	hash := preimage.Hash()

	// Initially no invoices are expected.
	_, _, err := persister.Get(hash)
	require.ErrorIs(t, err, lnmux.ErrInvoiceNotFound)

	require.NoError(t, persister.Add(&InvoiceCreationData{
		CreatedAt:      time.Unix(100, 0),
		PaymentRequest: "ln...",
		InvoiceCreationData: lnmux.InvoiceCreationData{
			FinalCltvDelta:  40,
			PaymentPreimage: preimage,
			Value:           100,
			PaymentAddr:     [32]byte{2},
		},
		ID: 123,
	}))

	invoice, htlcs, err := persister.Get(hash)
	require.NoError(t, err)
	require.Empty(t, htlcs, 0)
	require.Equal(t, invoice.PaymentRequest, "ln...")
	require.False(t, invoice.Settled)

	htlcs = map[lnmux.CircuitKey]int64{
		{
			ChanID: 10,
			HtlcID: 11,
		}: 70,
		{
			ChanID: 11,
			HtlcID: 12,
		}: 30,
	}
	require.NoError(t, persister.Settle(hash, htlcs))
}
