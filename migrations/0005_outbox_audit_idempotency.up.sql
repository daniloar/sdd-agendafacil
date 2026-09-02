-- 0005 up — outbox transacional (AD-8), trilha de auditoria imutável (AD-11),
-- substrato do envelope de idempotência (AD-5).

CREATE TABLE slot_events (
    id             uuid PRIMARY KEY,
    aggregate_type text        NOT NULL,
    aggregate_id   uuid        NOT NULL,
    event_type     text        NOT NULL,
    payload        jsonb       NOT NULL,
    occurred_at    timestamptz NOT NULL DEFAULT now(),
    published_at   timestamptz
);

-- Varredura do relay: linhas de outbox não publicadas, mais antigas primeiro.
CREATE INDEX ix_slot_events_unpublished
    ON slot_events (occurred_at)
    WHERE published_at IS NULL;

CREATE TABLE audit_records (
    id         uuid PRIMARY KEY,
    entity     text        NOT NULL,
    entity_id  uuid        NOT NULL,
    prev_state text,
    new_state  text        NOT NULL,
    actor      text        NOT NULL,
    cause      text        NOT NULL,
    ts         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_audit_records_entity ON audit_records (entity, entity_id, ts);

CREATE TABLE idempotency_keys (
    key                 uuid PRIMARY KEY,
    request_fingerprint text        NOT NULL,
    response_status     int,
    response_body       jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL
);

CREATE INDEX ix_idempotency_keys_expires_at ON idempotency_keys (expires_at);
