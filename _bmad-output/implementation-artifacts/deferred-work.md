# Deferred Work

- source_spec: `_bmad-output/specs/spec-agendamento/stories/1-schema-slot-and-appointment-state-machines-clock-port.md`
  summary: CAS repos should return a distinct not-found error (vs lost-race) once the HTTP layer needs 404 vs 409.
  evidence: SlotRepo/AppointmentRepo Transition return ErrSlotUnavailable/ErrAppointmentConflict for both a missing id and a failed status guard; the future HTTP layer (Story 2+) cannot pick the right status code.
- source_spec: `_bmad-output/specs/spec-agendamento/stories/1-schema-slot-and-appointment-state-machines-clock-port.md`
  summary: migrate.Down (drop all schema to version 0) is exported from a package imported by cmd/*; gate it behind a test-only build tag or move to a test helper.
  evidence: internal/platform/migrate/migrate.go exposes Down(); only TestMigrations_RoundTrip uses it, but nothing prevents production code from calling it.
- source_spec: `_bmad-output/specs/spec-agendamento/stories/1-schema-slot-and-appointment-state-machines-clock-port.md`
  summary: no ParseState/IsValid boundary for slot.State / appointment.State; add when the first DB read path lands (Story 6 discover).
  evidence: slot.State("garbage") is freely constructible; future repository reads will convert arbitrary status text from Postgres straight into a State with no rejection point.
- source_spec: `_bmad-output/specs/spec-agendamento/stories/1-schema-slot-and-appointment-state-machines-clock-port.md`
  summary: Clock port exposes only Now(); scheduled jobs and the outbox relay will need fakeable After/NewTicker/NewTimer.
  evidence: cmd/worker is described as the host for scheduled jobs; deterministic tests of job loops (Story 13/15) cannot control time with the current single-method port.
- source_spec: `_bmad-output/specs/spec-agendamento/stories/1-schema-slot-and-appointment-state-machines-clock-port.md`
  summary: audit_records immutability (AD-11 append-only) is not enforced at the DB layer; add a trigger/rule or privilege revoke when audit is wired in Story 4.
  evidence: migration 0005 comments audit_records as an immutable trail but adds no mechanism to block UPDATE/DELETE.
- source_spec: `_bmad-output/specs/spec-agendamento/stories/1-schema-slot-and-appointment-state-machines-clock-port.md`
  summary: acceptance criteria "no domain import of adapter/platform" and "no time.Now( in domain/app" have no automated test guard.
  evidence: both are only checked by manual grep in the spec Verification section; a domain -> internal/platform/config import would compile cleanly with nothing flagging it.
- source_spec: `_bmad-output/specs/spec-agendamento/stories/1-schema-slot-and-appointment-state-machines-clock-port.md`
  summary: cmd/api and cmd/worker boot path has no run() error seam beyond the shared boot helper and no direct tests.
  evidence: entrypoints are exercised only by hand; a table test over the boot helper (config load -> migrate -> version log, error branches) is worth adding once a second consumer needs it.
