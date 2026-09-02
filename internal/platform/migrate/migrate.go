// Package migrate aplica as migrações forward-only embutidas (golang-migrate).
// É invocado por cmd/api e cmd/worker no boot e pelos testes de integração do
// postgres. Todo acesso ao banco aqui passa pelo driver stdlib do pgx v5 — sem
// lib/pq.
package migrate

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registra o driver database/sql "pgx/v5"

	"github.com/agendafacil/agendafacil/migrations"
)

// sourceName e databaseName são rótulos opacos que o golang-migrate usa nas
// linhas de log.
const (
	sourceName   = "iofs"
	databaseName = "pgx5"
)

// LatestVersion é a versão de topo do schema — a contagem de arquivos *.up.sql
// embutidos. Assume numeração sequencial 1..N (convenção deste projeto), então
// um schema no head reporta esta versão. Os testes derivam a expectativa daqui
// em vez de hardcodar um número.
var LatestVersion = mustCountUpMigrations()

func mustCountUpMigrations() uint {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		panic(fmt.Sprintf("migrate: contar migrações embutidas: %v", err))
	}
	var n uint
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			n++
		}
	}
	if n == 0 {
		panic("migrate: nenhuma migração *.up.sql embutida")
	}
	return n
}

func newMigrate(databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: load embedded migrations: %w", err)
	}

	db, err := sql.Open("pgx/v5", databaseURL)
	if err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("migrate: open database: %w", err)
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		_ = src.Close()
		_ = db.Close()
		return nil, fmt.Errorf("migrate: init pgx driver: %w", err)
	}

	m, err := migrate.NewWithInstance(sourceName, src, databaseName, driver)
	if err != nil {
		_ = src.Close()
		_ = db.Close()
		return nil, fmt.Errorf("migrate: init migrator: %w", err)
	}
	return m, nil
}

// Up aplica toda migração pendente. É no-op (retorna nil) quando o schema já
// está no head.
func Up(databaseURL string) error {
	m, err := newMigrate(databaseURL)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Down reverte toda migração aplicada de volta à versão 0.
func Down(databaseURL string) error {
	m, err := newMigrate(databaseURL)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// Version reporta a versão atual do schema e se o banco está num estado dirty
// (falhou no meio). Um banco novo reporta versão 0, dirty false.
func Version(databaseURL string) (version uint, dirty bool, err error) {
	m, err := newMigrate(databaseURL)
	if err != nil {
		return 0, false, err
	}
	defer m.Close()

	v, d, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("migrate version: %w", err)
	}
	return v, d, nil
}
