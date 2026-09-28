package repository

import (
	"context"
	"math/big"

	"crypto-payment-service/ent"
	"crypto-payment-service/ent/withdrawal"
	"crypto-payment-service/internal/domain"

	"entgo.io/ent/dialect/sql"
)

type WithdrawalRepo struct {
	client *ent.Client
}

func NewWithdrawalRepo(client *ent.Client) *WithdrawalRepo {
	return &WithdrawalRepo{client: client}
}

func (repo *WithdrawalRepo) Create(ctx context.Context, w *domain.Withdrawal) error {
	if w.AmountAtomic == nil {
		return domain.ValidationError{Field: "amount_atomic", Message: "required"}
	}

	row, err := repo.client.Withdrawal.Create().
		SetID(w.ID).
		SetCustomerID(w.CustomerID).
		SetIdempotencyKey(w.IdempotencyKey).
		SetDestinationAddress(w.DestinationAddress).
		SetNillableTxHash(w.TxHash).
		SetCurrency(string(w.Currency)).
		SetNetwork(string(w.Network)).
		SetAsset(string(w.Asset)).
		SetAmountAtomic(w.AmountAtomic.String()).
		SetStatus(string(w.Status)).
		SetConfirmations(w.Confirmations).
		SetRequiredConfirmations(w.RequiredConfirmations).
		SetRetryCount(w.RetryCount).
		SetNillableNextRetryAt(w.NextRetryAt).
		SetNillableFailureReason(w.FailureReason).
		SetTransactionMetadata(w.TransactionMetadata).
		SetVersion(w.Version).
		Save(ctx)

	if err != nil {
		return translateWithdrawalError(err)
	}

	w.ID = row.ID
	w.CreatedAt = row.CreatedAt
	w.UpdatedAt = row.UpdatedAt
	return nil
}

func (repo *WithdrawalRepo) GetByID(ctx context.Context, id string) (*domain.Withdrawal, error) {
	row, err := repo.client.Withdrawal.Get(ctx, id)
	if err != nil {
		return nil, translateWithdrawalError(err)
	}
	return mapWithdrawal(row), nil
}

func (repo *WithdrawalRepo) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Withdrawal, error) {
	row, err := repo.client.Withdrawal.Query().
		Where(withdrawal.IdempotencyKey(idempotencyKey)).
		Only(ctx)
	if err != nil {
		return nil, translateWithdrawalError(err)
	}
	return mapWithdrawal(row), nil
}

func (repo *WithdrawalRepo) Save(ctx context.Context, w *domain.Withdrawal) error {
	if w.AmountAtomic == nil {
		return domain.ValidationError{Field: "amount_atomic", Message: "required"}
	}

	updated, err := repo.client.Withdrawal.UpdateOneID(w.ID).
		Where(withdrawal.Version(w.Version)).
		SetVersion(w.Version + 1).
		SetNillableTxHash(w.TxHash).
		SetStatus(string(w.Status)).
		SetConfirmations(w.Confirmations).
		SetRequiredConfirmations(w.RequiredConfirmations).
		SetRetryCount(w.RetryCount).
		SetNillableNextRetryAt(w.NextRetryAt).
		SetNillableFailureReason(w.FailureReason).
		SetTransactionMetadata(w.TransactionMetadata).
		Save(ctx)

	if ent.IsNotFound(err) {
		return domain.OptimisticLockError{Resource: "withdrawal", ID: w.ID}
	}
	if err != nil {
		return translateWithdrawalError(err)
	}

	w.Version = updated.Version
	w.UpdatedAt = updated.UpdatedAt
	return nil
}

func (repo *WithdrawalRepo) ClaimByStatus(ctx context.Context, status domain.WithdrawalStatus, limit int) ([]*domain.Withdrawal, error) {
	if status == domain.WithdrawalStatusApproved {
		return repo.claimAndMark(ctx, limit)
	}
	return repo.claim(ctx, status, limit)
}

func (repo *WithdrawalRepo) claim(ctx context.Context, status domain.WithdrawalStatus, limit int) ([]*domain.Withdrawal, error) {
	tx, err := repo.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Withdrawal.Query().
		Where(withdrawal.Status(string(status))).
		Limit(limit).
		ForUpdate(sql.WithLockAction(sql.SkipLocked)).
		All(ctx)

	if err != nil {
		return nil, translateWithdrawalError(err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	withdrawals := make([]*domain.Withdrawal, len(rows))
	for i, row := range rows {
		withdrawals[i] = mapWithdrawal(row)
	}
	return withdrawals, nil
}

func (repo *WithdrawalRepo) claimAndMark(ctx context.Context, limit int) ([]*domain.Withdrawal, error) {
	tx, err := repo.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Withdrawal.Query().
		Where(withdrawal.Status(string(domain.WithdrawalStatusApproved))).
		Limit(limit).
		ForUpdate(sql.WithLockAction(sql.SkipLocked)).
		All(ctx)

	if err != nil {
		return nil, translateWithdrawalError(err)
	}

	for _, row := range rows {
		_, err := tx.Withdrawal.UpdateOneID(row.ID).
			Where(withdrawal.Version(row.Version)).
			SetVersion(row.Version + 1).
			SetStatus(string(domain.WithdrawalStatusBroadcasting)).
			Save(ctx)
		if err != nil {
			return nil, translateWithdrawalError(err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	withdrawals := make([]*domain.Withdrawal, len(rows))
	for i, row := range rows {
		w := mapWithdrawal(row)
		w.Status = domain.WithdrawalStatusBroadcasting
		w.Version = row.Version + 1
		withdrawals[i] = w
	}
	return withdrawals, nil
}

func mapWithdrawal(row *ent.Withdrawal) *domain.Withdrawal {
	amount := new(big.Int)
	if _, ok := amount.SetString(row.AmountAtomic, 10); !ok {
		amount = big.NewInt(0)
	}

	return &domain.Withdrawal{
		ID:                    row.ID,
		CustomerID:            row.CustomerID,
		IdempotencyKey:        row.IdempotencyKey,
		DestinationAddress:    row.DestinationAddress,
		TxHash:                row.TxHash,
		Asset:                 domain.Asset(row.Asset),
		AmountAtomic:          amount,
		Status:                domain.WithdrawalStatus(row.Status),
		Confirmations:         row.Confirmations,
		RequiredConfirmations: row.RequiredConfirmations,
		RetryCount:            row.RetryCount,
		NextRetryAt:           row.NextRetryAt,
		FailureReason:         row.FailureReason,
		TransactionMetadata:   row.TransactionMetadata,
		Version:               row.Version,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
}

func translateWithdrawalError(err error) error {
	if ent.IsNotFound(err) {
		return domain.NotFoundError{Resource: "withdrawal"}
	}
	if ent.IsConstraintError(err) {
		return domain.ConflictError{Resource: "withdrawal", Message: err.Error()}
	}
	return err
}
