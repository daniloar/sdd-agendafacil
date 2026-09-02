-- 0001 up — tabelas fundacionais da agenda (doctors, slots).
-- AD-1: PostgreSQL é o store transacional canônico.
-- AD-7: o status do slot é uma máquina de estados explícita; os literais são os
-- do Glossário do PRD verbatim.
-- Colunas monetárias: numeric(12,2), não-negativas; currency é código ISO-4217
-- de 3 letras.

CREATE TABLE doctors (
    id              uuid          PRIMARY KEY,
    name            text          NOT NULL,
    specialty       text          NOT NULL,
    reference_value numeric(12,2) NOT NULL,
    currency        text          NOT NULL,
    created_at      timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT doctors_reference_value_non_negative CHECK (reference_value >= 0),
    CONSTRAINT doctors_currency_iso4217 CHECK (char_length(currency) = 3)
);

CREATE TABLE slots (
    id              uuid          PRIMARY KEY,
    doctor_id       uuid          NOT NULL REFERENCES doctors (id),
    starts_at       timestamptz   NOT NULL,
    ends_at         timestamptz   NOT NULL,
    status          text          NOT NULL DEFAULT 'livre',
    reference_value numeric(12,2) NOT NULL,
    currency        text          NOT NULL,
    held_by         uuid,
    held_until      timestamptz,
    created_at      timestamptz   NOT NULL DEFAULT now(),
    updated_at      timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT slots_interval_valid CHECK (ends_at > starts_at),
    CONSTRAINT slots_status_valid CHECK (status IN ('livre', 'reservado', 'confirmado', 'em_risco', 'bloqueado')),
    CONSTRAINT slots_reference_value_non_negative CHECK (reference_value >= 0),
    CONSTRAINT slots_currency_iso4217 CHECK (char_length(currency) = 3)
);

CREATE INDEX ix_slots_doctor_starts_at ON slots (doctor_id, starts_at);

-- NOTA: a constraint EXCLUDE USING gist ("Ask First") que proibiria quaisquer
-- duas linhas de slot sobrepostas para o mesmo médico NÃO é adicionada aqui.
-- A rejeição do lote por sobreposição (FR-2) fica com o caso de uso de
-- publicação da História 5 até que um humano confirme o trade-off da
-- invariante de armazenamento (ver Boundaries da história).
