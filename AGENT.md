# crypto-payment-service

Crypto deposit/withdrawal settlement behind a custody-provider adapter. The subject is the **trust boundary**: what the
provider (BitGo) guarantees versus what this service must prove
for itself — idempotent crediting, exact-once claims, atomic audit events, a state machine
that refuses invalid transitions.

## Commands

One Go module (Go 1.26), ent ORM, Atlas migrations. Short mode skips every DB-touching test.

- Build/lint: `go build ./...`, `make lint` ← run before finishing any task
- Test: `go test ./... -short`; full (needs Postgres): `make db-up && go test ./... -race`
- Concurrency evidence: `go test -race -run ConcurrentRace ./internal/repository/` — CI-only, needs Postgres
- Regenerate: `make mocks` (mockery), `go generate ./ent`, `go run ./cmd/entmigrate`

**Never hand-edit `ent/` or `internal/mocks/`** — change the schema or `.mockery.yaml` and regenerate. **Known limits:**
BitGo webhook payloads are illustrative and unverified; the `bitgo` broadcaster is
real but unexercised without testnet credentials. Don't report either as verified.

## Architecture

Deliberately a monolith — one service, one database — because the write path needs one atomic
transaction across state change + audit event. Full design: `docs/architecture.md`. Glossary:
`CONTEXT.md`. Schema: `docs/data-model.md`. Phases: `docs/roadmap.md`. Records: `docs/adr/`.

- `cmd/webhook-server` (HTTP + approve/reject/cancel) and `cmd/withdrawal-worker` (claim/broadcast/confirm) are thin
  wrappers over `internal/service`. `internal/repository` is ent-backed; `internal/adapter/bitgo` is the provider
  boundary; `internal/broadcast` is the broadcast seam (`stub` default, `bitgo` behind `BROADCAST_BACKEND`).
- **Two claim strategies, deliberately different** (ADR-0002): irreversible broadcast uses state-transition
  claim-and-mark; idempotent confirmation polling uses a self-expiring Lease. Irreversible work never uses a Lease.
- **Audit Events are atomic with the transition they describe** (ADR-0003). A transition without its event is a bug, not
  a tolerated loss.
- **The deposit state machine is forward-only today** — `COMPLETED`/`FAILED` are hard-terminal and confirmation depth is
  a monotonic max, so it cannot represent "a confirmed deposit got reorged out." Reconciliation needs a real
  `COMPLETED → REVERSED` transition, not a config change; don't add it as a side effect of unrelated work.

## Conventions

- Use `CONTEXT.md` vocabulary: **Currency** vs **Network** are never interchangeable; **Caller** not "user";
  **Approver** ≠ the owning Customer; **Lease** not "lock."
- Business rules stay testable in memory; side effects live at the edges (HTTP, DB, provider HTTP).
- Comment only the *why*, never the *what*.
- Tests assert behaviour. Provider paths pin the contract via `httptest`; concurrency paths must hit real Postgres —
  mocks won't surface a contention bug.
- If a doc and the code disagree, surface the conflict before changing either.

### Go conventions

- *Effective Go* idiom; `/functional-programming-patterns` where it fits — pure functions, side effects confined to
  `internal/*` boundary types.
- Packages flat, one level under `internal/`. Only `internal/adapter/bitgo` and `internal/migrate/seed` nest; don't add
  more.
- Mocks via `mockery` + `testify`. Mock the repository/broadcaster interfaces, not what Postgres or `httptest` should
  exercise.
- Schema changes go in `ent/schema/`, then regenerate + add an Atlas migration. No hand-written SQL, no second query
  layer.

## Workflow

- Small commits, plain imperative messages (match history: `Add lease`).
- Never commit on your own initiative — stage/propose changes and leave the commit to me.
- Ask before adding a dependency to `go.mod`.

### Carrying context across sessions

- **`PLAN.md`** (root, create only when multi-session work is in flight): checkboxes, updated as work progresses. Delete
  when done.
- **Decision records**: add to `docs/adr/` (holds ADR-0001..0003); don't start a parallel log.
- **`HANDOFF.md`** (root, delete after use): current state + next steps before clearing context.
- **Git history**: small descriptive commits make `git log`/`git diff` double as context.
