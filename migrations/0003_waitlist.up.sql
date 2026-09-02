-- 0003 up — autoridade da fila de espera de dois escopos + contador de quota VIP.
-- AD-9: waitlist_entries é a autoridade de quem está em cada fila;
-- waitlist_vip_counters (uma linha por médico+data) é a autoridade da quota do
-- AD-4.
-- CHECKs de forma: status no conjunto do ciclo de vida; scope prefixado por
-- 'slot:' ou 'doctor:'; e exatamente um dos dois escopos preenchido — por slot
-- (slot_id) OU por médico+data (doctor_id + wl_date), nunca ambos nem nenhum.

CREATE TABLE waitlist_entries (
    id          uuid PRIMARY KEY,
    patient_id  uuid        NOT NULL,
    scope       text        NOT NULL,
    slot_id     uuid        REFERENCES slots (id),
    doctor_id   uuid        REFERENCES doctors (id),
    wl_date     date,
    class       text        NOT NULL,
    enqueued_at timestamptz NOT NULL DEFAULT now(),
    status      text        NOT NULL,
    CONSTRAINT waitlist_entries_class_valid CHECK (class IN ('comum', 'vip')),
    CONSTRAINT waitlist_entries_status_valid CHECK (status IN ('aguardando', 'ofertado', 'atendido', 'removido')),
    CONSTRAINT waitlist_entries_scope_shape CHECK (scope LIKE 'slot:%' OR scope LIKE 'doctor:%'),
    CONSTRAINT waitlist_entries_scope_columns CHECK (
        (slot_id IS NOT NULL AND doctor_id IS NULL AND wl_date IS NULL)
        OR (slot_id IS NULL AND doctor_id IS NOT NULL AND wl_date IS NOT NULL)
    )
);

-- Um paciente aparece no máximo uma vez por escopo de fila enquanto ainda ativo.
CREATE UNIQUE INDEX uq_waitlist_active_per_scope
    ON waitlist_entries (patient_id, scope)
    WHERE status IN ('aguardando', 'ofertado');

CREATE INDEX ix_waitlist_entries_scope ON waitlist_entries (scope, enqueued_at);

CREATE TABLE waitlist_vip_counters (
    doctor_id             uuid NOT NULL REFERENCES doctors (id),
    wl_date               date NOT NULL,
    consecutive_vip_grants int  NOT NULL DEFAULT 0,
    PRIMARY KEY (doctor_id, wl_date)
);
