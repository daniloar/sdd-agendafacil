package clock

import (
	"sync"
	"time"

	"github.com/agendafacil/agendafacil/internal/domain"
)

// FakeClock é um domain.Clock determinístico para testes. Now retorna um
// instante fixo (em UTC) até que Set ou Advance o movam. Seguro para uso
// concorrente.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock retorna um FakeClock fixado em t (normalizado para UTC).
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t.UTC()}
}

// Now retorna o instante fake atual em UTC.
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance move o relógio fake para frente em d.
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d).UTC()
}

// Set reposiciona o relógio fake em t (normalizado para UTC).
func (f *FakeClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}

var _ domain.Clock = (*FakeClock)(nil)
