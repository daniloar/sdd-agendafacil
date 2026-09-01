---
id: SPEC-agendamento
companions:
  - ../../planning-artifacts/architecture/architecture-sdd-agendafacil-2026-09-01/ARCHITECTURE-SPINE.md
sources:
  - ../../planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/brief.md
  - ../../planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/addendum.md
  - ../../planning-artifacts/architecture/architecture-sdd-agendafacil-2026-09-01/SOLUTION-DESIGN.md
---

> **Canonical contract.** This SPEC and the files in `companions:` are the complete, preservation-validated contract for what to build, test, and validate. Source documents listed in frontmatter are for traceability — consult them only if you need narrative rationale or prose color this contract intentionally omits.
>
> The companion `ARCHITECTURE-SPINE.md` carries the binding architecture decisions (AD-1…AD-9), the slot state machine, the data-model names, and the consistency conventions. Every capability below cites the ADs that govern it; implement against both files together.

# AgendaFácil — Scheduling Core (Epic: Agendamento)

## Why

A pain to solve, framed as an engineering mandate. Appointment scheduling is trivial with one active user and breaks the moment there are two: two patients confirm the same slot in the same second and both leave booked (double-booking, discovered at the front desk); a cancelled slot sits idle while patients wait because nobody is offered it in time; last-minute fit-ins and VIP priority let an ordinary patient wait forever; a no-show burns a slot with no early signal; an unstable network turns one "confirm" click into two appointments or two penalties. The clinic-side simple fix — a pessimistic row lock held while the patient decides — serializes care and does not scale. This epic builds the concurrent scheduling engine that is the whole product: optimistic reservation, reliable expiry, a fair and auditable queue, idempotent writes, UTC time throughout. Affected: patients (want a slot that is certainly theirs and a fair queue), doctors (want the day's agenda to match reality), and the System actor (runs expiry, queue advance, and reminders unattended). It matters now because getting consistency right first is what makes every deferred module — payment, teleconsult, EHR — an increment instead of a rewrite.

## Capabilities

- **CAP-1 — Discover published agenda**
  - **intent:** A patient can browse a doctor's published slots for a date and see each slot's availability so they can pick one to reserve.
  - **success:** A slot in `held` or `confirmed` state renders as unavailable; a `held` slot whose `held_until` is in the past renders as available (lazy-expiry read semantics, AD-2); listing is read-only and opens no write transaction.

- **CAP-2 — Soft-lock a free slot (15 min)**
  - **intent:** A patient can place a 15-minute hold on a `free` slot so they have exclusive time to confirm without another patient taking it.
  - **success:** `free→held` succeeds only via `UPDATE slots … WHERE slot_id=? AND status='free'` with `rows_affected=1`; a second concurrent attempt gets `rows_affected=0` and a `slot_unavailable` domain error; the winning transaction sets `held_by` and `held_until = now()+15m` (UTC) and writes `slot:<id>:hold` to Redis with `EX 900`; a `slot.held` event lands in the outbox in the same transaction. Governed by AD-2, AD-6, AD-7, AD-8.

- **CAP-3 — Confirm a held slot into an appointment**
  - **intent:** The patient holding a slot can confirm it to create the appointment, and repeating the confirmation never creates a second appointment.
  - **success:** Confirmation is `held→confirmed` via CAS guarded by `held_by=? AND held_until > now()`; expiry at the confirm instant yields `rows_affected=0`, a `slot_unavailable` error, and an offer to join the waitlist (CAP-6); the partial unique index on `slot_id WHERE status='confirmed'` rejects any second confirmed row even if application CAS is bypassed; a replay of the same `Idempotency-Key` + fingerprint returns the first response with no new appointment. Governed by AD-1, AD-2, AD-5, AD-7, AD-8.

- **CAP-4 — Expire an abandoned soft lock**
  - **intent:** A hold that is not confirmed within its window returns to the pool so the slot can be reserved again.
  - **success:** Any write-path use case that reads `status='held' AND held_until < now()` performs the `held→free` CAS (via the single transition method, emitting the freed event to the outbox in the same transaction) before proceeding; the System worker performs the same transition proactively off the Redis TTL; a read-only path that cannot open a write transaction treats the slot as free for display only and does not write. Governed by AD-2, AD-6, AD-7, AD-8.

- **CAP-5 — Cancel a confirmed appointment**
  - **intent:** A patient can cancel a confirmed appointment; cancelling less than 2 hours before the start records a 50% penalty as owed.
  - **success:** `confirmed→cancelled` CAS succeeds once; when `start - now() < 2h` (UTC, via `Clock`) a `cancellation_penalty` row is written in the same transaction with a concrete `amount` = 50% of the slot's `consultation_price` and its `currency`; the slot then transitions `cancelled→free` and emits `slot.cancelled`, which drives waitlist advance (CAP-7); a replayed cancel returns the first result and writes no second penalty. No charge or collection occurs. Governed by AD-5, AD-6, AD-7, AD-8.

- **CAP-6 — Join the waitlist for a doctor-day**
  - **intent:** When no `free` slot is available, a patient can join the waitlist for a given doctor on a given date to be offered the next opening.
  - **success:** A `waitlist_entry` row (authority) is written in the use-case transaction with `doctor_id`, the UTC calendar `date`, `enqueued_at`, and `is_vip`; the Redis sorted set `waitlist:<doctor_id>:<yyyy-mm-dd>` gets a best-effort `ZADD` with the domain-computed score; a replayed join with the same `Idempotency-Key` adds no second entry. Governed by AD-3, AD-4, AD-5, AD-9.

- **CAP-7 — Advance the waitlist and make a 30-minute offer**
  - **intent:** When a slot for a doctor-day is freed, the System offers it to the next eligible waitlisted patient, naming that slot, with a bounded window to accept before moving on.
  - **success:** On a doctor-day slot becoming free (cancel, expiry, or reopen), the next entry by ascending score is selected; an `offer:<doctor_id>:<yyyy-mm-dd>` key is set with `EX 1800` **and** a `waitlist_offer` row is written for audit and idempotency; the offer payload carries the concrete `slot_id`; the 30-minute clock starts at notification **sent**; on key expiry the System advances to the next eligible entry; acceptance runs the CAP-3 confirm path against the named slot. Governed by AD-3, AD-4, AD-5, AD-8, AD-9.

- **CAP-8 — VIP priority with bounded anti-starvation**
  - **intent:** VIP patients are served ahead of ordinary patients in the same queue, but an ordinary patient's wait is bounded regardless of VIP inflow.
  - **success:** Ordering uses one sorted set per doctor-day with the pure domain score `score = enqueued_at_unix_ms − VIP_BOOST − age_promotion(waited)`; with defaults `VIP_BOOST=24h` and `AGE_PROMOTION_CEILING=48h` (both 12-factor env vars), a common entry that has waited past the ceiling has a lower score than a VIP that just enqueued; re-score is an idempotent `ZADD`; there is no separate VIP queue. Governed by AD-4.

- **CAP-9 — D-1 presence reminder with two-stage risk/release**
  - **intent:** One day before the appointment the patient gets a reminder with a presence-confirmation link; non-confirmation is an early no-show signal that first flags risk and later frees the slot.
  - **success:** The System sends the reminder ~24h before start (deterministic idempotency key `reminder:<appointment_id>:D-1`); if the patient has not confirmed presence by the D-1 cutoff, `appointment.risk` is set to `high` and the slot stays `confirmed` with no waitlist trigger; if still unconfirmed at the T-2h cutoff, the slot is released toward `free` (driving CAP-7) with **no** patient penalty. Governed by AD-5, AD-6, AD-7, AD-8.

- **CAP-10 — Idempotency envelope on every mutation**
  - **intent:** Any write can be safely retried by a client or an automation and produce exactly one effect.
  - **success:** Every mutating `POST`/`PUT` requires an `Idempotency-Key` (UUID) header — absent → `400`; the key row is inserted `ON CONFLICT DO NOTHING` with a rows-affected check **in the same transaction** as the domain mutation, and the response status/body are stored; same key + same `request_fingerprint` replays the stored response with zero side effects; same key + different fingerprint → `409`; keys expire after 24h and a later retry is treated as new. Governed by AD-5.

- **CAP-11 — Domain events via transactional outbox**
  - **intent:** Every slot state change is durably recorded and delivered to internal consumers without coupling the scheduler to them by direct call.
  - **success:** Each transition writes a past-tense event (`slot.held`, `slot.confirmed`, `slot.cancelled`, `waitlist.offer_made`, `waitlist.offer_expired`, …) to `slot_events` in the mutation's transaction; a relay publishes outbox rows to a Redis stream; consumers (waitlist advance, reminder scheduler, penalty recorder, audit) are idempotent (CAP-10 discipline) and reprocess-safe. Governed by AD-8.

- **CAP-12 — Waitlist reconciliation from the authority table**
  - **intent:** The queue survives a Redis restart or drift because it can be rebuilt entirely from Postgres.
  - **success:** On worker boot and periodically, the sorted set for each active doctor-day is reprojected from `waitlist_entry` (idempotent); "next eligible" reads Redis and, on miss or detected inconsistency, falls back to `SELECT … ORDER BY score` on Postgres. Governed by AD-9.

## Constraints

- **No pessimistic database locks on the decision path.** No `SELECT … FOR UPDATE`, no blocking advisory lock held across a patient's decision. Every state mutation is CAS: `UPDATE … WHERE status = <expected>` or `INSERT … ON CONFLICT`, always with a rows-affected check; zero rows affected = lost the race = domain error.
- **PostgreSQL is the canonical transactional store; Redis is an accelerator only.** "At most one `confirmed` appointment per slot" is a Postgres partial unique index, not an application check. On any divergence Postgres wins — `held_until` for the lock, `waitlist_entry` for the queue.
- **Every API mutation and every System automation is idempotent.** `Idempotency-Key` (UUID) is mandatory on mutating `POST`/`PUT`; the key and its response are persisted in the same transaction as the domain mutation. System automations derive a deterministic key from `(type, entity, window)`.
- **All time is UTC internally.** Every persisted timestamp is `timestamptz` in UTC. Every window (15 min, 30 min, 2 h, D-1, T-2h) is computed in the domain through an injectable `Clock` port, never from request wall-clock. Timezone conversion happens only at the HTTP edge serializer.
- **Slot state is one explicit machine with one writer.** States `free | held | confirmed | cancelled`; all transitions go through a single `Slot.transition(to, guard)` domain method that emits the CAS SQL. Only the scheduling use case writes `slots.status` — no read path, queue worker, or event consumer writes it directly.
- **Every state transition emits a domain event to the Postgres outbox in the same transaction.** Consumers are idempotent and replay-safe. No automation is coupled to another by direct call.
- **Waitlist authority is the Postgres `waitlist_entry` table.** The Redis sorted set is a derived projection, fully rebuildable from the table. Every enqueue/dequeue writes the table in the use-case transaction and does a best-effort `ZADD`/`ZREM`.
- **Waitlist is keyed per `doctor_id` + UTC calendar date.** One queue per doctor-day: `waitlist:<doctor_id>:<yyyy-mm-dd>`. Any freed slot of that doctor on that date is offered to that one queue, and the offer names the concrete `slot_id`.
- **The penalty is recorded as owed, never charged.** `cancellation_penalty` stores a concrete `amount` (50% of the slot's `consultation_price`) and `currency`; no payment, invoicing, or collection path exists in this epic.
- **Hexagonal boundaries.** The domain declares ports; adapters (`http`, `postgres`, `redis`, `worker`) implement them. The domain imports nothing from `adapter`/`platform`. The System-actor worker invokes the same application use cases as the HTTP API — no automation has its own write path.

## Non-goals

- Payment, billing, invoicing, or collection of the cancellation penalty.
- Teleconsultation and electronic health records.
- Rich frontend or native app — the deliverable is the API; a thin client or nothing.
- Doctor registration and availability management beyond seed/config (including how `consultation_price` is populated, pending the open question).
- Multi-tenant support / multiple clinic units.
- A message broker (RabbitMQ/Kafka) between services — the outbox + Redis stream is the whole transport until a second service consumes domain events.
- Authentication and identity — patient identity is assumed resolved by upstream middleware.
- Soft-lock renewal (a patient asking for more time) — not in v1.
- Detailed observability design (metrics, tracing, alerting).
- Deployment beyond Docker Compose (Kubernetes, autoscaling, managed-service topology).

## Success signal

Under a deliberate-contention load test — many patients racing the same slots and the same doctor-days, clients retrying every write, VIPs entering mid-window, locks expiring at the exact confirm instant — the system produces **zero double-booked slots** (no slot with two `confirmed` appointments), **every repeated write returns its first result** with no duplicate appointment, penalty, or queue entry, **every freed slot is offered to the next eligible patient within seconds** and reused inside the 30-minute window at the target rate, and **no ordinary patient's queue wait exceeds `AGE_PROMOTION_CEILING`** even under continuous VIP inflow. All five time windows (15 min, 30 min, 2 h, D-1, T-2h) compute correctly regardless of the client's timezone. A doctor arriving for the day finds the agenda matches reality.

## Assumptions

- No PRD exists; NFRs (expected load, latency targets, RTO/RPO, slot volume) are unformalized. Deployment context assumed "one small clinic or network".
- The second D-1 release cutoff is **T-2h before appointment start** (mirrors the <2h cancellation boundary); a tunable parameter.
- The 30-minute waitlist accept window counts from notification **sent**, not delivered (AD-3).
- `Idempotency-Key` TTL is 24h; a retry after expiry is treated as a new request (AD-5).
- The D-1 reminder dispatches ~24h before appointment start; the exact wall-clock dispatch time is a worker schedule parameter.
- The slot state machine permits `cancelled→free` reopening so a cancelled slot re-enters the waitlist flow (AD-7).
- IDs are UUIDv7; the concurrency discriminator is `status` + timestamps, not a numeric `version` column (AD-2, conventions).
- Redis runs with `appendonly yes` (AOF); tolerable loss on restart is the last second (AD-3).
- The soft lock is not renewable in v1.
- `consultation_price` is carried on the slot (a doctor-level default is acceptable) and seeded/configured, since payment is out of scope.

## Open Questions

- **NFRs (D6):** expected concurrent load, latency targets, RTO/RPO, and slot volume are undefined without a PRD — needed to size Postgres/Redis and choose a deployment strategy.
- **`consultation_price` seeding:** is there an admin surface in this epic to set slot/doctor price, or is it purely DB seed/config? The brief assumes minimal admin.
- **D-1 dispatch time:** the exact wall-clock hour the reminder job fires relative to the 24h-before mark.
- **Outbox relay retry/backoff policy:** left to the spec by the spine (Deferred); still unspecified.
