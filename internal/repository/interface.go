package repository

import (
	"context"
	"time"

	"crypto-payment-service/internal/domain"
)

// TxManager runs fn within a single database transaction. Implementations
// derive transaction-scoped repositories and pass a ctx carrying them (or an
// equivalent tx handle) into fn; a non-nil return from fn rolls back.
type TxManager interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type AddressRepository interface {
	GetByAddress(ctx context.Context, currency domain.Currency, network domain.Network, address string) (*domain.Address, error)
}

type DepositRepository interface {
	Create(ctx context.Context, d *domain.Deposit) error
	GetByID(ctx context.Context, id string) (*domain.Deposit, error)
	GetByExternalTxID(ctx context.Context, externalTxID string) (*domain.Deposit, error)
	Save(ctx context.Context, d *domain.Deposit) error
	// LeaseClaimByStatus claims up to limit rows in the given status by taking
	// a processing lease (locked_by/locked_until). Rows with an unexpired
	// lease held by another worker are skipped (ADR-0002).
	LeaseClaimByStatus(ctx context.Context, status domain.DepositStatus, limit int, workerID string, leaseTTL time.Duration) ([]*domain.Deposit, error)
}

type WithdrawalRepository interface {
	Create(ctx context.Context, w *domain.Withdrawal) error
	GetByID(ctx context.Context, id string) (*domain.Withdrawal, error)
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Withdrawal, error)
	Save(ctx context.Context, w *domain.Withdrawal) error
	// ClaimForBroadcast atomically moves up to limit APPROVED withdrawals —
	// skipping those whose retry backoff has not elapsed — into BROADCASTING
	// and returns them. Broadcast is irreversible, so the claim is an
	// exclusive state transition, not a lease (ADR-0002).
	ClaimForBroadcast(ctx context.Context, limit int) ([]*domain.Withdrawal, error)
	// LeaseClaimByStatus claims up to limit rows in the given status by taking
	// a processing lease (locked_by/locked_until). Rows with an unexpired
	// lease held by another worker are skipped (ADR-0002).
	LeaseClaimByStatus(ctx context.Context, status domain.WithdrawalStatus, limit int, workerID string, leaseTTL time.Duration) ([]*domain.Withdrawal, error)
}

type DepositEventRepository interface {
	Create(ctx context.Context, event *domain.DepositEvent) error
}

type WithdrawalEventRepository interface {
	Create(ctx context.Context, event *domain.WithdrawalEvent) error
}
