package migrations

import (
	"github.com/go-pg/migrations/v8"
)

var Collection = migrations.NewCollection()
var sqlDiscovered = false

func init() {
	Collection.DisableSQLAutodiscover(true)
	Collection.SetTableName("lnmux.migrations")

}

// DiscoverSQLMigrations scans dir for migrations.
func DiscoverSQLMigrations(dir string) error {
	if dir == "" {
		dir = "persistence/migrations"
	}
	err := Collection.DiscoverSQLMigrations(dir)
	if err == nil {
		sqlDiscovered = true
	}

	return err
}

// Run runs command on the db. Supported commands are:
// - up [target] - runs all available migrations by default or up to target one if argument is provided.
// - down - reverts last migration.
// - reset - reverts all migrations.
// - version - prints current db version.
// - set_version - sets db version without running migrations.
func Run(db migrations.DB, a ...string) (int64, int64, error) {
	if !sqlDiscovered {
		err := DiscoverSQLMigrations("")
		if err != nil {
			return 0, 0, err
		}
	}

	return Collection.Run(db, a...)
}
