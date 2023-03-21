CREATE TYPE invoice_status as ENUM (
    'SETTLE_REQUESTED',
    'SETTLED',
    'FAILED'
);

-- Migrate invoices table
ALTER TABLE lnmux.invoices ADD "status" invoice_status;
ALTER TABLE lnmux.invoices ADD "finalized_at" TIMESTAMPTZ;

UPDATE lnmux.invoices SET
        status = CASE WHEN settled = FALSE THEN 'SETTLE_REQUESTED'::invoice_status ELSE 'SETTLED'::invoice_status END,
        finalized_at = CASE WHEN settled = TRUE THEN settled_at ELSE NULL END;

ALTER TABLE lnmux.invoices DROP COLUMN "settled";
ALTER TABLE lnmux.invoices DROP COLUMN "settled_at";

CREATE TYPE htlc_status as ENUM (
    'SETTLE_REQUESTED',
    'SETTLED',
    'FAILED'
);

-- Same for the htlcs table
ALTER TABLE lnmux.htlcs ADD "status" htlc_status;
ALTER TABLE lnmux.htlcs ADD "finalized_at" TIMESTAMPTZ;

UPDATE lnmux.htlcs SET
        status = CASE WHEN settled = FALSE THEN 'SETTLE_REQUESTED'::htlc_status ELSE 'SETTLED'::htlc_status END,
        finalized_at = CASE WHEN settled = TRUE THEN settled_at ELSE NULL END;


ALTER TABLE lnmux.htlcs DROP COLUMN "settled";
ALTER TABLE lnmux.htlcs DROP COLUMN "settled_at";