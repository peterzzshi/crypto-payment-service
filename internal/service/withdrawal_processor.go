package service

import (
	"context"
	"errors"
	"time"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	defaultMaxRetries     = 3
	defaultRetryBaseDelay = 1 * time.Minute

	// maxConcurrentRowProcesses bounds how many claimed rows one worker
	// processes in parallel. Rows are exclusively claimed before processing
	// begins, so parallelism within the batch is race-free; the bound exists
	// to protect the two shared resources underneath — the DB connection
	// pool and the custody provider's rate limit.
	maxConcurrentRowProcesses = 8
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

// BroadcastApproved broadcasts withdrawals the repository has already claimed
// into BROADCASTING. Rows whose backoff has not yet elapsed are skipped — the
// claim query normally filters these, so this is a second line of defense.
func (p *WithdrawalProcessor) BroadcastApproved(ctx context.Context, approved []*domain.Withdrawal) {
	now := time.Now()
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentRowProcesses)
	for _, withdrawal := range approved {
		if !withdrawalReadyToRetry(withdrawal, now) {
			continue
		}
		group.Go(func() error {
			p.broadcastWithdrawal(ctx, withdrawal)
			return nil
		})
	}
	_ = group.Wait()
}

func (p *WithdrawalProcessor) ProcessConfirming(ctx context.Context, confirming []*domain.Withdrawal) {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentRowProcesses)
	for _, withdrawal := range confirming {
		if withdrawal.TxHash == nil {
			continue
		}
		group.Go(func() error {
			p.checkConfirmations(ctx, withdrawal)
			return nil
		})
	}
	_ = group.Wait()
}

func (p *WithdrawalProcessor) broadcastWithdrawal(ctx context.Context, withdrawal *domain.Withdrawal) {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawal.ID),
		zap.String("customer_id", withdrawal.CustomerID),
		zap.String("currency", string(withdrawal.Currency)),
		zap.String("network", string(withdrawal.Network)),
		zap.String("status", string(withdrawal.Status)),
		zap.String("amount_atomic", withdrawal.AmountAtomic.String()),
	)
	log.Info("broadcasting withdrawal transaction")

	txHash, err := p.broadcaster.Broadcast(ctx, withdrawal)
	if err != nil {
		log.Error("broadcast failed", zap.Error(err))
		p.handleBroadcastError(ctx, withdrawal, err)
		return
	}

	// The version we hold is the post-claim version, so ApplyBroadcastResult's
	// optimistic save detects any intervening actor (e.g. a cancel).
	prevStatus := withdrawal.Status
	withdrawal.TxHash = &txHash
	withdrawal.RetryCount = 0
	withdrawal.NextRetryAt = nil
	withdrawal.Status = domain.WithdrawalStatusConfirming

	if err := p.persistResult(ctx, withdrawal, prevStatus); err != nil {
		return
	}

	log.With(zap.String("tx_hash", txHash)).Info("withdrawal broadcast successful")
}

func (p *WithdrawalProcessor) checkConfirmations(ctx context.Context, withdrawal *domain.Withdrawal) {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawal.ID),
		zap.String("customer_id", withdrawal.CustomerID),
		zap.String("currency", string(withdrawal.Currency)),
		zap.String("network", string(withdrawal.Network)),
		zap.String("tx_hash", *withdrawal.TxHash),
		zap.String("status", string(withdrawal.Status)),
	)

	confirmations, err := p.broadcaster.Confirmations(ctx, *withdrawal.TxHash)
	if err != nil {
		log.Warn("failed to get confirmations", zap.Error(err))
		return
	}

	log = log.With(zap.Int("confirmations", confirmations))
	log.Info("confirmation status checked")

	if _, err := p.withdrawalService.UpdateConfirmations(ctx, withdrawal.ID, confirmations, withdrawal.TxHash); err != nil {
		log.Error("failed to update confirmations", zap.Error(err))
	}
}

func withdrawalReadyToRetry(withdrawal *domain.Withdrawal, now time.Time) bool {
	if withdrawal.NextRetryAt == nil {
		return true
	}
	return !now.Before(*withdrawal.NextRetryAt)
}

func (p *WithdrawalProcessor) handleBroadcastError(ctx context.Context, withdrawal *domain.Withdrawal, err error) {
	outcome := classifyBroadcastError(err, withdrawal.RetryCount, p.maxRetries, p.baseDelay, time.Now())
	if outcome.status == domain.WithdrawalStatusApproved {
		zap.L().Info("retryable broadcast error, scheduled for retry",
			zap.String("withdrawal_id", withdrawal.ID),
			zap.Int("retry_count", outcome.retryCount),
			zap.Time("next_retry_at", *outcome.nextRetryAt))
	} else {
		zap.L().Warn("broadcast failed terminally",
			zap.String("withdrawal_id", withdrawal.ID),
			zap.String("failure_reason", *outcome.failureReason))
	}

	prevStatus := withdrawal.Status
	withdrawal.Status = outcome.status
	withdrawal.RetryCount = outcome.retryCount
	withdrawal.NextRetryAt = outcome.nextRetryAt
	withdrawal.FailureReason = outcome.failureReason

	if err := p.persistResult(ctx, withdrawal, prevStatus); err != nil {
		zap.L().Error("failed to persist broadcast failure outcome",
			zap.String("withdrawal_id", withdrawal.ID), zap.Error(err))
	}
}

// persistResult saves the mutated row via the service, which validates the
// transition and writes the audit event atomically. An optimistic lock
// conflict means another actor changed the row after we claimed it (e.g. a
// cancel landing between claim and broadcast) — the other actor's write
// wins, and the row is no longer ours to mutate.
func (p *WithdrawalProcessor) persistResult(ctx context.Context, withdrawal *domain.Withdrawal, prevStatus domain.WithdrawalStatus) error {
	err := p.withdrawalService.ApplyBroadcastResult(ctx, withdrawal, prevStatus)
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			zap.L().Warn("optimistic lock conflict persisting broadcast result; row changed by another actor",
				zap.String("withdrawal_id", withdrawal.ID), zap.Error(err))
		} else {
			zap.L().Error("failed to persist broadcast result",
				zap.String("withdrawal_id", withdrawal.ID), zap.Error(err))
		}
		return err
	}
	return nil
}

// broadcastOutcome is the state a withdrawal must move to after a failed
// broadcast attempt: back to APPROVED with retry metadata, or terminally
// FAILED with a reason.
type broadcastOutcome struct {
	status        domain.WithdrawalStatus
	retryCount    int
	nextRetryAt   *time.Time
	failureReason *string
}

// classifyBroadcastError maps a broadcast failure to its outcome. Retryable
// errors with attempts remaining return to APPROVED; everything else is
// terminally FAILED. BROADCASTING → PENDING is deliberately never produced:
// PENDING means "awaiting approval", and a broadcast that already left the
// approval gate must not silently re-enter it.
func classifyBroadcastError(err error, retryCount, maxRetries int, baseDelay time.Duration, now time.Time) broadcastOutcome {
	retryable := isRetryableBroadcastError(err)
	if retryable && retryCount < maxRetries {
		retryCount++
		nextRetryAt := now.Add(backoffDelay(baseDelay, retryCount))
		return broadcastOutcome{
			status:      domain.WithdrawalStatusApproved,
			retryCount:  retryCount,
			nextRetryAt: &nextRetryAt,
		}
	}

	reason := err.Error()
	if retryable {
		reason = "max retries exceeded: " + reason
	}
	return broadcastOutcome{
		status:        domain.WithdrawalStatusFailed,
		retryCount:    retryCount,
		failureReason: &reason,
	}
}

func isRetryableBroadcastError(err error) bool {
	var networkErr domain.NetworkError
	var rateLimitErr domain.RateLimitError
	if errors.As(err, &networkErr) || errors.As(err, &rateLimitErr) {
		return true
	}
	var hotWalletErr domain.HotWalletInsufficientFundsError
	var validationErr domain.TransactionValidationError
	if errors.As(err, &hotWalletErr) || errors.As(err, &validationErr) {
		return false
	}
	// Unknown errors are assumed transient: the failure mode to avoid is
	// giving up on a broadcast that a retry would have landed.
	return true
}

func backoffDelay(base time.Duration, retryCount int) time.Duration {
	return base << (retryCount - 1)
}
