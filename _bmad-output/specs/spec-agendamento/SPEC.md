---
id: SPEC-agendamento
companions:
  - ../../planning-artifacts/architecture/architecture-sdd-agendafacil-2026-09-01/ARCHITECTURE-SPINE.md
  - ../../planning-artifacts/prds/prd-sdd-agendafacil-2026-09-01/prd.md
sources:
  - ../../planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/brief.md
  - ../../planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/addendum.md
  - ../../planning-artifacts/architecture/architecture-sdd-agendafacil-2026-09-01/SOLUTION-DESIGN.md
---

> **Canonical contract.** This SPEC and the files in `companions:` are the complete, preservation-validated contract for what to build, test, and validate. Source documents in frontmatter are traceability only.
>
> Read the two companions alongside this file: `prd.md` carries every FR's testable consequences, the glossary (§3), the API surface (§11) and the success metrics (§7); `ARCHITECTURE-SPINE.md` carries the binding architecture decisions `AD-1..AD-11`, the slot and appointment state machines, and the consistency conventions. Capabilities below cite `FR-*` (PRD) and `AD-*` (spine); implement against all three together.

# AgendaFácil — Scheduling Core (Epic: Agendamento)

## Why

A pain to solve, framed as an engineering mandate. Appointment scheduling is trivial with one active user and breaks the moment there are two: two patients confirm the same slot in the same second and both leave booked; a cancelled slot sits idle while patients wait; VIP priority lets an ordinary patient wait forever; a no-show burns a slot with no early signal; an unstable network turns one "confirm" click into two appointments or two penalties. The clinic-side simple fix — a pessimistic row lock held while the patient decides — serializes care and does not scale. This epic is the whole product: the concurrent scheduling engine — schedule publication, optimistic reservation, reliable expiry, a fair and auditable two-scope queue, idempotent writes, a full no-show lifecycle, an immutable audit trail, UTC time throughout. Affected: patients (want a slot that is certainly theirs and a fair queue), doctors (want the day's agenda to match reality plus an early no-show signal), the Administrator (publishes doctors and slots), and the System actor (runs expiry, queue advance, reminders and no-show marking unattended). It matters now because getting consistency and auditability right first is what makes every deferred module — payment, teleconsult, EHR — an increment instead of a rewrite. Numeric targets exist to be verified in a contention test, not to describe production capacity.

## Capabilities

- **CAP-1 — Discover published agenda** (FR-4)
  - **intent:** A patient can list a doctor's slots for a date range and see each slot's availability so they can pick one to reserve.
  - **success:** Only `livre` slots show as available; `reservado`/`confirmado`/`em_risco`/`bloqueado` show unavailable or are omitted when the request passes `only=available`; times are UTC with the clinic offset attached as presentation metadata; a slot whose soft lock has expired shows available again with no patient action (lazy read semantics, AD-2). Read-only — no write transaction.

- **CAP-2 — Soft-lock a free slot** (FR-5)
  - **intent:** A patient can place a 15-minute hold on a `livre` slot so they have exclusive time to confirm.
  - **success:** Under K concurrent requests for the same `livre` slot (K up to 50) **exactly one** creates the hold via `UPDATE ... WHERE slot_id=? AND status='livre'` with `rows_affected=1`; the rest get `409`; the winner sets `held_by` and `held_until = now()+15m` (UTC) and writes `slot:<id>:hold` to Redis `EX 900`; locking a non-`livre` slot → `409`; a patient may hold at most **1 active soft lock per doctor+date** (`409` beyond); no code path takes a pessimistic row lock. Governed by AD-2, AD-6, AD-7, AD-8, AD-11.

- **CAP-3 — Confirm a held slot into an appointment** (FR-7)
  - **intent:** The patient holding a slot can confirm it to create the appointment, and repeating the confirmation never creates a second one.
  - **success:** `reservado→confirmado` via CAS guarded by `held_by=? AND held_until > now()`; expiry at the confirm instant → `409` + an offer to join the waitlist; the partial unique index on `slot_id WHERE appointment.status='confirmada'` rejects any second confirmed appointment even if application CAS is bypassed; replaying the same `Idempotency-Key` returns the first appointment unchanged; repeating with a *different* key when the patient already has an appointment on that slot returns the existing appointment, not an error. Governed by AD-1, AD-2, AD-5, AD-7, AD-10, AD-11.

- **CAP-4 — Expire an abandoned soft lock** (FR-6)
  - **intent:** A hold not confirmed within its window returns to the pool so the slot can be reserved again.
  - **success:** Any write-path use case that reads `status='reservado' AND held_until < now()` performs the `reservado→livre` CAS (via the single transition method, emitting the outbox event and audit record in the same transaction) before proceeding; the System worker does the same proactively off the Redis TTL; a read-only path that cannot open a write transaction treats the slot as free for display only; the delay between expiry and effective release is ≤ 30 s (SM-5). Governed by AD-2, AD-6, AD-7, AD-8, AD-11.

- **CAP-5 — Cancel a confirmed appointment with late penalty** (FR-8, FR-9)
  - **intent:** A patient can cancel a confirmed appointment; cancelling less than 2 hours before the slot start records a 50% penalty as owed.
  - **success:** `confirmado→cancelado` CAS succeeds once; when `start - now() < 2h` (UTC, via `Clock`) a `cancellation_penalty` row is written in the same transaction with `amount` = 50% of the slot's Valor de Referência (fixed at slot creation) and its currency; the slot then goes `livre` and emits `slot.cancelled`, driving waitlist advance (CAP-7); cancelling ≥ 2h ahead writes no penalty; an appointment produces at most one penalty under concurrent/repeated cancels; cancelling a `concluida`/`no_show` appointment → `409`. No charge or collection. Governed by AD-5, AD-6, AD-7, AD-8, AD-10, AD-11.

- **CAP-6 — Join a waitlist (per-slot and per-doctor+date)** (FR-10, FR-15)
  - **intent:** When no `livre` slot is available, a patient can join the waitlist for a specific slot and/or for any slot of a doctor on a date, to be offered the next opening.
  - **success:** A `waitlist_entry` row (authority) is written in the use-case transaction with `scope` (`slot:<id>` or `doctor:<id>:<yyyy-mm-dd>` UTC), `enqueued_at`, `class`; a best-effort `ZADD` mirrors it to the matching Redis sorted set; a patient appears at most once per queue (replay / same `Idempotency-Key` returns the existing position); joining the waitlist of a slot where the patient already has a `confirmada` appointment → `409`. Governed by AD-3, AD-4, AD-5, AD-9.

- **CAP-7 — Advance the waitlist and make a 30-minute offer** (FR-12, FR-13)
  - **intent:** When a slot for a doctor-day is freed, the System offers it — naming that slot — to the next eligible waitlisted patient across both queue scopes, with a bounded window to accept before moving on.
  - **success:** On a slot becoming `livre` (cancel FR-8, expiry FR-6, no-show FR-22) the next entry by the CAP-8 ordering over the **merged** slot-scope + doctor+date-scope candidates is selected; `offer:<slot_id>` is set `EX 1800` and a `offers` row is inserted via `INSERT ... ON CONFLICT DO NOTHING` on the partial unique index `offers(slot_id) WHERE status='pendente'` so at most **one pending offer per slot** exists and concurrent release+advance converge; the offer payload names the `slot_id`; the 30-minute clock starts at notification **sent**; on expiry or refusal the System advances to the next entry, or leaves the slot `livre` if none remain; an empty waitlist leaves the slot `livre`; a served patient is removed from **all** queues for that doctor+date. Governed by AD-3, AD-4, AD-5, AD-8, AD-9, AD-11.

- **CAP-8 — VIP priority with quota + ceiling anti-starvation** (FR-16, FR-17, FR-18)
  - **intent:** VIP patients are served ahead of ordinary patients in the same queue, but an ordinary patient's wait is bounded regardless of VIP inflow.
  - **success:** Ordering is a pure, deterministic domain function: (1) class — `VIP` before `comum`; (2) within a class — entry order (`enqueued_at` UTC); (3) **quota** — a consecutive-VIP-grant counter per doctor+date; past `ANTI_STARVATION_QUOTA` (default 3, configurable) the next offer goes to the first eligible `comum`, and the counter resets on any grant to a `comum`; (4) **ceiling** — a `comum` waiting longer than `ANTI_STARVATION_CEILING` (default 24h, configurable) gets top priority ahead of any VIP. Same queue state + same counter ⇒ same next patient, no tie broken by chance; with continuous VIP inflow a `comum` is served within `N+1` releases worst case; max `comum` wait ≤ 24h + 10% (SM-4). Governed by AD-4, AD-9.

- **CAP-9 — Send the D-1 presence reminder** (FR-19)
  - **intent:** One day before the slot the System sends the patient a reminder carrying a presence-confirmation request.
  - **success:** For each `confirmada` appointment whose slot starts the next day, the System sends one reminder via `NotificationGateway` (reference impl logs it), with deterministic key `reminder:<appointment_id>:D-1`; re-running the job the same day does not resend; `cancelada`/`concluida`/`no_show` appointments get none; dispatch is ~18:00 in the clinic timezone converted to UTC at scheduling. Governed by AD-5, AD-6, AD-8.

- **CAP-10 — Idempotency envelope on every mutation** (FR-25)
  - **intent:** Any write can be safely retried by a client or an automation and produce exactly one effect.
  - **success:** Every mutating route (FR-1, 2, 3, 5, 7, 8, 10, 11, 14, 20, 23, 24) requires an `Idempotency-Key` (UUID) header — absent → `400`; the key row is inserted `ON CONFLICT DO NOTHING` with a rows-affected check **in the same transaction** as the domain mutation, response status/body stored; same key + same fingerprint replays the stored response with zero side effects; same key + **different** body → `422`; two concurrent requests with the same key → one executes, the other waits for and returns the original response or gets `409 "em processamento"` (open question); keys expire after 24h (fixed) and a later retry is treated as new, then bounded by domain invariants. Governed by AD-5.

- **CAP-11 — Domain events via transactional outbox** (part of FR-26)
  - **intent:** Every state change is durably recorded and delivered to internal consumers without coupling the scheduler to them by direct call.
  - **success:** Each transition writes a past-tense event (`slot.reserved`, `slot.confirmed`, `slot.cancelled`, `slot.at_risk`, `appointment.no_show`, `offer.made`, `offer.expired`, `offer.accepted`, `penalty.assessed`, …) to `slot_events` in the mutation's transaction; a relay publishes outbox rows to a Redis stream; consumers (waitlist advance, reminder scheduler, penalty recorder) are idempotent and reprocess-safe. Governed by AD-8.

- **CAP-12 — Waitlist reconciliation from the authority tables** (AD-9)
  - **intent:** The queues and the anti-starvation counter survive a Redis restart or drift because they can be rebuilt entirely from Postgres.
  - **success:** On worker boot and periodically, both Redis sorted sets (per-slot and per-doctor+date) and the `waitlist_vip_counter` (one row per doctor+date) are reprojected from `waitlist_entry` / the counter table (idempotent); "next eligible" reads Redis and, on miss or detected inconsistency, falls back to the CAP-8 domain function over Postgres. Governed by AD-9.

- **CAP-13 — Register a Doctor** (FR-1)
  - **intent:** An Administrator can register a Doctor so slots can be published for them.
  - **success:** A Doctor is created with name, specialty and a default Valor de Referência; a freshly registered Doctor has an empty Agenda (zero slots); registering without a Valor de Referência → `422`; idempotent (replay returns the same Doctor, no duplicate). Governed by AD-1, AD-5, AD-11.

- **CAP-14 — Publish slots in batch** (FR-2)
  - **intent:** An Administrator can publish many slots for a Doctor in one request, including to build deliberate-contention test scenarios.
  - **success:** All created slots are `livre` and carry the Doctor's Valor de Referência at publication time; overlapping intervals for the same Doctor reject the **whole batch** with `422` (no slot created); an interval with end ≤ start → `422`; idempotent replay returns the same slot set. Governed by AD-1, AD-5, AD-7, AD-11.

- **CAP-15 — Block / unblock a free slot** (FR-3)
  - **intent:** An Administrator or the owning Doctor can take a `livre` slot out of circulation and put it back.
  - **success:** `livre ↔ bloqueado` succeeds; blocking a slot that is not `livre` (`reservado`/`confirmado`/`em_risco`) → `409`; a `bloqueado` slot is absent from discovery (CAP-1) and cannot receive a soft lock. Governed by AD-5, AD-7, AD-11.

- **CAP-16 — Accept an offer** (FR-14)
  - **intent:** A waitlisted patient can accept a pending offer addressed to them and get the slot.
  - **success:** Accepting a `pendente` offer addressed to the patient within the accept window yields a `confirmada` appointment on the named slot; idempotent (same `Idempotency-Key` → same appointment); accepting an `expirada`/`recusada` offer or one addressed to another patient → `409`; the same `(slot, confirmed appointment)` uniqueness invariant as CAP-3 holds. Governed by AD-5, AD-7, AD-10, AD-11.

- **CAP-17 — Leave a waitlist** (FR-11)
  - **intent:** A patient can remove themselves from a slot's waitlist.
  - **success:** Idempotent — leaving a queue the patient is not in returns a neutral result; leaving while a `pendente` offer is addressed to them marks that offer `recusada` and triggers advance (CAP-7). Governed by AD-5, AD-9, AD-11.

- **CAP-18 — Register presence confirmation** (FR-20)
  - **intent:** A patient can confirm they will attend, in reply to the D-1 reminder.
  - **success:** Idempotent; confirming presence after the slot start → `409` (the valid path then is Check-in, CAP-21). Governed by AD-5, AD-6, AD-7, AD-11.

- **CAP-19 — Mark a slot em_risco on absent presence confirmation** (FR-21, MVP subset)
  - **intent:** Absence of a presence confirmation close to the slot is an early no-show signal that flags the slot without releasing it.
  - **success:** At `T-2h` (UTC) with no presence confirmation the System sets the slot `em_risco`; the original appointment stays `confirmada` and the original patient keeps priority if they show; a presence confirmation or Check-in by the original patient returns the slot to `confirmado`; the cycle is idempotent. **No pré-oferta in MVP** — a real offer is created only after a confirmed no-show (CAP-20). Governed by AD-6, AD-7, AD-8, AD-11.

- **CAP-20 — Auto-mark no-show** (FR-22)
  - **intent:** A patient who does not check in shortly after the slot start is marked absent so the slot can be reused.
  - **success:** The System marks `no_show` any `confirmada` appointment whose slot started more than 15 min ago with no Check-in; idempotent (re-run makes no second event, no second release); a confirmed no-show frees the slot and triggers offer creation (CAP-7); every marking writes an audit record with instant and cause. Governed by AD-5, AD-6, AD-7, AD-10, AD-11.

- **CAP-21 — Register check-in** (FR-23)
  - **intent:** A patient (or the Doctor on their behalf) can record that the patient arrived.
  - **success:** Check-in is accepted from 30 min before to 15 min after slot start; within the window it prevents auto no-show; idempotent; outside the window → `409`. Governed by AD-5, AD-6, AD-7, AD-11.

- **CAP-22 — Doctor override of no-show** (FR-24)
  - **intent:** The owning Doctor can undo a wrongly marked no-show or mark one manually before the automatic deadline.
  - **success:** Undoing a no-show returns the appointment to `confirmada`; undoing when the slot was already re-offered/re-confirmed for a third party → `409` with the conflict stated (it does not silently drop the third party's appointment); every override writes an audit record with `doctor_id`; idempotent. Governed by AD-5, AD-7, AD-10, AD-11.

- **CAP-23 — Immutable audit trail of state transitions** (FR-27, AD-11)
  - **intent:** Every relevant state change leaves a queryable, tamper-evident trace so history can be reconstructed and invariants verified.
  - **success:** Every transition of Slot, Appointment, SoftLock, Offer and Penalty writes an append-only `audit_record` (`entity`, `entity_id`, `prev_state`, `new_state`, `actor ∈ {patient_id|doctor_id|system}`, `cause`, `ts` UTC) in the **same transaction** as the mutation; records are never updated or deleted; a given entity's full history is reconstructable from the trail; state-transition count equals audit-record count (SM-8). Governed by AD-11, AD-8.

- **CAP-24 — Invariant-verification metrics endpoint** (FR-28, AD-11)
  - **intent:** The system can assert its core invariants at any moment without manual inspection.
  - **success:** An endpoint exposes counters/queries for: zero slots with two `confirmada` appointments, zero duplicate `pendente` offers per slot, zero duplicate penalties per appointment, and the common-patient waitlist wait-time distribution; after a contention run (50 concurrent requests per slot over ~1000 slots) every violation counter stays zero; the max common-patient wait metric is queryable and within SM-4. Governed by AD-11.

- **CAP-25 — System jobs run idempotently and observably** (FR-26)
  - **intent:** Every unattended automation is safe to re-run and reports what it did.
  - **success:** The System jobs (expire lock, advance queue, quota/ceiling promotion, D-1 reminder, em_risco marking, no-show marking) run on a configurable interval, are safe to re-run (running twice over the same state converges — no orphan-locked slot, no ownerless pending offer, no partial penalty), and each run emits items processed / transitions made / duration. Governed by AD-5, AD-8, AD-11.

## Constraints

- **No pessimistic database locks on the decision path.** No `SELECT … FOR UPDATE`, no blocking advisory lock across a patient's decision. Every state mutation is CAS (`UPDATE … WHERE status = <expected>` or `INSERT … ON CONFLICT`) with a rows-affected check; zero rows affected = lost the race = domain error.
- **PostgreSQL is the canonical transactional store; Redis is an accelerator only.** "At most one `confirmada` appointment per slot" is a Postgres partial unique index on `slot_id WHERE appointment.status = 'confirmada'`, not an application check. On divergence Postgres wins — `held_until` for the lock, `waitlist_entry` / `waitlist_vip_counter` for the queue.
- **Slot state is one explicit machine with one writer.** States `livre | reservado | confirmado | em_risco | bloqueado`; all transitions go through a single `Slot.transition(to, guard)` domain method emitting the CAS. Only the scheduling use case writes `slots.status` — no read path, queue worker, no-show job, reminder job, or event consumer writes it directly.
- **Appointment state is its own explicit machine with one writer.** States `confirmada | cancelada | concluida | no_show`; transitions go through `Appointment.transition(to, guard)` in the same use case and same transaction as the paired slot transition and outbox event. The no-show job, check-in, and Doctor override call the scheduling use case — never write `appointments.status` directly.
- **Every state transition emits, in the same transaction as the mutation, both an outbox event and an immutable audit record.** Consumers are idempotent and replay-safe. Audit records are never updated or deleted; transition count must equal audit-record count.
- **Every API mutation and every System automation is idempotent.** `Idempotency-Key` (UUID) mandatory on mutating routes; missing → `400`; same key + different body → `422`; key and response persisted in the same transaction as the mutation. System automations derive a deterministic key from `(type, entity, window)`.
- **All time is UTC internally.** Every persisted timestamp is `timestamptz` UTC. Every window (15 min, 30 min, 2 h, D-1, T-2h, +15 min check-in end, −30 min check-in start, 24 h ceiling) is computed in the domain via an injectable `Clock` port, never from request wall-clock. Timezone conversion only at the HTTP edge serializer. Uniqueness invariants do not depend on app/DB clock sync.
- **Two waitlist scopes, one merged selection.** `waitlist:slot:<slot_id>` and `waitlist:doctor:<doctor_id>:<yyyy-mm-dd>` (UTC). A freed slot merges candidates from both, ordered by the queue-ordering rule below. At most one `pendente` offer per slot, enforced by `INSERT ON CONFLICT` on a partial unique index. A served patient leaves all queues for that doctor+date.
- **Queue ordering is a pure, deterministic domain function:** class (`VIP` before `comum`) then entry order; anti-starvation = quota (`ANTI_STARVATION_QUOTA`, default 3 — consecutive-VIP-grant counter per doctor+date, resets on any `comum` grant) + ceiling (`ANTI_STARVATION_CEILING`, default 24h — a `comum` past it gets top priority). No separate VIP queue, no composite score. Redis holds only the computed result.
- **The penalty is recorded as owed, never charged.** `cancellation_penalty` stores `amount` = 50% of the slot's Valor de Referência (inherited from the Doctor at publication, fixed at slot creation) and `currency`; no payment, invoicing, collection, refund or dispute path exists in this epic.
- **Identity is a trusted middleware claim** (`patient_id` / `doctor_id`); the System is an internal job actor. Authentication, session, and identity registration are out of scope.
- **Hexagonal boundaries.** Domain declares ports; adapters (`http`, `postgres`, `redis`, `worker`) implement them; domain imports nothing from `adapter`/`platform`. Notifications go through `NotificationGateway` (reference impl = log/fake). The System-actor worker invokes the same application use cases as the HTTP API.
- **API version policy:** breaking changes only in a new route major (`/v2/...`); new fields are additive.

## Non-goals

- Payment, billing, invoicing, collection, refund or dispute of the cancellation penalty.
- Teleconsultation, electronic health records, prescription, or storing any clinical data.
- Rich frontend, native app, or web UI — the deliverable is the API.
- Authentication, identity registration, session management, or multi-role authorization beyond distinguishing actors by a trusted claim.
- Real notification channels (email/SMS/push) — only `NotificationGateway` with a reference log adapter.
- Advanced availability management: recurrence, holidays, mass block/reallocation of already-confirmed slots, automatic patient reallocation.
- Pré-oferta condicional on `em_risco` slots (FR-21 full form) — deferred to v2; MVP marks `em_risco` and offers only after a confirmed no-show.
- Penalty exemption by clinical rule (emergency, medical certificate) — v2.
- Soft-lock renewal (a patient asking for more time) — not in v1.
- Persistence of the idempotent response beyond 24h.
- Multi-tenant / multiple clinic units / public doctor discovery.
- A message broker (RabbitMQ/Kafka) between services.
- Production capacity/scale; production SLA; infrastructure cost target; RTO/RPO; deployment beyond Docker Compose (Kubernetes, autoscaling).

## Success signal

Under the deliberate-contention test at the PRD scale target (~50 doctors, ~1000 slots/day, 200 concurrent patients, up to 50 concurrent requests per slot):

- **SM-1** zero slots with two `confirmada` appointments;
- **SM-2** 100% of replays (same `Idempotency-Key`) return the original response with no new side effect;
- **SM-3** ≥ 80% of freed slots with a non-empty waitlist become a `confirmada` appointment for a queued patient within one accept window;
- **SM-4** maximum ordinary-patient waitlist wait ≤ 24h + 10% under continuous VIP inflow;
- **SM-5** p95 of the delay between a trigger (lock expired, accept window expired, no-show) and the effective state transition ≤ 30 s;
- **SM-6** zero window-boundary errors (15 min / 30 min / 2 h / D-1 / 24 h) across a suite with clients in varied timezones;
- **SM-8** audit-record count equals state-transition count.

A Doctor opening the day's agenda sees one real patient per line and no double-booking; the invariant-metrics endpoint reports all violation counters at zero.

## Assumptions

- Second D-1 cutoff (slot marked `em_risco`) is `T-2h` before slot start; pré-oferta condicional deferred to v2.
- The 30-minute accept window counts from notification **sent**, not delivered.
- `Idempotency-Key` TTL is 24h, fixed; a retry after expiry is treated as new.
- The D-1 reminder dispatches ~18:00 in the clinic timezone, converted to UTC at job scheduling.
- A patient holds at most 1 active soft lock per doctor+date.
- No-show is auto-marked 15 min after slot start with no Check-in; Check-in window is −30 min to +15 min around slot start.
- Undoing a no-show after the slot was re-confirmed for a third party returns `409`.
- Penalty base = 50% of the slot's Valor de Referência fixed at slot creation, not re-read later.
- IDs are UUIDv7; the concurrency discriminator is `status` + timestamps, not a numeric `version` column.
- Redis runs with `appendonly yes` (AOF); tolerable loss on restart is the last second.
- The soft lock is not renewable in v1.
- Replay with the same key + different body → `422`; concurrent same-key → one executes, the other waits or gets `409 "em processamento"`.
- API breaking changes only in a new route major.

## Open Questions

- **Concurrent same-key idempotency (FR-25):** does the second request block until the response, or return `409 "em processamento"`?
- **VIP class source (FR-16):** does the trusted auth claim carry the patient's VIP class, or is it looked up internally?
- **Anti-starvation calibration (PRD Q3):** are `ANTI_STARVATION_QUOTA=3` and `ANTI_STARVATION_CEILING=24h` right for the expected volume? Should they vary by specialty?
- **D-1 dispatch (PRD Q7):** is the 18:00-clinic-timezone dispatch per-clinic configurable, and what about a clinic with slots in multiple timezones?
- **Undo-no-show conflict (FR-24 / PRD Q6):** is `409` the final answer after third-party re-confirmation, or is there a resolution flow?
- **Outbox relay retry/backoff policy** — left to the spec by the spine (Deferred); still unspecified.
- **Data-protection scope (PRD §12):** does storing a per-patient notification contact bring the system into sensitive-data (LGPD) scope? PRD flags this as unconfirmed; spine defers compliance to architecture.
