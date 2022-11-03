SET search_path TO lnmux;

ALTER TABLE lnmux.htlcs ADD "node" BYTEA  NOT NULL CHECK (LENGTH(node) = 33);

-- Remove PRIMARY KEY
ALTER TABLE lnmux.htlcs DROP CONSTRAINT htlcs_pkey;

-- Lastly set your new PRIMARY KEY
ALTER TABLE lnmux.htlcs ADD PRIMARY KEY (node, chan_id, htlc_id);