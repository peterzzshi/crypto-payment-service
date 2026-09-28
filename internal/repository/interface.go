package repository

import (
	"context"

	"crypto-payment-service/internal/domain"
)

// TxManager runs fn within a single database transaction. Implementations
// derive transaction-scoped repositories and pass a ctx carrying them (or an
// equivalent tx handle) into fn; a non-nil return from fn rolls back.
type TxManager interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type AddressRepository interface {
	GetByAddress(ctx context.Context, asset domain.Asset, address string) (*domain.Address, error)
}

type DepositRepository interface {
	Create(ctx context.Context, d *domain.Deposit) error
	GetByID(ctx context.Context, id string) (*domain.Deposit, error)
	GetByExternalTxID(ctx context.Context, externalTxID string) (*domain.Deposit, error)
	Save(ctx context.Context, d *domain.Deposit) error
	ClaimByStatus(ctx context.Context, status domain.DepositStatus, limit int) ([]*domain.Deposit, error)
}

type WithdrawalRepository interface {
	Create(ctx context.Context, w *domain.Withdrawal) error
	GetByID(ctx context.Context, id string) (*domain.Withdrawal, error)
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Withdrawal, error)
	Save(ctx context.Context, w *domain.Withdrawal) error
	ClaimByStatus(ctx context.Context, status domain.WithdrawalStatus, limit int) ([]*domain.Withdrawal, error)
}

type DepositEventRepository interface {
	Create(ctx context.Context, event *domain.DepositEvent) error
}

type WithdrawalEventRepository interface {
	Create(ctx context.Context, event *domain.WithdrawalEvent) error
}
