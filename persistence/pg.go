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

	Hash       lntypes.Hash     `pg:"hash,pk"`
	Preimage   lntypes.Preimage `pg:"preimage"`
	AmountMsat int64            `pg:"amount_msat,use_zero"`

	SettleRequestedAt time.Time `pg:"settle_requested_at"`

	Settled   bool      `pg:"settled,use_zero"`
	SettledAt time.Time `pg:"settled_at"`
}

type dbHtlc struct {
	tableName struct{} `pg:"lnmux.htlcs,discard_unknown_columns"` // nolint

	Hash       lntypes.Hash `pg:"hash"`
	ChanID     uint64       `pg:"chan_id,use_zero,pk"`
	HtlcID     uint64       `pg:"htlc_id,use_zero,pk"`
	AmountMsat int64        `pg:"amount_msat,use_zero"`

	SettleRequestedAt time.Time `pg:"settle_requested_at"`

	Settled   bool      `pg:"settled,use_zero"`
	SettledAt time.Time `pg:"settled_at"`
}

type dbInstanceLock struct {
	tableName struct{} `pg:"lnmux.instance_lock, discard_unknown_columns"` // nolint

	LockUpdatedAt time.Time `pg:"lock_updated_at"`
}

// PostgresPersister persists items to Postgres
type PostgresPersister struct {
	conn *pg.DB

	logger *zap.SugaredLogger

	// No mutex required, only accessed inside transactions.
	lastUpdate time.Time

	lockUpdateInterval       time.Duration
	lockUpdateStartThreshold time.Duration
}

const (
	// DefaultLockUpdateInterval specifies a default for LockUpdateInterval.
	DefaultLockUpdateInterval = 10 * time.Second

	// DefaultLockUpdateStartThreshold specifies a default for
	// LockUpdateStartThreshold.
	DefaultLockUpdateStartThreshold = 30 * time.Second
)

// PostgresPersisterConfig is for instantiating PostgresPersister.
type PostgresPersisterConfig struct {
	Logger *zap.SugaredLogger

	// LockUpdateInterval specifies how often to update the lock_updated_at
	// timestamp in the instance locks table.
	LockUpdateInterval time.Duration

	// LockUpdateStartThreshold specifies how long it must be since the
	// timestamp in the instance locks table has been updated to be allowed
	// to start.
	LockUpdateStartThreshold time.Duration
}

func (p *PostgresPersister) Delete(ctx context.Context, hash lntypes.Hash) error {
	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		err := p.checkLockValidity(ctx, tx)
		if err != nil {
			// We've lost the lock, close the persister.
			p.Close()

			return err
		}

		result, err := p.conn.ModelContext(ctx, (*dbInvoice)(nil)).
			Where("hash = ?", hash).Delete() // nolint:contextcheck

		if err != nil {
			return err
		}

		if result.RowsAffected() == 0 {
			return errors.New("invoice not found")
		}

		return nil
	})
	if err != nil {
		return err
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
		Settled: invoice.Settled,
	}
}

func (p *PostgresPersister) Get(ctx context.Context, hash lntypes.Hash) (*Invoice,
	map[types.CircuitKey]int64, error) {

	var htlcs = make(map[types.CircuitKey]int64)

	var invoice *Invoice

	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		err := p.checkLockValidity(ctx, tx)
		if err != nil {
			// We've lost the lock, close the persister.
			p.Close()

			return err
		}

		var dbInvoice dbInvoice
		err = p.conn.ModelContext(ctx, &dbInvoice).
			Where("hash=?", hash).Select()
		switch {
		case err == pg.ErrNoRows:
			return types.ErrInvoiceNotFound

		case err != nil:
			return err
		}

		var dbHtlcs []*dbHtlc
		err = p.conn.ModelContext(ctx, &dbHtlcs).
			Where("hash=?", hash).Select()
		if err != nil {
			return err
		}

		invoice = unmarshallDbInvoice(&dbInvoice)

		for _, htlc := range dbHtlcs {
			htlcs[types.CircuitKey{
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
	invoice *InvoiceCreationData, htlcs map[types.CircuitKey]int64) error {

	return p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		err := p.checkLockValidity(ctx, tx)
		if err != nil {
			// We've lost the lock, close the persister.
			p.Close()

			return err
		}

		now := time.Now().UTC()

		dbInvoice := &dbInvoice{
			Hash:              invoice.PaymentPreimage.Hash(),
			Preimage:          invoice.PaymentPreimage,
			AmountMsat:        int64(invoice.Value),
			SettleRequestedAt: now,
		}

		_, err = tx.ModelContext(ctx, dbInvoice).Insert() //nolint:contextcheck
		if err != nil {
			return err
		}

		for key, amt := range htlcs {
			dbHtlc := dbHtlc{
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
	hash lntypes.Hash, key types.CircuitKey) (bool, error) {

	var invoiceSettled bool

	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		err := p.checkLockValidity(ctx, tx)
		if err != nil {
			// We've lost the lock, close the persister.
			p.Close()

			return err
		}

		now := time.Now().UTC()

		htlc := dbHtlc{
			ChanID: key.ChanID,
			HtlcID: key.HtlcID,
		}

		result, err := tx.ModelContext(ctx, &htlc).
			WherePK().
			Where("hash=?", hash).
			Set("settled=?", true).
			Set("settled_at=?", now).
			Update() // nolint:contextcheck
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return types.ErrHtlcNotFound
		}

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

func (p *PostgresPersister) checkLockValidity(ctx context.Context, tx *pg.Tx) error {
	now := time.Now().UTC()

	if now.Before(p.lastUpdate.Add(p.lockUpdateInterval)) {
		return nil
	}

	var lock dbInstanceLock

	err := tx.ModelContext(ctx, &lock).Select()
	if err != nil {
		return err
	}

	if lock.LockUpdatedAt != p.lastUpdate {
		return errors.New("another instance has lock")
	}

	result, err := tx.ModelContext(ctx, &dbInstanceLock{
		LockUpdatedAt: now,
	}).Where("lock_updated_at = ?", lock.LockUpdatedAt).
		Update()
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("another instance has lock")
	}

	p.lastUpdate = now

	return nil
}

func (p *PostgresPersister) checkStartPersister(ctx context.Context) {
	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		now := time.Now().UTC()

		var lock dbInstanceLock

		err := tx.ModelContext(ctx, &lock).Select()
		if err != nil {
			return err
		}

		if now.Before(lock.LockUpdatedAt.UTC().Add(
			p.lockUpdateStartThreshold)) {

			return errors.New("another instance has lock")
		}

		result, err := tx.ModelContext(ctx, &dbInstanceLock{
			LockUpdatedAt: now,
		}).Where("lock_updated_at = ?", lock.LockUpdatedAt).
			Update()
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return errors.New("another instance has lock")
		}

		p.lastUpdate = now

		return nil
	})
	if err != nil {
		panic(err)
	}
}

// NewPostgresPersisterFromOptions creates a new PostgresPersister using the options provided
func NewPostgresPersisterFromOptions(options *pg.Options,
	cfg *PostgresPersisterConfig) *PostgresPersister {

	conn := pg.Connect(options)

	persister := &PostgresPersister{
		logger:                   cfg.Logger,
		conn:                     conn,
		lockUpdateInterval:       cfg.LockUpdateInterval,
		lockUpdateStartThreshold: cfg.LockUpdateStartThreshold,
	}

	if int64(persister.lockUpdateInterval) == 0 {
		persister.lockUpdateInterval = DefaultLockUpdateInterval
	}

	if int64(persister.lockUpdateStartThreshold) == 0 {
		persister.lockUpdateStartThreshold = DefaultLockUpdateStartThreshold
	}

	// TODO(aakselrod): get context from caller? In the config struct or
	// an argument?
	persister.checkStartPersister(context.Background())

	return persister
}

// NewPostgresPersisterFromDSN creates a new PostgresPersister using the dsn provided
func NewPostgresPersisterFromDSN(dsn string, cfg *PostgresPersisterConfig) (
	*PostgresPersister, error) {

	options, err := pg.ParseURL(dsn)
	if err != nil {
		return nil, err
	}

	return NewPostgresPersisterFromOptions(options, cfg), nil
}
