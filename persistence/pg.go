package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/types"
	"github.com/go-pg/pg/v10"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"go.uber.org/zap"
)

// ErrHtlcAlreadyFinal is an error returned when we try to change the status
// of a HTLCs, but its status was already changed to the final one.
var ErrHtlcAlreadyFinal = errors.New("htlc already in its final status")

type Invoice struct {
	InvoiceCreationData

	SequenceNum       uint64
	SettleRequestedAt time.Time
	FinalizedAt       time.Time
	Status            types.InvoiceStatus
}

type InvoiceCreationData struct {
	types.InvoiceCreationData
}

type dbInvoice struct {
	tableName struct{} `pg:"lnmux.invoices,discard_unknown_columns"` // nolint

	// SequenceNum is a unique identifier used for pagination
	SequenceNum uint64 `pg:"sequence_num"`

	Hash       lntypes.Hash     `pg:"hash,use_zero,pk"`
	Preimage   lntypes.Preimage `pg:"preimage,use_zero"`
	AmountMsat int64            `pg:"amount_msat,use_zero"`

	SettleRequestedAt time.Time `pg:"settle_requested_at"`

	FinalizedAt time.Time `pg:"finalized_at"`

	Status types.InvoiceStatus `pg:"status"`
}

type dbHtlc struct {
	tableName struct{} `pg:"lnmux.htlcs,discard_unknown_columns"` // nolint

	Node   common.PubKey `pg:"node,use_zero,pk"`
	ChanID uint64        `pg:"chan_id,use_zero,pk"`
	HtlcID uint64        `pg:"htlc_id,use_zero,pk"`

	Hash       lntypes.Hash `pg:"hash,use_zero"`
	AmountMsat int64        `pg:"amount_msat,use_zero"`

	SettleRequestedAt time.Time `pg:"settle_requested_at"`

	FinalizedAt time.Time `pg:"finalized_at"`

	Status types.HtlcStatus `pg:"status"`
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
		SequenceNum:       invoice.SequenceNum,
		SettleRequestedAt: invoice.SettleRequestedAt,
		FinalizedAt:       invoice.FinalizedAt,
		Status:            invoice.Status,
	}
}

func (p *PostgresPersister) Get(ctx context.Context, hash lntypes.Hash) (*Invoice,
	map[types.HtlcKey]int64, error) {

	var htlcs = make(map[types.HtlcKey]int64)

	var invoice *Invoice

	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Retrieve the invoice and lock it FOR SHARE. This is to prevent an
		// update occurring before retrieving the htlcs in the next query.
		var dbInvoice dbInvoice
		err := tx.ModelContext(ctx, &dbInvoice).
			Where("hash=?", hash).
			For("SHARE").
			Select()
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

func (p *PostgresPersister) GetInvoices(ctx context.Context,
	maxInvoicesCount, sequenceStart int) ([]*Invoice, error) {

	var dbInvoices []*dbInvoice
	err := p.conn.WithContext(ctx).Model(&dbInvoices).
		Where("sequence_num>=?", sequenceStart).
		Order("sequence_num ASC").
		Limit(maxInvoicesCount).
		Select()
	if err != nil {
		return nil, err
	}

	invoices := make([]*Invoice, len(dbInvoices))
	for i := 0; i < len(dbInvoices); i++ {
		invoices[i] = unmarshallDbInvoice(dbInvoices[i])
	}

	return invoices, nil
}

// GetPendingHtlcs returns all htlcs for a node that are requested to be
// settled, but not yet settled.
func (p *PostgresPersister) GetPendingHtlcs(ctx context.Context, node common.PubKey) (
	map[types.CircuitKey]struct{}, error) {

	var htlcs = make(map[types.CircuitKey]struct{})
	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		var dbHtlcs []*dbHtlc
		err := tx.ModelContext(ctx, &dbHtlcs).
			Where("node=?", node).
			Where("status=?", types.HtlcStatusSettleRequested).
			Select()
		if err != nil {
			return err
		}

		for _, htlc := range dbHtlcs {
			htlcs[types.CircuitKey{
				ChanID: htlc.ChanID,
				HtlcID: htlc.HtlcID,
			}] = struct{}{}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return htlcs, nil
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
			Status:            types.InvoiceStatusSettleRequested,
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
				Status:            types.HtlcStatusSettleRequested,
			}
			_, err := tx.Model(&dbHtlc).Insert() // nolint:contextcheck
			if err != nil {
				return fmt.Errorf("cannot insert htlc: %w", err)
			}
		}

		return nil
	})
}

func (p *PostgresPersister) getHtlcHash(ctx context.Context, tx *pg.Tx,
	key types.HtlcKey) (lntypes.Hash, error) {

	htlc := dbHtlc{
		Node:   key.Node,
		ChanID: key.ChanID,
		HtlcID: key.HtlcID,
	}

	err := tx.ModelContext(ctx, &htlc).
		WherePK().
		Select() // nolint:contextcheck
	switch {
	case err == pg.ErrNoRows:
		return lntypes.Hash{}, types.ErrHtlcNotFound

	case err != nil:
		return lntypes.Hash{}, err
	}

	return htlc.Hash, nil
}

func (p *PostgresPersister) MarkHtlcFinal(ctx context.Context,
	key types.HtlcKey, settled bool) (*lntypes.Hash, error) {

	var invoiceHash *lntypes.Hash

	var htlcFinalStatus types.HtlcStatus
	if settled {
		htlcFinalStatus = types.HtlcStatusSettled
	} else {
		htlcFinalStatus = types.HtlcStatusFailed
	}

	err := p.conn.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Select invoice FOR UPDATE to prevent concurrent update on the
		// invoice/htlc.
		invoice, err := p.selectInvoiceForUpdate(ctx, tx, key)
		if err != nil {
			return err
		}

		now := time.Now().UTC()

		err = p.changeHTLCStatus(ctx, tx, key, htlcFinalStatus, now)
		if err != nil {
			return err
		}

		// It is possible that we received previously a HTLC that has
		// failed for this invoice. In consequence, the invoice should
		// have the FAILED status. However, a HTLC could have reached
		// its final status afterward.
		if invoice.Status == types.InvoiceStatusFailed {
			return nil
		}

		if settled {
			// The HTLC is settled, now we check if all other HTLCs
			// are settled as well.
			allHtlcsSettled, err := p.allHtlcsAreSettled(ctx,
				tx, invoice.Hash)
			if err != nil {
				return err
			}
			if !allHtlcsSettled {
				return nil
			}

			// If all htlcs are settled, settle the invoice.
			err = p.changeInvoiceStatus(ctx, tx, invoice.Hash,
				types.InvoiceStatusSettled, now)
			if err != nil {
				return err
			}
		} else {
			// As one HTLC failed, mark the whole invoice as failed.
			err = p.changeInvoiceStatus(ctx, tx, invoice.Hash,
				types.InvoiceStatusFailed, now)
			if err != nil {
				return err
			}
		}

		invoiceHash = &invoice.Hash

		return nil
	})
	if err != nil {
		return nil, err
	}

	return invoiceHash, nil
}

func (p *PostgresPersister) selectInvoiceForUpdate(ctx context.Context,
	tx *pg.Tx, key types.HtlcKey) (*dbInvoice, error) {

	// Look up the htlc hash.
	hash, err := p.getHtlcHash(ctx, tx, key)
	if err != nil {
		return nil, err
	}

	var invoice dbInvoice
	err = tx.ModelContext(ctx, &invoice).
		Where("hash=?", hash).
		For("UPDATE").
		Select()
	switch {
	case err == pg.ErrNoRows:
		return nil, types.ErrInvoiceNotFound

	case err != nil:
		return nil, err
	}

	return &invoice, nil
}

func (p *PostgresPersister) changeHTLCStatus(ctx context.Context,
	tx *pg.Tx, key types.HtlcKey, status types.HtlcStatus,
	finalizedAt time.Time) error {

	htlc := dbHtlc{
		Node:   key.Node,
		ChanID: key.ChanID,
		HtlcID: key.HtlcID,
	}

	// Change htlc status. If the htlc is not found at this point, status must
	// have been changed already. We were able to retrieve it earlier, so it
	// exists.
	result, err := tx.ModelContext(ctx, &htlc).
		WherePK().
		Where("status=?", types.HtlcStatusSettleRequested).
		Set("status=?", status).
		Set("finalized_at=?", finalizedAt).
		Update() // nolint:contextcheck
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrHtlcAlreadyFinal
	}

	return nil
}

func (p *PostgresPersister) changeInvoiceStatus(ctx context.Context,
	tx *pg.Tx, hash lntypes.Hash, status types.InvoiceStatus,
	finalizedAt time.Time) error {

	result, err := tx.ModelContext(ctx, (*dbInvoice)(nil)).
		Where("hash=?", hash).
		Set("status=?", status).
		Set("finalized_at=?", finalizedAt).
		Update() // nolint:contextcheck
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return types.ErrInvoiceNotFound
	}

	return nil
}

func (p *PostgresPersister) allHtlcsAreSettled(ctx context.Context,
	tx *pg.Tx, hash lntypes.Hash) (bool, error) {

	// Count number of htlcs that are not yet settled.
	count, err := tx.ModelContext(ctx, (*dbHtlc)(nil)).
		Where("hash=?", hash).
		Where("status=?", types.HtlcStatusSettleRequested).
		Count()
	if err != nil {
		return false, err
	}

	return count == 0, nil
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
