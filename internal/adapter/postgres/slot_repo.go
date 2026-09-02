package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/agendafacil/agendafacil/internal/domain"
	"github.com/agendafacil/agendafacil/internal/domain/slot"
)

// SlotRepo é o slot.SlotRepository apoiado em Postgres e a implementação de
// referência da disciplina de compare-and-set otimista: validar a transição com
// a máquina de estados pura do domínio, então emitir um único UPDATE guardado e
// checar as linhas afetadas. Nada de SELECT ... FOR UPDATE, nada de advisory
// lock, em lugar nenhum.
type SlotRepo struct {
	db Querier
}

// NewSlotRepo constrói um SlotRepo sobre db, que pode ser um *pgxpool.Pool ou um
// pgx.Tx.
func NewSlotRepo(db Querier) *SlotRepo {
	return &SlotRepo{db: db}
}

// Transition move o slot id de from -> to. Primeiro roda o validador puro do
// AD-7 (par ilegal -> domain.ErrIllegalTransition, nenhum SQL emitido), então
// executa o compare-and-set. Zero linhas afetadas -> domain.ErrSlotUnavailable
// (perdeu a corrida ou a linha não estava no estado esperado); a linha fica
// intocada.
func (r *SlotRepo) Transition(ctx context.Context, id uuid.UUID, from, to slot.State) error {
	return r.TransitionWith(ctx, r.db, id, from, to)
}

// TransitionWith é Transition executada contra um Querier fornecido por quem
// chama — uma transação envolvente — para que as Histórias 2+ possam gravar o
// evento de outbox e o registro de auditoria atomicamente com a mutação do
// slot.
func (r *SlotRepo) TransitionWith(ctx context.Context, q Querier, id uuid.UUID, from, to slot.State) error {
	if _, err := slot.Transition(from, to); err != nil {
		return err
	}

	tag, err := q.Exec(ctx,
		`UPDATE slots SET status = $1, updated_at = now() WHERE id = $2 AND status = $3`,
		string(to), id, string(from))
	if err != nil {
		return fmt.Errorf("postgres: slot transition: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrSlotUnavailable
	}
	return nil
}

var _ slot.SlotRepository = (*SlotRepo)(nil)
