package repository

import (
	"context"

	"crypto-payment-service/ent"
	"crypto-payment-service/ent/address"
	"crypto-payment-service/internal/domain"
)

type AddressRepo struct {
	client *ent.Client
}

func NewAddressRepo(client *ent.Client) *AddressRepo {
	return &AddressRepo{client: client}
}

func (repo *AddressRepo) GetByAddress(ctx context.Context, asset domain.Asset, addr string) (*domain.Address, error) {
	row, err := repo.client.Address.Query().
		Where(address.Asset(string(asset)), address.Address(addr)).
		Only(ctx)
	if err != nil {
		return nil, translateNotFound(err)
	}
	return &domain.Address{
		ID:         row.ID,
		CustomerID: row.CustomerID,
		Asset:      domain.Asset(row.Asset),
		Address:    row.Address,
		CreatedAt:  row.CreatedAt,
	}, nil
}
