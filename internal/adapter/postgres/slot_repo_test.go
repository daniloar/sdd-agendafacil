package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	pg "github.com/agendafacil/agendafacil/internal/adapter/postgres"
	"github.com/agendafacil/agendafacil/internal/domain"
	"github.com/agendafacil/agendafacil/internal/domain/appointment"
	"github.com/agendafacil/agendafacil/internal/domain/slot"
	"github.com/agendafacil/agendafacil/internal/platform/migrate"
)

var (
	testDBURL  string
	skipReason string
)

// coreTables é o conjunto completo que os critérios de aceite exigem existir no
// head e desaparecer após o `down` até a versão 0.
var coreTables = []string{
	"doctors", "slots", "appointments", "soft_locks",
	"waitlist_entries", "waitlist_vip_counters", "offers",
	"idempotency_keys", "slot_events", "audit_records", "cancellation_penalties",
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	if url := os.Getenv("DATABASE_URL"); url != "" {
		testDBURL = url
		os.Exit(m.Run())
	}

	container, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("agendafacil"),
		tcpostgres.WithUsername("agendafacil"),
		tcpostgres.WithPassword("agendafacil"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		skipReason = fmt.Sprintf("testcontainers unavailable and DATABASE_URL unset: %v", err)
		os.Exit(m.Run())
	}

	testDBURL, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		skipReason = fmt.Sprintf("connection string: %v", err)
		os.Exit(m.Run())
	}

	code := m.Run()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

func requireDB(t *testing.T) {
	t.Helper()
	if skipReason != "" {
		t.Skip(skipReason)
	}
}

// freshSchema reverte o schema para 0 e reaplica toda migração, para que cada
// teste comece de um banco limpo e idêntico.
func freshSchema(t *testing.T) {
	t.Helper()
	if err := migrate.Down(testDBURL); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := migrate.Up(testDBURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
}

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pg.NewPool(ctx, testDBURL)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedSlot(t *testing.T, pool *pgxpool.Pool, status slot.State) (doctorID, slotID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	doctorID = uuid.Must(uuid.NewV7())
	slotID = uuid.Must(uuid.NewV7())

	if _, err := pool.Exec(ctx,
		`INSERT INTO doctors (id, name, specialty, reference_value, currency)
		 VALUES ($1, 'Dr. Test', 'clinico', 200, 'BRL')`, doctorID); err != nil {
		t.Fatalf("seed doctor: %v", err)
	}
	start := time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx,
		`INSERT INTO slots (id, doctor_id, starts_at, ends_at, status, reference_value, currency)
		 VALUES ($1, $2, $3, $4, $5, 200, 'BRL')`,
		slotID, doctorID, start, start.Add(30*time.Minute), string(status)); err != nil {
		t.Fatalf("seed slot: %v", err)
	}
	return doctorID, slotID
}

func slotStatus(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) slot.State {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM slots WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read slot status: %v", err)
	}
	return slot.State(s)
}

func appointmentStatus(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) appointment.State {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM appointments WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read appointment status: %v", err)
	}
	return appointment.State(s)
}

// seedConfirmedAppointment semeia um slot confirmado e uma consulta confirmada
// nele, devolvendo o id da consulta.
func seedConfirmedAppointment(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	doctorID, slotID := seedSlot(t, pool, slot.Confirmado)
	apptID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO appointments (id, slot_id, patient_id, doctor_id, status)
		 VALUES ($1, $2, $3, $4, 'confirmada')`,
		apptID, slotID, uuid.Must(uuid.NewV7()), doctorID); err != nil {
		t.Fatalf("seed confirmed appointment: %v", err)
	}
	return apptID
}

func TestMigrations_RoundTrip(t *testing.T) {
	requireDB(t)

	// Deixa o schema no head para os testes seguintes, mesmo se este falhar no
	// meio (o corpo termina com um `down` deliberado).
	t.Cleanup(func() {
		if err := migrate.Up(testDBURL); err != nil {
			t.Errorf("restore schema: %v", err)
		}
	})

	freshSchema(t)

	version, dirty, err := migrate.Version(testDBURL)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version != migrate.LatestVersion || dirty {
		t.Fatalf("want schema at version %d dirty=false, got version=%d dirty=%t",
			migrate.LatestVersion, version, dirty)
	}

	pool := newPool(t)
	ctx := context.Background()

	for _, table := range coreTables {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("core table %s missing after up", table)
		}
	}

	for _, idx := range []string{"uq_appt_confirmed_per_slot", "uq_offer_pending_per_slot"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes
			 WHERE schemaname = 'public' AND indexname = $1)`, idx).Scan(&exists); err != nil {
			t.Fatalf("check index %s: %v", idx, err)
		}
		if !exists {
			t.Fatalf("partial unique index %s missing after up", idx)
		}
	}

	if err := migrate.Down(testDBURL); err != nil {
		t.Fatalf("down: %v", err)
	}
	for _, table := range coreTables {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s after down: %v", table, err)
		}
		if exists {
			t.Fatalf("core table %s still present after down to 0", table)
		}
	}
}

func TestSlotRepo_Transition_CASWinnerAndLoser(t *testing.T) {
	requireDB(t)
	freshSchema(t)
	pool := newPool(t)
	ctx := context.Background()

	_, slotID := seedSlot(t, pool, slot.Livre)
	repo := pg.NewSlotRepo(pool)

	if err := repo.Transition(ctx, slotID, slot.Livre, slot.Reservado); err != nil {
		t.Fatalf("winner: want nil, got %v", err)
	}
	if got := slotStatus(t, pool, slotID); got != slot.Reservado {
		t.Fatalf("winner: want status reservado, got %q", got)
	}

	err := repo.Transition(ctx, slotID, slot.Livre, slot.Reservado)
	if !errors.Is(err, domain.ErrSlotUnavailable) {
		t.Fatalf("loser: want ErrSlotUnavailable, got %v", err)
	}
	if got := slotStatus(t, pool, slotID); got != slot.Reservado {
		t.Fatalf("loser: row changed, status is %q", got)
	}
}

func TestAppointmentRepo_Transition_CASWinnerAndLoser(t *testing.T) {
	requireDB(t)
	freshSchema(t)
	pool := newPool(t)
	ctx := context.Background()

	apptID := seedConfirmedAppointment(t, pool)
	repo := pg.NewAppointmentRepo(pool)

	// Vencedor: confirmada -> no_show aplica em exatamente uma linha.
	if err := repo.Transition(ctx, apptID, appointment.Confirmada, appointment.NoShow); err != nil {
		t.Fatalf("winner: want nil, got %v", err)
	}
	if got := appointmentStatus(t, pool, apptID); got != appointment.NoShow {
		t.Fatalf("winner: want status no_show, got %q", got)
	}

	// Perdedor: a chamada idêntica repetida encontra a linha já em no_show, o
	// CAS afeta zero linhas -> ErrAppointmentConflict; nada de colisão de índice
	// único (o alvo é no_show, não confirmada); a linha não muda.
	err := repo.Transition(ctx, apptID, appointment.Confirmada, appointment.NoShow)
	if !errors.Is(err, domain.ErrAppointmentConflict) {
		t.Fatalf("loser: want ErrAppointmentConflict, got %v", err)
	}
	if got := appointmentStatus(t, pool, apptID); got != appointment.NoShow {
		t.Fatalf("loser: row changed, status is %q", got)
	}
}

func TestSlotRepo_Transition_IllegalPairIssuesNoSQL(t *testing.T) {
	requireDB(t)
	freshSchema(t)
	pool := newPool(t)
	ctx := context.Background()

	_, slotID := seedSlot(t, pool, slot.Livre)
	repo := pg.NewSlotRepo(pool)

	err := repo.Transition(ctx, slotID, slot.Livre, slot.Confirmado)
	if !errors.Is(err, domain.ErrIllegalTransition) {
		t.Fatalf("want ErrIllegalTransition, got %v", err)
	}
	if got := slotStatus(t, pool, slotID); got != slot.Livre {
		t.Fatalf("row must be untouched, status is %q", got)
	}
}

func TestAppointments_PartialUniqueRejectsSecondConfirmed(t *testing.T) {
	requireDB(t)
	freshSchema(t)
	pool := newPool(t)
	ctx := context.Background()

	doctorID, slotID := seedSlot(t, pool, slot.Confirmado)

	insertAppt := func(status string) error {
		_, err := pool.Exec(ctx,
			`INSERT INTO appointments (id, slot_id, patient_id, doctor_id, status)
			 VALUES ($1, $2, $3, $4, $5)`,
			uuid.Must(uuid.NewV7()), slotID, uuid.Must(uuid.NewV7()), doctorID, status)
		return err
	}

	if err := insertAppt("confirmada"); err != nil {
		t.Fatalf("first confirmed insert: %v", err)
	}
	err := insertAppt("confirmada")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want 23505 unique violation, got %v", err)
	}
}

func TestAppointmentRepo_Transition_MapsUniqueViolation(t *testing.T) {
	requireDB(t)
	freshSchema(t)
	pool := newPool(t)
	ctx := context.Background()

	doctorID, slotID := seedSlot(t, pool, slot.Confirmado)

	confirmedID := uuid.Must(uuid.NewV7())
	noShowID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO appointments (id, slot_id, patient_id, doctor_id, status)
		 VALUES ($1, $2, $3, $4, 'confirmada')`,
		confirmedID, slotID, uuid.Must(uuid.NewV7()), doctorID); err != nil {
		t.Fatalf("seed confirmed appt: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO appointments (id, slot_id, patient_id, doctor_id, status)
		 VALUES ($1, $2, $3, $4, 'no_show')`,
		noShowID, slotID, uuid.Must(uuid.NewV7()), doctorID); err != nil {
		t.Fatalf("seed no_show appt: %v", err)
	}

	repo := pg.NewAppointmentRepo(pool)
	err := repo.Transition(ctx, noShowID, appointment.NoShow, appointment.Confirmada)
	if !errors.Is(err, domain.ErrAppointmentConflict) {
		t.Fatalf("want ErrAppointmentConflict from mapped 23505, got %v", err)
	}
}

func TestOffers_PartialUniqueRejectsSecondPending(t *testing.T) {
	requireDB(t)
	freshSchema(t)
	pool := newPool(t)
	ctx := context.Background()

	_, slotID := seedSlot(t, pool, slot.Livre)

	newWaitlistEntry := func() uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		if _, err := pool.Exec(ctx,
			`INSERT INTO waitlist_entries (id, patient_id, scope, slot_id, class, status)
			 VALUES ($1, $2, $3, $4, 'comum', 'aguardando')`,
			id, uuid.Must(uuid.NewV7()), "slot:"+slotID.String(), slotID); err != nil {
			t.Fatalf("seed waitlist entry: %v", err)
		}
		return id
	}

	insertOffer := func(status string) error {
		_, err := pool.Exec(ctx,
			`INSERT INTO offers (id, slot_id, waitlist_entry_id, patient_id, status)
			 VALUES ($1, $2, $3, $4, $5)`,
			uuid.Must(uuid.NewV7()), slotID, newWaitlistEntry(), uuid.Must(uuid.NewV7()), status)
		return err
	}

	if err := insertOffer("pendente"); err != nil {
		t.Fatalf("first pending offer: %v", err)
	}
	err := insertOffer("pendente")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want 23505 unique violation on uq_offer_pending_per_slot, got %v", err)
	}
}

func TestSlotRepo_Transition_DeliberateContention(t *testing.T) {
	requireDB(t)
	freshSchema(t)
	pool := newPool(t)
	ctx := context.Background()

	_, slotID := seedSlot(t, pool, slot.Livre)
	repo := pg.NewSlotRepo(pool)

	const goroutines = 50
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		winners    int
		losers     int
		unexpected []error
		start      = make(chan struct{})
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := repo.Transition(ctx, slotID, slot.Livre, slot.Reservado)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners++
			case errors.Is(err, domain.ErrSlotUnavailable):
				losers++
			default:
				unexpected = append(unexpected, err)
			}
		}()
	}

	close(start)
	wg.Wait()

	if len(unexpected) > 0 {
		t.Fatalf("unexpected errors: %v", unexpected)
	}
	if winners != 1 {
		t.Fatalf("want exactly 1 winner, got %d", winners)
	}
	if losers != goroutines-1 {
		t.Fatalf("want %d losers, got %d", goroutines-1, losers)
	}
	if got := slotStatus(t, pool, slotID); got != slot.Reservado {
		t.Fatalf("want final status reservado, got %q", got)
	}
}
