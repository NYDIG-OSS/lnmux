SET search_path TO lnmux;

ALTER TABLE lnmux.htlcs DROP COLUMN "node";

-- Remove PRIMARY KEY
ALTER TABLE lnmux.htlcs DROP CONSTRAINT htlcs_pkey;

-- Set the new PRIMARY KEY
ALTER TABLE lnmux.htlcs ADD PRIMARY KEY (chan_id, htlc_id);