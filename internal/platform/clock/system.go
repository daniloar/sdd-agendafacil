// Package clock fornece implementações de domain.Clock (AD-6): SystemClock para
// produção e FakeClock para testes determinísticos.
package clock

import (
	"time"

	"github.com/agendafacil/agendafacil/internal/domain"
)

// SystemClock é um domain.Clock apoiado no relógio de parede do processo. Now
// sempre retorna o instante em UTC.
type SystemClock struct{}

// NewSystemClock retorna um SystemClock pronto para uso.
func NewSystemClock() SystemClock { return SystemClock{} }

// Now retorna o instante atual do relógio de parede convertido para UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

var _ domain.Clock = SystemClock{}
