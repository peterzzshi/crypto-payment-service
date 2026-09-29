// Package httpx holds the HTTP plumbing shared by the API and webhook
// handlers: domain-error-to-status mapping and JSON responses.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"crypto-payment-service/internal/domain"

	"go.uber.org/zap"
)

// StatusFromError maps a domain error to its HTTP status. Unknown errors are
// 500 — the domain error taxonomy is the contract for everything else.
func StatusFromError(err error) int {
	switch {
	case errors.As(err, new(domain.NotFoundError)):
		return http.StatusNotFound
	case errors.As(err, new(domain.AuthorizationError)):
		return http.StatusForbidden
	case errors.As(err, new(domain.InvalidAddressError)),
		errors.As(err, new(domain.InvalidAmountError)),
		errors.As(err, new(domain.UnsupportedAssetError)),
		errors.As(err, new(domain.ValidationError)),
		errors.As(err, new(domain.WebhookBadPayloadError)):
		return http.StatusBadRequest
	case errors.As(err, new(domain.InsufficientFundsError)):
		return http.StatusPaymentRequired
	default:
		return http.StatusInternalServerError
	}
}

// WriteJSON encodes v as the response body. Encode failures after WriteHeader
// can only be logged — the status line is already on the wire.
func WriteJSON(w http.ResponseWriter, log *zap.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Warn("failed to encode response", zap.Error(err))
	}
}

// WriteError maps err to a status and writes the message as plain text.
func WriteError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), StatusFromError(err))
}
