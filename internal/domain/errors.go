package domain

import (
	"fmt"
)

// Resource errors

type NotFoundError struct {
	Resource string
}

func (e NotFoundError) Error() string {
	if e.Resource != "" {
		return fmt.Sprintf("%s not found", e.Resource)
	}
	return "not found"
}

// OptimisticLockError indicates a write lost a concurrent-update race: the
// row's version no longer matched the version the caller last read. This is
// distinct from NotFoundError (the row was actually deleted) — callers
// should treat it as "reload and retry" or "skip until next tick", not as a
// hard failure.
type OptimisticLockError struct {
	Resource string
	ID       string
}

func (e OptimisticLockError) Error() string {
	return fmt.Sprintf("%s %s: optimistic lock conflict", e.Resource, e.ID)
}

type ConflictError struct {
	Resource string
	Message  string
}

func (e ConflictError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s conflict: %s", e.Resource, e.Message)
	}
	return fmt.Sprintf("%s conflict", e.Resource)
}

// Validation errors

type InvalidAddressError struct {
	Address string
	Reason  string
}

func (e InvalidAddressError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("invalid address: %s", e.Reason)
	}
	return "invalid address"
}

type InvalidAmountError struct {
	Amount string
}

func (e InvalidAmountError) Error() string {
	return "invalid amount"
}

type UnsupportedAssetError struct {
	Asset string
}

func (e UnsupportedAssetError) Error() string {
	return fmt.Sprintf("unsupported asset: %s", e.Asset)
}

// NetworkMismatchError is a terminal validation error: the deposit came on
// the wrong network for the currency (e.g., USDT on Tron when we expected
// Ethereum). Blockchain transactions are irreversible, so this cannot be
// retried — manual intervention required.
type NetworkMismatchError struct {
	Currency Currency
	Expected Network
	Got      Network
}

func (e NetworkMismatchError) Error() string {
	return fmt.Sprintf("network mismatch for %s: expected %s, got %s", e.Currency, e.Expected, e.Got)
}

type InsufficientFundsError struct {
	Available string
	Required  string
}

func (e InsufficientFundsError) Error() string {
	return "insufficient funds"
}

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

type ConfigurationError struct {
	Component string
	Reason    string
}

func (e ConfigurationError) Error() string {
	return fmt.Sprintf("configuration error in %s: %s", e.Component, e.Reason)
}

// Broadcast errors
//
// Retryable errors (transient failures):
// - NetworkError: timeouts, connection issues, node unavailable
// - RateLimitError: API rate limiting
//
// Non-retryable errors (permanent failures):
// - HotWalletInsufficientFundsError: not enough balance in hot wallet
// - TransactionValidationError: invalid tx structure, blacklisted address, exceeds limits

// Retryable errors

type NetworkError struct {
	Err error
}

func (e NetworkError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("network error: %v", e.Err)
	}
	return "network error"
}

func (e NetworkError) Unwrap() error { return e.Err }

type RateLimitError struct {
	Err error
}

func (e RateLimitError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("rate limit exceeded: %v", e.Err)
	}
	return "rate limit exceeded"
}

func (e RateLimitError) Unwrap() error { return e.Err }

// Non-retryable errors

type HotWalletInsufficientFundsError struct {
	Err error
}

func (e HotWalletInsufficientFundsError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("insufficient funds in hot wallet: %v", e.Err)
	}
	return "insufficient funds in hot wallet"
}

func (e HotWalletInsufficientFundsError) Unwrap() error { return e.Err }

type TransactionValidationError struct {
	Err error
}

func (e TransactionValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("transaction validation failed: %v", e.Err)
	}
	return "transaction validation failed"
}

func (e TransactionValidationError) Unwrap() error { return e.Err }

// Webhook errors

type WebhookBadPayloadError struct {
	Err error
}

func (e WebhookBadPayloadError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("bad webhook payload: %v", e.Err)
	}
	return "bad webhook payload"
}

func (e WebhookBadPayloadError) Unwrap() error { return e.Err }
