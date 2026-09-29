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

func (repo *AddressRepo) GetByAddress(ctx context.Context, currency domain.Currency, network domain.Network, addr string) (*domain.Address, error) {
	row, err := repo.client.Address.Query().
		Where(
			address.Currency(string(currency)),
			address.Network(string(network)),
			address.Address(addr),
		).
		Only(ctx)
	if err != nil {
		return nil, translateNotFound(err)
	}
	return &domain.Address{
		ID:         row.ID,
		CustomerID: row.CustomerID,
		Currency:   domain.Currency(row.Currency),
		Network:    domain.Network(row.Network),
		Address:    row.Address,
		CreatedAt:  row.CreatedAt,
	}, nil
}
