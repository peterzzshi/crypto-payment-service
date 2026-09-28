package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"crypto-payment-service/internal/domain"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type DepositIngestor interface {
	Process(ctx context.Context, payload []byte) (*domain.Deposit, error)
}

type DepositHandler struct {
	Adapters map[string]DepositIngestor
	Secret   string
}

func (webhookHandler *DepositHandler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	start := time.Now()
	segment := strings.TrimPrefix(request.URL.Path, "/webhooks/")
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
	}

	log := zap.L().With(
		zap.String("request_id", requestID),
		zap.String("segment", segment),
		zap.String("path", request.URL.Path),
		zap.String("method", request.Method),
		zap.String("remote", request.RemoteAddr),
	)
	ctx := request.Context()

	ingestor, ok := webhookHandler.Adapters[segment]
	if !ok {
		log.Warn("webhook endpoint not found")
		http.Error(responseWriter, "not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		log.Error("failed to read request body", zap.Error(err))
		http.Error(responseWriter, "cannot read body", http.StatusBadRequest)
		return
	}
	defer func() {
		if closeErr := request.Body.Close(); closeErr != nil {
			log.Warn("failed to close request body", zap.Error(closeErr))
		}
	}()

	if webhookHandler.Secret != "" {
		signature := request.Header.Get("X-Signature")
		if !verifyHMAC(body, webhookHandler.Secret, signature) {
			log.Warn("invalid webhook signature")
			http.Error(responseWriter, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	deposit, err := ingestor.Process(ctx, body)
	if err != nil {
		status := statusFromError(err)
		log.Error("webhook processing failed", zap.Error(err))
		http.Error(responseWriter, err.Error(), status)
		return
	}

	log = log.With(
		zap.String("deposit_id", deposit.ID),
		zap.String("customer_id", deposit.CustomerID),
		zap.String("asset", string(deposit.Asset)),
		zap.String("status", string(deposit.Status)),
	)

	latency := time.Since(start).Milliseconds()
	log.Info("webhook processed successfully")

	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(responseWriter, `{"deposit_id":"%s","status":"%s","latency_ms":%d}`, deposit.ID, deposit.Status, latency); err != nil {
		log.Warn("failed to write response", zap.Error(err))
	}
}

func verifyHMAC(body []byte, secret, providedSignature string) bool {
	if providedSignature == "" {
		return false
	}
	hasher := hmac.New(sha256.New, []byte(secret))
	hasher.Write(body)
	expectedSignature := hex.EncodeToString(hasher.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(expectedSignature)), []byte(strings.ToLower(providedSignature)))
}

func statusFromError(err error) int {
	switch {
	case errors.As(err, new(domain.NotFoundError)):
		return http.StatusNotFound
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
