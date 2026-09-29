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

func TestDepositService_UpsertIncoming(t *testing.T) {
	tests := []struct {
		name           string
		request        domain.IncomingDeposit
		setupMocks     func(*mockrepository.MockDepositRepository, *mockrepository.MockDepositEventRepository, *mockrepository.MockTxManager)
		expectError    bool
		errorType      interface{}
		validateResult func(*testing.T, *domain.Deposit)
	}{
		{
			name: "new deposit created",
			request: domain.IncomingDeposit{
				CustomerID:            "customer-1",
				AddressID:             "address-1",
				ExternalTxID:          "tx-123",
				TxHash:                stringPtr("hash-123"),
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				Asset:                 domain.AssetBTC,
				AmountAtomic:          big.NewInt(1000000),
				Confirmations:         3,
				RequiredConfirmations: 6,
				TransactionMetadata:   map[string]any{"block_height": "800000"},
			},
			setupMocks: func(dr *mockrepository.MockDepositRepository, er *mockrepository.MockDepositEventRepository, tm *mockrepository.MockTxManager) {
				dr.EXPECT().GetByExternalTxID(mock.Anything, "tx-123").
					Return(nil, domain.NotFoundError{Resource: "deposit"})

				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil)

				dr.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d *domain.Deposit) bool {
					return d.CustomerID == "customer-1" &&
						d.AddressID == "address-1" &&
						d.ExternalTxID == "tx-123" &&
						d.Asset == domain.AssetBTC &&
						d.Status == domain.DepositStatusConfirming &&
						d.Confirmations == 3
				})).Return(nil)

				er.EXPECT().Create(mock.Anything, mock.MatchedBy(func(e *domain.DepositEvent) bool {
					return e.EventType == domain.EventDepositCreated
				})).Return(nil)
			},
			expectError: false,
			validateResult: func(t *testing.T, d *domain.Deposit) {
				assert.Equal(t, "customer-1", d.CustomerID)
				assert.Equal(t, domain.AssetBTC, d.Asset)
				assert.Equal(t, domain.DepositStatusConfirming, d.Status)
				assert.Equal(t, 3, d.Confirmations)
			},
		},
		{
			name: "idempotent - returns existing deposit",
			request: domain.IncomingDeposit{
				CustomerID:            "customer-1",
				AddressID:             "address-1",
				ExternalTxID:          "tx-123",
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				Asset:                 domain.AssetBTC,
				AmountAtomic:          big.NewInt(1000000),
				Confirmations:         5,
				RequiredConfirmations: 6,
			},
			setupMocks: func(dr *mockrepository.MockDepositRepository, er *mockrepository.MockDepositEventRepository, tm *mockrepository.MockTxManager) {
				existing := &domain.Deposit{
					ID:            "deposit-1",
					ExternalTxID:  "tx-123",
					Status:        domain.DepositStatusConfirming,
					Confirmations: 3,
				}
				dr.EXPECT().GetByExternalTxID(mock.Anything, "tx-123").
					Return(existing, nil)

				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil)

				dr.EXPECT().Save(mock.Anything, mock.MatchedBy(func(d *domain.Deposit) bool {
					return d.ID == "deposit-1" && d.Confirmations == 5
				})).Return(nil)

				er.EXPECT().Create(mock.Anything, mock.AnythingOfType("*domain.DepositEvent")).
					Return(nil).Maybe()
			},
			expectError: false,
			validateResult: func(t *testing.T, d *domain.Deposit) {
				assert.Equal(t, "deposit-1", d.ID)
				assert.Equal(t, 5, d.Confirmations)
			},
		},
		{
			name: "invalid amount rejected",
			request: domain.IncomingDeposit{
				CustomerID:            "customer-1",
				AddressID:             "address-1",
				ExternalTxID:          "tx-123",
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				Asset:                 domain.AssetBTC,
				AmountAtomic:          big.NewInt(0),
				Confirmations:         3,
				RequiredConfirmations: 6,
			},
			setupMocks: func(dr *mockrepository.MockDepositRepository, er *mockrepository.MockDepositEventRepository, tm *mockrepository.MockTxManager) {
			},
			expectError: true,
			errorType:   domain.InvalidAmountError{},
		},
		{
			name: "deposit completes when confirmations reach required",
			request: domain.IncomingDeposit{
				CustomerID:            "customer-1",
				AddressID:             "address-1",
				ExternalTxID:          "tx-123",
				Currency:              domain.CurrencyBTC,
				Network:               domain.NetworkBitcoin,
				Asset:                 domain.AssetBTC,
				AmountAtomic:          big.NewInt(1000000),
				Confirmations:         6,
				RequiredConfirmations: 6,
			},
			setupMocks: func(dr *mockrepository.MockDepositRepository, er *mockrepository.MockDepositEventRepository, tm *mockrepository.MockTxManager) {
				existing := &domain.Deposit{
					ID:                    "deposit-1",
					ExternalTxID:          "tx-123",
					Status:                domain.DepositStatusConfirming,
					Confirmations:         5,
					RequiredConfirmations: 6,
				}
				dr.EXPECT().GetByExternalTxID(mock.Anything, "tx-123").
					Return(existing, nil)

				tm.EXPECT().WithTx(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
					Run(func(ctx context.Context, fn func(context.Context) error) {
						_ = fn(ctx)
					}).
					Return(nil)

				dr.EXPECT().Save(mock.Anything, mock.MatchedBy(func(d *domain.Deposit) bool {
					return d.ID == "deposit-1" &&
						d.Status == domain.DepositStatusCompleted &&
						d.Confirmations == 6
				})).Return(nil)

				er.EXPECT().Create(mock.Anything, mock.AnythingOfType("*domain.DepositEvent")).
					Return(nil).Maybe()
			},
			expectError: false,
			validateResult: func(t *testing.T, d *domain.Deposit) {
				assert.Equal(t, "deposit-1", d.ID)
				assert.Equal(t, domain.DepositStatusCompleted, d.Status)
				assert.Equal(t, 6, d.Confirmations)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDeposits := mockrepository.NewMockDepositRepository(t)
			mockEvents := mockrepository.NewMockDepositEventRepository(t)
			mockTxManager := mockrepository.NewMockTxManager(t)

			tt.setupMocks(mockDeposits, mockEvents, mockTxManager)

			service := NewDepositService(mockDeposits, mockEvents).
				WithTxManager(mockTxManager)

			result, err := service.UpsertIncoming(context.Background(), tt.request)

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

func TestIsValidDepositTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     domain.DepositStatus
		to       domain.DepositStatus
		expected bool
	}{
		{"pending to confirming", domain.DepositStatusPending, domain.DepositStatusConfirming, true},
		{"pending to completed", domain.DepositStatusPending, domain.DepositStatusCompleted, true},
		{"pending to failed", domain.DepositStatusPending, domain.DepositStatusFailed, true},
		{"confirming to completed", domain.DepositStatusConfirming, domain.DepositStatusCompleted, true},
		{"confirming to failed", domain.DepositStatusConfirming, domain.DepositStatusFailed, true},
		{"completed to any", domain.DepositStatusCompleted, domain.DepositStatusPending, false},
		{"failed to any", domain.DepositStatusFailed, domain.DepositStatusPending, false},
		{"same status", domain.DepositStatusConfirming, domain.DepositStatusConfirming, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidDepositTransition(tt.from, tt.to)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func stringPtr(s string) *string {
	return &s
}
