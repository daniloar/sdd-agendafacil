// Package appointment é o agregado Appointment: sua máquina de estados explícita
// (AD-10) e a porta de persistência que os adapters implementam. O validador de
// transição aqui é puro — não executa SQL.
package appointment

import (
	"context"

	"github.com/google/uuid"

	"github.com/agendafacil/agendafacil/internal/domain"
)

// State é um estado do ciclo de vida da consulta. Os literais são os do
// Glossário do PRD verbatim (português).
type State string

const (
	Confirmada State = "confirmada" // ativa, marcada
	Cancelada  State = "cancelada"  // cancelada pelo paciente
	Concluida  State = "concluida"  // a consulta aconteceu
	NoShow     State = "no_show"    // paciente não fez check-in
)

// legalTransitions é o conjunto exato do AD-10. Qualquer par ausente é ilegal.
var legalTransitions = map[State]map[State]bool{
	Confirmada: {Cancelada: true, NoShow: true, Concluida: true},
	NoShow:     {Confirmada: true},
}

// Transition é o validador puro da máquina de estados da Consulta. Verifica o
// par (from, to) contra o conjunto legal do AD-10 e retorna o estado alvo em
// caso de sucesso, ou domain.ErrIllegalTransition caso contrário. Não faz I/O.
func Transition(from, to State) (State, error) {
	if legalTransitions[from][to] {
		return to, nil
	}
	return "", domain.ErrIllegalTransition
}

// IsLegal informa se from -> to é uma transição legal do AD-10.
func IsLegal(from, to State) bool {
	return legalTransitions[from][to]
}

// AppointmentRepository é a porta de persistência para as mutações de estado da
// Consulta (AD-10). Implementações realizam cada mutação como um único
// compare-and-set — UPDATE appointments SET status = <to> ... WHERE id = <id>
// AND status = <from> — e exigem rows-affected == 1; zero linhas afetadas
// retorna domain.ErrAppointmentConflict. Uma violação de unicidade do Postgres
// (SQLSTATE 23505) no índice único parcial de "uma consulta confirmada por slot"
// é mapeada para o mesmo erro.
type AppointmentRepository interface {
	Transition(ctx context.Context, id uuid.UUID, from, to State) error
}
