# Two claim strategies: state-transition claims for irreversible work, leases for idempotent work

Multiple worker replicas poll the same tables. We reviewed every concurrency mechanism in the codebase (row locks, `SKIP LOCKED`, optimistic locking, in-process mutexes) and settled on a single rule: **the strength of the claim matches the cost of doing the work twice.**

- **Broadcast (irreversible)** — claiming an APPROVED withdrawal flips it to the transient BROADCASTING status inside the claim transaction (`claimAndMark`). Once a transaction is broadcast to BitGo it cannot be un-broadcast, so the claim must be a state-machine transition: even a crashed worker leaves a visible, recoverable BROADCASTING row rather than a double-send.
- **Confirmation polling (idempotent)** — claiming CONFIRMING deposits/withdrawals takes a **lease** (`locked_by`, `locked_until`) instead. Polling a confirmation count twice is harmless (updates are monotonic via `max()` and guarded by optimistic locking), so a cheap, self-expiring lease is sufficient; a crashed worker's rows automatically become claimable again when the lease expires. This is the production-standard pattern (pg-boss, graphile-worker) and needs no janitor process.

## Locking review findings

- `claimAndMark` (APPROVED → BROADCASTING) is correct: `SELECT ... FOR UPDATE SKIP LOCKED` plus a per-row optimistic version bump, all in one transaction, with a Postgres race test in CI.
- The old `claim()` for CONFIRMING committed immediately after `SELECT ... FOR UPDATE SKIP LOCKED`, releasing the locks before processing — it provided no exclusivity at all. This is the gap the lease closes.
- Optimistic locking (`version` column) remains the last line of defense for all writes and is unchanged.
- **No application-level mutexes exist, and none should be added.** The only `sync.Mutex` in the repo is inside ent's generated transaction driver. An in-process mutex would only serialize goroutines within one replica while giving false confidence across replicas — exactly the wrong tool for multi-instance coordination. Cross-replica exclusion belongs in the database, which is the single shared point of truth.

## In-process parallelism

Within one replica, each claimed batch is processed with bounded goroutine fan-out (`errgroup` + `SetLimit`). This composes safely with the claims above because of the ordering: **claim exclusively first, then fan out**. Each goroutine owns exactly one already-claimed row, so goroutines never contend with each other — no shared mutable state, no mutexes. The bound protects the two shared resources under the fan-out: the DB connection pool (capped in `OpenEnt`) and the custody provider's rate limit. Unbounded `go process(row)` per batch would be the classic Go foot-gun here: a 100-row batch would fire 100 concurrent external calls.

## Considered Options

- **Hold the claim transaction open during processing**: true exclusivity with no schema change, but holds row locks across external BitGo API calls for a whole batch — lock contention and long transactions in production.
- **Do nothing for CONFIRMING** (rely on optimistic locking): correct but wasteful — every replica polls every row and losers burn external API calls on duplicate work. Rejected because the duplicate-call cost lands on the custody provider's rate limit, the real bottleneck.

## Consequences

- One migration adds `locked_by` / `locked_until` to `deposits` and `withdrawals`; `Save` clears the lease.
- The two strategies must stay explainable in one sentence each — they are an interview talking point, and asymmetry without a stated reason would read as inconsistency.
