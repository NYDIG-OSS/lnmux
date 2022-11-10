package migrations

import (
	"sync"

	"github.com/go-pg/migrations/v8"
)

var (
	sqlDiscoveryOnce sync.Once

	Collection = migrations.NewCollection()
)

func init() {
	Collection.DisableSQLAutodiscover(true)
	Collection.SetTableName("lnmux.migrations")
}

// DiscoverSQLMigrations scans dir for migrations.
func DiscoverSQLMigrations(dir string) error {
	var err error

	sqlDiscoveryOnce.Do(func() {
		if dir == "" {
			dir = "persistence/migrations"
		}

		err = Collection.DiscoverSQLMigrations(dir)
	})

	return err
}

// Run runs command on the db. Supported commands are:
// - up [target] - runs all available migrations by default or up to target one if argument is provided.
// - down - reverts last migration.
// - reset - reverts all migrations.
// - version - prints current db version.
// - set_version - sets db version without running migrations.
func Run(db migrations.DB, a ...string) (int64, int64, error) {
	err := DiscoverSQLMigrations("")
	if err != nil {
		return 0, 0, err
	}

	return Collection.Run(db, a...)
}
