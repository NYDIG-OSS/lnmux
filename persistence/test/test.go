package test

import (
	"fmt"
	"os"
	"testing"

	"github.com/bottlepay/lnmux/persistence/migrations"
	"github.com/go-pg/pg/v10"
	"github.com/stretchr/testify/require"
)

const PGExpectedSchemaVersion = 1

type TestDBSettings struct {
	DSN                   string
	Schema                string
	MigrationsPath        string
	ExpectedSchemaVersion int64
}

func PGTestDSN() string {
	dsn, ok := os.LookupEnv("LNMUX_TEST_DB_DSN")
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
		settings.Schema = "lnmux"
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
