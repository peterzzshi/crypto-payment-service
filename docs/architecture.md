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

Alternate terminal transitions: PENDING → REJECTED or PENDING → CANCELLED.

- **BROADCASTING** - Transient atomic claim state during broadcast (prevents double-broadcast in multi-worker setup)
- **CANCELLED** - User cancellation (PENDING only, before blockchain broadcast)
- **APPROVED/REJECTED** - Implemented states and HTTP actions; authorization semantics and end-to-end tests are still unresolved (see [roadmap.md](roadmap.md))

## Error Handling

Domain errors implement `error` with `Unwrap()` for `errors.As()` checks.

**Retryable**: `NetworkError`, `RateLimitError`  
**Non-Retryable**: `HotWalletInsufficientFundsError`, `TransactionValidationError`  
**Validation**: `InvalidAddressError`, `InvalidAmountError`, `InsufficientFundsError`

**Retry intent**: Max 3 with exponential backoff from one minute. The current broadcast-error save path does not persist the intended retry state; Phase 7 remains incomplete until this is corrected and tested.

## Demo Boundaries

- Withdrawal broadcast and confirmation checks use `StubBroadcaster`; there is no live BitGo API client.
- The BitGo webhook payload model is illustrative and has not been checked against live provider payloads.
- Authentication and authorization are expected to be supplied by an upstream IAM boundary, but the trusted identity/role contract for approval endpoints is not yet defined.
- Currency/network fields exist in the schema and config, but current repository mappings are incomplete: deposit creation does not set the required fields, and deposit, withdrawal, and address reads omit them.
- Event insert errors are currently logged rather than returned, so transaction records and audit events are not yet guaranteed to commit or roll back together.


## Adding New Assets

All new assets go through the custody adapter (BitGo, or Fireblocks/etc. in a real deployment) — direct per-chain adapters were removed once BitGo covered every supported asset.

1. Add `Currency` + `Network` constants in `internal/domain/types.go`
2. Add `AssetConfig` entry to `DefaultRegistry()` in `internal/domain/config.go` with confirmation tiers and contract address (for tokens)
3. Update custody adapter's coin code parsing (e.g., `BitGoAdapter.ParseCoinCode`)
4. Run `go run ./cmd/validate-contracts` to verify no address collisions and valid EIP-55 checksums
5. Add provider-contract fixtures and normalization tests for the new asset code

**ERC-20 tokens**: Add to registry with contract address. Custody adapter handles normalization. Contract address validation runs at startup and in CI.
