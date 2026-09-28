package repository

import (
	"context"

	"crypto-payment-service/ent"
	"crypto-payment-service/internal/domain"
)

type DepositEventRepo struct {
	client *ent.Client
}

func NewDepositEventRepo(client *ent.Client) *DepositEventRepo {
	return &DepositEventRepo{client: client}
}

func (repo *DepositEventRepo) Create(ctx context.Context, event *domain.DepositEvent) error {
	builder := repo.client.DepositEvent.Create().
		SetDepositID(event.DepositID).
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
