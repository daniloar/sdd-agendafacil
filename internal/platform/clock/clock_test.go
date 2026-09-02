package clock_test

import (
	"testing"
	"time"

	"github.com/agendafacil/agendafacil/internal/domain"
	"github.com/agendafacil/agendafacil/internal/platform/clock"
)

func TestFakeClock_ReturnsSetInstantInUTC(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("clinic", -3*3600))
	c := clock.NewFakeClock(base)

	now := c.Now()
	if !now.Equal(base) {
		t.Fatalf("want instant %v, got %v", base, now)
	}
	if now.Location() != time.UTC {
		t.Fatalf("want UTC location, got %v", now.Location())
	}
}

func TestFakeClock_AdvanceIsDeterministic(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	c := clock.NewFakeClock(start)

	c.Advance(15 * time.Minute)
	c.Advance(45 * time.Minute)

	want := start.Add(time.Hour)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("want %v after advancing 1h, got %v", want, got)
	}
}

func TestFakeClock_SetRepositions(t *testing.T) {
	c := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	target := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC)

	c.Set(target)

	if got := c.Now(); !got.Equal(target) {
		t.Fatalf("want %v, got %v", target, got)
	}
}

func TestFakeClock_SatisfiesClockPort(t *testing.T) {
	var _ domain.Clock = clock.NewFakeClock(time.Now())
}

func TestSystemClock_ReturnsUTCWallClock(t *testing.T) {
	c := clock.NewSystemClock()

	before := time.Now().UTC()
	now := c.Now()
	after := time.Now().UTC()

	if now.Location() != time.UTC {
		t.Fatalf("want UTC location, got %v", now.Location())
	}
	if now.Before(before.Add(-time.Second)) || now.After(after.Add(time.Second)) {
		t.Fatalf("SystemClock.Now() %v outside [%v, %v]", now, before, after)
	}
}

func TestSystemClock_SatisfiesClockPort(t *testing.T) {
	var _ domain.Clock = clock.NewSystemClock()
}
