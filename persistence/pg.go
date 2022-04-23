package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
)

// TxConn defines a database connection capable of running transactions
type TxConn interface {
	orm.DB
	RunInTransaction(ctx context.Context, fn func(*pg.Tx) error) error
	Begin() (*pg.Tx, error)
}

type Invoice struct {
	InvoiceCreationData

	Settled   bool
	SettledAt time.Time
}

type InvoiceCreationData struct {
	lnmux.InvoiceCreationData

	CreatedAt      time.Time
	ID             int64
	PaymentRequest string
}

type dbInvoice struct {
	tableName struct{} `pg:"multiplexer.invoices,discard_unknown_columns"` // nolint

	Hash       lntypes.Hash     `pg:"hash"`
	Preimage   lntypes.Preimage `pg:"preimage"`
	CreatedAt  time.Time        `pg:"created_at"`
	AmountMsat int64            `pg:"amount_msat,use_zero"`
	ID         int64            `pg:"id,use_zero"`

	Settled        bool      `pg:"settled,use_zero"`
	SettledAt      time.Time `pg:"settled_at"`
	FinalCltvDelta int32     `pg:"final_cltv_delta,use_zero"`
	PaymentAddr    [32]byte  `pg:"payment_addr"`
	PaymentRequest string    `pg:"payment_request"`
}

type dbHtlc struct {
	tableName struct{} `pg:"multiplexer.htlcs,discard_unknown_columns"` // nolint

	Hash       lntypes.Hash `pg:"hash"`
	ChanID     uint64       `pg:"chan_id,use_zero"`
	HtlcID     uint64       `pg:"htlc_id,use_zero"`
	AmountMsat int64        `pg:"amount_msat,use_zero"`
}

// PostgresPersister persists items to Postgres
type PostgresPersister struct {
	conn TxConn
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

func (p *PostgresPersister) Get(ctx context.Context, hash lntypes.Hash) (*Invoice,
	map[lnmux.CircuitKey]int64, error) {

	var dbInvoice dbInvoice
	err := p.conn.ModelContext(ctx, &dbInvoice).
		Where("hash=?", hash).Select()
	switch {
	case err == pg.ErrNoRows:
		return nil, nil, lnmux.ErrInvoiceNotFound

	case err != nil:
		return nil, nil, err
	}

	var dbHtlcs []*dbHtlc
	err = p.conn.ModelContext(ctx, &dbHtlcs).
		Where("hash=?", hash).Select()
	if err != nil {
		return nil, nil, err
	}

	invoice := &Invoice{
		InvoiceCreationData: InvoiceCreationData{
			CreatedAt:      dbInvoice.CreatedAt,
			PaymentRequest: dbInvoice.PaymentRequest,
			InvoiceCreationData: lnmux.InvoiceCreationData{
				FinalCltvDelta:  dbInvoice.FinalCltvDelta,
				PaymentPreimage: dbInvoice.Preimage,
				Value:           lnwire.MilliSatoshi(dbInvoice.AmountMsat),
				PaymentAddr:     dbInvoice.PaymentAddr,
			},
			ID: dbInvoice.ID,
		},
		SettledAt: dbInvoice.SettledAt,
		Settled:   dbInvoice.Settled,
	}

	htlcs := make(map[lnmux.CircuitKey]int64)
	for _, htlc := range dbHtlcs {
		htlcs[lnmux.CircuitKey{
			ChanID: htlc.ChanID,
			HtlcID: htlc.HtlcID,
		}] = htlc.AmountMsat
	}

	return invoice, htlcs, nil
}

func (p *PostgresPersister) Settle(ctx context.Context, hash lntypes.Hash,
	htlcs map[lnmux.CircuitKey]int64) error {

	return p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		var totalAmt int64
		for key, amt := range htlcs {
			dbHtlc := dbHtlc{
				Hash:       hash,
				ChanID:     key.ChanID,
				HtlcID:     key.HtlcID,
				AmountMsat: amt,
			}
			_, err := tx.Model(&dbHtlc).Insert()
			if err != nil {
				return fmt.Errorf("cannot insert htlc: %w", err)
			}

			totalAmt += amt
		}

		result, err := tx.Model(&dbInvoice{}).
			Set("settled=?", true).
			Set("settled_at=?", time.Now().UTC()).
			Where("hash=?", hash).
			Where("amount_msat=?", totalAmt).
			Update()
		if err != nil {
			return fmt.Errorf("cannot settle invoice: %w", err)
		}
		if result.RowsAffected() == 0 {
			return errors.New("cannot settle invoice")
		}

		return nil
	})
}

func (p *PostgresPersister) Add(ctx context.Context, invoice *InvoiceCreationData) error {
	dbInvoice := &dbInvoice{
		CreatedAt:      invoice.CreatedAt,
		Hash:           invoice.PaymentPreimage.Hash(),
		Preimage:       invoice.PaymentPreimage,
		AmountMsat:     int64(invoice.Value),
		FinalCltvDelta: invoice.FinalCltvDelta,
		PaymentAddr:    invoice.PaymentAddr,
		PaymentRequest: invoice.PaymentRequest,
		ID:             invoice.ID,
	}

	_, err := p.conn.ModelContext(ctx, dbInvoice).Insert()

	return err
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

// NewPostgresPersisterFromOptions creates a new PostgresPersister using the options provided
func NewPostgresPersisterFromOptions(options *pg.Options) *PostgresPersister {
	conn := pg.Connect(options)

	return NewPostgresPersister(conn)
}

// NewPostgresPersisterFromDSN creates a new PostgresPersister using the dsn provided
func NewPostgresPersisterFromDSN(dsn string) (*PostgresPersister, error) {
	options, err := pg.ParseURL(dsn)
	if err != nil {
		return nil, err
	}

	return NewPostgresPersisterFromOptions(options), nil
}

// NewPostgresPersister creates a new PostgresPersister using the existing db connection provided
func NewPostgresPersister(conn TxConn) *PostgresPersister {
	persister := &PostgresPersister{
		conn: conn,
	}

	return persister
}
