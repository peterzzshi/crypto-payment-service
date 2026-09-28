package service

import (
	"context"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"

	"go.uber.org/zap"
)

type DepositProcessor struct {
	depositService *DepositService
	broadcaster    broadcast.Broadcaster
}

func NewDepositProcessor(depositService *DepositService, broadcaster broadcast.Broadcaster) *DepositProcessor {
	return &DepositProcessor{
		depositService: depositService,
		broadcaster:    broadcaster,
	}
}

func (processor *DepositProcessor) ProcessConfirming(ctx context.Context, confirming []*domain.Deposit) {
	for _, deposit := range confirming {
		if deposit.TxHash == nil {
			continue
		}

		processor.checkConfirmations(ctx, deposit)
	}
}

func (processor *DepositProcessor) checkConfirmations(ctx context.Context, deposit *domain.Deposit) {
	log := zap.L().With(
		zap.String("deposit_id", deposit.ID),
		zap.String("customer_id", deposit.CustomerID),
		zap.String("asset", string(deposit.Asset)),
		zap.String("status", string(deposit.Status)),
	)

	if deposit.TxHash == nil {
		log.Warn("deposit missing tx_hash, skipping confirmation check")
		return
	}
	log = log.With(zap.String("tx_hash", *deposit.TxHash))

	confirmations, err := processor.broadcaster.Confirmations(ctx, *deposit.TxHash)
	if err != nil {
		log.Warn("failed to get confirmations", zap.Error(err))
		return
	}

	log = log.With(zap.Int("confirmations", confirmations))
	log.Info("confirmation status checked")

	incoming := domain.IncomingDeposit{
		CustomerID:            deposit.CustomerID,
		AddressID:             deposit.AddressID,
		ExternalTxID:          deposit.ExternalTxID,
		TxHash:                deposit.TxHash,
		Asset:                 deposit.Asset,
		AmountAtomic:          deposit.AmountAtomic,
		Confirmations:         confirmations,
		RequiredConfirmations: deposit.RequiredConfirmations,
		TransactionMetadata:   deposit.TransactionMetadata,
	}

	if _, err := processor.depositService.UpsertIncoming(ctx, incoming); err != nil {
		log.Error("failed to update deposit confirmations", zap.Error(err))
	}
}
