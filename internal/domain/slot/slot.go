// Package slot é o agregado Slot: sua máquina de estados explícita (AD-7) e a
// porta de persistência que os adapters implementam. O validador de transição
// aqui é puro — não executa SQL. A escrita por compare-and-set vive no adapter
// postgres.
package slot

import (
	"context"

	"github.com/google/uuid"

	"github.com/agendafacil/agendafacil/internal/domain"
)

// State é um estado do ciclo de vida do slot. Os literais são os do Glossário do
// PRD verbatim (português), para que as constantes do domínio e os predicados
// dos índices únicos parciais permaneçam idênticos ao contrato.
type State string

const (
	Livre      State = "livre"      // disponível
	Reservado  State = "reservado"  // há um soft lock ativo
	Confirmado State = "confirmado" // existe uma consulta confirmada
	EmRisco    State = "em_risco"   // sem confirmação de presença dentro da janela
	Bloqueado  State = "bloqueado"  // retirado de circulação pelo admin/médico
)

// legalTransitions é o conjunto exato do AD-7. Qualquer par ausente é ilegal.
var legalTransitions = map[State]map[State]bool{
	Livre:      {Reservado: true, Bloqueado: true},
	Reservado:  {Livre: true, Confirmado: true},
	Confirmado: {EmRisco: true, Livre: true},
	EmRisco:    {Confirmado: true, Livre: true},
	Bloqueado:  {Livre: true},
}

// Transition é o validador puro da máquina de estados do Slot. Verifica o par
// (from, to) contra o conjunto legal do AD-7 e retorna o estado alvo em caso de
// sucesso, ou domain.ErrIllegalTransition caso contrário. Não faz I/O; o adapter
// postgres chama esta função primeiro e só então emite o UPDATE guardado
// (compare-and-set).
func Transition(from, to State) (State, error) {
	if legalTransitions[from][to] {
		return to, nil
	}
	return "", domain.ErrIllegalTransition
}

// IsLegal informa se from -> to é uma transição legal do AD-7.
func IsLegal(from, to State) bool {
	return legalTransitions[from][to]
}

// SlotRepository é a porta de persistência para as mutações de estado do Slot
// (AD-7, caminho único de escrita). Implementações realizam cada mutação como um
// único compare-and-set — UPDATE slots SET status = <to> ... WHERE id = <id> AND
// status = <from> — e exigem rows-affected == 1; zero linhas afetadas significa
// que quem chamou perdeu a corrida e a implementação retorna
// domain.ErrSlotUnavailable. Nenhuma implementação usa trava pessimista (nada de
// SELECT ... FOR UPDATE, nada de advisory lock).
type SlotRepository interface {
	Transition(ctx context.Context, id uuid.UUID, from, to State) error
}
