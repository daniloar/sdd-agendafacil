-- 0002 up — invariante de confirmação + histórico de soft lock.
-- AD-1 / AD-10: "no máximo uma consulta confirmada por slot" é uma invariante
-- de banco, um índice único parcial em slot_id WHERE status = 'confirmada' —
-- não lógica de aplicação.

CREATE TABLE appointments (
    id         uuid PRIMARY KEY,
    slot_id    uuid        NOT NULL REFERENCES slots (id),
    patient_id uuid        NOT NULL,
    doctor_id  uuid        NOT NULL REFERENCES doctors (id),
    status     text        NOT NULL DEFAULT 'confirmada',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT appointments_status_valid CHECK (status IN ('confirmada', 'cancelada', 'concluida', 'no_show'))
);

CREATE UNIQUE INDEX uq_appt_confirmed_per_slot
    ON appointments (slot_id)
    WHERE status = 'confirmada';

-- soft_locks.status: 'ativo' enquanto segura o slot, 'expirado' quando a janela
-- de 15 min venceu, 'liberado' quando devolvido explicitamente (desistência /
-- confirmação).
CREATE TABLE soft_locks (
    id          uuid PRIMARY KEY,
    slot_id     uuid        NOT NULL REFERENCES slots (id),
    patient_id  uuid        NOT NULL,
    acquired_at timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    released_at timestamptz,
    status      text        NOT NULL,
    CONSTRAINT soft_locks_status_valid CHECK (status IN ('ativo', 'expirado', 'liberado'))
);

CREATE INDEX ix_soft_locks_slot_id ON soft_locks (slot_id);
