package appointment_test

import (
	"errors"
	"testing"

	"github.com/agendafacil/agendafacil/internal/domain"
	"github.com/agendafacil/agendafacil/internal/domain/appointment"
)

// legalPairs é o conjunto exato do AD-10, reafirmado de forma independente do
// mapa de produção.
var legalPairs = map[[2]appointment.State]bool{
	{appointment.Confirmada, appointment.Cancelada}: true,
	{appointment.Confirmada, appointment.NoShow}:    true,
	{appointment.Confirmada, appointment.Concluida}: true,
	{appointment.NoShow, appointment.Confirmada}:    true,
}

var allStates = []appointment.State{
	appointment.Confirmada, appointment.Cancelada, appointment.Concluida, appointment.NoShow,
}

func TestTransition_FullMatrix(t *testing.T) {
	for _, from := range allStates {
		for _, to := range allStates {
			from, to := from, to
			wantLegal := legalPairs[[2]appointment.State{from, to}]
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				got, err := appointment.Transition(from, to)
				if wantLegal {
					if err != nil {
						t.Fatalf("legal transition rejected: %v", err)
					}
					if got != to {
						t.Fatalf("want target %q, got %q", to, got)
					}
					return
				}
				if !errors.Is(err, domain.ErrIllegalTransition) {
					t.Fatalf("illegal transition accepted or wrong error: got=%q err=%v", got, err)
				}
				if got != "" {
					t.Fatalf("want empty state on illegal transition, got %q", got)
				}
			})
		}
	}
}

func TestIsLegal_MatchesTransition(t *testing.T) {
	for _, from := range allStates {
		for _, to := range allStates {
			_, err := appointment.Transition(from, to)
			if appointment.IsLegal(from, to) != (err == nil) {
				t.Fatalf("IsLegal disagrees with Transition for %q->%q", from, to)
			}
		}
	}
}

func TestStateLiterals_MatchGlossary(t *testing.T) {
	want := map[appointment.State]string{
		appointment.Confirmada: "confirmada",
		appointment.Cancelada:  "cancelada",
		appointment.Concluida:  "concluida",
		appointment.NoShow:     "no_show",
	}
	for state, literal := range want {
		if string(state) != literal {
			t.Fatalf("state literal drift: %q != %q", string(state), literal)
		}
	}
}
