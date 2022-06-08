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

	State           InvoiceState
	CancelledReason CancelledReason

	SettledAt         time.Time
	SettleRequestedAt time.Time
}

type InvoiceState int

const (
	InvoiceStateOpen     InvoiceState = iota
	InvoiceStateAccepted              // This state is not persisted in the database.
	InvoiceStateSettleRequested
	InvoiceStateSettled
	InvoiceStateCancelled
)

type CancelledReason int

const (
	CancelledReasonNone CancelledReason = iota
	CancelledReasonExpired
	CancelledReasonAcceptTimeout
	CancelledReasonExternal
)

type InvoiceCreationData struct {
	types.InvoiceCreationData

	CreatedAt      time.Time
	ExpiresAt      time.Time
	ID             int64
	PaymentRequest string
	AutoSettle     bool
}

type dbInvoiceState string

const (
	dbInvoiceStateOpen            = dbInvoiceState("OPEN")
	dbInvoiceStateSettleRequested = dbInvoiceState("SETTLE_REQUESTED")
	dbInvoiceStateSettled         = dbInvoiceState("SETTLED")
	dbInvoiceStateExpired         = dbInvoiceState("EXPIRED")
	dbInvoiceStateCancelled       = dbInvoiceState("CANCELLED")
)

type dbCancelledReason string

const (
	dbCancelledReasonExpired       = dbCancelledReason("EXPIRED")
	dbCancelledReasonAcceptTimeout = dbCancelledReason("ACCEPT_TIMEOUT")
	dbCancelledReasonExternal      = dbCancelledReason("EXTERNAL")
)

type dbInvoice struct {
	tableName struct{} `pg:"lnmux.invoices,discard_unknown_columns"` // nolint

	Hash       lntypes.Hash     `pg:"hash"`
	Preimage   lntypes.Preimage `pg:"preimage"`
	CreatedAt  time.Time        `pg:"created_at"`
	ExpiresAt  time.Time        `pg:"expires_at"`
	AmountMsat int64            `pg:"amount_msat,use_zero"`
	ID         int64            `pg:"id,use_zero"`

	State             dbInvoiceState     `pg:"state"`
	CancelledReason   *dbCancelledReason `pg:"cancelled_reason"`
	SettledAt         time.Time          `pg:"settled_at"`
	SettleRequestedAt time.Time          `pg:"settle_requested_at"`
	FinalCltvDelta    int32              `pg:"final_cltv_delta,use_zero"`
	PaymentAddr       [32]byte           `pg:"payment_addr"`
	PaymentRequest    string             `pg:"payment_request"`
	AutoSettle        bool               `pg:"auto_settle,use_zero"`
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

func unmarshallDbInvoiceState(state dbInvoiceState) InvoiceState {
	switch state {
	case dbInvoiceStateOpen:
		return InvoiceStateOpen

	case dbInvoiceStateSettleRequested:
		return InvoiceStateSettleRequested

	case dbInvoiceStateSettled:
		return InvoiceStateSettled

	case dbInvoiceStateCancelled:
		return InvoiceStateCancelled

	default:
		panic("unknown invoice state")
	}
}

func unmarshallDbCancelledReason(reason *dbCancelledReason) CancelledReason {
	if reason == nil {
		return CancelledReasonNone
	}

	switch *reason {
	case dbCancelledReasonExpired:
		return CancelledReasonExpired

	case dbCancelledReasonAcceptTimeout:
		return CancelledReasonAcceptTimeout

	case dbCancelledReasonExternal:
		return CancelledReasonExternal

	default:
		panic("unknown cancelled reason")
	}
}

func marshallCancelledReason(reason CancelledReason) *dbCancelledReason {
	var dbReason dbCancelledReason
	switch reason {
	case CancelledReasonNone:
		return nil

	case CancelledReasonExpired:
		dbReason = dbCancelledReasonExpired

	case CancelledReasonAcceptTimeout:
		dbReason = dbCancelledReasonAcceptTimeout

	case CancelledReasonExternal:
		dbReason = dbCancelledReasonExternal

	default:
		panic("unknown cancelled reason")
	}

	return &dbReason
}

func unmarshallDbInvoice(invoice *dbInvoice) *Invoice {
	return &Invoice{
		InvoiceCreationData: InvoiceCreationData{
			CreatedAt:      invoice.CreatedAt,
			PaymentRequest: invoice.PaymentRequest,
			InvoiceCreationData: types.InvoiceCreationData{
				FinalCltvDelta:  invoice.FinalCltvDelta,
				PaymentPreimage: invoice.Preimage,
				Value:           lnwire.MilliSatoshi(invoice.AmountMsat),
				PaymentAddr:     invoice.PaymentAddr,
			},
			ID:         invoice.ID,
			ExpiresAt:  invoice.ExpiresAt,
			AutoSettle: invoice.AutoSettle,
		},
		SettledAt:       invoice.SettledAt,
		State:           unmarshallDbInvoiceState(invoice.State),
		CancelledReason: unmarshallDbCancelledReason(invoice.CancelledReason),
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

func (p *PostgresPersister) GetOpen(ctx context.Context) ([]*Invoice,
	error) {

	var dbInvoices []dbInvoice
	err := p.conn.ModelContext(ctx, &dbInvoices).
		Where("state=?", dbInvoiceStateOpen).Select()

	if err != nil {
		return nil, err
	}

	var invoices []*Invoice
	for _, dbInvoice := range dbInvoices {
		invoice := unmarshallDbInvoice(&dbInvoice)
		invoices = append(invoices, invoice)
	}

	return invoices, nil
}

func (p *PostgresPersister) RequestSettle(ctx context.Context,
	hash lntypes.Hash, htlcs map[types.CircuitKey]int64) error {

	return p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		result, err := p.conn.ModelContext(ctx, &dbInvoice{}).
			Set("state=?", dbInvoiceStateSettleRequested).
			Set("settle_requested_at=?", time.Now().UTC()).
			Where("hash=?", hash).
			Where("state=?", dbInvoiceStateOpen).
			Update()
		if err != nil {
			return fmt.Errorf("cannot request settle: %w", err)
		}
		if result.RowsAffected() == 0 {
			return errors.New("cannot request settle")
		}

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
		}

		return nil
	})
}

func (p *PostgresPersister) Settle(ctx context.Context,
	hash lntypes.Hash) error {

	result, err := p.conn.ModelContext(ctx, &dbInvoice{}).
		Set("state=?", dbInvoiceStateSettled).
		Set("settled_at=?", time.Now().UTC()).
		Where("hash=?", hash).
		Where("state=?", dbInvoiceStateSettleRequested).
		Update()
	if err != nil {
		return fmt.Errorf("cannot settle invoice: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("cannot settle invoice")
	}

	return nil
}

func (p *PostgresPersister) Fail(ctx context.Context,
	hash lntypes.Hash, reason CancelledReason) error {

	if reason == CancelledReasonNone {
		return errors.New("no cancelled reason specified")
	}

	result, err := p.conn.ModelContext(ctx, &dbInvoice{}).
		Set("state=?", dbInvoiceStateCancelled).
		Set("settled_at=?", time.Now().UTC()).
		Set("cancelled_reason=?", marshallCancelledReason(reason)).
		Where("hash=?", hash).
		Where("state=?", dbInvoiceStateOpen).
		Update()
	if err != nil {
		return fmt.Errorf("cannot fail invoice: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("cannot fail invoice")
	}

	return nil
}

func (p *PostgresPersister) Add(ctx context.Context, invoice *InvoiceCreationData) error {
	dbInvoice := &dbInvoice{
		CreatedAt:      invoice.CreatedAt,
		ExpiresAt:      invoice.ExpiresAt,
		Hash:           invoice.PaymentPreimage.Hash(),
		Preimage:       invoice.PaymentPreimage,
		AmountMsat:     int64(invoice.Value),
		FinalCltvDelta: invoice.FinalCltvDelta,
		PaymentAddr:    invoice.PaymentAddr,
		PaymentRequest: invoice.PaymentRequest,
		ID:             invoice.ID,
		State:          dbInvoiceStateOpen,
		AutoSettle:     invoice.AutoSettle,
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
