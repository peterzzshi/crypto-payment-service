package broadcast

import (
	"context"
	"fmt"
	"strings"
	"time"

	"crypto-payment-service/internal/domain"
)

// Broadcaster abstracts transaction broadcasting and confirmation polling.
type Broadcaster interface {
	Broadcast(ctx context.Context, withdrawal *domain.Withdrawal) (txHash string, err error)
	Confirmations(ctx context.Context, txHash string) (int, error)
}

// StubBroadcaster simulates blockchain interactions for testing.
// Error simulation based on DestinationAddress content:
//   - "rate-limit": returns RateLimitError (retryable)
//   - "network-error": returns NetworkError (retryable)
//   - "insufficient-funds": returns HotWalletInsufficientFundsError (non-retryable)
//   - "invalid-tx": returns TransactionValidationError (non-retryable)
//   - Otherwise: returns successful stub transaction hash
//
// Confirmations returns the configured ConfirmationsValue.
type StubBroadcaster struct {
	ConfirmationsValue int
}

func (s StubBroadcaster) Broadcast(_ context.Context, withdrawal *domain.Withdrawal) (string, error) {
	address := withdrawal.DestinationAddress
	switch {
	case strings.Contains(address, "rate-limit"):
		return "", domain.RateLimitError{Err: fmt.Errorf("429 Too Many Requests")}
	case strings.Contains(address, "network-error"):
		return "", domain.NetworkError{Err: fmt.Errorf("connection timed out")}
	case strings.Contains(address, "insufficient-funds"):
		return "", domain.HotWalletInsufficientFundsError{Err: fmt.Errorf("balance too low")}
	case strings.Contains(address, "invalid-tx"):
		return "", domain.TransactionValidationError{Err: fmt.Errorf("invalid transaction structure")}
	}
	return fmt.Sprintf("stub-%s-%d", withdrawal.ID, time.Now().UnixNano()), nil
}

func (s StubBroadcaster) Confirmations(_ context.Context, _ string) (int, error) {
	return s.ConfirmationsValue, nil
}
