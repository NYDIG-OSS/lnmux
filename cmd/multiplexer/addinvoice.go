package main

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/urfave/cli/v2"
)

const finalCltvExpiry = 40

var addInvoiceCommand = &cli.Command{
	Name:   "addinvoice",
	Action: addInvoiceAction,
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name: "amt",
		},
		&cli.StringFlag{
			Name: "desc",
		},
	},
}

func addInvoiceAction(c *cli.Context) (err error) {
	// Get invoice parameters.
	amt := c.Int64("amt")
	if amt == 0 {
		return errors.New("amount not specified")
	}

	desc := c.String("desc")

	// Load config from disk.
	cfg, err := loadConfig(c.String("config"))
	if err != nil {
		return err
	}

	// Setup persistence.
	db, err := persistence.NewPostgresPersister(&persistence.PostgresOptions{
		DSN:    cfg.DSN,
		Logger: log,
	})
	if err != nil {
		return err
	}

	// Get identity key for signing the invoice.
	identityKey, err := cfg.GetIdentityKey()
	if err != nil {
		return err
	}

	keyRing := lnmux.NewKeyRing(identityKey)

	// Parse lnd connection info from the configuration.
	var gwPubKeys []common.PubKey
	for _, lnd := range cfg.Lnd.Nodes {
		pubKey, err := common.NewPubKeyFromStr(lnd.PubKey)
		if err != nil {
			return err
		}

		gwPubKeys = append(gwPubKeys, pubKey)
	}

	// Get a new creator instance.
	creator, err := lnmux.NewInvoiceCreator(
		&lnmux.InvoiceCreatorConfig{
			KeyRing:         keyRing,
			GwPubKeys:       gwPubKeys,
			ActiveNetParams: &chaincfg.RegressionNetParams,
		},
	)
	if err != nil {
		return err
	}

	// Create the invoice.
	invoice, _, err := creator.Create(amt, time.Hour, desc, finalCltvExpiry)
	if err != nil {
		return err
	}

	// Store invoice.
	dbInvoice := &persistence.InvoiceCreationData{
		InvoiceCreationData: invoice.InvoiceCreationData,
		CreatedAt:           invoice.CreationDate,
		ID:                  int64(rand.Int31()),
		PaymentRequest:      invoice.PaymentRequest,
	}
	if err := db.Add(dbInvoice); err != nil {
		return err
	}

	// Print the bolt11 payment request.
	fmt.Println(invoice.PaymentRequest)

	return nil
}
