package repository

import (
	"context"
	"math/big"
	"time"

	"crypto-payment-service/ent"
	"crypto-payment-service/ent/deposit"
	"crypto-payment-service/internal/domain"

	"entgo.io/ent/dialect/sql"
)

type DepositRepo struct {
	client *ent.Client
}

func NewDepositRepo(client *ent.Client) *DepositRepo {
	return &DepositRepo{client: client}
}

func (repo *DepositRepo) Create(ctx context.Context, d *domain.Deposit) error {
	if d.AmountAtomic == nil {
		return domain.ValidationError{Field: "amount_atomic", Message: "required"}
	}

	builder := repo.client.Deposit.Create().
		SetCustomerID(d.CustomerID).
		SetAddressID(d.AddressID).
		SetExternalTxID(d.ExternalTxID).
		SetNillableTxHash(d.TxHash).
		SetCurrency(string(d.Currency)).
		SetNetwork(string(d.Network)).
		SetAmountAtomic(d.AmountAtomic.String()).
		SetStatus(string(d.Status)).
		SetConfirmations(d.Confirmations).
		SetRequiredConfirmations(d.RequiredConfirmations).
		SetTransactionMetadata(d.TransactionMetadata).
		SetVersion(d.Version)

	if d.ID != "" {
		builder = builder.SetID(d.ID)
	}
	if d.Asset != "" {
		builder = builder.SetAsset(string(d.Asset))
	}

	row, err := builder.Save(ctx)

	if err != nil {
		return translateDepositError(err)
	}

	d.ID = row.ID
	d.CreatedAt = row.CreatedAt
	d.UpdatedAt = row.UpdatedAt
	return nil
}

func (repo *DepositRepo) GetByID(ctx context.Context, id string) (*domain.Deposit, error) {
	row, err := repo.client.Deposit.Get(ctx, id)
	if err != nil {
		return nil, translateDepositError(err)
	}
	return mapDeposit(row), nil
}

func (repo *DepositRepo) GetByExternalTxID(ctx context.Context, externalTxID string) (*domain.Deposit, error) {
	row, err := repo.client.Deposit.Query().
		Where(deposit.ExternalTxID(externalTxID)).
		Only(ctx)
	if err != nil {
		return nil, translateDepositError(err)
	}
	return mapDeposit(row), nil
}

func (repo *DepositRepo) Save(ctx context.Context, d *domain.Deposit) error {
	if d.AmountAtomic == nil {
		return domain.ValidationError{Field: "amount_atomic", Message: "required"}
	}

	updated, err := repo.client.Deposit.UpdateOneID(d.ID).
		Where(deposit.Version(d.Version)).
		SetVersion(d.Version + 1).
		SetNillableTxHash(d.TxHash).
		SetStatus(string(d.Status)).
		SetConfirmations(d.Confirmations).
		SetRequiredConfirmations(d.RequiredConfirmations).
		SetTransactionMetadata(d.TransactionMetadata).
		ClearLockedBy().
		ClearLockedUntil().
		Save(ctx)

	if ent.IsNotFound(err) {
		return domain.OptimisticLockError{Resource: "deposit", ID: d.ID}
	}
	if err != nil {
		return translateDepositError(err)
	}

	d.Version = updated.Version
	d.UpdatedAt = updated.UpdatedAt
	return nil
}

// LeaseClaimByStatus atomically takes a processing lease on up to limit rows
// in the given status. A row is claimable when it has no lease or its lease
// has expired; the SELECT ... FOR UPDATE SKIP LOCKED plus the per-row version
// bump make the claim exclusive across replicas (ADR-0002).
func (repo *DepositRepo) LeaseClaimByStatus(ctx context.Context, status domain.DepositStatus, limit int, workerID string, leaseTTL time.Duration) ([]*domain.Deposit, error) {
	tx, err := repo.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now()
	lockedUntil := now.Add(leaseTTL)
	rows, err := tx.Deposit.Query().
		Where(
			deposit.Status(string(status)),
			deposit.Or(
				deposit.LockedUntilIsNil(),
				deposit.LockedUntilLT(now),
			),
		).
		Limit(limit).
		ForUpdate(sql.WithLockAction(sql.SkipLocked)).
		All(ctx)

	if err != nil {
		return nil, translateDepositError(err)
	}

	for _, row := range rows {
		_, err := tx.Deposit.UpdateOneID(row.ID).
			Where(deposit.Version(row.Version)).
			SetVersion(row.Version + 1).
			SetLockedBy(workerID).
			SetLockedUntil(lockedUntil).
			Save(ctx)
		if err != nil {
			return nil, translateDepositError(err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	deposits := make([]*domain.Deposit, len(rows))
	for i, row := range rows {
		d := mapDeposit(row)
		d.LockedBy = &workerID
		d.LockedUntil = &lockedUntil
		d.Version = row.Version + 1
		deposits[i] = d
	}
	return deposits, nil
}

func mapDeposit(row *ent.Deposit) *domain.Deposit {
	amount := new(big.Int)
	if _, ok := amount.SetString(row.AmountAtomic, 10); !ok {
		amount = big.NewInt(0)
	}

	return &domain.Deposit{
		ID:                    row.ID,
		CustomerID:            row.CustomerID,
		AddressID:             row.AddressID,
		ExternalTxID:          row.ExternalTxID,
		TxHash:                row.TxHash,
		Currency:              domain.Currency(row.Currency),
		Network:               domain.Network(row.Network),
		Asset:                 domain.Asset(row.Asset),
		AmountAtomic:          amount,
		Status:                domain.DepositStatus(row.Status),
		Confirmations:         row.Confirmations,
		RequiredConfirmations: row.RequiredConfirmations,
		TransactionMetadata:   row.TransactionMetadata,
		LockedBy:              row.LockedBy,
		LockedUntil:           row.LockedUntil,
		Version:               row.Version,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
}

func translateDepositError(err error) error {
	if ent.IsNotFound(err) {
		return domain.NotFoundError{Resource: "deposit"}
	}
	if ent.IsConstraintError(err) {
		return domain.ConflictError{Resource: "deposit", Message: err.Error()}
	}
	return err
}
