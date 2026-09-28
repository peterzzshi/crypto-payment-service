package service

import (
	"context"

	"crypto-payment-service/internal/adapter"
	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/repository"

	"go.uber.org/zap"
)

type DepositIngestor struct {
	Adapter   adapter.ChainAdapter
	Deposits  *DepositService
	Addresses repository.AddressRepository
}

func NewDepositIngestor(
	chainAdapter adapter.ChainAdapter,
	depositService *DepositService,
	addressRepo repository.AddressRepository,
) *DepositIngestor {
	return &DepositIngestor{
		Adapter:   chainAdapter,
		Deposits:  depositService,
		Addresses: addressRepo,
	}
}

func (ingestor *DepositIngestor) Process(ctx context.Context, payload []byte) (*domain.Deposit, error) {
	log := zap.L()

	if ingestor.Adapter == nil || ingestor.Deposits == nil || ingestor.Addresses == nil {
		log.Error("ingestor not fully configured")
		return nil, domain.ConfigurationError{
			Component: "deposit_ingestor",
			Reason:    "adapter, deposit service, and address repository are required",
		}
	}

	log.Info("normalising incoming transaction")
	normalised, err := ingestor.Adapter.NormaliseIncomingTransaction(payload)
	if err != nil {
		log.Error("failed to normalise transaction", zap.Error(err))
		return nil, err
	}

	log.Info("looking up address")
	address, err := ingestor.Addresses.GetByAddress(ctx, ingestor.Adapter.Asset(), normalised.Address)
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
		zap.String("asset", string(incoming.Asset)),
		zap.String("amount_atomic", incoming.AmountAtomic.String()),
	)

	log.Info("upserting deposit")
	deposit, err := ingestor.Deposits.UpsertIncoming(ctx, incoming)
	if err != nil {
		log.Error("failed to upsert deposit", zap.Error(err))
		return nil, err
	}

	log.With(zap.String("deposit_id", deposit.ID)).Info("deposit ingestion completed")
	return deposit, nil
}
