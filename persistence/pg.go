package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/types"
	"github.com/go-pg/pg/v10"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"go.uber.org/zap"
)

type Invoice struct {
	InvoiceCreationData

	SettleRequestedAt time.Time
	Settled           bool
}

type InvoiceState int

const (
	InvoiceStateAccepted InvoiceState = iota
	InvoiceStateSettleRequested
)

type InvoiceCreationData struct {
	types.InvoiceCreationData
}

type dbInvoice struct {
	tableName struct{} `pg:"lnmux.invoices,discard_unknown_columns"` // nolint

	Hash       lntypes.Hash     `pg:"hash,use_zero,pk"`
	Preimage   lntypes.Preimage `pg:"preimage,use_zero"`
	AmountMsat int64            `pg:"amount_msat,use_zero"`

	SettleRequestedAt time.Time `pg:"settle_requested_at"`

	Settled   bool      `pg:"settled,use_zero"`
	SettledAt time.Time `pg:"settled_at"`
}

type dbHtlc struct {
	tableName struct{} `pg:"lnmux.htlcs,discard_unknown_columns"` // nolint

	Node   common.PubKey `pg:"node,use_zero,pk"`
	ChanID uint64        `pg:"chan_id,use_zero,pk"`
	HtlcID uint64        `pg:"htlc_id,use_zero,pk"`

	Hash       lntypes.Hash `pg:"hash,use_zero"`
	AmountMsat int64        `pg:"amount_msat,use_zero"`

	SettleRequestedAt time.Time `pg:"settle_requested_at"`

	Settled   bool      `pg:"settled,use_zero"`
	SettledAt time.Time `pg:"settled_at"`
}

// PostgresPersister persists items to Postgres
type PostgresPersister struct {
	conn *pg.DB

	logger *zap.SugaredLogger
}

func unmarshallDbInvoice(invoice *dbInvoice) *Invoice {
	return &Invoice{
		InvoiceCreationData: InvoiceCreationData{
			InvoiceCreationData: types.InvoiceCreationData{
				PaymentPreimage: invoice.Preimage,
				Value:           lnwire.MilliSatoshi(invoice.AmountMsat),
			},
		},
		Settled: invoice.Settled,
	}
}

func (p *PostgresPersister) Get(ctx context.Context, hash lntypes.Hash) (*Invoice,
	map[types.HtlcKey]int64, error) {

	var htlcs = make(map[types.HtlcKey]int64)

	var invoice *Invoice

	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		var dbInvoice dbInvoice
		err := tx.ModelContext(ctx, &dbInvoice).
			Where("hash=?", hash).Select()
		switch {
		case err == pg.ErrNoRows:
			return types.ErrInvoiceNotFound

		case err != nil:
			return err
		}

		var dbHtlcs []*dbHtlc
		err = tx.ModelContext(ctx, &dbHtlcs).
			Where("hash=?", hash).Select()
		if err != nil {
			return err
		}

		invoice = unmarshallDbInvoice(&dbInvoice)

		for _, htlc := range dbHtlcs {
			htlcs[types.HtlcKey{
				Node:   htlc.Node,
				ChanID: htlc.ChanID,
				HtlcID: htlc.HtlcID,
			}] = htlc.AmountMsat
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return invoice, htlcs, nil
}

func (p *PostgresPersister) RequestSettle(ctx context.Context,
	invoice *InvoiceCreationData, htlcs map[types.HtlcKey]int64) error {

	return p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		now := time.Now().UTC()

		dbInvoice := &dbInvoice{
			Hash:              invoice.PaymentPreimage.Hash(),
			Preimage:          invoice.PaymentPreimage,
			AmountMsat:        int64(invoice.Value),
			SettleRequestedAt: now,
		}

		_, err := tx.ModelContext(ctx, dbInvoice).Insert() //nolint:contextcheck
		if err != nil {
			return err
		}

		for key, amt := range htlcs {
			dbHtlc := dbHtlc{
				Node:              key.Node,
				Hash:              invoice.PaymentPreimage.Hash(),
				ChanID:            key.ChanID,
				HtlcID:            key.HtlcID,
				AmountMsat:        amt,
				SettleRequestedAt: now,
			}
			_, err := tx.Model(&dbHtlc).Insert() // nolint:contextcheck
			if err != nil {
				return fmt.Errorf("cannot insert htlc: %w", err)
			}
		}

		return nil
	})
}

func (p *PostgresPersister) MarkHtlcSettled(ctx context.Context,
	key types.HtlcKey) (bool, error) {

	var invoiceSettled bool

	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		now := time.Now().UTC()

		htlc := dbHtlc{
			Node:   key.Node,
			ChanID: key.ChanID,
			HtlcID: key.HtlcID,
		}

		result, err := tx.ModelContext(ctx, &htlc).
			WherePK().
			Set("settled=?", true).
			Set("settled_at=?", now).
			Returning("hash").
			Update() // nolint:contextcheck
		switch {
		case err == pg.ErrNoRows:
			return types.ErrHtlcNotFound

		case err != nil:
			return err
		}

		hash := htlc.Hash

		count, err := tx.ModelContext(ctx, (*dbHtlc)(nil)).
			Where("hash=?", hash).
			Where("settled=?", false).
			Count()
		if err != nil {
			return err
		}

		if count == 0 {
			_, err := tx.ModelContext(ctx, (*dbInvoice)(nil)).
				Where("hash=?", hash).
				Set("settled=?", true).
				Set("settled_at=?", now).
				Update() // nolint:contextcheck
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return types.ErrInvoiceNotFound
			}

			invoiceSettled = true
		}

		return nil
	})
	if err != nil {
		return false, err
	}

	return invoiceSettled, nil
}

// Ping pings the database connection to ensure it is available
func (p *PostgresPersister) Ping(ctx context.Context) error {
	if p.conn != nil {
		if _, err := p.conn.ExecOneContext(ctx, "SELECT 1"); err != nil {
			return err
		}
	}

	return nil
}

func (p *PostgresPersister) Close() error {
	return p.conn.Close()
}

// NewPostgresPersisterFromOptions creates a new PostgresPersister using the options provided
func NewPostgresPersisterFromOptions(options *pg.Options,
	logger *zap.SugaredLogger) *PostgresPersister {

	conn := pg.Connect(options)

	persister := &PostgresPersister{
		logger: logger,
		conn:   conn,
	}

	return persister
}

// NewPostgresPersisterFromDSN creates a new PostgresPersister using the dsn provided
func NewPostgresPersisterFromDSN(dsn string, logger *zap.SugaredLogger) (
	*PostgresPersister, error) {

	options, err := pg.ParseURL(dsn)
	if err != nil {
		return nil, err
	}

	return NewPostgresPersisterFromOptions(options, logger), nil
}
