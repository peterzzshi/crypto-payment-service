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

## Phase 2 — Concurrency hardening ✓

**Status**: Complete

**Problem**: In-memory retry state and no row locking only work for one worker. Production ran multiple replicas.

**Solution**: 
- Transactional writes (deposit/withdrawal + event in single tx), enforced: audit-event write failures roll back the state change (ADR-0003)
- `version` column for optimistic locking
- `SELECT ... FOR UPDATE SKIP LOCKED` for batch claims
- Two claim strategies matched to the cost of duplicate work (ADR-0002): state-transition claim-and-mark via transient `BROADCASTING` status for the irreversible broadcast path; self-expiring leases (`locked_by`/`locked_until`) for idempotent confirmation polling
- Persisted retry state (`retry_count`, `next_retry_at`); the APPROVED claim only picks up rows whose backoff has elapsed
- `hot_wallets` table with `next_nonce` removed (BitGo manages nonces internally for custody wallets)

## Phase 3 — Currency/Network model + BitGo

**Status**: Mostly complete — model works end-to-end; provider contract still illustrative

**Problem**: Flat `Asset` enum can't express "USDT on Tron" vs "USDT on Ethereum". Can't detect wrong-network deposits.

**Solution**: Split into `Currency` + `Network`, config-driven `(Currency, Network)` pairs, BitGo adapter with single `/webhooks/bitgo` route (initially supporting BTC, ETH, USDT/Ethereum, USDT/Tron — BNB and USDT/BSC added in Phase 4). Risk-based confirmation tiers by amount (low/medium/high thresholds with increasing confirmation requirements). Legacy per-chain BTC/ETH adapters (`internal/adapter/btc`, `internal/adapter/eth`) have since been deleted — BitGo now unifies deposit ingestion and withdrawal confirmation-checking for all supported assets, and `RequiredConfirmations` is read directly off the persisted deposit/withdrawal record instead of re-derived from a per-chain adapter. Persistence is complete end-to-end: `currency`/`network` are written on create, read on every mapping, and address lookup keys on `(currency, network, address)`.

**Remaining gaps**:
- The BitGo webhook structure is illustrative and has not been verified against live payloads
- Withdrawal broadcast defaults to the stub; a BitGo HTTP broadcaster exists behind `BROADCAST_BACKEND=bitgo` (see Phase 8) but is unverified without testnet credentials

## Phase 4 — New assets + contract safety ✓

**Status**: Complete

**Problem**: Prove model generalizes. Wrong contract address for tokens is silent failure.

**Solution**: Added BNB/BSC (native), USDT/Ethereum, USDT/Tron, USDT/BSC to registry. Static validation: contract addresses checksum-valid (EIP-55) and no collisions. Startup validation in webhook-server, CI workflow (`validate-contracts.yml`), and standalone validation tool (`cmd/validate-contracts`).

## Phase 5 — K8s manifests (demo-only) ✓

**Status**: Complete

**Problem**: Document target topology from architecture.md as manifests.

**Solution**: `deploy/k8s/` with Deployment (webhook-server + worker as separate), Service, ConfigMap, Secret (GCP Secret Manager CSI), HPA, PodDisruptionBudget. Illustrative, not applied.

## Phase 6 — Concurrency test evidence ✓

**Status**: Complete

**Problem**: Exclusive claim-and-mark needs PostgreSQL contention evidence. A test file alone is insufficient if it exercises the wrong source status or is skipped in CI.

**Current state**: Two contention tests run against PostgreSQL in CI (`-run ConcurrentRace`): `withdrawal_repo_concurrency_test.go` races APPROVED rows through the exclusive claim-and-mark path (each row claimed once, atomically BROADCASTING); `lease_claim_concurrency_test.go` races lease claims on CONFIRMING rows (each row leased to exactly one worker, unexpired leases block reclaiming, expired leases are reclaimable). Local test targets use short mode and require no database; worker orchestration uses function injection and is tested in memory.

## Phase 7 — Approval workflow ✓

**Status**: Complete

**Problem**: The demo needs an explicit approval decision before irreversible broadcast, with a defined actor, auditable events, retry behavior, and concurrency evidence.

**State flow**: PENDING → APPROVED → BROADCASTING → CONFIRMING → COMPLETED, with PENDING → REJECTED and PENDING/APPROVED → CANCELLED as terminal exits. Failed broadcasts return to APPROVED with backoff (`retry_count`, `next_retry_at`); after max retries, FAILED (terminal).

**Implementation details**:
- Approver trust boundary defined (ADR-0001): actor identity arrives via the gateway-injected `X-Actor-ID` header; approve/reject enforce separation of duties (Approver ≠ owning Customer) and record the actor in the audit event; cancel requires the actor to own the withdrawal
- `Approve()`/`Reject()`/`Cancel()` persist status + audit event atomically — event-write failures roll back the transaction (ADR-0003)
- `claimAndMark` claims APPROVED rows whose retry backoff has elapsed, flips them to BROADCASTING, and writes the claim audit event in the same transaction
- The processor persists broadcast outcomes through `ApplyBroadcastResult`: success → CONFIRMING; retryable → APPROVED with retry metadata; max-retries or non-retryable → FAILED. Invalid transitions are loud errors, never silent no-ops
- Focused tests cover approve/reject (happy path, wrong status, separation of duties, event-write rollback), the processor (all four broadcast outcomes, optimistic-lock tolerance, retry-due filtering), and the HTTP handlers (actor extraction, 401/403/404)

## Phase 8 — BitGo HTTP broadcaster

**Status**: Implemented behind env switch, unverified against live API

**Problem**: The roadmap asked to decide between a documented stub and a real BitGo client.

**Decision**: Both. The broadcaster seam (`internal/broadcast/Broadcaster`) now has two implementations selected by `BROADCAST_BACKEND`: `stub` (default) and `bitgo` (HTTP client: sendcoins for broadcast, tx lookup for confirmations, BitGo coin codes mapped from `Currency`+`Network` with testnet variants, HTTP failures classified into the same retryable/non-retryable domain errors the state machine keys on). Unit tests pin the assumed request/response contract via `httptest`. The BitGo path is unverified without testnet credentials — flipping it on requires `BITGO_API_TOKEN`, `BITGO_WALLET_ID`, and optionally `BITGO_TESTNET`/`BITGO_COIN`/`BITGO_API_BASE`.

## Next milestone — Reconciliation

**Status**: Not started

**Problem**: "What if the webhook never arrives?" currently has no implemented answer. A custody-grade system needs a periodic job that compares internal state against the provider's ledger and flags drift.

**Scope to design**: ledger model (what is the internal source of truth for balances), which BitGo APIs to reconcile against, drift thresholds and alerting, and how reconciliation interacts with in-flight CONFIRMING rows.
