-- Migrate down invoices table
ALTER TABLE lnmux.invoices ADD "settled" BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE lnmux.invoices ADD "settled_at" TIMESTAMPTZ;

UPDATE lnmux.invoices SET
        settled = CASE WHEN status = 'SETTLED' THEN TRUE ELSE FALSE END,
        settled_at = CASE WHEN status = 'SETTLED' THEN finalized_at ELSE NULL END;

ALTER TABLE lnmux.invoices DROP COLUMN "status";
ALTER TABLE lnmux.invoices DROP COLUMN "finalized_at";

DROP TYPE invoice_status;

-- Same for the htlcs table
ALTER TABLE lnmux.htlcs ADD "settled" BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE lnmux.htlcs ADD "settled_at" TIMESTAMPTZ;

UPDATE lnmux.htlcs SET
        settled = CASE WHEN status = 'SETTLED' THEN TRUE ELSE FALSE END,
        settled_at = CASE WHEN status = 'SETTLED' THEN finalized_at ELSE NULL END;

ALTER TABLE lnmux.htlcs DROP COLUMN "status";
ALTER TABLE lnmux.htlcs DROP COLUMN "finalized_at";

DROP TYPE htlc_status;


