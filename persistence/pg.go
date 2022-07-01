package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux/types"
	"github.com/go-pg/pg/v10"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"go.uber.org/zap"
)

type Invoice struct {
	InvoiceCreationData

	Settled           bool
	SettledAt         time.Time
	SettleRequestedAt time.Time
}

type InvoiceState int

const (
	InvoiceStateAccepted InvoiceState = iota
	InvoiceStateSettleRequested
	InvoiceStateSettled
)

type InvoiceCreationData struct {
	types.InvoiceCreationData
}

type dbInvoice struct {
	tableName struct{} `pg:"lnmux.invoices,discard_unknown_columns"` // nolint

	Hash       lntypes.Hash     `pg:"hash"`
	Preimage   lntypes.Preimage `pg:"preimage"`
	AmountMsat int64            `pg:"amount_msat,use_zero"`

	Settled           bool      `pg:"settled,use_zero"`
	SettledAt         time.Time `pg:"settled_at"`
	SettleRequestedAt time.Time `pg:"settle_requested_at"`
}

type dbHtlc struct {
	tableName struct{} `pg:"lnmux.htlcs,discard_unknown_columns"` // nolint

	Hash       lntypes.Hash `pg:"hash"`
	ChanID     uint64       `pg:"chan_id,use_zero"`
	HtlcID     uint64       `pg:"htlc_id,use_zero"`
	AmountMsat int64        `pg:"amount_msat,use_zero"`
}

// PostgresPersister persists items to Postgres
type PostgresPersister struct {
	conn *pg.DB

	logger *zap.SugaredLogger
}

func (p *PostgresPersister) Delete(ctx context.Context, hash lntypes.Hash) error {
	result, err := p.conn.ModelContext(ctx, (*dbInvoice)(nil)).
		Where("hash = ?", hash).Delete()

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return errors.New("invoice not found")
	}

	return nil
}

func unmarshallDbInvoice(invoice *dbInvoice) *Invoice {
	return &Invoice{
		InvoiceCreationData: InvoiceCreationData{
			InvoiceCreationData: types.InvoiceCreationData{
				PaymentPreimage: invoice.Preimage,
				Value:           lnwire.MilliSatoshi(invoice.AmountMsat),
			},
		},
		SettledAt: invoice.SettledAt,
		Settled:   invoice.Settled,
	}
}

func (p *PostgresPersister) Get(ctx context.Context, hash lntypes.Hash) (*Invoice,
	map[types.CircuitKey]int64, error) {

	var dbInvoice dbInvoice
	err := p.conn.ModelContext(ctx, &dbInvoice).
		Where("hash=?", hash).Select()
	switch {
	case err == pg.ErrNoRows:
		return nil, nil, types.ErrInvoiceNotFound

	case err != nil:
		return nil, nil, err
	}

	var dbHtlcs []*dbHtlc
	err = p.conn.ModelContext(ctx, &dbHtlcs).
		Where("hash=?", hash).Select()
	if err != nil {
		return nil, nil, err
	}

	invoice := unmarshallDbInvoice(&dbInvoice)

	htlcs := make(map[types.CircuitKey]int64)
	for _, htlc := range dbHtlcs {
		htlcs[types.CircuitKey{
			ChanID: htlc.ChanID,
			HtlcID: htlc.HtlcID,
		}] = htlc.AmountMsat
	}

	return invoice, htlcs, nil
}

func (p *PostgresPersister) RequestSettle(ctx context.Context,
	invoice *InvoiceCreationData, htlcs map[types.CircuitKey]int64) error {

	return p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		dbInvoice := &dbInvoice{
			Hash:              invoice.PaymentPreimage.Hash(),
			Preimage:          invoice.PaymentPreimage,
			AmountMsat:        int64(invoice.Value),
			SettleRequestedAt: time.Now(),
		}

		_, err := p.conn.ModelContext(ctx, dbInvoice).Insert()
		if err != nil {
			return err
		}

		for key, amt := range htlcs {
			dbHtlc := dbHtlc{
				Hash:       invoice.PaymentPreimage.Hash(),
				ChanID:     key.ChanID,
				HtlcID:     key.HtlcID,
				AmountMsat: amt,
			}
			_, err := tx.Model(&dbHtlc).Insert()
			if err != nil {
				return fmt.Errorf("cannot insert htlc: %w", err)
			}
		}

		return nil
	})
}

func (p *PostgresPersister) Settle(ctx context.Context,
	hash lntypes.Hash) error {

	result, err := p.conn.ModelContext(ctx, &dbInvoice{}).
		Set("settled=?", true).
		Set("settled_at=?", time.Now().UTC()).
		Where("hash=?", hash).
		Where("settled=?", false).
		Update()
	if err != nil {
		return fmt.Errorf("cannot settle invoice: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("cannot settle invoice")
	}

	return nil
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
