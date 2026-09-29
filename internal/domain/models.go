package domain

import (
	"math/big"
	"time"
)

type Deposit struct {
	ID                    string
	CustomerID            string
	AddressID             string
	ExternalTxID          string
	TxHash                *string
	Currency              Currency
	Network               Network
	Asset                 Asset // Deprecated: use Currency + Network
	AmountAtomic          *big.Int
	Status                DepositStatus
	Confirmations         int
	RequiredConfirmations int
	TransactionMetadata   map[string]any
	LockedBy              *string    // Worker holding the processing lease (ADR-0002)
	LockedUntil           *time.Time // Lease expiry; expired leases are reclaimable
	Version               int32
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Withdrawal struct {
	ID                    string
	CustomerID            string
	IdempotencyKey        string
	DestinationAddress    string
	TxHash                *string
	Currency              Currency
	Network               Network
	Asset                 Asset // Deprecated: use Currency + Network
	AmountAtomic          *big.Int
	Status                WithdrawalStatus
	Confirmations         int
	RequiredConfirmations int
	RetryCount            int
	NextRetryAt           *time.Time
	FailureReason         *string
	TransactionMetadata   map[string]any
	LockedBy              *string    // Worker holding the processing lease (ADR-0002)
	LockedUntil           *time.Time // Lease expiry; expired leases are reclaimable
	Version               int32
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type DepositEvent struct {
	ID         string
	DepositID  string
	EventType  string
	FromStatus *DepositStatus
	ToStatus   *DepositStatus
	Metadata   map[string]any
	CreatedAt  time.Time
}

type WithdrawalEvent struct {
	ID           string
	WithdrawalID string
	EventType    string
	FromStatus   *WithdrawalStatus
	ToStatus     *WithdrawalStatus
	Metadata     map[string]any
	CreatedAt    time.Time
}

type IncomingDeposit struct {
	CustomerID            string         `validate:"required"`
	AddressID             string         `validate:"required"`
	ExternalTxID          string         `validate:"required"`
	TxHash                *string        `validate:"omitempty"`
	Currency              Currency       `validate:"required"`
	Network               Network        `validate:"required"`
	Asset                 Asset          `validate:"omitempty"` // Deprecated: use Currency + Network
	AmountAtomic          *big.Int       `validate:"required"`
	Confirmations         int            `validate:"gte=0"`
	RequiredConfirmations int            `validate:"required,gt=0"`
	TransactionMetadata   map[string]any `validate:"omitempty"`
}

type Address struct {
	ID         string
	CustomerID string
	Currency   Currency
	Network    Network
	Asset      Asset // Deprecated: use Currency + Network
	Address    string
	CreatedAt  time.Time
}

type Customer struct {
	ID         string
	ExternalID string
	Email      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type HotWallet struct {
	ID        string
	Currency  Currency
	Network   Network
	Asset     Asset // Deprecated: use Currency + Network
	NextNonce int64
	Version   int32
	CreatedAt time.Time
	UpdatedAt time.Time
}
