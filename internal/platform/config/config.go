// Package config carrega a configuração de ambiente 12-factor. A História 1
// precisa da URL do banco; as demais chaves do spine são plugadas aqui com seus
// defaults documentados para que histórias posteriores as consumam sem tocar
// neste arquivo.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// IdempotencyTTL é fixo em 24h no MVP (Consistency Conventions do spine —
// "IDEMPOTENCY_TTL (24h, fixo)"); deliberadamente não é ajustável por ambiente.
const IdempotencyTTL = 24 * time.Hour

// Config é a configuração do processo já resolvida.
//
// DatabaseURL é obrigatória — não há default com credencial embutida. RedisURL é
// opcional (string vazia quando REDIS_URL não está setada); nenhuma credencial
// default é assumida, e nenhum código da História 1 a consome.
type Config struct {
	DatabaseURL string
	RedisURL    string

	AntiStarvationQuota       int           // ANTI_STARVATION_QUOTA (>= 0)
	AntiStarvationCeiling     time.Duration // ANTI_STARVATION_CEILING (> 0)
	SoftLockTTL               time.Duration // SOFT_LOCK_TTL (> 0)
	OfferWindow               time.Duration // OFFER_WINDOW (> 0)
	CancellationPenaltyWindow time.Duration // CANCELLATION_PENALTY_WINDOW (> 0)
	SystemJobInterval         time.Duration // SYSTEM_JOB_INTERVAL (> 0)
}

// Load lê a configuração do ambiente, aplicando os defaults do spine para
// qualquer variável não setada. Retorna erro quando DATABASE_URL está ausente ou
// vazia, ou quando uma variável setada não pode ser parseada ou fere sua
// restrição de faixa (durações devem ser > 0; a quota deve ser >= 0).
func Load() (Config, error) {
	databaseURL, ok := os.LookupEnv("DATABASE_URL")
	if !ok || databaseURL == "" {
		return Config{}, errors.New("config: DATABASE_URL is required")
	}

	cfg := Config{
		DatabaseURL: databaseURL,
		RedisURL:    os.Getenv("REDIS_URL"), // opcional; "" quando não setada
	}

	var err error
	if cfg.AntiStarvationQuota, err = getInt("ANTI_STARVATION_QUOTA", 3); err != nil {
		return Config{}, err
	}
	if cfg.AntiStarvationCeiling, err = getDuration("ANTI_STARVATION_CEILING", 24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.SoftLockTTL, err = getDuration("SOFT_LOCK_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.OfferWindow, err = getDuration("OFFER_WINDOW", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.CancellationPenaltyWindow, err = getDuration("CANCELLATION_PENALTY_WINDOW", 2*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.SystemJobInterval, err = getDuration("SYSTEM_JOB_INTERVAL", 10*time.Second); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// getInt lê uma variável inteira. Ausente/vazia -> def. Não-parseável ou
// negativa -> erro.
func getInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("config: %s must be >= 0", key)
	}
	return n, nil
}

// getDuration lê uma variável de duração (formato time.ParseDuration). Ausente/
// vazia -> def. Não-parseável ou <= 0 -> erro.
func getDuration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config: %s must be positive", key)
	}
	return d, nil
}
