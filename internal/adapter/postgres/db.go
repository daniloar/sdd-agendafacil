// Package postgres implementa as portas de persistência do domínio com pgx v5 e
// SQL cru (sem ORM). Toda mutação de estado é um compare-and-set otimista —
// UPDATE ... WHERE status = <esperado> com checagem de linhas afetadas, ou
// INSERT ... ON CONFLICT — nunca trava pessimista (AD-1, AD-2).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pingTimeout limita o Ping de verificação em NewPool quando o contexto recebido
// não traz deadline próprio.
const pingTimeout = 5 * time.Second

// Querier é o subconjunto do comportamento do pgx de que os repositórios
// precisam. Tanto *pgxpool.Pool quanto pgx.Tx o satisfazem, então um caso de uso
// da aplicação (Histórias 2+) pode passar sua transação envolvente e fazer a
// mutação, o evento de outbox e o registro de auditoria commitarem
// atomicamente.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewPool abre e verifica um pool de conexões pgx. Fechar é responsabilidade de
// quem chamou.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: new pool: %w", err)
	}

	pingCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		pingCtx, cancel = context.WithTimeout(ctx, pingTimeout)
		defer cancel()
	}
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}

// sqlStateUniqueViolation é o SQLSTATE do Postgres para violação de constraint
// de unicidade (ex.: uma segunda consulta confirmada para um mesmo slot).
const sqlStateUniqueViolation = "23505"

// isUniqueViolationOn informa se err é uma violação de unicidade do Postgres
// (SQLSTATE 23505) originada especificamente na constraint/índice de nome
// constraint. Um 23505 de qualquer outra constraint retorna false — quem chama
// deve tratá-lo como erro genérico de banco, não como o conflito de domínio
// esperado.
func isUniqueViolationOn(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == sqlStateUniqueViolation &&
		pgErr.ConstraintName == constraint
}
