<!-- bmad:context -->
<!-- Verified 2026-09-01 against aff4e661430e90b13d7b27fc316cda5a9286e0b6. Managed by bmad-project-context; edits inside this block are replaced on refresh. Keep anything you want preserved outside the markers. -->

## sdd-agendafacil

AgendaFácil — an API-only medical-appointment scheduling engine whose value is correctness under concurrency: no double-booking, fair waitlist advancement, every write idempotent. Greenfield — no code yet. Decided stack: Go + Echo, PostgreSQL, Redis, Docker. Planning artifacts live in `_bmad-output/planning-artifacts/`; the architecture is the next artifact and lands there.

## Policy

- Never resolve Slot-state concurrency with a pessimistic DB lock (`SELECT ... FOR UPDATE`) — this is a spec-level design restriction, not a preference. Use optimistic reservation instead: a version column or `UPDATE ... WHERE status = 'free'` with an affected-row check, a Redis-TTL soft lock, and a uniqueness constraint on (slot, confirmed appointment).

## Where things are

- PRD (final): `_bmad-output/planning-artifacts/prds/prd-sdd-agendafacil-2026-09-01/prd.md` — features, FR-1..FR-28, glossary, API surface.
- Brief + technical addendum: `_bmad-output/planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/` — `addendum.md` carries the stack and the architectural constraints verbatim from the user.
- Planning artifacts are managed by the BMad skills that produced them — prefer `bmad-prd`, `bmad-architecture`, and siblings over hand-editing those files.

## Running and verifying

- No build or test tooling exists yet. TODO once Go code lands: `go test ./...` for units; integration tests need Postgres and Redis up first (`docker compose up -d`). Verify and replace this line on the first refresh after code exists.

## Conventions that differ from defaults

- All persisted timestamps and all window arithmetic (15 min, 30 min, 2 h, D-1, 24 h) are UTC. Convert to the clinic timezone only at response serialization; no business rule reads the client timezone.
- Every mutating write requires an `Idempotency-Key` (UUID) header; replays within 24 h return the original response with no new side effect, and a missing key is a 400.
- Glossary terms in PRD §3 are canonical — downstream specs, code, and tests use them verbatim; introducing a synonym is a discipline violation.
- Planning docs are authored in Portuguese even though BMad `communication_language` is English.

<!-- /bmad:context -->
