-- 0004 up — unicidade da oferta + uma penalidade por consulta.
-- AD-3: no máximo uma oferta 'pendente' por slot, garantida por um índice único
-- parcial para que liberação + avanço concorrentes convirjam
-- (INSERT ... ON CONFLICT DO NOTHING).
-- Coluna monetária: numeric(12,2), não-negativa; currency é código ISO-4217 de
-- 3 letras.

CREATE TABLE offers (
    id                uuid        PRIMARY KEY,
    slot_id           uuid        NOT NULL REFERENCES slots (id),
    waitlist_entry_id uuid        NOT NULL REFERENCES waitlist_entries (id),
    patient_id        uuid        NOT NULL,
    status            text        NOT NULL,
    notified_at       timestamptz,
    expires_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT offers_status_valid CHECK (status IN ('pendente', 'aceita', 'expirada', 'recusada'))
);

CREATE UNIQUE INDEX uq_offer_pending_per_slot
    ON offers (slot_id)
    WHERE status = 'pendente';

CREATE TABLE cancellation_penalties (
    id             uuid          PRIMARY KEY,
    appointment_id uuid          NOT NULL UNIQUE REFERENCES appointments (id),
    patient_id     uuid          NOT NULL,
    amount         numeric(12,2) NOT NULL,
    currency       text          NOT NULL,
    status         text          NOT NULL DEFAULT 'devida',
    created_at     timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT cancellation_penalties_status_valid CHECK (status IN ('devida')),
    CONSTRAINT cancellation_penalties_amount_non_negative CHECK (amount >= 0),
    CONSTRAINT cancellation_penalties_currency_iso4217 CHECK (char_length(currency) = 3)
);
