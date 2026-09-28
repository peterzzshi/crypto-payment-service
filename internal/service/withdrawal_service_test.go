package service

import (
	"context"
	"math/big"
	"testing"

	"crypto-payment-service/internal/domain"
	mockrepository "crypto-payment-service/internal/mocks/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestWithdrawalService_Initiate(t *testing.T) {
	tests := []struct {
		name          string
		request       WithdrawalRequest
		setupMocks    func(*mockrepository.MockWithdrawalRepository, *mockrepository.MockWithdrawalEventRepository, *mockrepository.MockTxManager)
		expectError   bool
		errorType     interface{}
		validateResult func(*testing.T, *domain.Withdrawal)
	}{
		{
			name: "successful withdrawal initiation",
			request: WithdrawalRequest{
				CustomerID:            "customer-1",
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				AmountAtomic:          big.NewInt(1000000),
				DestinationAddress:    "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
				IdempotencyKey:        "idempotency-1",
				RequiredConfirmations: 6,
			},
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				wr.EXPECT().GetByIdempotencyKey(mock.Anything, "idempotency-1").
					Return(nil, domain.NotFoundError{Resource: "withdrawal"})
				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil)
				wr.EXPECT().Create(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
					return w.CustomerID == "customer-1" &&
						w.Currency == domain.CurrencyBTC &&
						w.Network == domain.NetworkBitcoin &&
						w.AmountAtomic.Cmp(big.NewInt(1000000)) == 0 &&
						w.Status == domain.WithdrawalStatusPending &&
						w.RequiredConfirmations == 6
				})).Return(nil)
				er.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
					return e.EventType == "CREATED"
				})).Return(nil)
			},
			expectError: false,
			validateResult: func(t *testing.T, w *domain.Withdrawal) {
				assert.Equal(t, "customer-1", w.CustomerID)
				assert.Equal(t, domain.CurrencyBTC, w.Currency)
				assert.Equal(t, domain.NetworkBitcoin, w.Network)
				assert.Equal(t, domain.WithdrawalStatusPending, w.Status)
				assert.Equal(t, 6, w.RequiredConfirmations)
			},
		},
		{
			name: "idempotent request returns existing withdrawal",
			request: WithdrawalRequest{
				CustomerID:            "customer-1",
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				AmountAtomic:          big.NewInt(1000000),
				DestinationAddress:    "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
				IdempotencyKey:        "idempotency-1",
				RequiredConfirmations: 6,
			},
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				existing := &domain.Withdrawal{
					ID:             "existing-id",
					CustomerID:     "customer-1",
					IdempotencyKey: "idempotency-1",
					Status:         domain.WithdrawalStatusConfirming,
				}
				wr.EXPECT().GetByIdempotencyKey(mock.Anything, "idempotency-1").
					Return(existing, nil)
			},
			expectError: false,
			validateResult: func(t *testing.T, w *domain.Withdrawal) {
				assert.Equal(t, "existing-id", w.ID)
				assert.Equal(t, domain.WithdrawalStatusConfirming, w.Status)
			},
		},
		{
			name: "invalid amount (zero)",
			request: WithdrawalRequest{
				CustomerID:            "customer-1",
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				AmountAtomic:          big.NewInt(0),
				DestinationAddress:    "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
				IdempotencyKey:        "idempotency-1",
				RequiredConfirmations: 6,
			},
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
			},
			expectError: true,
			errorType:   domain.InvalidAmountError{},
		},
		{
			name: "invalid required confirmations",
			request: WithdrawalRequest{
				CustomerID:            "customer-1",
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				AmountAtomic:          big.NewInt(1000000),
				DestinationAddress:    "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
				IdempotencyKey:        "idempotency-1",
				RequiredConfirmations: 0,
			},
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				wr.EXPECT().GetByIdempotencyKey(mock.Anything, "idempotency-1").
					Return(nil, domain.NotFoundError{Resource: "withdrawal"})
			},
			expectError: true,
			errorType:   domain.ValidationError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockWithdrawals := mockrepository.NewMockWithdrawalRepository(t)
			mockEvents := mockrepository.NewMockWithdrawalEventRepository(t)
			mockTxManager := mockrepository.NewMockTxManager(t)

			tt.setupMocks(mockWithdrawals, mockEvents, mockTxManager)

			service := NewWithdrawalService(mockWithdrawals, mockEvents).
				WithTxManager(mockTxManager)

			result, err := service.Initiate(context.Background(), tt.request)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.IsType(t, tt.errorType, err)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validateResult != nil {
					tt.validateResult(t, result)
				}
			}
		})
	}
}

func TestWithdrawalService_Cancel(t *testing.T) {
	tests := []struct {
		name        string
		withdrawalID string
		customerID  string
		setupMocks  func(*mockrepository.MockWithdrawalRepository, *mockrepository.MockWithdrawalEventRepository, *mockrepository.MockTxManager)
		expectError bool
		errorType   interface{}
	}{
		{
			name:         "successful cancellation",
			withdrawalID: "withdrawal-1",
			customerID:   "customer-1",
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				withdrawal := &domain.Withdrawal{
					ID:         "withdrawal-1",
					CustomerID: "customer-1",
					Status:     domain.WithdrawalStatusPending,
				}
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(withdrawal, nil)
				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil)
				wr.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
					return w.Status == domain.WithdrawalStatusCancelled
				})).Return(nil)
				er.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
					return e.EventType == "CANCELLED"
				})).Return(nil)
			},
			expectError: false,
		},
		{
			name:         "cannot cancel confirming withdrawal",
			withdrawalID: "withdrawal-1",
			customerID:   "customer-1",
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				withdrawal := &domain.Withdrawal{
					ID:         "withdrawal-1",
					CustomerID: "customer-1",
					Status:     domain.WithdrawalStatusConfirming,
				}
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(withdrawal, nil)
			},
			expectError: true,
			errorType:   domain.ValidationError{},
		},
		{
			name:         "customer mismatch",
			withdrawalID: "withdrawal-1",
			customerID:   "customer-2",
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				withdrawal := &domain.Withdrawal{
					ID:         "withdrawal-1",
					CustomerID: "customer-1",
					Status:     domain.WithdrawalStatusPending,
				}
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(withdrawal, nil)
			},
			expectError: true,
			errorType:   domain.NotFoundError{},
		},
		{
			name:         "withdrawal not found",
			withdrawalID: "withdrawal-1",
			customerID:   "customer-1",
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(nil, domain.NotFoundError{Resource: "withdrawal"})
			},
			expectError: true,
			errorType:   domain.NotFoundError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockWithdrawals := mockrepository.NewMockWithdrawalRepository(t)
			mockEvents := mockrepository.NewMockWithdrawalEventRepository(t)
			mockTxManager := mockrepository.NewMockTxManager(t)

			tt.setupMocks(mockWithdrawals, mockEvents, mockTxManager)

			service := NewWithdrawalService(mockWithdrawals, mockEvents).
				WithTxManager(mockTxManager)

			err := service.Cancel(context.Background(), tt.withdrawalID, tt.customerID)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.IsType(t, tt.errorType, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWithdrawalService_UpdateConfirmations(t *testing.T) {
	txHash := "tx-hash-123"

	tests := []struct {
		name           string
		withdrawalID   string
		confirmations  int
		txHash         *string
		setupMocks     func(*mockrepository.MockWithdrawalRepository, *mockrepository.MockWithdrawalEventRepository, *mockrepository.MockTxManager)
		expectError    bool
		validateResult func(*testing.T, *domain.Withdrawal)
	}{
		{
			name:          "update confirmations and complete",
			withdrawalID:  "withdrawal-1",
			confirmations: 6,
			txHash:        &txHash,
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				withdrawal := &domain.Withdrawal{
					ID:                    "withdrawal-1",
					Status:                domain.WithdrawalStatusConfirming,
					Confirmations:         3,
					RequiredConfirmations: 6,
					Version:               1,
				}
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(withdrawal, nil)
				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil)
				wr.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
					return w.Status == domain.WithdrawalStatusCompleted &&
						w.Confirmations == 6 &&
						w.TxHash != nil
				})).Return(nil)
				er.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
					return e.EventType == "CONFIRMATION_UPDATED"
				})).Return(nil)
				er.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
					return e.EventType == "STATUS_CHANGED"
				})).Return(nil)
			},
			expectError: false,
			validateResult: func(t *testing.T, w *domain.Withdrawal) {
				assert.Equal(t, domain.WithdrawalStatusCompleted, w.Status)
				assert.Equal(t, 6, w.Confirmations)
				assert.NotNil(t, w.TxHash)
			},
		},
		{
			name:          "transition from broadcasting to confirming",
			withdrawalID:  "withdrawal-1",
			confirmations: 0,
			txHash:        &txHash,
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				withdrawal := &domain.Withdrawal{
					ID:                    "withdrawal-1",
					Status:                domain.WithdrawalStatusBroadcasting,
					Confirmations:         0,
					RequiredConfirmations: 6,
					Version:               1,
				}
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(withdrawal, nil)
				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil)
				wr.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
					return w.Status == domain.WithdrawalStatusConfirming && w.TxHash != nil
				})).Return(nil)
				er.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.WithdrawalEvent) bool {
					return e.EventType == "STATUS_CHANGED"
				})).Return(nil)
			},
			expectError: false,
			validateResult: func(t *testing.T, w *domain.Withdrawal) {
				assert.Equal(t, domain.WithdrawalStatusConfirming, w.Status)
			},
		},
		{
			name:          "optimistic lock retry success",
			withdrawalID:  "withdrawal-1",
			confirmations: 5,
			setupMocks: func(wr *mockrepository.MockWithdrawalRepository, er *mockrepository.MockWithdrawalEventRepository, tm *mockrepository.MockTxManager) {
				withdrawal := &domain.Withdrawal{
					ID:                    "withdrawal-1",
					Status:                domain.WithdrawalStatusConfirming,
					Confirmations:         3,
					RequiredConfirmations: 6,
					Version:               1,
				}
				refetched := &domain.Withdrawal{
					ID:                    "withdrawal-1",
					Status:                domain.WithdrawalStatusConfirming,
					Confirmations:         4,
					RequiredConfirmations: 6,
					Version:               2,
				}
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(withdrawal, nil).Once()
				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(domain.OptimisticLockError{Resource: "withdrawal"}).Once()
				wr.EXPECT().GetByID(mock.Anything, "withdrawal-1").
					Return(refetched, nil).Once()
				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil).Once()
				wr.EXPECT().Save(mock.Anything, mock.MatchedBy(func(w *domain.Withdrawal) bool {
					return w.Confirmations == 5
				})).Return(nil)
				er.EXPECT().Create(mock.Anything, mock.AnythingOfType("*domain.WithdrawalEvent")).
					Return(nil).Maybe()
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockWithdrawals := mockrepository.NewMockWithdrawalRepository(t)
			mockEvents := mockrepository.NewMockWithdrawalEventRepository(t)
			mockTxManager := mockrepository.NewMockTxManager(t)

			tt.setupMocks(mockWithdrawals, mockEvents, mockTxManager)

			service := NewWithdrawalService(mockWithdrawals, mockEvents).
				WithTxManager(mockTxManager)

			result, err := service.UpdateConfirmations(context.Background(), tt.withdrawalID, tt.confirmations, tt.txHash)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validateResult != nil {
					tt.validateResult(t, result)
				}
			}
		})
	}
}

func TestIsValidWithdrawalTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     domain.WithdrawalStatus
		to       domain.WithdrawalStatus
		expected bool
	}{
		{"pending to approved", domain.WithdrawalStatusPending, domain.WithdrawalStatusApproved, true},
		{"pending to rejected", domain.WithdrawalStatusPending, domain.WithdrawalStatusRejected, true},
		{"pending to cancelled", domain.WithdrawalStatusPending, domain.WithdrawalStatusCancelled, true},
		{"approved to broadcasting", domain.WithdrawalStatusApproved, domain.WithdrawalStatusBroadcasting, true},
		{"approved to cancelled", domain.WithdrawalStatusApproved, domain.WithdrawalStatusCancelled, true},
		{"broadcasting to approved", domain.WithdrawalStatusBroadcasting, domain.WithdrawalStatusApproved, true},
		{"broadcasting to confirming", domain.WithdrawalStatusBroadcasting, domain.WithdrawalStatusConfirming, true},
		{"broadcasting to failed", domain.WithdrawalStatusBroadcasting, domain.WithdrawalStatusFailed, true},
		{"confirming to completed", domain.WithdrawalStatusConfirming, domain.WithdrawalStatusCompleted, true},
		{"confirming to failed", domain.WithdrawalStatusConfirming, domain.WithdrawalStatusFailed, true},
		{"completed to any", domain.WithdrawalStatusCompleted, domain.WithdrawalStatusPending, false},
		{"failed to any", domain.WithdrawalStatusFailed, domain.WithdrawalStatusPending, false},
		{"cancelled to any", domain.WithdrawalStatusCancelled, domain.WithdrawalStatusPending, false},
		{"rejected to any", domain.WithdrawalStatusRejected, domain.WithdrawalStatusPending, false},
		{"pending to broadcasting", domain.WithdrawalStatusPending, domain.WithdrawalStatusBroadcasting, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidWithdrawalTransition(tt.from, tt.to)
			assert.Equal(t, tt.expected, result)
		})
	}
}
