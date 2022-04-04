SET search_path TO multiplexer;

CREATE TABLE invoices
(
        "created_at"            TIMESTAMPTZ     NOT NULL,
        "settled_at"            TIMESTAMPTZ,
        "hash"                  BYTEA           NOT NULL PRIMARY KEY CHECK (LENGTH(hash) = 32),
        "preimage"              BYTEA           NOT NULL UNIQUE CHECK (LENGTH(preimage) = 32),
        "amount_msat"           BIGINT          NOT NULL CHECK (amount_msat > 0),
        "id"                    BIGINT          NOT NULL UNIQUE CHECK (id > 0),

        "settled"               BOOLEAN         NOT NULL,
        "final_cltv_delta"      INTEGER         NOT NULL CHECK (final_cltv_delta > 0),
        "payment_addr"          BYTEA           NOT NULL UNIQUE CHECK (LENGTH(payment_addr) = 32),
        "payment_request"       TEXT            NOT NULL CHECK (LENGTH(payment_request) > 0)
);

CREATE TABLE htlcs
(
        "hash"                  BYTEA           NOT NULL CHECK (LENGTH(hash) = 32),
        "chan_id"               BIGINT          NOT NULL,
        "htlc_id"               BIGINT          NOT NULL,
        "amount_msat"           BIGINT          NOT NULL CHECK (amount_msat > 0),

        PRIMARY KEY (chan_id, htlc_id),

        CONSTRAINT fk_hash FOREIGN KEY(hash) REFERENCES invoices(hash) ON DELETE CASCADE
);