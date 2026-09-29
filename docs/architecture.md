# Architecture

Component overview and extension patterns.

## Components

- **Webhook Handler** (`internal/webhook/`) - Routes `/webhooks/bitgo` and optionally validates a demo HMAC signature; the scheme is not verified against BitGo's live contract
- **Deposit/Withdrawal Services** (`internal/service/`) - State machines, idempotency
- **Processors** (`internal/service/`) - Confirmation tracking, broadcast with retry
- **Workers** (`internal/worker/`) - Poll every 5s, batch size 100
- **Adapters** (`internal/adapter/`) - Custody-provider (BitGo) adapter behind the `ChainAdapter` interface. Validates addresses, converts atomic units, sets risk-based confirmation thresholds for all supported assets (BTC, ETH, BNB, USDT/Ethereum, USDT/Tron, USDT/BSC). The legacy per-chain BTC/ETH adapters have been deleted; `RequiredConfirmations` is read directly off the persisted deposit/withdrawal record rather than re-derived from an adapter during confirmation checks.
- **Asset Registry** (`internal/domain/config.go`) - Centralized configuration for supported (Currency, Network) pairs with confirmation tiers
- **Store** (PostgreSQL with ent ORM) - See [data-model.md](data-model.md)

## State Machines

**Deposits**: CONFIRMING → COMPLETED | FAILED

**Withdrawals**: PENDING → APPROVED → BROADCASTING → CONFIRMING → COMPLETED | FAILED

Alternate terminal transitions: PENDING → REJECTED, PENDING/APPROVED → CANCELLED. Failed broadcasts return BROADCASTING → APPROVED with retry metadata (never PENDING — a transient error must not re-enter the human approval gate); after max retries or on non-retryable errors, FAILED (terminal).

- **BROADCASTING** - Transient atomic claim state during broadcast (prevents double-broadcast in multi-worker setup); claimed with a state-transition claim (`claimAndMark`)
- **CONFIRMING (polling)** - Claimed with a self-expiring processing lease (`locked_by`/`locked_until`, ADR-0002); a crashed worker's rows become claimable again at lease expiry
- **CANCELLED** - Customer cancellation from PENDING or APPROVED (before blockchain broadcast)
- **APPROVED/REJECTED** - Set by an Approver via the approval endpoints; actor identity arrives via the gateway-injected `X-Actor-ID` header and separation of duties (Approver ≠ owning Customer) is enforced in the service (ADR-0001)
- Every state change commits with its audit event in the same transaction; event-write failures roll back (ADR-0003)

## In-Process Concurrency

Cross-replica exclusion lives in the database (claims and leases, ADR-0002); within one replica, each claimed batch is processed with **bounded goroutine fan-out** (`errgroup` + `SetLimit(8)`). This is race-free by construction: rows are exclusively claimed before fan-out begins, each goroutine owns exactly one row, and no mutable state is shared between goroutines — which is also why there are no application-level mutexes.

The bound exists to protect the two shared resources underneath:

- **DB connection pool** — capped at 20 open / 5 idle connections with a 30-minute connection lifetime (`OpenEnt`)
- **Custody provider rate limit** — at most 8 concurrent external calls per worker

Each pipeline round shares one 10-second timeout so a slow provider cannot stretch a tick indefinitely, and worker shutdown waits for the in-flight round to finish before returning. A dedicated test proves the bound: a blocking broadcaster with a max-in-flight counter runs under `-race`.

## Error Handling

Domain errors implement `error` with `Unwrap()` for `errors.As()` checks.

**Retryable**: `NetworkError`, `RateLimitError`  
**Non-Retryable**: `HotWalletInsufficientFundsError`, `TransactionValidationError`  
**Validation**: `InvalidAddressError`, `InvalidAmountError`, `InsufficientFundsError`  
**Authorization**: `AuthorizationError` (403 — e.g. separation-of-duties violation)

**Retry behavior**: Max 3 with exponential backoff from one minute, persisted as `retry_count`/`next_retry_at`; the APPROVED claim only picks up rows whose backoff has elapsed.

## Demo Boundaries

- Withdrawal broadcast defaults to `StubBroadcaster`; a BitGo HTTP broadcaster exists behind `BROADCAST_BACKEND=bitgo` (see [roadmap.md](roadmap.md) Phase 8), pinned by `httptest` unit tests but unverified against live testnet without credentials.
- The BitGo webhook payload model is illustrative and has not been checked against live provider payloads.
- Authentication is supplied by an upstream IAM boundary; this service trusts the gateway-injected `X-Actor-ID` header and enforces domain rules (ownership, separation of duties) on it (ADR-0001). There is no role check beyond that — "who may be an Approver" is the gateway's concern.
- Reconciliation (comparing internal state against the provider ledger) is not yet implemented; it is the next milestone in the roadmap.


## Adding New Assets

All new assets go through the custody adapter (BitGo, or Fireblocks/etc. in a real deployment) — direct per-chain adapters were removed once BitGo covered every supported asset.

1. Add `Currency` + `Network` constants in `internal/domain/types.go`
2. Add `AssetConfig` entry to `DefaultRegistry()` in `internal/domain/config.go` with confirmation tiers and contract address (for tokens)
3. Update custody adapter's coin code parsing (e.g., `BitGoAdapter.ParseCoinCode`)
4. Run `go run ./cmd/validate-contracts` to verify no address collisions and valid EIP-55 checksums
5. Add provider-contract fixtures and normalization tests for the new asset code

**ERC-20 tokens**: Add to registry with contract address. Custody adapter handles normalization. Contract address validation runs at startup and in CI.
