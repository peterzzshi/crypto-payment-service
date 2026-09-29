package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/httpx"
	"crypto-payment-service/internal/service"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ActorIDHeader carries the authenticated actor's identity, injected by the
// upstream IAM gateway (see CONTEXT.md "Trust Boundaries" and ADR-0001). This
// service does not authenticate the actor itself; it trusts the gateway and
// enforces domain rules (separation of duties, ownership) on the identity it
// receives.
const ActorIDHeader = "X-Actor-ID"

type rejectRequest struct {
	RejectionNote string `json:"rejection_note,omitempty"`
}

type statusResponse struct {
	WithdrawalID string `json:"withdrawal_id"`
	Status       string `json:"status"`
}

type WithdrawalHandler struct {
	service *service.WithdrawalService
}

func NewWithdrawalHandler(service *service.WithdrawalService) *WithdrawalHandler {
	return &WithdrawalHandler{service: service}
}

// withdrawalAction is one POST /withdrawals/{id}/<action> endpoint; the
// endpoints share routing, actor extraction, and error mapping, and differ
// only in the service call and the status reported on success.
type withdrawalAction struct {
	suffix        string
	successStatus string
	run           func(ctx context.Context, withdrawalID, actorID string, r *http.Request) error
}

func (h *WithdrawalHandler) actions() []withdrawalAction {
	return []withdrawalAction{
		// Cancel is customer-facing: the actor is the authenticated customer
		// and the service enforces ownership.
		{"/cancel", "CANCELLED", func(ctx context.Context, id, actor string, _ *http.Request) error {
			return h.service.Cancel(ctx, id, actor)
		}},
		// Approve/Reject are operator-facing: the actor is an Approver
		// (internal operator or risk service), never the owning customer —
		// enforced by the service.
		{"/approve", "APPROVED", func(ctx context.Context, id, actor string, _ *http.Request) error {
			return h.service.Approve(ctx, id, actor)
		}},
		{"/reject", "REJECTED", func(ctx context.Context, id, actor string, r *http.Request) error {
			var body rejectRequest
			if r.Body != nil && r.ContentLength != 0 {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return domain.ValidationError{Field: "body", Message: "invalid JSON"}
				}
			}
			return h.service.Reject(ctx, id, actor, body.RejectionNote)
		}},
	}
}

func (h *WithdrawalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/withdrawals/")
	for _, action := range h.actions() {
		withdrawalID, found := strings.CutSuffix(path, action.suffix)
		if !found {
			continue
		}
		h.serveAction(w, r, action, withdrawalID)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (h *WithdrawalHandler) serveAction(w http.ResponseWriter, r *http.Request, action withdrawalAction, withdrawalID string) {
	log := requestLog(r, withdrawalID).With(zap.String("action", action.suffix))

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if withdrawalID == "" {
		http.Error(w, "withdrawal ID required", http.StatusBadRequest)
		return
	}
	actorID, ok := actorFromRequest(w, r)
	if !ok {
		log.Error("missing actor identity")
		return
	}

	if err := action.run(r.Context(), withdrawalID, actorID, r); err != nil {
		log.Error("withdrawal action failed", zap.Error(err))
		httpx.WriteError(w, err)
		return
	}

	log.Info("withdrawal action completed")
	httpx.WriteJSON(w, log, http.StatusOK, statusResponse{WithdrawalID: withdrawalID, Status: action.successStatus})
}

// actorFromRequest extracts the trusted actor identity injected by the
// gateway. A missing header means the request never passed authentication.
func actorFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	actorID := strings.TrimSpace(r.Header.Get(ActorIDHeader))
	if actorID == "" {
		http.Error(w, "missing "+ActorIDHeader+" header", http.StatusUnauthorized)
		return "", false
	}
	return actorID, true
}

func requestLog(r *http.Request, withdrawalID string) *zap.Logger {
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
	}
	return zap.L().With(
		zap.String("request_id", requestID),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.String("remote", r.RemoteAddr),
		zap.String("withdrawal_id", withdrawalID),
	)
}
