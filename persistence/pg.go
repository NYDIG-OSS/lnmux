package persistence

import (
	"errors"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/go-pg/pg/v9"
	"github.com/go-pg/pg/v9/orm"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"go.uber.org/zap"
)

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
	Logger *zap.SugaredLogger

	Options *PostgresOptions
	Conn    orm.DB

	parent *PostgresPersister
}

// PostgresOptions defines options for the Postgres server
type PostgresOptions struct {
	DSN string

	Logger *zap.SugaredLogger
}

// child clones the current PostgresPersister and sets its connections to the ones provided.
func (p *PostgresPersister) child(writeConn orm.DB) *PostgresPersister {
	ret := *p
	ret.Conn = writeConn
	ret.parent = p

	return &ret
}

// Atomic executes a function atomically. If it returns an error then any changes
// made will be rolled back.
// In the context of Postgres, the function is executed within a transaction.
func (p *PostgresPersister) Atomic(f func(p *PostgresPersister) error) error {
	if p.parent != nil {
		return fmt.Errorf("postgres does not support nested transactions")
	}

	writeConn, ok := p.Conn.(*pg.DB)
	if !ok {
		return fmt.Errorf("unexpected internal connection type")
	}

	tx, err := writeConn.Begin()
	if err != nil {
		return err
	}

	// Always rollback if there's a panic
	defer func() {
		if r := recover(); r != nil {
			if tx != nil {
				_ = tx.Rollback()
				panic(r)
			}
		}
	}()

	child := p.child(tx)
	err = f(child)
	if err != nil {
		_ = tx.Rollback()

		return err
	}

	return tx.Commit()
}

// NewPostgresPersister creates a new PostgresPersister using the options provided
func NewPostgresPersister(options *PostgresOptions) (*PostgresPersister, error) {
	persister := &PostgresPersister{
		Logger:  options.Logger,
		Options: options,
	}

	if options.DSN == "" {
		return nil, fmt.Errorf("primary connection string is required")
	}

	writeConn, _, err := persister.newConnection(options.DSN)
	if err != nil {
		return nil, err
	}
	persister.Conn = writeConn

	// Ensure the server(s) can be connected to
	if err = persister.Ping(); err != nil {
		return nil, err
	}

	return persister, nil
}

func (p *PostgresPersister) Delete(hash lntypes.Hash) error {
	result, err := p.Conn.Model((*dbInvoice)(nil)).
		Where("hash = ?", hash).Delete()

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return errors.New("invoice not found")
	}

	return nil
}

func (p *PostgresPersister) Get(hash lntypes.Hash) (*Invoice,
	map[lnmux.CircuitKey]int64, error) {

	var dbInvoice dbInvoice
	err := p.Conn.Model(&dbInvoice).
		Where("hash=?", hash).Select()
	switch {
	case err == pg.ErrNoRows:
		return nil, nil, lnmux.ErrInvoiceNotFound

	case err != nil:
		return nil, nil, err
	}

	var dbHtlcs []*dbHtlc
	err = p.Conn.Model(&dbHtlcs).
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

func (p *PostgresPersister) Settle(hash lntypes.Hash,
	htlcs map[lnmux.CircuitKey]int64) error {

	return p.Atomic(func(p *PostgresPersister) error {
		writeConn := p.Conn

		var totalAmt int64
		for key, amt := range htlcs {
			dbHtlc := dbHtlc{
				Hash:       hash,
				ChanID:     key.ChanID,
				HtlcID:     key.HtlcID,
				AmountMsat: amt,
			}
			_, err := writeConn.Model(&dbHtlc).Insert()
			if err != nil {
				return fmt.Errorf("cannot insert htlc: %w", err)
			}

			totalAmt += amt
		}

		result, err := writeConn.Model(&dbInvoice{}).
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

func (p *PostgresPersister) Add(invoice *InvoiceCreationData) error {
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

	_, err := p.Conn.Model(dbInvoice).Insert()

	return err
}

// Ping pings the database connections to ensure they exist
func (p *PostgresPersister) Ping() error {
	if p.Conn != nil {
		if _, err := p.Conn.ExecOne("SELECT 1"); err != nil {
			return err
		}
	}

	if _, err := p.Conn.ExecOne("SELECT 1"); err != nil {
		return err
	}

	return nil
}

func (p *PostgresPersister) newConnection(dsn string) (orm.DB, *pg.Options, error) {
	settings, err := pg.ParseURL(dsn)
	if err != nil {
		return nil, nil, err
	}
	conn := pg.Connect(settings)

	return conn, settings, nil
}
