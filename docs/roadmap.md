# Roadmap

Portfolio demo of a BitGo-style crypto-payments system using ent and a multi-asset model. Provider calls are stubbed; the BitGo webhook contract is illustrative. Each phase notes the real problem it solves.

**Status source of truth**: Here.

## Architecture: Monolith

Deliberately single service, single database. Real custody/payment systems (Stripe, Fireblocks) keep write-path monolithic for atomicity. Hardest concurrency problems (nonce allocation, optimistic locking, exclusive claims) demonstrate better in a monolith than microservices.

**Distributed systems thinking**: Concurrent worker replicas (Phase 2), read replicas (future), reconciliation (future).

## Phase 0 — Migration integrity ✓

**Problem**: Two migrations with timestamp filenames can apply out-of-merge-order.

**Solution**: `atlas.sum` hash chain, `atlas migrate lint` in CI. First migration hand-named (`0001_init.sql`).

## Phase 1 — ent migration ✓

**Problem**: GORM `AutoMigrate` has no migration history, no compile-time query safety.

**Solution**: ent schema-as-code in `ent/schema/`, generated client, repository interfaces unchanged (only implementations swap).

## Phase 2 — Concurrency hardening

**Status**: Incomplete

**Problem**: In-memory retry state and no row locking only work for one worker. Production ran multiple replicas.

**Solution**: 
- Transactional writes (deposit/withdrawal + event in single tx)
- `version` column for optimistic locking
- `SELECT ... FOR UPDATE SKIP LOCKED` for batch claims
- Exclusive claim-and-mark via transient `BROADCASTING` status for APPROVED withdrawals (broadcast is irreversible)
- Persisted retry state (`retry_count`, `next_retry_at`)
- `hot_wallets` table with `next_nonce` removed (BitGo manages nonces internally for custody wallets)

**Remaining gap**: Event creation errors are logged and swallowed by service helpers, so the advertised transaction-plus-event atomicity is not enforced. Confirmation polling also releases row locks before processing and relies on optimistic locking, which prevents stale saves but does not provide exclusive processing.

## Phase 3 — Currency/Network model + BitGo

**Status**: Incomplete

**Problem**: Flat `Asset` enum can't express "USDT on Tron" vs "USDT on Ethereum". Can't detect wrong-network deposits.

**Solution**: Split into `Currency` + `Network`, config-driven `(Currency, Network)` pairs, BitGo adapter with single `/webhooks/bitgo` route (initially supporting BTC, ETH, USDT/Ethereum, USDT/Tron — BNB and USDT/BSC added in Phase 4). Risk-based confirmation tiers by amount (low/medium/high thresholds with increasing confirmation requirements). Legacy per-chain BTC/ETH adapters (`internal/adapter/btc`, `internal/adapter/eth`) have since been deleted — BitGo now unifies deposit ingestion and withdrawal confirmation-checking for all supported assets, and `RequiredConfirmations` is read directly off the persisted deposit/withdrawal record instead of re-derived from a per-chain adapter.

**Remaining gaps**:
- `DepositRepo.Create` does not set required `currency` and `network` fields
- Deposit, withdrawal, and address read mappings omit `currency` and `network`
- Address lookup still uses the deprecated flat `asset` field
- The BitGo webhook structure is illustrative and has not been verified against live payloads
- Withdrawal broadcast and confirmation checks use a stub rather than a BitGo API client

## Phase 4 — New assets + contract safety ✓

**Status**: Complete

**Problem**: Prove model generalizes. Wrong contract address for tokens is silent failure.

**Solution**: Added BNB/BSC (native), USDT/Ethereum, USDT/Tron, USDT/BSC to registry. Static validation: contract addresses checksum-valid (EIP-55) and no collisions. Startup validation in webhook-server, CI workflow (`validate-contracts.yml`), and standalone validation tool (`cmd/validate-contracts`).

## Phase 5 — K8s manifests (demo-only) ✓

**Status**: Complete

**Problem**: Document target topology from architecture.md as manifests.

**Solution**: `deploy/k8s/` with Deployment (webhook-server + worker as separate), Service, ConfigMap, Secret (GCP Secret Manager CSI), HPA, PodDisruptionBudget. Illustrative, not applied.

## Phase 6 — Concurrency test evidence

**Status**: Incomplete

**Problem**: Exclusive claim-and-mark needs PostgreSQL contention evidence. A test file alone is insufficient if it exercises the wrong source status or is skipped in CI.

**Current state**: `internal/repository/withdrawal_repo_concurrency_test.go` races APPROVED withdrawals through the exclusive claim-and-mark path. CI supplies `TEST_DB_URL` and runs the test against its PostgreSQL service. Local test targets use short mode and require no database. The former placeholder shell scripts were removed; worker orchestration uses function injection and is tested in memory.

**Completion evidence required**: Race N goroutines against the same APPROVED rows, verify each row is returned once and atomically becomes BROADCASTING, and run that test against PostgreSQL in CI.

## Phase 7 — Approval workflow

**Status**: In progress

**Problem**: The demo needs an explicit approval decision before irreversible broadcast, with a defined actor, auditable events, retry behavior, and concurrency evidence.

**Target**: Add APPROVED/REJECTED status constants and API actions, update transition validation, and make the worker claim APPROVED withdrawals. State flow: PENDING → APPROVED → BROADCASTING → CONFIRMING → COMPLETED (or PENDING → REJECTED as terminal state). Failed broadcasts should return to APPROVED for retry.

**Implementation details**:
- Added `WithdrawalStatusApproved` and `WithdrawalStatusRejected` constants
- Created HTTP endpoints with customer-ownership and source-status checks
- Implemented `Approve()` and `Reject()` service methods with validation
- Updated `IsValidWithdrawalTransition()` with new state flow rules
- Modified worker to claim APPROVED withdrawals using `claimAndMark()` pattern
- Worker atomically transitions APPROVED → BROADCASTING using `SELECT ... FOR UPDATE SKIP LOCKED`
- `go test ./...` and `go test -race ./...` pass when the PostgreSQL test is skipped, but there are no focused service or HTTP handler tests for Approve/Reject
- The processor still assigns PENDING after retryable broadcast failures even though BROADCASTING → PENDING is no longer valid
- The processor save helper re-fetches the row through `UpdateConfirmations`, so it does not persist the mutated retry count, retry time, failure reason, or intended APPROVED/FAILED status
- Approval and rejection accept `customer_id` from the request body; the trusted actor and authorization contract are not yet defined
- Approval/rejection event insert failures are logged and swallowed inside the transaction, so status and audit history are not atomic

## Next milestone — Finish approval and broadcast correctness

**Status**: Awaiting design clarification

**Problem**: The approval actor/authorization boundary is unresolved, and the broadcast processor cannot yet persist the documented retry transitions. Starting another feature would compound an unverified money-movement lifecycle.

**Acceptance criteria**:
- Complete currency/network persistence and read mapping before treating multi-asset flows as working
- Make event persistence participate in transaction failure rather than swallowing audit-write errors
- Define who can approve/reject and how trusted actor identity reaches this service
- Persist successful and failed broadcast outcomes atomically, including retry metadata and audit events
- Add focused service and HTTP tests for approve/reject, conflicts, and event-write failures
- Keep the PostgreSQL APPROVED-row contention test running in CI
- Add broader database integration coverage in CI and keep worker orchestration database-free through injected functions
- Decide whether live BitGo API integration is in scope or keep the broadcaster explicitly documented as a stub
