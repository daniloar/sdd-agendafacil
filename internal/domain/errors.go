// Package domain contém o núcleo de agendamento do AgendaFácil: entidades,
// máquinas de estado explícitas e as portas que os adapters implementam. Não
// importa nada de internal/adapter nem de internal/platform (fronteira
// hexagonal, architecture spine).
package domain

import "errors"

// Erros sentinela do domínio, compartilhados entre agregados e adapters. Quem
// chama mapeia estes erros para códigos de transporte (4xx) na borda; o domínio
// nunca conhece HTTP.
var (
	// ErrIllegalTransition é retornado pelo validador puro de transição de
	// estado quando o par (from, to) não está no conjunto legal do agregado
	// (AD-7 para slots, AD-10 para consultas).
	ErrIllegalTransition = errors.New("domain: illegal state transition")

	// ErrSlotUnavailable é retornado quando uma mutação de slot por
	// compare-and-set (UPDATE ... WHERE status = <esperado>) afeta zero linhas:
	// quem chamou perdeu a corrida ou o slot não estava no estado esperado.
	ErrSlotUnavailable = errors.New("domain: slot unavailable")

	// ErrAppointmentConflict é retornado quando uma mutação de consulta por
	// compare-and-set afeta zero linhas, ou quando o Postgres levanta uma
	// violação de unicidade (SQLSTATE 23505) no índice único parcial de "uma
	// consulta confirmada por slot".
	ErrAppointmentConflict = errors.New("domain: appointment conflict")
)
