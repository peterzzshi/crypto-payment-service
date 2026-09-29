package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/httpx"
	mockrepository "crypto-payment-service/internal/mocks/repository"
	"crypto-payment-service/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type handlerMocks struct {
	withdrawals *mockrepository.MockWithdrawalRepository
	events      *mockrepository.MockWithdrawalEventRepository
	txManager   *mockrepository.MockTxManager
}

func newHandler(t *testing.T) (*WithdrawalHandler, handlerMocks) {
	t.Helper()
	m := handlerMocks{
		withdrawals: mockrepository.NewMockWithdrawalRepository(t),
		events:      mockrepository.NewMockWithdrawalEventRepository(t),
		txManager:   mockrepository.NewMockTxManager(t),
	}
	svc := service.NewWithdrawalService(m.withdrawals, m.events).WithTxManager(m.txManager)
	return NewWithdrawalHandler(svc), m
}

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

func TestHandler_Approve_RequiresActorHeader(t *testing.T) {
	handler, _ := newHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/withdrawals/withdrawal-1/approve", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_Approve_Success(t *testing.T) {
	handler, m := newHandler(t)

	m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
		return w.Status == domain.WithdrawalStatusApproved
	})).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
		return e.EventType == domain.EventWithdrawalApproved && e.Metadata["actor_id"] == "operator-7"
	})).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/withdrawals/withdrawal-1/approve", nil)
	req.Header.Set(ActorIDHeader, "operator-7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "APPROVED")
}

func TestHandler_Approve_SeparationOfDutiesReturns403(t *testing.T) {
	handler, m := newHandler(t)

	// The actor owns the withdrawal: the gateway authenticated them, but the
	// domain forbids self-approval.
	m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)

	req := httptest.NewRequest(http.MethodPost, "/withdrawals/withdrawal-1/approve", nil)
	req.Header.Set(ActorIDHeader, "customer-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandler_Reject_RequiresActorHeader(t *testing.T) {
	handler, _ := newHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/withdrawals/withdrawal-1/reject", strings.NewReader(`{"rejection_note":"x"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_Reject_Success(t *testing.T) {
	handler, m := newHandler(t)

	m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
		return w.Status == domain.WithdrawalStatusRejected
	})).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
		return e.EventType == domain.EventWithdrawalRejected && e.Metadata["rejection_note"] == "suspicious"
	})).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/withdrawals/withdrawal-1/reject", strings.NewReader(`{"rejection_note":"suspicious"}`))
	req.Header.Set(ActorIDHeader, "operator-7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "REJECTED")
}

func TestHandler_Cancel_CustomerCancelsOwnWithdrawal(t *testing.T) {
	handler, m := newHandler(t)

	m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)
	runTx(m.txManager)
	m.withdrawals.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
		return w.Status == domain.WithdrawalStatusCancelled
	})).Return(nil)
	m.events.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/withdrawals/withdrawal-1/cancel", nil)
	req.Header.Set(ActorIDHeader, "customer-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandler_Cancel_OtherCustomersWithdrawalReturns404(t *testing.T) {
	handler, m := newHandler(t)

	// The actor is authenticated but does not own the withdrawal; the service
	// answers NotFound to avoid leaking existence.
	m.withdrawals.EXPECT().GetByID(mock.Anything, "withdrawal-1").Return(pendingWithdrawal(), nil)

	req := httptest.NewRequest(http.MethodPost, "/withdrawals/withdrawal-1/cancel", nil)
	req.Header.Set(ActorIDHeader, "customer-2")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStatusFromError_AuthorizationErrorIs403(t *testing.T) {
	assert.Equal(t, http.StatusForbidden, httpx.StatusFromError(domain.AuthorizationError{Reason: "nope"}))
	assert.Equal(t, http.StatusNotFound, httpx.StatusFromError(domain.NotFoundError{Resource: "withdrawal"}))
	assert.Equal(t, http.StatusBadRequest, httpx.StatusFromError(domain.ValidationError{Field: "status"}))
	assert.Equal(t, http.StatusInternalServerError, httpx.StatusFromError(errors.New("boom")))
}
