package service

import (
	"context"

	"crypto-payment-service/internal/adapter"
	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/repository"

	"go.uber.org/zap"
)

type DepositIngestor struct {
	adapter   adapter.CustodyAdapter
	deposits  *DepositService
	addresses repository.AddressRepository
}

func NewDepositIngestor(
	custodyAdapter adapter.CustodyAdapter,
	depositService *DepositService,
	addressRepo repository.AddressRepository,
) *DepositIngestor {
	return &DepositIngestor{
		adapter:   custodyAdapter,
		deposits:  depositService,
		addresses: addressRepo,
	}
}

func (i *DepositIngestor) Process(ctx context.Context, payload []byte) (*domain.Deposit, error) {
	log := zap.L()

	normalised, err := i.adapter.NormaliseIncomingTransaction(payload)
	if err != nil {
		log.Error("failed to normalise transaction", zap.Error(err))
		return nil, err
	}

	address, err := i.addresses.GetByAddress(ctx, normalised.Deposit.Currency, normalised.Deposit.Network, normalised.Address)
	if err != nil {
		log.Error("failed to find address", zap.Error(err))
		return nil, err
	}

	incoming := normalised.Deposit
	incoming.CustomerID = address.CustomerID
	incoming.AddressID = address.ID
	incoming.TransactionMetadata = normalised.TransactionMetadata

	log = log.With(
		zap.String("customer_id", address.CustomerID),
		zap.String("currency", string(incoming.Currency)),
		zap.String("network", string(incoming.Network)),
		zap.String("amount_atomic", incoming.AmountAtomic.String()),
	)

	deposit, err := i.deposits.UpsertIncoming(ctx, incoming)
	if err != nil {
		log.Error("failed to upsert deposit", zap.Error(err))
		return nil, err
	}

	log.With(zap.String("deposit_id", deposit.ID)).Info("deposit ingestion completed")
	return deposit, nil
}
