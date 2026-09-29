package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"crypto-payment-service/internal/domain"
	mockbroadcast "crypto-payment-service/internal/mocks/broadcast"
	mockrepository "crypto-payment-service/internal/mocks/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type processorMocks struct {
	withdrawals *mockrepository.MockWithdrawalRepository
	events      *mockrepository.MockWithdrawalEventRepository
	txManager   *mockrepository.MockTxManager
	broadcaster *mockbroadcast.MockBroadcaster
}

func newProcessor(t *testing.T) (*WithdrawalProcessor, processorMocks) {
	t.Helper()
	m := processorMocks{
		withdrawals: mockrepository.NewMockWithdrawalRepository(t),
		events:      mockrepository.NewMockWithdrawalEventRepository(t),
		txManager:   mockrepository.NewMockTxManager(t),
		broadcaster: mockbroadcast.NewMockBroadcaster(t),
	}
	service := NewWithdrawalService(m.withdrawals, m.events).WithTxManager(m.txManager)
	// Zero base delay keeps retry scheduling deterministic to assert on.
	processor := NewWithdrawalProcessorWithConfig(service, m.broadcaster, 3, time.Minute)
	return processor, m
}

// claimedWithdrawal is a row as ClaimForBroadcast hands it to the processor:
// already in BROADCASTING with the post-claim version.
func claimedWithdrawal() *domain.Withdrawal {
	return &domain.Withdrawal{
		ID:                    "withdrawal-1",
		CustomerID:            "customer-1",
		Status:                domain.WithdrawalStatusBroadcasting,
		RequiredConfirmations: 6,
		Version:               2,
	}
}

func TestWithdrawalProcessor_BroadcastSuccess(t *testing.T) {
	processor, m := newProcessor(t)
	withdrawal := claimedWithdrawal()

	m.broadcaster.EXPECT().Broadcast(mock.Anything, withdrawal).Return("tx-hash-1", nil)
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
		return w.Status == domain.WithdrawalStatusConfirming &&
			w.TxHash != nil && *w.TxHash == "tx-hash-1" &&
			w.RetryCount == 0 && w.NextRetryAt == nil
	})).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
		return e.EventType == domain.EventWithdrawalStatusChanged &&
			*e.FromStatus == domain.WithdrawalStatusBroadcasting &&
			*e.ToStatus == domain.WithdrawalStatusConfirming
	})).Return(nil)

	processor.BroadcastApproved(context.Background(), []*domain.Withdrawal{withdrawal})
}

func TestWithdrawalProcessor_RetryableErrorReturnsToApproved(t *testing.T) {
	processor, m := newProcessor(t)
	withdrawal := claimedWithdrawal()

	m.broadcaster.EXPECT().Broadcast(mock.Anything, withdrawal).
		Return("", domain.NetworkError{Err: fmt.Errorf("connection timed out")})
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
		// BROADCASTING -> APPROVED (never PENDING: the approval gate must
		// not be re-entered), with retry metadata for the backoff claim.
		return w.Status == domain.WithdrawalStatusApproved &&
			w.RetryCount == 1 &&
			w.NextRetryAt != nil && w.NextRetryAt.After(time.Now())
	})).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)

	processor.BroadcastApproved(context.Background(), []*domain.Withdrawal{withdrawal})
}

func TestWithdrawalProcessor_MaxRetriesExhaustedFails(t *testing.T) {
	processor, m := newProcessor(t)
	withdrawal := claimedWithdrawal()
	withdrawal.RetryCount = 3 // maxRetries configured as 3

	m.broadcaster.EXPECT().Broadcast(mock.Anything, withdrawal).
		Return("", domain.RateLimitError{Err: fmt.Errorf("429 Too Many Requests")})
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
		return w.Status == domain.WithdrawalStatusFailed &&
			w.FailureReason != nil &&
			w.NextRetryAt == nil
	})).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)

	processor.BroadcastApproved(context.Background(), []*domain.Withdrawal{withdrawal})
}

func TestWithdrawalProcessor_NonRetryableErrorFailsImmediately(t *testing.T) {
	processor, m := newProcessor(t)
	withdrawal := claimedWithdrawal()

	m.broadcaster.EXPECT().Broadcast(mock.Anything, withdrawal).
		Return("", domain.HotWalletInsufficientFundsError{Err: fmt.Errorf("balance too low")})
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
		return w.Status == domain.WithdrawalStatusFailed &&
			w.FailureReason != nil &&
			w.RetryCount == 0 // no retry budget spent on a non-retryable error
	})).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)

	processor.BroadcastApproved(context.Background(), []*domain.Withdrawal{withdrawal})
}

func TestWithdrawalProcessor_SkipsWithdrawalsNotYetDueForRetry(t *testing.T) {
	processor, _ := newProcessor(t)
	withdrawal := claimedWithdrawal()
	future := time.Now().Add(time.Hour)
	withdrawal.NextRetryAt = &future
	withdrawal.Status = domain.WithdrawalStatusApproved

	// No broadcaster or persistence calls expected: the row is skipped.
	processor.BroadcastApproved(context.Background(), []*domain.Withdrawal{withdrawal})
}

func TestWithdrawalProcessor_OptimisticLockConflictIsTolerated(t *testing.T) {
	processor, m := newProcessor(t)
	withdrawal := claimedWithdrawal()

	// Another actor (e.g. a cancel) changed the row after we claimed it; the
	// save loses the race and the processor must not retry or crash.
	m.broadcaster.EXPECT().Broadcast(mock.Anything, withdrawal).Return("tx-hash-1", nil)
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.Anything).
		Return(domain.OptimisticLockError{Resource: "withdrawal", ID: "withdrawal-1"})

	processor.BroadcastApproved(context.Background(), []*domain.Withdrawal{withdrawal})
}

func TestApplyBroadcastResult_RejectsInvalidTransition(t *testing.T) {
	m := processorMocks{
		withdrawals: mockrepository.NewMockWithdrawalRepository(t),
		events:      mockrepository.NewMockWithdrawalEventRepository(t),
		txManager:   mockrepository.NewMockTxManager(t),
	}
	service := NewWithdrawalService(m.withdrawals, m.events).WithTxManager(m.txManager)

	// BROADCASTING -> PENDING is not a valid transition; persisting it must
	// be a loud error, never a silent no-op.
	withdrawal := claimedWithdrawal()
	withdrawal.Status = domain.WithdrawalStatusPending
	err := service.ApplyBroadcastResult(context.Background(), withdrawal, domain.WithdrawalStatusBroadcasting)
	assert.Error(t, err)
	var validationErr domain.ValidationError
	assert.True(t, errors.As(err, &validationErr))
}

func TestWithdrawalProcessor_ProcessesBatchConcurrently(t *testing.T) {
	processor, m := newProcessor(t)
	const batch = 16

	var inFlight atomic.Int32
	var maxSeen atomic.Int32
	release := make(chan struct{})

	withdrawals := make([]*domain.Withdrawal, batch)
	for i := range withdrawals {
		w := claimedWithdrawal()
		w.ID = fmt.Sprintf("withdrawal-%d", i)
		withdrawals[i] = w
	}

	m.broadcaster.EXPECT().Broadcast(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, w *domain.Withdrawal) (string, error) {
			n := inFlight.Add(1)
			for {
				peak := maxSeen.Load()
				if n <= peak || maxSeen.CompareAndSwap(peak, n) {
					break
				}
			}
			<-release
			inFlight.Add(-1)
			return "tx-" + w.ID, nil
		})
	m.txManager.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	m.withdrawals.EXPECT().Save(mock.Anything, mock.Anything).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)

	time.AfterFunc(50*time.Millisecond, func() { close(release) })
	processor.BroadcastApproved(context.Background(), withdrawals)

	peak := maxSeen.Load()
	assert.Greater(t, peak, int32(1), "rows must be processed concurrently, not one RTT at a time")
	assert.LessOrEqual(t, peak, int32(maxConcurrentRowProcesses), "fan-out must respect the configured bound")
}

func TestClassifyBroadcastError(t *testing.T) {
	now := time.Now()
	base := time.Minute
	const maxRetries = 3

	networkErr := domain.NetworkError{Err: errors.New("connection timed out")}
	rateLimitErr := domain.RateLimitError{Err: errors.New("429 Too Many Requests")}
	insufficientFunds := domain.HotWalletInsufficientFundsError{Err: errors.New("balance too low")}
	validationErr := domain.TransactionValidationError{Err: errors.New("invalid transaction structure")}

	tests := []struct {
		name               string
		err                error
		retryCount         int
		wantStatus         domain.WithdrawalStatus
		wantRetryCount     int
		wantRetryDelay     *time.Duration
		wantReasonContains string
	}{
		{name: "retryable with attempts left", err: networkErr, retryCount: 0,
			wantStatus: domain.WithdrawalStatusApproved, wantRetryCount: 1, wantRetryDelay: ptr(time.Minute)},
		{name: "backoff doubles per attempt", err: rateLimitErr, retryCount: 2,
			wantStatus: domain.WithdrawalStatusApproved, wantRetryCount: 3, wantRetryDelay: ptr(4 * time.Minute)},
		{name: "retryable exhausted fails", err: networkErr, retryCount: maxRetries,
			wantStatus: domain.WithdrawalStatusFailed, wantRetryCount: maxRetries, wantReasonContains: "max retries exceeded"},
		{name: "insufficient funds fails immediately", err: insufficientFunds, retryCount: 0,
			wantStatus: domain.WithdrawalStatusFailed, wantRetryCount: 0, wantReasonContains: "balance too low"},
		{name: "validation fails immediately", err: validationErr, retryCount: 0,
			wantStatus: domain.WithdrawalStatusFailed, wantRetryCount: 0, wantReasonContains: "invalid transaction structure"},
		{name: "unknown error treated as retryable", err: errors.New("weird"), retryCount: 0,
			wantStatus: domain.WithdrawalStatusApproved, wantRetryCount: 1, wantRetryDelay: ptr(time.Minute)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome := classifyBroadcastError(tt.err, tt.retryCount, maxRetries, base, now)

			assert.Equal(t, tt.wantStatus, outcome.status)
			assert.Equal(t, tt.wantRetryCount, outcome.retryCount)
			if tt.wantRetryDelay == nil {
				assert.Nil(t, outcome.nextRetryAt)
			} else {
				assert.NotNil(t, outcome.nextRetryAt)
				assert.Equal(t, now.Add(*tt.wantRetryDelay), *outcome.nextRetryAt)
			}
			if tt.wantReasonContains == "" {
				assert.Nil(t, outcome.failureReason)
			} else {
				assert.NotNil(t, outcome.failureReason)
				assert.Contains(t, *outcome.failureReason, tt.wantReasonContains)
			}
		})
	}
}

func ptr(d time.Duration) *time.Duration { return &d }
