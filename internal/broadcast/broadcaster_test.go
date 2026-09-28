package broadcast_test

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"
)

func TestStubBroadcaster_Broadcast(t *testing.T) {
	stub := broadcast.StubBroadcaster{ConfirmationsValue: 6}

	withdrawal := &domain.Withdrawal{
		ID:           "test-id-123",
		Asset:        domain.AssetBTC,
		AmountAtomic: big.NewInt(100000),
	}

	ctx := context.Background()
	txHash, err := stub.Broadcast(ctx, withdrawal)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if txHash == "" {
		t.Fatal("expected non-empty tx_hash")
	}

	if !strings.Contains(txHash, "test-id-123") {
		t.Errorf("tx_hash should contain withdrawal ID, got: %s", txHash)
	}
}

func TestStubBroadcaster_RetryableErrors(t *testing.T) {
	stub := broadcast.StubBroadcaster{}
	ctx := context.Background()

	tests := []struct {
		name            string
		address         string
		expectedMessage string
		errorType       string
	}{
		{
			name:            "rate_limit",
			address:         "bc1qrate-limit-test",
			expectedMessage: "rate limit",
			errorType:       "RateLimitError",
		},
		{
			name:            "network_error",
			address:         "bc1qnetwork-error-test",
			expectedMessage: "network",
			errorType:       "NetworkError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withdrawal := &domain.Withdrawal{
				ID:                 "test-temp",
				Asset:              domain.AssetBTC,
				AmountAtomic:       big.NewInt(100000),
				DestinationAddress: tt.address,
			}

			_, err := stub.Broadcast(ctx, withdrawal)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			switch tt.errorType {
			case "RateLimitError":
				var rateLimitErr domain.RateLimitError
				if !errors.As(err, &rateLimitErr) {
					t.Errorf("expected RateLimitError, got %T", err)
				}
			case "NetworkError":
				var networkErr domain.NetworkError
				if !errors.As(err, &networkErr) {
					t.Errorf("expected NetworkError, got %T", err)
				}
			}

			if !strings.Contains(err.Error(), tt.expectedMessage) {
				t.Errorf("expected error message to contain %q, got: %v", tt.expectedMessage, err)
			}
		})
	}
}

func TestStubBroadcaster_NonRetryableErrors(t *testing.T) {
	stub := broadcast.StubBroadcaster{}
	ctx := context.Background()

	tests := []struct {
		name            string
		address         string
		expectedMessage string
		errorType       string
	}{
		{
			name:            "insufficient_funds",
			address:         "bc1qinsufficient-funds-test",
			expectedMessage: "insufficient funds",
			errorType:       "HotWalletInsufficientFundsError",
		},
		{
			name:            "invalid_transaction",
			address:         "bc1qinvalid-tx-test",
			expectedMessage: "validation failed",
			errorType:       "TransactionValidationError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withdrawal := &domain.Withdrawal{
				ID:                 "test-perm",
				Asset:              domain.AssetBTC,
				AmountAtomic:       big.NewInt(100000),
				DestinationAddress: tt.address,
			}

			_, err := stub.Broadcast(ctx, withdrawal)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			switch tt.errorType {
			case "HotWalletInsufficientFundsError":
				var fundsErr domain.HotWalletInsufficientFundsError
				if !errors.As(err, &fundsErr) {
					t.Errorf("expected HotWalletInsufficientFundsError, got %T", err)
				}
			case "TransactionValidationError":
				var validationErr domain.TransactionValidationError
				if !errors.As(err, &validationErr) {
					t.Errorf("expected TransactionValidationError, got %T", err)
				}
			}

			if !strings.Contains(err.Error(), tt.expectedMessage) {
				t.Errorf("expected error message to contain %q, got: %v", tt.expectedMessage, err)
			}
		})
	}
}

func TestStubBroadcaster_Confirmations(t *testing.T) {
	expectedConfs := 12
	stub := broadcast.StubBroadcaster{ConfirmationsValue: expectedConfs}

	ctx := context.Background()
	confs, err := stub.Confirmations(ctx, "any-tx-hash")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if confs != expectedConfs {
		t.Errorf("expected %d confirmations, got %d", expectedConfs, confs)
	}
}

func TestStubBroadcaster_ConfigurableConfirmations(t *testing.T) {
	tests := []struct {
		name  string
		confs int
	}{
		{"zero", 0},
		{"one", 1},
		{"btc_threshold", 6},
		{"eth_threshold", 12},
		{"high", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := broadcast.StubBroadcaster{ConfirmationsValue: tt.confs}

			got, err := stub.Confirmations(context.Background(), "test-hash")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.confs {
				t.Errorf("expected %d, got %d", tt.confs, got)
			}
		})
	}
}

func TestStubBroadcaster_MultipleWithdrawals(t *testing.T) {
	stub := broadcast.StubBroadcaster{ConfirmationsValue: 6}
	ctx := context.Background()

	withdrawals := []*domain.Withdrawal{
		{ID: "tx-001", Asset: domain.AssetBTC, AmountAtomic: big.NewInt(100000)},
		{ID: "tx-002", Asset: domain.AssetETH, AmountAtomic: big.NewInt(1000000)},
		{ID: "tx-003", Asset: domain.AssetBTC, AmountAtomic: big.NewInt(50000)},
	}

	hashes := make(map[string]bool)
	for _, withdrawal := range withdrawals {
		hash, err := stub.Broadcast(ctx, withdrawal)
		if err != nil {
			t.Fatalf("broadcast failed for %s: %v", withdrawal.ID, err)
		}

		if hash == "" {
			t.Errorf("empty hash for %s", withdrawal.ID)
		}
		hashes[hash] = true
	}

	if len(hashes) != len(withdrawals) {
		t.Errorf("expected %d unique hashes, got %d", len(withdrawals), len(hashes))
	}
}
