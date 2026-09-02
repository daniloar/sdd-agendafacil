package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/agendafacil/agendafacil/internal/platform/config"
)

// setMinimalEnv deixa só DATABASE_URL setada (as demais chaves usam default).
func setMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	for _, k := range []string{
		"REDIS_URL",
		"ANTI_STARVATION_QUOTA", "ANTI_STARVATION_CEILING",
		"SOFT_LOCK_TTL", "OFFER_WINDOW", "CANCELLATION_PENALTY_WINDOW",
		"SYSTEM_JOB_INTERVAL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_SpineDefaults(t *testing.T) {
	setMinimalEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"SoftLockTTL", cfg.SoftLockTTL, 15 * time.Minute},
		{"OfferWindow", cfg.OfferWindow, 30 * time.Minute},
		{"CancellationPenaltyWindow", cfg.CancellationPenaltyWindow, 2 * time.Hour},
		{"AntiStarvationCeiling", cfg.AntiStarvationCeiling, 24 * time.Hour},
		{"AntiStarvationQuota", cfg.AntiStarvationQuota, 3},
		{"SystemJobInterval", cfg.SystemJobInterval, 10 * time.Second},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	if config.IdempotencyTTL != 24*time.Hour {
		t.Errorf("IdempotencyTTL = %v, want 24h", config.IdempotencyTTL)
	}
	if cfg.RedisURL != "" {
		t.Errorf("RedisURL = %q, want empty when REDIS_URL unset", cfg.RedisURL)
	}
}

func TestLoad_DatabaseURLRequired(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("DATABASE_URL", "")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("want DATABASE_URL required error, got %v", err)
	}
}

func TestLoad_RejectsBadValues(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantSub string
	}{
		{"unparseable duration", "SOFT_LOCK_TTL", "abc", "SOFT_LOCK_TTL"},
		{"non-positive duration", "SOFT_LOCK_TTL", "0s", "must be positive"},
		{"negative quota", "ANTI_STARVATION_QUOTA", "-1", "must be >= 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setMinimalEnv(t)
			t.Setenv(c.key, c.value)

			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("want error containing %q, got %v", c.wantSub, err)
			}
		})
	}
}
