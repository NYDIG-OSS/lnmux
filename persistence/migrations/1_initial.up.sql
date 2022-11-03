SET search_path TO lnmux;

CREATE TABLE invoices
(
        "settle_requested_at"   TIMESTAMPTZ,
        "hash"                  BYTEA           NOT NULL CHECK (LENGTH(hash) = 32),
        "preimage"              BYTEA           NOT NULL UNIQUE CHECK (LENGTH(preimage) = 32),
        "amount_msat"           BIGINT          NOT NULL CHECK (amount_msat > 0),
        "settled"               BOOLEAN         NOT NULL DEFAULT FALSE,
        "settled_at"            TIMESTAMPTZ,

        PRIMARY KEY (hash)
);

CREATE TABLE htlcs
(
        "settle_requested_at"   TIMESTAMPTZ,
        "hash"                  BYTEA           NOT NULL CHECK (LENGTH(hash) = 32),
        "chan_id"               BIGINT          NOT NULL,
        "htlc_id"               BIGINT          NOT NULL,
        "amount_msat"           BIGINT          NOT NULL CHECK (amount_msat > 0),
        "settled"               BOOLEAN         NOT NULL DEFAULT FALSE,
        "settled_at"            TIMESTAMPTZ,
        
        PRIMARY KEY (chan_id, htlc_id),

        CONSTRAINT fk_hash FOREIGN KEY(hash) REFERENCES invoices(hash) ON DELETE CASCADE
);
