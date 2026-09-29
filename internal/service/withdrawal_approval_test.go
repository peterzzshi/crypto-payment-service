package service

import (
	"context"
	"errors"
	"testing"

	"crypto-payment-service/internal/domain"
	mockrepository "crypto-payment-service/internal/mocks/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type approvalMocks struct {
	withdrawals *mockrepository.MockWithdrawalRepository
	events      *mockrepository.MockWithdrawalEventRepository
	txManager   *mockrepository.MockTxManager
}

func newApprovalService(t *testing.T) (*WithdrawalService, approvalMocks) {
	t.Helper()
	m := approvalMocks{
		withdrawals: mockrepository.NewMockWithdrawalRepository(t),
		events:      mockrepository.NewMockWithdrawalEventRepository(t),
		txManager:   mockrepository.NewMockTxManager(t),
	}
	service := NewWithdrawalService(m.withdrawals, m.events).WithTxManager(m.txManager)
	return service, m
}

// runTx makes the mocked TxManager execute fn and propagate its error, so
// tests observe the same rollback signal the real transaction would produce.
func runTx(m *mockrepository.MockTxManager) {
	m.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
}

func pendingWithdrawal() *domain.Withdrawal {
	return &domain.Withdrawal{
		ID:         "withdrawal-1",
		CustomerID: "customer-1",
		Status:     domain.WithdrawalStatusPending,
		Version:    1,
	}
}

func TestWithdrawalService_Approve(t *testing.T) {
	t.Run("approver approves a pending withdrawal", func(t *testing.T) {
		service, m := newApprovalService(t)
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)
		runTx(m.txManager)
		m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
			return w.Status == domain.WithdrawalStatusApproved
		})).Return(nil)
		m.events.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
			return e.EventType == domain.EventWithdrawalApproved &&
				e.Metadata["actor_id"] == "operator-7" &&
				e.Metadata["actor_role"] == "approver"
		})).Return(nil)

		err := service.Approve(context.Background(), "withdrawal-1", "operator-7")
		assert.NoError(t, err)
	})

	t.Run("customer cannot approve their own withdrawal (separation of duties)", func(t *testing.T) {
		service, m := newApprovalService(t)
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)

		err := service.Approve(context.Background(), "withdrawal-1", "customer-1")
		require := assert.New(t)
		require.Error(err)
		var authErr domain.AuthorizationError
		require.True(errors.As(err, &authErr))
	})

	t.Run("only pending withdrawals can be approved", func(t *testing.T) {
		service, m := newApprovalService(t)
		w := pendingWithdrawal()
		w.Status = domain.WithdrawalStatusConfirming
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(w, nil)

		err := service.Approve(context.Background(), "withdrawal-1", "operator-7")
		assert.Error(t, err)
		var validationErr domain.ValidationError
		assert.True(t, errors.As(err, &validationErr))
	})

	t.Run("audit event failure rolls back the approval", func(t *testing.T) {
		service, m := newApprovalService(t)
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)
		runTx(m.txManager)
		m.withdrawals.EXPECT().Save(mock.Anything, mock.Anything).Return(nil)
		m.events.EXPECT().Create(mock.Anything, mock.Anything).Return(errors.New("event store down"))

		err := service.Approve(context.Background(), "withdrawal-1", "operator-7")
		assert.Error(t, err, "event write failure must propagate so the tx rolls back (ADR-0003)")
	})
}

func TestWithdrawalService_Reject(t *testing.T) {
	t.Run("approver rejects a pending withdrawal with a note", func(t *testing.T) {
		service, m := newApprovalService(t)
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)
		runTx(m.txManager)
		m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
			return w.Status == domain.WithdrawalStatusRejected &&
				w.FailureReason != nil && *w.FailureReason == "suspicious destination"
		})).Return(nil)
		m.events.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
			return e.EventType == domain.EventWithdrawalRejected &&
				e.Metadata["actor_id"] == "operator-7" &&
				e.Metadata["rejection_note"] == "suspicious destination"
		})).Return(nil)

		err := service.Reject(context.Background(), "withdrawal-1", "operator-7", "suspicious destination")
		assert.NoError(t, err)
	})

	t.Run("customer cannot reject their own withdrawal (separation of duties)", func(t *testing.T) {
		service, m := newApprovalService(t)
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)

		err := service.Reject(context.Background(), "withdrawal-1", "customer-1", "note")
		assert.Error(t, err)
		var authErr domain.AuthorizationError
		assert.True(t, errors.As(err, &authErr))
	})

	t.Run("only pending withdrawals can be rejected", func(t *testing.T) {
		service, m := newApprovalService(t)
		w := pendingWithdrawal()
		w.Status = domain.WithdrawalStatusApproved
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(w, nil)

		err := service.Reject(context.Background(), "withdrawal-1", "operator-7", "note")
		assert.Error(t, err)
		var validationErr domain.ValidationError
		assert.True(t, errors.As(err, &validationErr))
	})

	t.Run("audit event failure rolls back the rejection", func(t *testing.T) {
		service, m := newApprovalService(t)
		m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)
		runTx(m.txManager)
		m.withdrawals.EXPECT().Save(mock.Anything, mock.Anything).Return(nil)
		m.events.EXPECT().Create(mock.Anything, mock.Anything).Return(errors.New("event store down"))

		err := service.Reject(context.Background(), "withdrawal-1", "operator-7", "note")
		assert.Error(t, err, "event write failure must propagate so the tx rolls back (ADR-0003)")
	})
}
