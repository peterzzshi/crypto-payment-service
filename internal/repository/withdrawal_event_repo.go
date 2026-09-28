package repository

import (
	"context"

	"crypto-payment-service/ent"
	"crypto-payment-service/internal/domain"
)

type WithdrawalEventRepo struct {
	client *ent.Client
}

func NewWithdrawalEventRepo(client *ent.Client) *WithdrawalEventRepo {
	return &WithdrawalEventRepo{client: client}
}

func (repo *WithdrawalEventRepo) Create(ctx context.Context, event *domain.WithdrawalEvent) error {
	builder := repo.client.WithdrawalEvent.Create().
		SetWithdrawalID(event.WithdrawalID).
		SetEventType(event.EventType).
		SetCreatedAt(event.CreatedAt).
		SetMetadata(event.Metadata)

	if event.FromStatus != nil {
		builder.SetFromStatus(string(*event.FromStatus))
	}
	if event.ToStatus != nil {
		builder.SetToStatus(string(*event.ToStatus))
	}

	row, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	event.ID = row.ID
	return nil
}
