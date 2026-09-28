# Crypto Payment Service

A crypto deposit and withdrawal layer for BTC, ETH, BNB, and USDT that hides custody-provider and network-specific details behind adapters. The demo does not implement fiat rails or balance tracking.

**Portfolio demo** of crypto-payment architecture (custody-shaped webhook ingestion, multi-asset modeling, worker concurrency).

## Features

- **Deposit/Withdrawal Model**: Separate `deposits` and `withdrawals` tables with `currency`+`network` fields for BTC, ETH, BNB, and USDT on Ethereum, Tron, and BSC, using integer atomic units. Repository mapping gaps are tracked in the roadmap.
- **Custody Adapter**: A pluggable BitGo-shaped adapter at `/webhooks/bitgo` normalizes demo payloads, validates addresses, and selects configured confirmation thresholds
- **Asset Registry**: Centralized configuration for (Currency, Network) pairs with risk-based confirmation tiers
- **Webhook Ingestion**: The service models idempotent deposit creation/update by external transaction ID; current currency/network persistence gaps are tracked in the roadmap
- **Withdrawal Worker**: Background processor simulates withdrawal broadcast through a stub and tracks confirmations
- **State Machine**: Approval-aware lifecycle (`PENDING → APPROVED → BROADCASTING → CONFIRMING → COMPLETED`, or `PENDING → REJECTED/CANCELLED`)
- **Comprehensive Errors**: Explicit error types for validation, broadcast failures, and resource issues

## Quick Start

### Prerequisites
- Go 1.21+
- Docker (for PostgreSQL)

### Run the Application

```bash
# Start database
docker-compose up -d db

# Run webhook server (applies migrations + seeds test data)
export DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5433/crypto_payment_service?sslmode=disable
go run cmd/webhook-server/main.go
# Server listening on :8080

# Cleanup
docker-compose down
```

## Running Tests

```bash
# Unit tests
go test ./...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Documentation

- **[Architecture](docs/architecture.md)** - Component overview and adding new assets
- **[Data Model](docs/data-model.md)** - Schema decisions (atomic units, concurrency, indexes)
- **[Roadmap](docs/roadmap.md)** - Phase breakdown and implementation status

## Project Structure

```
cmd/
├── webhook-server/     # HTTP server for provider webhooks
├── withdrawal-worker/  # Standalone worker for withdrawals + confirmations
└── migrate/            # Database migrations

internal/
├── adapter/            # Chain adapters (btc/, eth/) and custody adapters (bitgo/)
├── service/            # Business logic (deposits, withdrawals)
├── repository/         # Database access (ent ORM)
├── webhook/            # Webhook handlers with HMAC validation
├── worker/             # Background workers
├── broadcast/          # Transaction broadcasting interface
└── domain/             # Types, errors, config
```

## Adding New Assets

See [docs/architecture.md](docs/architecture.md) for custody adapter vs chain adapter approach.

## Key Design Decisions

- **Currency/Network Model**: Separate `currency` (BTC, ETH, USDT) from `network` (bitcoin, ethereum, tron) to support multi-network assets
- **Asset Registry**: Config-driven (Currency, Network) pairs with risk-based confirmation tiers by amount
- **Atomic Units**: BTC (satoshis), ETH (wei), and token base units - no floating-point errors
- **Idempotent Webhooks**: `external_tx_id` ensures duplicate deliveries update existing records
- **Explicit Errors**: No sentinel errors or codes - each failure type is a distinct error struct
- **Worker Pattern**: Background processor handles withdrawal broadcasting and confirmation tracking
- **JSONB Flexibility**: Chain-specific metadata stored in `transaction_metadata` for future extensibility
- **ent ORM**: Schema-as-code with generated typed client and versioned migrations via Atlas
- **Balance Validation**: Not implemented - intended to be delegated to a separate ledger service (UTXO-aware for BTC, account model for account-based networks)
- **Custody Integration**: Webhook payloads and withdrawal broadcasting are demo doubles, not a verified live BitGo integration

## Observability

Structured JSON logging with per-request IDs in HTTP handlers. Logs include category (webhook/worker/api), entity IDs, and operation context.

---

**Note**: Uses sample data and a stub broadcaster. The BitGo webhook shape is illustrative and has not been verified against live BitGo payloads.
