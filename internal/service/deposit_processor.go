package service

import (
	"context"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
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

func (p *DepositProcessor) ProcessConfirming(ctx context.Context, confirming []*domain.Deposit) {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentRowProcesses)
	for _, deposit := range confirming {
		if deposit.TxHash == nil {
			continue
		}
		group.Go(func() error {
			p.checkConfirmations(ctx, deposit)
			return nil
		})
	}
	_ = group.Wait()
}

func (p *DepositProcessor) checkConfirmations(ctx context.Context, deposit *domain.Deposit) {
	log := zap.L().With(
		zap.String("deposit_id", deposit.ID),
		zap.String("customer_id", deposit.CustomerID),
		zap.String("currency", string(deposit.Currency)),
		zap.String("network", string(deposit.Network)),
		zap.String("tx_hash", *deposit.TxHash),
		zap.String("status", string(deposit.Status)),
	)

	confirmations, err := p.broadcaster.Confirmations(ctx, *deposit.TxHash)
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
		Currency:              deposit.Currency,
		Network:               deposit.Network,
		AmountAtomic:          deposit.AmountAtomic,
		Confirmations:         confirmations,
		RequiredConfirmations: deposit.RequiredConfirmations,
		TransactionMetadata:   deposit.TransactionMetadata,
	}

	if _, err := p.depositService.UpsertIncoming(ctx, incoming); err != nil {
		log.Error("failed to update deposit confirmations", zap.Error(err))
	}
}
