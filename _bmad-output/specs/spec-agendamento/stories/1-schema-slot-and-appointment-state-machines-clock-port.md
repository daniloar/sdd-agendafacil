---
title: 'História 1 — Schema, máquinas de estado de slot e consulta, porta Clock'
type: 'feature'
created: '2026-09-01'
status: 'done'
baseline_commit: '793284672d00e5fb3b6da065fc2f704cf373cf20'
review_loop_iteration: 0
context:
  - '{project-root}/_bmad-output/specs/spec-agendamento/SPEC.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-sdd-agendafacil-2026-09-01/ARCHITECTURE-SPINE.md'
  - '{project-root}/_bmad-output/planning-artifacts/prds/prd-sdd-agendafacil-2026-09-01/prd.md'
---

<frozen-after-approval reason="intenção do humano — não modificar sem renegociação com o humano">

## Intenção

**Problema:** O núcleo de agendamento do AgendaFácil é um módulo Go greenfield sem código. Toda capacidade (CAP-1..CAP-25) depende de um schema Postgres canônico, de duas máquinas de estado explícitas com um único caminho de escrita guardado por CAS, e de aritmética de tempo em UTC via um relógio injetável. Nada pode ser construído antes desse substrato existir.

**Abordagem:** Montar o módulo Go hexagonal conforme a árvore-fonte do architecture spine. Entregar migrations forward-only para as 11 tabelas do núcleo mais os dois índices únicos parciais que tornam "uma consulta confirmada por slot" e "uma oferta pendente por slot" invariantes de banco (AD-1). Implementar as máquinas de estado `Slot` e `Appointment` como validadores de transição puros no domínio (AD-7, AD-10), apoiados por um repositório Postgres que executa cada mutação como `UPDATE ... WHERE status = <esperado>` com checagem de linhas afetadas — nunca um lock pessimista. Adicionar uma porta `Clock` com uma implementação de sistema e um fake de teste (AD-6).

## Fronteiras e restrições

**Sempre:**
- Migrations forward-only, versionadas (golang-migrate); cada migration tem `up` e `down`; `down` é o reverso best-effort. Não editar migration já aplicada — adicionar uma nova.
- Literais de estado são persistidos verbatim como no glossário do PRD: slot `livre|reservado|confirmado|em_risco|bloqueado`; consulta `confirmada|cancelada|concluida|no_show`; oferta `pendente|aceita|expirada|recusada`; penalidade `devida`.
- Nomes de tabela são o plural do nome da entidade (convenção do spine): `doctors, slots, appointments, soft_locks, waitlist_entries, waitlist_vip_counters, offers, idempotency_keys, slot_events, audit_records, cancellation_penalties`.
- Toda mutação de estado em `slots`/`appointments` passa pelo método `Transition` do repositório: um `UPDATE ... SET status=$to ... WHERE id=$id AND status=$from`, exigindo `rows_affected==1`; `0` linhas → erro de domínio (`ErrSlotUnavailable` / `ErrAppointmentConflict`). Nenhum `SELECT ... FOR UPDATE`, nenhum advisory lock, em lugar nenhum.
- `domain` não importa nada de `adapter`/`platform`. As portas (`Clock`, `SlotRepository`, `AppointmentRepository`) são declaradas em `domain`.
- Todas as colunas de timestamp são `timestamptz`; toda leitura de tempo no domínio passa por `Clock.Now()`, que retorna UTC. Nenhum `time.Now()` em `domain` ou `app`.
- IDs são UUIDv7, gerados pela aplicação (`github.com/google/uuid` `NewV7`); colunas de banco são `uuid`.
- Transições legais de slot (conjunto exato, AD-7): `livre→reservado`, `reservado→livre`, `reservado→confirmado`, `confirmado→em_risco`, `em_risco→confirmado`, `confirmado→livre`, `em_risco→livre`, `livre→bloqueado`, `bloqueado→livre`. Qualquer outro par → `ErrIllegalTransition`.
- Transições legais de consulta (AD-10): `confirmada→cancelada`, `confirmada→no_show`, `confirmada→concluida`, `no_show→confirmada`. Qualquer outro par → `ErrIllegalTransition`.

**Perguntar antes:**
- Adicionar uma constraint Postgres `EXCLUDE` (btree_gist) que proíbe quaisquer duas linhas de `slots` com intervalo sobreposto para o mesmo médico, independente do status — proposta aqui para tornar a rejeição de sobreposição do FR-2 uma invariante de banco. Confirmar antes de depender disso, em vez de aplicar a sobreposição só no caso de uso da História 5.
- Qualquer coluna ou índice de tabela não derivável do modelo de dados do SPEC/spine (ex.: desnormalização especulativa).

**Nunca:**
- Nenhuma lógica de negócio de caso de uso (publicar, soft-lock, confirmar, cancelar, fila, oferta, no-show) — essas são as Histórias 2–15. Esta história entrega só schema + primitivas de máquina de estado + Clock.
- Nenhuma camada HTTP, nenhum handler Echo, nenhum worker, nenhum código de cliente Redis (um serviço Redis no compose é ok; nenhum código Go o toca ainda).
- Nenhum ORM. SQL cru via pgx v5.
- Nenhuma coluna inteira `version`/optimistic-lock — `status` + timestamps são o discriminador de concorrência.
- Nenhum dado de seed/fixture nas migrations.

## Matriz de I/O e casos de borda

| Cenário | Entrada / Estado | Saída / Comportamento esperado | Tratamento de erro |
|---------|------------------|-------------------------------|--------------------|
| Transição pura — legal | `slot.Transition("livre","reservado")` | retorna `("reservado", nil)` | N/A |
| Transição pura — ilegal | `slot.Transition("livre","confirmado")` | retorna `("", ErrIllegalTransition)` | chamador mapeia para 4xx depois |
| CAS do repo — vencedor | linha de slot `status='livre'`; `Transition(id,"livre","reservado")` | 1 linha atualizada; `status='reservado'`; retorna nil | N/A |
| CAS do repo — perdeu a corrida | linha de slot `status='reservado'`; `Transition(id,"livre","reservado")` | 0 linhas atualizadas; retorna `ErrSlotUnavailable`; linha inalterada | erro de domínio, sem panic |
| Índice único parcial — 2ª confirmação | uma linha em `appointments` `status='confirmada'` para o slot; INSERT de uma 2ª `status='confirmada'` no mesmo `slot_id` | INSERT rejeitado por `uq_appt_confirmed_per_slot` | `23505` aflora como `ErrAppointmentConflict` |
| Índice único parcial — oferta | uma linha em `offers` `status='pendente'` para o slot; INSERT de 2ª `pendente` no mesmo `slot_id` | rejeitado por `uq_offer_pending_per_slot` | `23505` |
| Round-trip de migrations | banco vazio | `up` aplica todas as migrations sem erro; `schema_migrations` no topo; `down` até 0 não deixa nenhuma tabela do núcleo | falha o teste em qualquer erro |
| Contenção deliberada | slot `status='livre'`; 50 goroutines chamando `Transition(id,"livre","reservado")` | exatamente 1 retorna nil, 49 retornam `ErrSlotUnavailable`; `status` final `reservado` | N/A |
| Clock fake | `FakeClock` setado em `T`; `Now()` | retorna `T` em UTC; avançar o fake move de forma determinística | N/A |
| Clock de sistema | `SystemClock.Now()` | retorna a hora de parede convertida para UTC (`.Location()==time.UTC`) | N/A |

</frozen-after-approval>

## Mapa de código

Greenfield — nenhum código-fonte existente. Todo caminho abaixo é novo. A investigação é a síntese de `SPEC.md` (Constraints, lista de CAP), `ARCHITECTURE-SPINE.md` (AD-1, AD-6, AD-7, AD-8, AD-10, AD-11; "Árvore-fonte"; "Consistency Conventions"; Stack) e `prd.md` (§3 Glossário para os literais de estado; FR-1/FR-2/FR-7/FR-8 para as necessidades de coluna; §10 NFRs). A lista de **Execução** abaixo é o manifesto de arquivos — uma linha por arquivo, com seu papel. O layout segue a árvore-fonte do spine verbatim: `cmd/{api,worker}`, `internal/{domain,adapter/postgres,platform}`, `migrations/`.

## Tarefas e aceitação

**Execução:**
- [x] `go.mod` / `go.sum` -- `go mod init` + adicionar deps pinadas -- módulo deve compilar.
- [x] `docker-compose.yml` -- postgres:18 + redis:8 com healthchecks -- dev local + fallback se testcontainers indisponível.
- [x] `migrations/0001_doctors_slots.sql` -- `doctors` (id, name, specialty, reference_value numeric, currency, created_at); `slots` (id, doctor_id FK, starts_at, ends_at, status default 'livre', reference_value, currency, held_by uuid null, held_until timestamptz null, created_at, updated_at); `CHECK (ends_at > starts_at)`; `CHECK (status IN (...))`; índice em `(doctor_id, starts_at)`; `EXCLUDE USING gist` opcional (Perguntar antes) -- tabelas fundacionais da agenda.
- [x] `migrations/0002_appointments_softlocks.sql` -- `appointments` (id, slot_id FK, patient_id, doctor_id, status default 'confirmada', created_at, updated_at) + único parcial `uq_appt_confirmed_per_slot ON appointments(slot_id) WHERE status='confirmada'`; `soft_locks` (id, slot_id FK, patient_id, acquired_at, expires_at, released_at null, status) -- invariante de confirmação + histórico de soft-lock.
- [x] `migrations/0003_waitlist.sql` -- `waitlist_entries` (id, patient_id, scope text, slot_id uuid null, doctor_id uuid null, wl_date date null, class text CHECK in ('comum','vip'), enqueued_at, status) + único parcial `(patient_id, scope) WHERE status IN ('aguardando','ofertado')`; `waitlist_vip_counters` (doctor_id, wl_date, consecutive_vip_grants int default 0, PRIMARY KEY(doctor_id, wl_date)) -- autoridade das filas de dois escopos + contador de quota (AD-9).
- [x] `migrations/0004_offers_penalties.sql` -- `offers` (id, slot_id FK, waitlist_entry_id FK, patient_id, status text, notified_at timestamptz null, expires_at timestamptz null, created_at) + único parcial `uq_offer_pending_per_slot ON offers(slot_id) WHERE status='pendente'`; `cancellation_penalties` (id, appointment_id UNIQUE, patient_id, amount numeric, currency, status default 'devida', created_at) -- unicidade de oferta + uma-penalidade-por-consulta.
- [x] `migrations/0005_outbox_audit_idempotency.sql` -- `slot_events` (id, aggregate_type, aggregate_id, event_type, payload jsonb, occurred_at, published_at timestamptz null) + índice `WHERE published_at IS NULL`; `audit_records` (id, entity, entity_id, prev_state, new_state, actor text, cause text, ts timestamptz) append-only; `idempotency_keys` (key uuid PK, request_fingerprint text, response_status int, response_body jsonb, created_at, expires_at) + índice em `expires_at` -- substrato de AD-8, AD-11, AD-5.
- [x] `internal/domain/errors.go` -- erros de domínio sentinela -- compartilhados entre agregados e adapters.
- [x] `internal/domain/clock.go` -- porta `Clock` -- injetada onde quer que se leia tempo.
- [x] `internal/domain/slot/slot.go` -- `State`, mapa de transições legais, `Transition` puro, porta `SlotRepository` -- caminho único de escrita do AD-7.
- [x] `internal/domain/appointment/appointment.go` -- mesma forma -- AD-10.
- [x] `internal/adapter/postgres/db.go` -- pool pgx -- compartilhado por repos + checagem de migrate.
- [x] `internal/adapter/postgres/slot_repo.go` -- CAS `Transition` + mapeamento de `pgErr 23505` -- implementação de referência da disciplina CAS otimista.
- [x] `internal/adapter/postgres/appointment_repo.go` -- CAS `Transition` -- espelho.
- [x] `internal/platform/clock/system.go` + `fake.go` -- `SystemClock`, `FakeClock` -- testabilidade do AD-6.
- [x] `internal/platform/config/config.go` -- config por env (DB agora; env vars do spine como stub) -- 12-factor.
- [x] `internal/platform/migrate/migrate.go` -- runner de migrations embutido -- usado por `cmd/*` e pelos testes de integração.
- [x] `cmd/api/main.go`, `cmd/worker/main.go` -- carrega config + migrate + log "not implemented" -- mantém a árvore-fonte honesta.
- [x] `internal/domain/slot/slot_test.go`, `.../appointment/appointment_test.go`, `internal/platform/clock/clock_test.go` -- teste unitário de cada linha de lógica pura da Matriz de I/O -- sem DB.
- [x] `internal/adapter/postgres/slot_repo_test.go` (`testcontainers-go`) -- round-trip de migrations, CAS vencedor/perdedor, rejeições de único parcial, linha de contenção deliberada com 50 goroutines -- prova as afirmações de concorrência.

**Critérios de aceitação:**
- Dado um banco Postgres 18 vazio, quando o runner de migrations embutido aplica todas as migrations, então todas as 11 tabelas do núcleo e ambos os índices únicos parciais existem e `schema_migrations` está no topo com `dirty=false`; rodar `down` até a versão 0 dropa todas as tabelas do núcleo.
- Dado o módulo, quando `go build ./...` e `go vet ./...` rodam, então ambos passam sem diagnósticos.
- Dado `domain`, quando seus imports são inspecionados, então nenhum arquivo importa `internal/adapter/*` ou `internal/platform/*`.
- Dada uma linha de slot e 50 chamadas concorrentes a `SlotRepository.Transition(id,"livre","reservado")`, quando completam, então exatamente uma retorna `nil`, as demais retornam `ErrSlotUnavailable`, e o `status` final da linha é `reservado`.
- Dados dois `INSERT`s de consultas `status='confirmada'` para o mesmo `slot_id`, quando o segundo commita, então o Postgres levanta `23505` e o helper do repo o mapeia para `ErrAppointmentConflict`.
- Dado qualquer pacote de domínio ou app, quando pesquisado, então não contém nenhuma chamada a `time.Now(` (só `Clock.Now()`).

## Notas de design

**Literais de estado ficam em português.** O índice único parcial do spine é definido verbatim como `... WHERE appointment.status = 'confirmada'` e o glossário do PRD é a única fonte do vocabulário de estados. Os literais em inglês no SQL da SOLUTION-DESIGN §2 são apenas ilustrativos. Persistir os literais do glossário mantém as definições de índice e as constantes `State` do domínio idênticas ao contrato.

**Método de domínio vs. SQL do CAS — a divisão.** `Slot.Transition(from,to)` é uma função pura: valida o par contra o conjunto de transições legais e retorna o estado alvo ou `ErrIllegalTransition`. Não roda SQL. O **CAS** é o `SlotRepository.Transition` do adapter postgres, que chama o validador puro primeiro, então emite o único `UPDATE` guardado e checa `CommandTag().RowsAffected()`. Isso satisfaz "os métodos de domínio Slot.transition / Appointment.transition com seu SQL de CAS" mantendo `domain` livre de infra (fronteira hexagonal, spine).

Forma canônica (adapter de slot):
```go
func (r *SlotRepo) Transition(ctx context.Context, q pgxQuerier, id uuid.UUID, from, to slot.State) error {
    if _, err := slot.Transition(from, to); err != nil { return err } // guarda pura
    ct, err := q.Exec(ctx,
        `UPDATE slots SET status=$1, updated_at=now() WHERE id=$2 AND status=$3`, to, id, from)
    if err != nil { return mapPgError(err) }
    if ct.RowsAffected() != 1 { return domain.ErrSlotUnavailable }
    return nil
}
```
`q pgxQuerier` (satisfeito por `*pgxpool.Pool` e `pgx.Tx`) permite às Histórias 2–15 passar a transação envolvente para que o evento de outbox + o registro de auditoria caiam atomicamente com a mutação.

**Constraint de exclusão de sobreposição** é o único item de Perguntar antes — é a forma mais limpa de tornar o "rejeita o lote inteiro em sobreposição" do FR-2 uma invariante de armazenamento, mas também proíbe manter duas linhas de slot sobrepostas (incluindo uma re-liberada), então o humano deve confirmar.

## Verificação

**Comandos:**
- `go build ./... && go vet ./...` -- esperado: exit 0, sem saída.
- `go test ./internal/domain/... ./internal/platform/...` -- esperado: tudo passa, sem Docker.
- `docker compose up -d postgres` e então `go test ./internal/adapter/postgres/...` -- esperado: testcontainers sobe Postgres 18; round-trip de migrations, CAS, único parcial e contenção com 50 goroutines passam.
- `grep -rn "time.Now(" internal/domain internal/app` -- esperado: nenhum match.

## Suggested Review Order

**Máquinas de estado (intenção de design — ponto de entrada)**

- O mapa de transições legais É o contrato AD-7 em código; comece aqui.
  [`slot.go:29`](../../../../internal/domain/slot/slot.go#L29)

- `Transition` puro: valida o par, retorna o alvo ou `ErrIllegalTransition`, zero SQL.
  [`slot.go:42`](../../../../internal/domain/slot/slot.go#L42)

- Espelho para consulta — conjunto exato AD-10 (4 transições).
  [`appointment.go:26`](../../../../internal/domain/appointment/appointment.go#L26)

**Caminho de escrita CAS (fronteira domínio↔infra)**

- `TransitionWith` recebe um `Querier` (pool ou tx) — guarda pura, depois um `UPDATE ... WHERE status=$from`.
  [`slot_repo.go:41`](../../../../internal/adapter/postgres/slot_repo.go#L41)

- 23505 vira `ErrAppointmentConflict` só para `uq_appt_confirmed_per_slot`; outros 23505 embrulhados.
  [`appointment_repo.go:59`](../../../../internal/adapter/postgres/appointment_repo.go#L59)

- `isUniqueViolationOn` casa código + nome da constraint; `NewPool` limita o `Ping` sem deadline.
  [`db.go:63`](../../../../internal/adapter/postgres/db.go#L63)

**Schema e invariantes**

- `slots` + `CHECK` de status/valor; base da máquina do slot.
  [`0001_doctors_slots.up.sql:1`](../../../../migrations/0001_doctors_slots.up.sql#L1)

- `uq_appt_confirmed_per_slot WHERE status='confirmada'` — "uma consulta confirmada por slot" no banco (AD-1).
  [`0002_appointments_softlocks.up.sql:1`](../../../../migrations/0002_appointments_softlocks.up.sql#L1)

- `waitlist_entries`: `CHECK` de scope + exclusividade slot-xor-médico+data.
  [`0003_waitlist.up.sql:1`](../../../../migrations/0003_waitlist.up.sql#L1)

- `uq_offer_pending_per_slot WHERE status='pendente'` + `cancellation_penalties.appointment_id` UNIQUE.
  [`0004_offers_penalties.up.sql:1`](../../../../migrations/0004_offers_penalties.up.sql#L1)

- Outbox / auditoria append-only / chaves de idempotência (substrato AD-8/AD-11/AD-5).
  [`0005_outbox_audit_idempotency.up.sql:1`](../../../../migrations/0005_outbox_audit_idempotency.up.sql#L1)

**Portas e tempo**

- Porta `Clock` de método único (`Now()` UTC) — AD-6.
  [`clock.go:11`](../../../../internal/domain/clock.go#L11)

- Erros sentinela de domínio compartilhados entre agregados e adapters.
  [`errors.go:1`](../../../../internal/domain/errors.go#L1)

**Boot e config**

- `boot.Run`: load config → migrate Up → aborta se `dirty`; `cmd/api` e `cmd/worker` só o chamam.
  [`boot.go:28`](../../../../internal/platform/boot/boot.go#L28)

- `Load` exige `DATABASE_URL`, rejeita durações ≤ 0 e inteiros < 0.
  [`config.go:40`](../../../../internal/platform/config/config.go#L40)

- `LatestVersion` derivado da contagem de `*.up.sql` embutidos — sem versão hardcoded.
  [`migrate.go:33`](../../../../internal/platform/migrate/migrate.go#L33)

**Testes (suporte)**

- Contenção deliberada: 50 goroutines, exatamente 1 vencedor.
  [`slot_repo_test.go:389`](../../../../internal/adapter/postgres/slot_repo_test.go#L389)

- CAS de consulta vencedor/perdedor (adicionado no review).
  [`slot_repo_test.go:249`](../../../../internal/adapter/postgres/slot_repo_test.go#L249)

- Round-trip de migrations contra `migrate.LatestVersion`, com restauração do schema.
  [`slot_repo_test.go:159`](../../../../internal/adapter/postgres/slot_repo_test.go#L159)
