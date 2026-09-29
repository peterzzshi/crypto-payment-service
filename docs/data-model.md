# Data Model

Key schema decisions not obvious from code.

## Schema Highlights

**Currency + Network**: Separate fields (not single `asset`) to support multi-network tokens like USDT/Ethereum vs USDT/Tron

**Idempotency**: `external_tx_id` (deposits), `idempotency_key` (withdrawals) with unique constraints

**Concurrency**: `version` column for optimistic locking; `retry_count` + `next_retry_at` persisted (no in-memory state); `locked_by` + `locked_until` processing leases for idempotent polling claims (ADR-0002)

**Atomic units**: All amounts are stored as integer strings in the ent schema and `NUMERIC(78,0)` in the migration - for example satoshis, wei, or token base units

**Balance tracking**: Deferred to future ledger service. Current implementation has no balance validation on withdrawals.

## Concurrency Strategy

**Optimistic locking**: Every update checks `WHERE version = old_version`, increments on success. Zero rows affected → `OptimisticLockError`, caller retries.

**Row claiming**: Two strategies — the strength of the claim matches the cost of doing the work twice (ADR-0002):
- **Lease** (CONFIRMING deposits/withdrawals — idempotent polling): `SELECT ... FOR UPDATE SKIP LOCKED` over rows with no lease or an expired lease, then set `locked_by`/`locked_until` with a version bump in the same transaction. `Save` clears the lease; a crashed worker's rows become claimable at lease expiry.
- **State-transition claim** (APPROVED withdrawals — irreversible broadcast): atomic claim-and-mark to transient `BROADCASTING` status within a transaction, with the claim audit event committed alongside (ADR-0003).

**Nonce management**: BitGo manages nonces internally for custody wallets. Payment service never touches nonce allocation.

**Confirmation tiers**: Risk-based by amount per (Currency, Network). Config in `internal/domain/config.go`. Example — BTC small: 3, medium: 6, large: 12 confirmations.

## Schema Files

See `ent/schema/*.go` for source of truth. Migrations in `migrations/*.sql` with Atlas hash chain (`atlas.sum`).
