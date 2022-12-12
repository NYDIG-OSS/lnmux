package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
)

var nonStrictConfigFlag = &cli.BoolFlag{
	Name:  "non-strict-config",
	Usage: "If set to true, unknown config fields are silently ignored",
}

func main() {
	app := &cli.App{
		Name: "lnmuxd",
		Commands: []*cli.Command{
			runCommand,
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
			},
			nonStrictConfigFlag,
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
	}
}
