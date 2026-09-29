package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/repository"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

type WithdrawalService struct {
	withdrawals repository.WithdrawalRepository
	events      repository.WithdrawalEventRepository
	txManager   repository.TxManager
	validate    *validator.Validate
}

type WithdrawalRequest struct {
	CustomerID            string          `validate:"required"`
	Currency              domain.Currency `validate:"required"`
	Network               domain.Network  `validate:"required"`
	AmountAtomic          *big.Int        `validate:"required"`
	DestinationAddress    string          `validate:"required"`
	IdempotencyKey        string          `validate:"required"`
	RequiredConfirmations int             `validate:"gte=0"`
}

func NewWithdrawalService(withdrawals repository.WithdrawalRepository, events repository.WithdrawalEventRepository) *WithdrawalService {
	return &WithdrawalService{
		withdrawals: withdrawals,
		events:      events,
		validate:    validator.New(),
	}
}

func (s *WithdrawalService) WithTxManager(txManager repository.TxManager) *WithdrawalService {
	s.txManager = txManager
	return s
}

func (s *WithdrawalService) reposFor(ctx context.Context) (repository.WithdrawalRepository, repository.WithdrawalEventRepository) {
	if repos, ok := repository.GetTxRepos(ctx); ok {
		return repos.Withdrawals, repos.WithdrawalEvents
	}
	return s.withdrawals, s.events
}

func (s *WithdrawalService) withTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.txManager == nil {
		return fn(ctx)
	}
	return s.txManager.WithTx(ctx, fn)
}

func (s *WithdrawalService) Initiate(ctx context.Context, request WithdrawalRequest) (*domain.Withdrawal, error) {
	log := zap.L()

	if err := s.validate.Struct(request); err != nil {
		log.Warn("withdrawal request validation failed", zap.Error(err))
		return nil, err
	}

	if request.AmountAtomic == nil || request.AmountAtomic.Cmp(big.NewInt(0)) <= 0 {
		return nil, domain.InvalidAmountError{}
	}

	log = log.With(
		zap.String("customer_id", request.CustomerID),
		zap.String("currency", string(request.Currency)),
		zap.String("network", string(request.Network)),
		zap.String("idempotency_key", request.IdempotencyKey),
		zap.String("destination", request.DestinationAddress),
	)

	log.Info("initiating withdrawal")

	existing, err := s.withdrawals.GetByIdempotencyKey(ctx, request.IdempotencyKey)
	if err == nil {
		log.Info("idempotent withdrawal request, returning existing withdrawal")
		return existing, nil
	}
	var notFound domain.NotFoundError
	if !errors.As(err, &notFound) {
		log.Error("failed to check idempotency key", zap.Error(err))
		return nil, err
	}

	if request.RequiredConfirmations <= 0 {
		log.Error("invalid required confirmations")
		return nil, domain.ValidationError{
			Field:   "required_confirmations",
			Message: "must be greater than 0 for withdrawals",
		}
	}

	withdrawal := &domain.Withdrawal{
		CustomerID:            request.CustomerID,
		IdempotencyKey:        request.IdempotencyKey,
		DestinationAddress:    request.DestinationAddress,
		Currency:              request.Currency,
		Network:               request.Network,
		AmountAtomic:          cloneBig(request.AmountAtomic),
		Status:                domain.WithdrawalStatusPending,
		Confirmations:         0,
		RequiredConfirmations: request.RequiredConfirmations,
	}

	err = s.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := s.reposFor(ctx)
		if err := withdrawals.Create(ctx, withdrawal); err != nil {
			return err
		}
		return emitWithdrawalEvent(ctx, events, domain.EventWithdrawalCreated, withdrawal, nil, &withdrawal.Status,
			map[string]any{"idempotency_key": withdrawal.IdempotencyKey})
	})
	if err != nil {
		// A concurrent request with the same idempotency key won the
		// check-then-create race: the unique constraint fired. Return the
		// winning row instead of surfacing a conflict to the caller.
		var conflict domain.ConflictError
		if errors.As(err, &conflict) {
			existing, getErr := s.withdrawals.GetByIdempotencyKey(ctx, request.IdempotencyKey)
			if getErr == nil {
				log.Info("idempotent withdrawal request (concurrent), returning existing withdrawal")
				return existing, nil
			}
		}
		log.Error("failed to create withdrawal", zap.Error(err))
		return nil, err
	}
	log.Info("withdrawal initiated successfully")
	return withdrawal, nil
}

// Cancel cancels a PENDING or APPROVED withdrawal. The actor must be the
// Customer who owns the withdrawal; a mismatch returns NotFoundError to avoid
// leaking the existence of other customers' withdrawals.
func (s *WithdrawalService) Cancel(ctx context.Context, withdrawalID, actorID string) error {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.String("actor_id", actorID),
	)

	if withdrawalID == "" {
		return domain.ValidationError{Field: "withdrawalID", Message: "required"}
	}
	if actorID == "" {
		return domain.ValidationError{Field: "actorID", Message: "required"}
	}

	log.Info("attempting to cancel withdrawal")

	withdrawal, err := s.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return err
	}

	if withdrawal.CustomerID != actorID {
		log.Warn("actor is not the owning customer")
		return domain.NotFoundError{Resource: "withdrawal"}
	}

	if withdrawal.Status != domain.WithdrawalStatusPending && withdrawal.Status != domain.WithdrawalStatusApproved {
		log.Warn(fmt.Sprintf("cannot cancel withdrawal in %s status", withdrawal.Status))
		return domain.ValidationError{
			Field:   "status",
			Message: fmt.Sprintf("only PENDING or APPROVED withdrawals can be cancelled, current status: %s", withdrawal.Status),
		}
	}

	fromStatus := withdrawal.Status
	withdrawal.Status = domain.WithdrawalStatusCancelled
	err = s.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := s.reposFor(ctx)
		if err := withdrawals.Save(ctx, withdrawal); err != nil {
			return err
		}
		return emitWithdrawalEvent(ctx, events, domain.EventWithdrawalCancelled, withdrawal, &fromStatus, &withdrawal.Status,
			map[string]any{"actor_id": actorID, "actor_role": "customer", "reason": "user_requested"})
	})
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			log.Warn("optimistic lock conflict cancelling withdrawal", zap.Error(err))
		} else {
			log.Error("failed to update withdrawal status", zap.Error(err))
		}
		return err
	}

	log.Info("withdrawal cancelled successfully")
	return nil
}

// Approve marks a PENDING withdrawal as ready for broadcast. The actor is an
// Approver (internal operator or risk service) and is recorded in the audit
// event. Separation of duties: the Approver can never be the Customer who
// owns the withdrawal.
func (s *WithdrawalService) Approve(ctx context.Context, withdrawalID, actorID string) error {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.String("actor_id", actorID),
	)

	if withdrawalID == "" {
		return domain.ValidationError{Field: "withdrawalID", Message: "required"}
	}
	if actorID == "" {
		return domain.ValidationError{Field: "actorID", Message: "required"}
	}

	log.Info("attempting to approve withdrawal")

	withdrawal, err := s.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return err
	}

	if withdrawal.CustomerID == actorID {
		log.Warn("approver must not be the owning customer (separation of duties)")
		return domain.AuthorizationError{Reason: "approver must not be the withdrawal owner"}
	}

	if withdrawal.Status != domain.WithdrawalStatusPending {
		log.Warn(fmt.Sprintf("cannot approve withdrawal in %s status", withdrawal.Status))
		return domain.ValidationError{
			Field:   "status",
			Message: fmt.Sprintf("only PENDING withdrawals can be approved, current status: %s", withdrawal.Status),
		}
	}

	fromStatus := withdrawal.Status
	withdrawal.Status = domain.WithdrawalStatusApproved
	err = s.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := s.reposFor(ctx)
		if err := withdrawals.Save(ctx, withdrawal); err != nil {
			return err
		}
		return emitWithdrawalEvent(ctx, events, domain.EventWithdrawalApproved, withdrawal, &fromStatus, &withdrawal.Status,
			map[string]any{"actor_id": actorID, "actor_role": "approver"})
	})
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			log.Warn("optimistic lock conflict approving withdrawal", zap.Error(err))
		} else {
			log.Error("failed to update withdrawal status", zap.Error(err))
		}
		return err
	}

	log.Info("withdrawal approved successfully")
	return nil
}

// Reject marks a PENDING withdrawal as terminally rejected, with the same
// Approver identity and separation-of-duties rules as Approve.
func (s *WithdrawalService) Reject(ctx context.Context, withdrawalID, actorID, rejectionNote string) error {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.String("actor_id", actorID),
	)

	if withdrawalID == "" {
		return domain.ValidationError{Field: "withdrawalID", Message: "required"}
	}
	if actorID == "" {
		return domain.ValidationError{Field: "actorID", Message: "required"}
	}

	log.Info("attempting to reject withdrawal")

	withdrawal, err := s.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return err
	}

	if withdrawal.CustomerID == actorID {
		log.Warn("rejector must not be the owning customer (separation of duties)")
		return domain.AuthorizationError{Reason: "rejector must not be the withdrawal owner"}
	}

	if withdrawal.Status != domain.WithdrawalStatusPending {
		log.Warn(fmt.Sprintf("cannot reject withdrawal in %s status", withdrawal.Status))
		return domain.ValidationError{
			Field:   "status",
			Message: fmt.Sprintf("only PENDING withdrawals can be rejected, current status: %s", withdrawal.Status),
		}
	}

	fromStatus := withdrawal.Status
	withdrawal.Status = domain.WithdrawalStatusRejected
	if rejectionNote != "" {
		withdrawal.FailureReason = &rejectionNote
	}
	err = s.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := s.reposFor(ctx)
		if err := withdrawals.Save(ctx, withdrawal); err != nil {
			return err
		}
		metadata := map[string]any{"actor_id": actorID, "actor_role": "approver"}
		if rejectionNote != "" {
			metadata["rejection_note"] = rejectionNote
		}
		return emitWithdrawalEvent(ctx, events, domain.EventWithdrawalRejected, withdrawal, &fromStatus, &withdrawal.Status, metadata)
	})
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			log.Warn("optimistic lock conflict rejecting withdrawal", zap.Error(err))
		} else {
			log.Error("failed to update withdrawal status", zap.Error(err))
		}
		return err
	}

	log.Info("withdrawal rejected successfully")
	return nil
}

// ApplyBroadcastResult persists the outcome of a broadcast attempt — success
// (CONFIRMING with tx hash) or failure (APPROVED with retry metadata, or
// FAILED) — atomically with its audit event. The withdrawal must carry the
// version obtained when the row was claimed (BROADCASTING). An invalid
// transition is a loud error, not a silent no-op: persisting a wrong state on
// a money-movement path must never be swallowed.
func (s *WithdrawalService) ApplyBroadcastResult(ctx context.Context, withdrawal *domain.Withdrawal, prevStatus domain.WithdrawalStatus) error {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawal.ID),
		zap.String("from_status", string(prevStatus)),
		zap.String("to_status", string(withdrawal.Status)),
	)

	if !IsValidWithdrawalTransition(prevStatus, withdrawal.Status) {
		log.Error("invalid broadcast result transition, refusing to persist")
		return domain.ValidationError{
			Field:   "status",
			Message: fmt.Sprintf("invalid transition from %s to %s", prevStatus, withdrawal.Status),
		}
	}

	metadata := map[string]any{"retry_count": withdrawal.RetryCount}
	if withdrawal.TxHash != nil {
		metadata["tx_hash"] = *withdrawal.TxHash
	}
	if withdrawal.FailureReason != nil {
		metadata["failure_reason"] = *withdrawal.FailureReason
	}

	err := s.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := s.reposFor(ctx)
		if err := withdrawals.Save(ctx, withdrawal); err != nil {
			return err
		}
		return emitWithdrawalEvent(ctx, events, domain.EventWithdrawalStatusChanged, withdrawal, &prevStatus, &withdrawal.Status, metadata)
	})
	if err != nil {
		return err
	}
	log.Info("broadcast result persisted")
	return nil
}

func (s *WithdrawalService) UpdateConfirmations(ctx context.Context, withdrawalID string, confirmations int, txHash *string) (*domain.Withdrawal, error) {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.Int("confirmations", confirmations),
	)

	withdrawal, err := s.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return nil, err
	}

	updated, ok, err := s.applyConfirmationUpdate(ctx, withdrawal, confirmations, txHash)
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			log.Warn("optimistic lock conflict on confirmation update, retrying once", zap.Error(err))
			refetched, getErr := s.withdrawals.GetByID(ctx, withdrawalID)
			if getErr != nil {
				log.Error("failed to re-fetch withdrawal after lock conflict", zap.Error(getErr))
				return nil, getErr
			}
			updated, ok, err = s.applyConfirmationUpdate(ctx, refetched, confirmations, txHash)
			if err != nil {
				log.Error("failed to save withdrawal update after retry", zap.Error(err))
				return nil, err
			}
		} else {
			log.Error("failed to save withdrawal update", zap.Error(err))
			return nil, err
		}
	}
	if !ok {
		return withdrawal, nil
	}
	log.Info("withdrawal confirmations updated successfully")
	return updated, nil
}

func (s *WithdrawalService) applyConfirmationUpdate(ctx context.Context, existing *domain.Withdrawal, confirmations int, txHash *string) (*domain.Withdrawal, bool, error) {
	log := zap.L()

	updated := cloneWithdrawal(existing)
	prevStatus := existing.Status
	prevConfirmations := existing.Confirmations

	if txHash != nil {
		updated.TxHash = txHash
	}
	updated.Confirmations = max(existing.Confirmations, confirmations)
	newStatus := nextWithdrawalStatus(updated.Confirmations, updated.RequiredConfirmations, existing.Status)

	if !IsValidWithdrawalTransition(existing.Status, newStatus) {
		log.Warn("invalid status transition ignored",
			zap.String("from", string(existing.Status)), zap.String("to", string(newStatus)))
		return nil, false, nil
	}

	updated.Status = newStatus

	err := s.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := s.reposFor(ctx)
		if err := withdrawals.Save(ctx, &updated); err != nil {
			return err
		}
		return emitWithdrawalProgressEvents(ctx, events, &updated, prevStatus, prevConfirmations)
	})
	if err != nil {
		return nil, false, err
	}
	return &updated, true, nil
}

func cloneBig(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}

func cloneWithdrawal(w *domain.Withdrawal) domain.Withdrawal {
	clone := *w
	if w.AmountAtomic != nil {
		clone.AmountAtomic = new(big.Int).Set(w.AmountAtomic)
	}
	if w.TxHash != nil {
		hash := *w.TxHash
		clone.TxHash = &hash
	}
	if w.NextRetryAt != nil {
		retryAt := *w.NextRetryAt
		clone.NextRetryAt = &retryAt
	}
	if w.FailureReason != nil {
		reason := *w.FailureReason
		clone.FailureReason = &reason
	}
	if w.TransactionMetadata != nil {
		clone.TransactionMetadata = make(map[string]any, len(w.TransactionMetadata))
		for k, v := range w.TransactionMetadata {
			clone.TransactionMetadata[k] = v
		}
	}
	return clone
}

func nextWithdrawalStatus(confirmations, required int, currentStatus domain.WithdrawalStatus) domain.WithdrawalStatus {
	if confirmations >= required {
		return domain.WithdrawalStatusCompleted
	}
	if currentStatus == domain.WithdrawalStatusConfirming {
		return domain.WithdrawalStatusConfirming
	}
	if currentStatus == domain.WithdrawalStatusBroadcasting {
		return domain.WithdrawalStatusConfirming
	}
	return currentStatus
}

// emitWithdrawalEvent writes a single audit event. Failures propagate so the
// surrounding transaction rolls back: a state change without its audit event
// is a failed state change (ADR-0003).
func emitWithdrawalEvent(ctx context.Context, events repository.WithdrawalEventRepository, eventType string, withdrawal *domain.Withdrawal, fromStatus, toStatus *domain.WithdrawalStatus, metadata map[string]any) error {
	if events == nil {
		return nil
	}
	event := domain.WithdrawalEvent{
		WithdrawalID: withdrawal.ID,
		EventType:    eventType,
		FromStatus:   fromStatus,
		ToStatus:     toStatus,
		Metadata:     metadata,
		CreatedAt:    time.Now(),
	}
	if err := events.Create(ctx, &event); err != nil {
		zap.L().Error("failed to create withdrawal event",
			zap.String("event_type", eventType), zap.Error(err))
		return fmt.Errorf("audit event %s: %w", eventType, err)
	}
	return nil
}

func emitWithdrawalProgressEvents(ctx context.Context, events repository.WithdrawalEventRepository, withdrawal *domain.Withdrawal, prevStatus domain.WithdrawalStatus, prevConfs int) error {
	if withdrawal.Confirmations > prevConfs {
		if err := emitWithdrawalEvent(ctx, events, domain.EventWithdrawalConfirmationUpdated, withdrawal, nil, nil,
			map[string]any{"confirmations": withdrawal.Confirmations}); err != nil {
			return err
		}
	}
	if withdrawal.Status != prevStatus {
		if err := emitWithdrawalEvent(ctx, events, domain.EventWithdrawalStatusChanged, withdrawal, &prevStatus, &withdrawal.Status, nil); err != nil {
			return err
		}
	}
	return nil
}

func IsValidWithdrawalTransition(from, to domain.WithdrawalStatus) bool {
	if from == to {
		return true
	}

	switch from {
	case domain.WithdrawalStatusCompleted, domain.WithdrawalStatusFailed, domain.WithdrawalStatusCancelled, domain.WithdrawalStatusRejected:
		return false

	case domain.WithdrawalStatusPending:
		return to == domain.WithdrawalStatusApproved ||
			to == domain.WithdrawalStatusRejected ||
			to == domain.WithdrawalStatusCancelled

	case domain.WithdrawalStatusApproved:
		return to == domain.WithdrawalStatusBroadcasting ||
			to == domain.WithdrawalStatusCancelled

	case domain.WithdrawalStatusBroadcasting:
		return to == domain.WithdrawalStatusApproved ||
			to == domain.WithdrawalStatusConfirming ||
			to == domain.WithdrawalStatusFailed

	case domain.WithdrawalStatusConfirming:
		return to == domain.WithdrawalStatusCompleted || to == domain.WithdrawalStatusFailed

	default:
		return false
	}
}
