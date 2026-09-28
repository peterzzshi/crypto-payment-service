package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"

	"go.uber.org/zap"
)

const (
	defaultMaxRetries     = 3
	defaultRetryBaseDelay = 1 * time.Minute
)

type WithdrawalProcessor struct {
	withdrawalService *WithdrawalService
	broadcaster       broadcast.Broadcaster
	maxRetries        int
	baseDelay         time.Duration
}

func NewWithdrawalProcessor(withdrawalService *WithdrawalService, broadcaster broadcast.Broadcaster) *WithdrawalProcessor {
	return NewWithdrawalProcessorWithConfig(
		withdrawalService,
		broadcaster,
		defaultMaxRetries,
		defaultRetryBaseDelay,
	)
}

func NewWithdrawalProcessorWithConfig(withdrawalService *WithdrawalService, broadcaster broadcast.Broadcaster, maxRetries int, baseDelay time.Duration) *WithdrawalProcessor {
	return &WithdrawalProcessor{
		withdrawalService: withdrawalService,
		broadcaster:       broadcaster,
		maxRetries:        maxRetries,
		baseDelay:         baseDelay,
	}
}

func (processor *WithdrawalProcessor) ProcessPending(ctx context.Context, pending []*domain.Withdrawal) {
	now := time.Now()
	for _, withdrawal := range pending {
		if !withdrawalReadyToRetry(withdrawal, now) {
			continue
		}
		processor.broadcastWithdrawal(ctx, withdrawal)
	}
}

func (processor *WithdrawalProcessor) broadcastWithdrawal(ctx context.Context, withdrawal *domain.Withdrawal) {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawal.ID),
		zap.String("customer_id", withdrawal.CustomerID),
		zap.String("currency", string(withdrawal.Currency)),
		zap.String("network", string(withdrawal.Network)),
		zap.String("status", string(withdrawal.Status)),
		zap.String("amount_atomic", withdrawal.AmountAtomic.String()),
	)
	log.Info("broadcasting withdrawal transaction")

	txHash, err := processor.broadcaster.Broadcast(ctx, withdrawal)
	if err != nil {
		log.Error("broadcast failed", zap.Error(err))
		processor.handleBroadcastError(ctx, withdrawal, err)
		return
	}

	withdrawal.TxHash = &txHash
	withdrawal.RetryCount = 0
	withdrawal.NextRetryAt = nil
	prevStatus := withdrawal.Status

	if !IsValidWithdrawalTransition(prevStatus, domain.WithdrawalStatusConfirming) {
		log.Warn("invalid state transition, skipping",
			zap.String("from", string(prevStatus)), zap.String("to", string(domain.WithdrawalStatusConfirming)))
		return
	}

	withdrawal.Status = domain.WithdrawalStatusConfirming
	if err := processor.save(ctx, withdrawal, prevStatus); err != nil {
		return
	}

	log.With(zap.String("tx_hash", txHash)).Info("withdrawal broadcast successful")
}

func (processor *WithdrawalProcessor) ProcessConfirming(ctx context.Context, confirming []*domain.Withdrawal) {
	for _, withdrawal := range confirming {
		if withdrawal.TxHash == nil {
			continue
		}

		processor.checkConfirmations(ctx, withdrawal)
	}
}

func (processor *WithdrawalProcessor) checkConfirmations(ctx context.Context, withdrawal *domain.Withdrawal) {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawal.ID),
		zap.String("customer_id", withdrawal.CustomerID),
		zap.String("currency", string(withdrawal.Currency)),
		zap.String("network", string(withdrawal.Network)),
		zap.String("tx_hash", *withdrawal.TxHash),
		zap.String("status", string(withdrawal.Status)),
	)

	confirmations, err := processor.broadcaster.Confirmations(ctx, *withdrawal.TxHash)
	if err != nil {
		log.Warn("failed to get confirmations", zap.Error(err))
		return
	}

	log = log.With(zap.Int("confirmations", confirmations))
	log.Info("confirmation status checked")

	if _, err := processor.withdrawalService.UpdateConfirmations(ctx, withdrawal.ID, confirmations, withdrawal.TxHash); err != nil {
		log.Error("failed to update confirmations", zap.Error(err))
	}
}

func withdrawalReadyToRetry(withdrawal *domain.Withdrawal, now time.Time) bool {
	if withdrawal.NextRetryAt == nil {
		return true
	}
	return now.After(*withdrawal.NextRetryAt) || now.Equal(*withdrawal.NextRetryAt)
}

func (processor *WithdrawalProcessor) handleBroadcastError(ctx context.Context, withdrawal *domain.Withdrawal, err error) {
	var networkErr domain.NetworkError
	var rateLimitErr domain.RateLimitError
	var hotWalletErr domain.HotWalletInsufficientFundsError
	var validationErr domain.TransactionValidationError

	prevStatus := withdrawal.Status
	log := zap.L()

	switch {
	case errors.As(err, &networkErr), errors.As(err, &rateLimitErr):
		log.Info("retryable error, scheduling retry")
		if withdrawal.RetryCount >= processor.maxRetries {
			log.Warn("max retries exceeded, marking as failed")
			withdrawal.Status = domain.WithdrawalStatusFailed
			reason := fmt.Sprintf("max retries exceeded: %v", err)
			withdrawal.FailureReason = &reason
			withdrawal.NextRetryAt = nil
		} else {
			withdrawal.Status = domain.WithdrawalStatusPending
			withdrawal.RetryCount++
			nextRetry := computeNextRetry(withdrawal.RetryCount, processor.baseDelay)
			withdrawal.NextRetryAt = &nextRetry
		}

	case errors.As(err, &hotWalletErr), errors.As(err, &validationErr):
		log.Warn("non-retryable error, marking as failed")
		withdrawal.Status = domain.WithdrawalStatusFailed
		reason := err.Error()
		withdrawal.FailureReason = &reason
		withdrawal.NextRetryAt = nil

	default:
		log.Warn("unknown error type, treating as retryable")
		if withdrawal.RetryCount >= processor.maxRetries {
			withdrawal.Status = domain.WithdrawalStatusFailed
			reason := fmt.Sprintf("max retries exceeded: %v", err)
			withdrawal.FailureReason = &reason
			withdrawal.NextRetryAt = nil
		} else {
			withdrawal.Status = domain.WithdrawalStatusPending
			withdrawal.RetryCount++
			nextRetry := computeNextRetry(withdrawal.RetryCount, processor.baseDelay)
			withdrawal.NextRetryAt = &nextRetry
		}
	}

	if err := processor.save(ctx, withdrawal, prevStatus); err != nil {
		log.Error("failed to save withdrawal after broadcast error", zap.Error(err))
	}
}

func (processor *WithdrawalProcessor) save(ctx context.Context, withdrawal *domain.Withdrawal, prevStatus domain.WithdrawalStatus) error {
	_, err := processor.withdrawalService.UpdateConfirmations(ctx, withdrawal.ID, withdrawal.Confirmations, withdrawal.TxHash)
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			zap.L().Warn("optimistic lock conflict saving withdrawal", zap.Error(err))
		} else {
			zap.L().Error("failed to save withdrawal", zap.Error(err))
		}
		return err
	}
	return nil
}

func computeNextRetry(retryCount int, baseDelay time.Duration) time.Time {
	backoff := baseDelay
	for i := 1; i < retryCount; i++ {
		backoff *= 2
	}
	return time.Now().Add(backoff)
}
