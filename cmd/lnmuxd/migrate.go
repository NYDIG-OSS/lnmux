package main

import (
	"fmt"

	"github.com/bottlepay/lnmux/persistence/migrations"
	"github.com/go-pg/pg/v10"
	"github.com/urfave/cli/v2"
)

var (
	migrateCommand = &cli.Command{
		Name:   "migrate",
		Action: migrateAction,
		ArgsUsage: `runs command on the db. Supported commands are:
	- init - creates version info table in the database
	- up - runs all available migrations.
	- up [target] - runs available migrations up to the target one.
	- down - reverts last migration.
	- reset - reverts all migrations.
	- version - prints current db version.
	- set_version [version] - sets db version without running migrations.`,
	}
)

func migrateAction(c *cli.Context) error {
	if c.Args().Len() == 0 {
		return cli.ShowCommandHelp(c, "migrate")
	}

	cfg, err := loadConfig(c.String("config"))
	if err != nil {
		return err
	}

	dsn := cfg.DB.DSN

	dbSettings, err := pg.ParseURL(dsn)
	if err != nil {
		return err
	}

	db := pg.Connect(dbSettings)

	if err := migrations.DiscoverSQLMigrations(""); err != nil {
		return err
	}

	oldVersion, newVersion, err := migrations.Run(db, c.Args().Slice()...)
	if err != nil {
		return err
	}

	if newVersion != oldVersion {
		fmt.Printf("migrated from version %d to %d\n", oldVersion, newVersion)
	} else {
		fmt.Printf("version is %d\n", oldVersion)
	}

	return nil
}
