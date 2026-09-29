package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"

	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/httpx"

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

type depositResponse struct {
	DepositID string `json:"deposit_id"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

func (h *DepositHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	segment := strings.TrimPrefix(r.URL.Path, "/webhooks/")
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
	}

	log := zap.L().With(
		zap.String("request_id", requestID),
		zap.String("segment", segment),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.String("remote", r.RemoteAddr),
	)

	ingestor, ok := h.Adapters[segment]
	if !ok {
		log.Warn("webhook endpoint not found")
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error("failed to read request body", zap.Error(err))
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	if h.Secret != "" {
		if !verifyHMAC(body, h.Secret, r.Header.Get("X-Signature")) {
			log.Warn("invalid webhook signature")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	deposit, err := ingestor.Process(r.Context(), body)
	if err != nil {
		log.Error("webhook processing failed", zap.Error(err))
		httpx.WriteError(w, err)
		return
	}

	log.With(
		zap.String("deposit_id", deposit.ID),
		zap.String("customer_id", deposit.CustomerID),
		zap.String("status", string(deposit.Status)),
	).Info("webhook processed successfully")

	httpx.WriteJSON(w, log, http.StatusOK, depositResponse{
		DepositID: deposit.ID,
		Status:    string(deposit.Status),
		LatencyMs: time.Since(start).Milliseconds(),
	})
}

func verifyHMAC(body []byte, secret, providedSignature string) bool {
	if providedSignature == "" {
		return false
	}
	hasher := hmac.New(sha256.New, []byte(secret))
	hasher.Write(body)
	expected := hasher.Sum(nil)
	provided, err := hex.DecodeString(strings.ToLower(providedSignature))
	if err != nil {
		return false
	}
	return hmac.Equal(expected, provided)
}
