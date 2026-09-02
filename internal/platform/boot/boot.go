// Package boot é o caminho de inicialização compartilhado por cmd/api e
// cmd/worker: carrega a config, aplica as migrações e verifica o schema. Isolar
// isso num pacote dá um seam testável e evita a duplicação entre os dois
// binários.
package boot

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/agendafacil/agendafacil/internal/platform/config"
	"github.com/agendafacil/agendafacil/internal/platform/migrate"
)

// migrateTimeout limita a fase de migração + verificação de schema. O
// golang-migrate v4 não expõe API sensível a context em Up/Version, então o
// limite é aplicado sobre a espera: se estourar, boot.Run retorna erro e o
// processo encerra (a migração em si pode seguir até o processo morrer).
const migrateTimeout = 30 * time.Second

// Run executa a sequência de boot para o processo name ("api" ou "worker"):
// carrega a config, aplica as migrações pendentes e confere a versão do schema.
// Retorna erro se a config for inválida, se a migração falhar, ou se o schema
// estiver dirty (uma migração anterior falhou no meio). Em sucesso, loga a
// versão do schema.
func Run(ctx context.Context, name string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	if err := withTimeout(ctx, migrateTimeout, func() error {
		return migrate.Up(cfg.DatabaseURL)
	}); err != nil {
		return fmt.Errorf("%s: migrate: %w", name, err)
	}

	var (
		version uint
		dirty   bool
	)
	if err := withTimeout(ctx, migrateTimeout, func() error {
		var verr error
		version, dirty, verr = migrate.Version(cfg.DatabaseURL)
		return verr
	}); err != nil {
		return fmt.Errorf("%s: schema version: %w", name, err)
	}
	if dirty {
		return fmt.Errorf("%s: schema dirty at version %d", name, version)
	}

	log.Printf("%s: schema at version %d (dirty=%t)", name, version, dirty)
	return nil
}

// withTimeout roda fn e espera no máximo d (ou até ctx cancelar).
func withTimeout(ctx context.Context, d time.Duration, fn func() error) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- fn() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
