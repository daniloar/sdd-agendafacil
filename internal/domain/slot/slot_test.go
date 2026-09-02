package slot_test

import (
	"errors"
	"testing"

	"github.com/agendafacil/agendafacil/internal/domain"
	"github.com/agendafacil/agendafacil/internal/domain/slot"
)

// legalPairs é o conjunto exato do AD-7, reafirmado de forma independente do
// mapa de produção.
var legalPairs = map[[2]slot.State]bool{
	{slot.Livre, slot.Reservado}:      true,
	{slot.Reservado, slot.Livre}:      true,
	{slot.Reservado, slot.Confirmado}: true,
	{slot.Confirmado, slot.EmRisco}:   true,
	{slot.EmRisco, slot.Confirmado}:   true,
	{slot.Confirmado, slot.Livre}:     true,
	{slot.EmRisco, slot.Livre}:        true,
	{slot.Livre, slot.Bloqueado}:      true,
	{slot.Bloqueado, slot.Livre}:      true,
}

var allStates = []slot.State{slot.Livre, slot.Reservado, slot.Confirmado, slot.EmRisco, slot.Bloqueado}

func TestTransition_LegalPair(t *testing.T) {
	got, err := slot.Transition(slot.Livre, slot.Reservado)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got != slot.Reservado {
		t.Fatalf("want %q, got %q", slot.Reservado, got)
	}
}

func TestTransition_IllegalPair(t *testing.T) {
	got, err := slot.Transition(slot.Livre, slot.Confirmado)
	if !errors.Is(err, domain.ErrIllegalTransition) {
		t.Fatalf("want ErrIllegalTransition, got %v", err)
	}
	if got != "" {
		t.Fatalf("want empty state on error, got %q", got)
	}
}

func TestTransition_FullMatrix(t *testing.T) {
	for _, from := range allStates {
		for _, to := range allStates {
			from, to := from, to
			wantLegal := legalPairs[[2]slot.State{from, to}]
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				got, err := slot.Transition(from, to)
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
			_, err := slot.Transition(from, to)
			if slot.IsLegal(from, to) != (err == nil) {
				t.Fatalf("IsLegal disagrees with Transition for %q->%q", from, to)
			}
		}
	}
}

func TestStateLiterals_MatchGlossary(t *testing.T) {
	want := map[slot.State]string{
		slot.Livre:      "livre",
		slot.Reservado:  "reservado",
		slot.Confirmado: "confirmado",
		slot.EmRisco:    "em_risco",
		slot.Bloqueado:  "bloqueado",
	}
	for state, literal := range want {
		if string(state) != literal {
			t.Fatalf("state literal drift: %q != %q", string(state), literal)
		}
	}
}
