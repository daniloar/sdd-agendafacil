package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/agendafacil/agendafacil/internal/domain"
	"github.com/agendafacil/agendafacil/internal/domain/appointment"
)

// uqAppointmentConfirmedPerSlot é o nome do índice único parcial (migração 0002)
// que garante "no máximo uma consulta confirmada por slot". É a única violação
// de unicidade que este repositório traduz para domain.ErrAppointmentConflict.
const uqAppointmentConfirmedPerSlot = "uq_appt_confirmed_per_slot"

// AppointmentRepo é o appointment.AppointmentRepository apoiado em Postgres.
// Espelha a disciplina de CAS do SlotRepo: valida a transição com a máquina de
// estados pura do domínio, então emite um único UPDATE guardado e checa as
// linhas afetadas. Adicionalmente, quando o UPDATE colide com o índice único
// parcial uq_appt_confirmed_per_slot (SQLSTATE 23505 nessa constraint), mapeia
// para domain.ErrAppointmentConflict — o banco é a última linha de defesa mesmo
// que o CAS da aplicação seja contornado (AD-1, AD-10). Qualquer outro 23505 é
// devolvido como erro de banco embrulhado, não como conflito de confirmação.
type AppointmentRepo struct {
	db Querier
}

// NewAppointmentRepo constrói um AppointmentRepo sobre db, que pode ser um
// *pgxpool.Pool ou um pgx.Tx.
func NewAppointmentRepo(db Querier) *AppointmentRepo {
	return &AppointmentRepo{db: db}
}

// Transition move a consulta id de from -> to. Roda o validador puro do AD-10
// primeiro (par ilegal -> domain.ErrIllegalTransition, nenhum SQL emitido),
// então executa o compare-and-set. Zero linhas afetadas, ou uma violação de
// unicidade no índice uq_appt_confirmed_per_slot, ->
// domain.ErrAppointmentConflict. Um 23505 de qualquer outra constraint é
// devolvido embrulhado.
func (r *AppointmentRepo) Transition(ctx context.Context, id uuid.UUID, from, to appointment.State) error {
	return r.TransitionWith(ctx, r.db, id, from, to)
}

// TransitionWith é Transition executada contra um Querier fornecido por quem
// chama — uma transação envolvente — para que as Histórias 2+ possam gravar a
// transição pareada do slot, o evento de outbox e o registro de auditoria
// atomicamente com a mutação da consulta.
func (r *AppointmentRepo) TransitionWith(ctx context.Context, q Querier, id uuid.UUID, from, to appointment.State) error {
	if _, err := appointment.Transition(from, to); err != nil {
		return err
	}

	tag, err := q.Exec(ctx,
		`UPDATE appointments SET status = $1, updated_at = now() WHERE id = $2 AND status = $3`,
		string(to), id, string(from))
	if err != nil {
		if isUniqueViolationOn(err, uqAppointmentConfirmedPerSlot) {
			return domain.ErrAppointmentConflict
		}
		return fmt.Errorf("postgres: appointment transition: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrAppointmentConflict
	}
	return nil
}

var _ appointment.AppointmentRepository = (*AppointmentRepo)(nil)
