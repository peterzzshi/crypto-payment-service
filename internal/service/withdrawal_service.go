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
	if withdrawals == nil {
		panic("withdrawals repository is required")
	}
	return &WithdrawalService{
		withdrawals: withdrawals,
		events:      events,
		validate:    validator.New(),
	}
}

func (service *WithdrawalService) WithTxManager(txManager repository.TxManager) *WithdrawalService {
	service.txManager = txManager
	return service
}

func (service *WithdrawalService) reposFor(ctx context.Context) (repository.WithdrawalRepository, repository.WithdrawalEventRepository) {
	if repos, ok := repository.GetTxRepos(ctx); ok {
		return repos.Withdrawals, repos.WithdrawalEvents
	}
	return service.withdrawals, service.events
}

func (service *WithdrawalService) withTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if service.txManager == nil {
		return fn(ctx)
	}
	return service.txManager.WithTx(ctx, fn)
}

func (service *WithdrawalService) Initiate(ctx context.Context, request WithdrawalRequest) (*domain.Withdrawal, error) {
	log := zap.L()

	if err := service.validate.Struct(request); err != nil {
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

	existing, err := service.withdrawals.GetByIdempotencyKey(ctx, request.IdempotencyKey)
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

	err = service.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := service.reposFor(ctx)
		if err := withdrawals.Create(ctx, withdrawal); err != nil {
			return err
		}
		emitWithdrawalCreated(ctx, events, withdrawal)
		return nil
	})
	if err != nil {
		log.Error("failed to create withdrawal", zap.Error(err))
		return nil, err
	}
	log.Info("withdrawal initiated successfully")
	return withdrawal, nil
}

func (service *WithdrawalService) Cancel(ctx context.Context, withdrawalID, customerID string) error {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.String("customer_id", customerID),
	)

	if withdrawalID == "" {
		return domain.ValidationError{Field: "withdrawalID", Message: "required"}
	}
	if customerID == "" {
		return domain.ValidationError{Field: "customerID", Message: "required"}
	}

	log.Info("attempting to cancel withdrawal")

	withdrawal, err := service.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return err
	}

	if withdrawal.CustomerID != customerID {
		log.Warn("customer ID mismatch")
		return domain.NotFoundError{Resource: "withdrawal"}
	}

	if withdrawal.Status != domain.WithdrawalStatusPending {
		log.Warn(fmt.Sprintf("cannot cancel withdrawal in %s status", withdrawal.Status))
		return domain.ValidationError{
			Field:   "status",
			Message: fmt.Sprintf("only PENDING withdrawals can be cancelled, current status: %s", withdrawal.Status),
		}
	}

	withdrawal.Status = domain.WithdrawalStatusCancelled
	err = service.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := service.reposFor(ctx)
		if err := withdrawals.Save(ctx, withdrawal); err != nil {
			return err
		}
		emitWithdrawalCancelled(ctx, events, withdrawal, domain.WithdrawalStatusPending)
		return nil
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

func (service *WithdrawalService) Approve(ctx context.Context, withdrawalID, customerID string) error {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.String("customer_id", customerID),
	)

	if withdrawalID == "" {
		return domain.ValidationError{Field: "withdrawalID", Message: "required"}
	}
	if customerID == "" {
		return domain.ValidationError{Field: "customerID", Message: "required"}
	}

	log.Info("attempting to approve withdrawal")

	withdrawal, err := service.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return err
	}

	if withdrawal.CustomerID != customerID {
		log.Warn("customer ID mismatch")
		return domain.NotFoundError{Resource: "withdrawal"}
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
	err = service.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := service.reposFor(ctx)
		if err := withdrawals.Save(ctx, withdrawal); err != nil {
			return err
		}
		if events != nil {
			event := &domain.WithdrawalEvent{
				WithdrawalID: withdrawal.ID,
				EventType:    "withdrawal.approved",
				FromStatus:   &fromStatus,
				ToStatus:     &withdrawal.Status,
			}
			if err := events.Create(ctx, event); err != nil {
				log.Error("failed to create approval event", zap.Error(err))
			}
		}
		return nil
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

func (service *WithdrawalService) Reject(ctx context.Context, withdrawalID, customerID, rejectionNote string) error {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.String("customer_id", customerID),
	)

	if withdrawalID == "" {
		return domain.ValidationError{Field: "withdrawalID", Message: "required"}
	}
	if customerID == "" {
		return domain.ValidationError{Field: "customerID", Message: "required"}
	}

	log.Info("attempting to reject withdrawal")

	withdrawal, err := service.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return err
	}

	if withdrawal.CustomerID != customerID {
		log.Warn("customer ID mismatch")
		return domain.NotFoundError{Resource: "withdrawal"}
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
	err = service.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := service.reposFor(ctx)
		if err := withdrawals.Save(ctx, withdrawal); err != nil {
			return err
		}
		if events != nil {
			metadata := make(map[string]any)
			if rejectionNote != "" {
				metadata["rejection_note"] = rejectionNote
			}
			event := &domain.WithdrawalEvent{
				WithdrawalID: withdrawal.ID,
				EventType:    "withdrawal.rejected",
				FromStatus:   &fromStatus,
				ToStatus:     &withdrawal.Status,
				Metadata:     metadata,
			}
			if err := events.Create(ctx, event); err != nil {
				log.Error("failed to create rejection event", zap.Error(err))
			}
		}
		return nil
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

func (service *WithdrawalService) UpdateConfirmations(ctx context.Context, withdrawalID string, confirmations int, txHash *string) (*domain.Withdrawal, error) {
	log := zap.L().With(
		zap.String("withdrawal_id", withdrawalID),
		zap.Int("confirmations", confirmations),
	)

	withdrawal, err := service.withdrawals.GetByID(ctx, withdrawalID)
	if err != nil {
		log.Error("failed to retrieve withdrawal", zap.Error(err))
		return nil, err
	}

	updated, ok, err := service.applyConfirmationUpdate(ctx, withdrawal, confirmations, txHash)
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			log.Warn("optimistic lock conflict on confirmation update, retrying once", zap.Error(err))
			refetched, getErr := service.withdrawals.GetByID(ctx, withdrawalID)
			if getErr != nil {
				log.Error("failed to re-fetch withdrawal after lock conflict", zap.Error(getErr))
				return nil, getErr
			}
			updated, ok, err = service.applyConfirmationUpdate(ctx, refetched, confirmations, txHash)
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

func (service *WithdrawalService) applyConfirmationUpdate(ctx context.Context, existing *domain.Withdrawal, confirmations int, txHash *string) (*domain.Withdrawal, bool, error) {
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

	err := service.withTx(ctx, func(ctx context.Context) error {
		withdrawals, events := service.reposFor(ctx)
		if err := withdrawals.Save(ctx, &updated); err != nil {
			return err
		}
		emitWithdrawalEvents(ctx, events, &updated, prevStatus, prevConfirmations)
		return nil
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

func emitWithdrawalCreated(ctx context.Context, events repository.WithdrawalEventRepository, withdrawal *domain.Withdrawal) {
	if events == nil {
		return
	}
	event := domain.WithdrawalEvent{
		WithdrawalID: withdrawal.ID,
		EventType:    "CREATED",
		ToStatus:     &withdrawal.Status,
		Metadata:     map[string]any{"idempotency_key": withdrawal.IdempotencyKey},
		CreatedAt:    time.Now(),
	}
	if err := events.Create(ctx, &event); err != nil {
		zap.L().Error("failed to create event", zap.Error(err))
	}
}

func emitWithdrawalEvents(ctx context.Context, events repository.WithdrawalEventRepository, withdrawal *domain.Withdrawal, prevStatus domain.WithdrawalStatus, prevConfs int) {
	if events == nil {
		return
	}
	if withdrawal.Confirmations > prevConfs {
		event := domain.WithdrawalEvent{
			WithdrawalID: withdrawal.ID,
			EventType:    "CONFIRMATION_UPDATED",
			Metadata:     map[string]any{"confirmations": withdrawal.Confirmations},
			CreatedAt:    time.Now(),
		}
		if err := events.Create(ctx, &event); err != nil {
			zap.L().Error("failed to create confirmation event", zap.Error(err))
		}
	}
	if withdrawal.Status != prevStatus {
		event := domain.WithdrawalEvent{
			WithdrawalID: withdrawal.ID,
			EventType:    "STATUS_CHANGED",
			FromStatus:   &prevStatus,
			ToStatus:     &withdrawal.Status,
			CreatedAt:    time.Now(),
		}
		if err := events.Create(ctx, &event); err != nil {
			zap.L().Error("failed to create status change event", zap.Error(err))
		}
	}
}

func emitWithdrawalCancelled(ctx context.Context, events repository.WithdrawalEventRepository, withdrawal *domain.Withdrawal, prevStatus domain.WithdrawalStatus) {
	if events == nil {
		return
	}
	event := domain.WithdrawalEvent{
		WithdrawalID: withdrawal.ID,
		EventType:    "CANCELLED",
		FromStatus:   &prevStatus,
		ToStatus:     &withdrawal.Status,
		Metadata:     map[string]any{"reason": "user_requested"},
		CreatedAt:    time.Now(),
	}
	if err := events.Create(ctx, &event); err != nil {
		zap.L().Error("failed to create cancellation event", zap.Error(err))
	}
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
