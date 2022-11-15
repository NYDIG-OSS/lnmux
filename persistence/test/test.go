package test

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bottlepay/lnmux/persistence/migrations"
	"github.com/go-pg/pg/v10"
	"github.com/stretchr/testify/require"
)

const PGExpectedSchemaVersion = 3

var dbSuffix uint32

type TestDBSettings struct {
	MigrationsPath        string
	ExpectedSchemaVersion int64
}

func PGTestDSN() string {
	dsn, ok := os.LookupEnv("LNMUX_TEST_DB_DSN")
	if !ok {
		dsn = "postgres://bottle:bottle@localhost:45432/postgres?sslmode=disable"
	}

	return dsn
}

func CreatePGTestDB(t *testing.T, settings *TestDBSettings) *pg.Options {
	// To create a DB, we need a connection to the server that isn't to
	// the nonexistent DB itself. We connect to the postgres DB, which
	// almost always exists.
	dsn := PGTestDSN()

	if settings.ExpectedSchemaVersion == 0 {
		settings.ExpectedSchemaVersion = PGExpectedSchemaVersion
	}

	dbSettings, err := pg.ParseURL(dsn)
	require.NoError(t, err)

	defaultDb := pg.Connect(dbSettings)
	dbSettings = defaultDb.Options()

	// Get the filename of the caller of this function. This lets us
	// distinguish between different packages calling it, which prevents
	// tests executing as multiple binaries from running over each other.
	_, callerFileName, _, ok := runtime.Caller(1)
	require.True(t, ok)

	// Format the filename in a way we can insert it into the DB name.
	// First, we split based on path separator.
	callerPathParts := strings.Split(callerFileName,
		string(os.PathSeparator))

	// Then, we take the last two directories in the caller's filename.
	// This way, it's not too long for a DB name, but still leaves some
	// context about the package name.
	numParts := len(callerPathParts)
	callerFileName = strings.Join(callerPathParts[numParts-3:numParts-1],
		"_")

	// Combine caller package info with sequence to ensure unique DB per
	// test inside the calling package.
	dbName := fmt.Sprintf("bottle_test_%s_%d",
		callerFileName, atomic.AddUint32(&dbSuffix, 1))

	_, err = defaultDb.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s",
		dbName))
	require.NoError(t, err)

	_, err = defaultDb.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)

	dbSettings.Database = dbName

	defaultDb.Close()

	db := pg.Connect(dbSettings)

	err = migrations.DiscoverSQLMigrations(settings.MigrationsPath)
	require.NoError(t, err)

	_, _, err = migrations.Run(db, "init")
	require.NoError(t, err)

	_, newVersion, err := migrations.Run(db, "up")
	require.NoError(t, err)

	require.Equal(t, settings.ExpectedSchemaVersion, newVersion)

	db.Close()

	return dbSettings
}

func DropTestDB(t *testing.T, opts pg.Options) {
	dbName := opts.Database

	// To drop the DB, we need a connection to the server that isn't
	// accessing the DB itself. We connect to the postgres DB, which
	// almost always exists.
	opts.Database = "postgres"

	defaultDb := pg.Connect(&opts)

	_, err := defaultDb.Exec(fmt.Sprintf("DROP DATABASE %s", dbName))
	require.NoError(t, err)

	defaultDb.Close()
}
