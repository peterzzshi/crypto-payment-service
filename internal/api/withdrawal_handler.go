package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/service"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type cancelRequest struct {
	CustomerID string `json:"customer_id"`
}

type approveRequest struct {
	CustomerID string `json:"customer_id"`
}

type rejectRequest struct {
	CustomerID     string `json:"customer_id"`
	RejectionNote  string `json:"rejection_note,omitempty"`
}

type WithdrawalHandler struct {
	service *service.WithdrawalService
}

func NewWithdrawalHandler(service *service.WithdrawalService) *WithdrawalHandler {
	return &WithdrawalHandler{service: service}
}

func (h *WithdrawalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/withdrawals/")

	if strings.HasSuffix(path, "/cancel") {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		withdrawalID := strings.TrimSuffix(path, "/cancel")
		if withdrawalID == "" {
			http.Error(w, "withdrawal ID required", http.StatusBadRequest)
			return
		}
		h.handleCancel(w, r, withdrawalID)
		return
	}

	if strings.HasSuffix(path, "/approve") {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		withdrawalID := strings.TrimSuffix(path, "/approve")
		if withdrawalID == "" {
			http.Error(w, "withdrawal ID required", http.StatusBadRequest)
			return
		}
		h.handleApprove(w, r, withdrawalID)
		return
	}

	if strings.HasSuffix(path, "/reject") {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		withdrawalID := strings.TrimSuffix(path, "/reject")
		if withdrawalID == "" {
			http.Error(w, "withdrawal ID required", http.StatusBadRequest)
			return
		}
		h.handleReject(w, r, withdrawalID)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (h *WithdrawalHandler) handleCancel(w http.ResponseWriter, r *http.Request, withdrawalID string) {
	defer r.Body.Close()

	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
	}

	log := zap.L().With(
		zap.String("request_id", requestID),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.String("remote", r.RemoteAddr),
		zap.String("withdrawal_id", withdrawalID),
	)
	ctx := r.Context()

	var req cancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.CustomerID == "" {
		log.Error("missing customer_id in request")
		http.Error(w, "customer_id is required", http.StatusBadRequest)
		return
	}

	log.Info("attempting to cancel withdrawal")

	if err := h.service.Cancel(ctx, withdrawalID, req.CustomerID); err != nil {
		status := statusFromError(err)
		log.Error("cancellation failed", zap.Error(err))
		http.Error(w, err.Error(), status)
		return
	}

	log.Info("cancellation completed successfully")

	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"withdrawal_id": withdrawalID,
		"status":        "CANCELLED",
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to encode response", zap.Error(err))
	}
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

func (h *WithdrawalHandler) handleApprove(w http.ResponseWriter, r *http.Request, withdrawalID string) {
	defer r.Body.Close()

	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
	}

	log := zap.L().With(
		zap.String("request_id", requestID),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.String("remote", r.RemoteAddr),
		zap.String("withdrawal_id", withdrawalID),
	)
	ctx := r.Context()

	var req approveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.CustomerID == "" {
		log.Error("missing customer_id in request")
		http.Error(w, "customer_id is required", http.StatusBadRequest)
		return
	}

	log.Info("attempting to approve withdrawal")

	if err := h.service.Approve(ctx, withdrawalID, req.CustomerID); err != nil {
		status := statusFromError(err)
		log.Error("approval failed", zap.Error(err))
		http.Error(w, err.Error(), status)
		return
	}

	log.Info("approval completed successfully")

	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"withdrawal_id": withdrawalID,
		"status":        "APPROVED",
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to encode response", zap.Error(err))
	}
}

func (h *WithdrawalHandler) handleReject(w http.ResponseWriter, r *http.Request, withdrawalID string) {
	defer r.Body.Close()

	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
	}

	log := zap.L().With(
		zap.String("request_id", requestID),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.String("remote", r.RemoteAddr),
		zap.String("withdrawal_id", withdrawalID),
	)
	ctx := r.Context()

	var req rejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("failed to decode request", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.CustomerID == "" {
		log.Error("missing customer_id in request")
		http.Error(w, "customer_id is required", http.StatusBadRequest)
		return
	}

	log.Info("attempting to reject withdrawal")

	if err := h.service.Reject(ctx, withdrawalID, req.CustomerID, req.RejectionNote); err != nil {
		status := statusFromError(err)
		log.Error("rejection failed", zap.Error(err))
		http.Error(w, err.Error(), status)
		return
	}

	log.Info("rejection completed successfully")

	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"withdrawal_id": withdrawalID,
		"status":        "REJECTED",
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to encode response", zap.Error(err))
	}
}
