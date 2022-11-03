SET search_path TO lnmux;

ALTER TABLE lnmux.invoices DROP COLUMN "node";

-- Remove PRIMARY KEY
ALTER TABLE lnmux.htlcs DROP CONSTRAINT htlcs_pkey;

-- Set the new PRIMARY KEY
ALTER TABLE lnmux.htlcs PRIMARY KEY (chan_id, htlc_id);