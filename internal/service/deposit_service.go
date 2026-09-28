package service

import (
	"context"
	"errors"
	"math/big"
	"time"

	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/repository"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

type DepositService struct {
	deposits  repository.DepositRepository
	events    repository.DepositEventRepository
	txManager repository.TxManager
	validate  *validator.Validate
}

func NewDepositService(deposits repository.DepositRepository, events repository.DepositEventRepository) *DepositService {
	if deposits == nil {
		panic("deposits repository is required")
	}
	return &DepositService{
		deposits: deposits,
		events:   events,
		validate: validator.New(),
	}
}

func (service *DepositService) WithTxManager(txManager repository.TxManager) *DepositService {
	service.txManager = txManager
	return service
}

func (service *DepositService) reposFor(ctx context.Context) (repository.DepositRepository, repository.DepositEventRepository) {
	if repos, ok := repository.GetTxRepos(ctx); ok {
		return repos.Deposits, repos.DepositEvents
	}
	return service.deposits, service.events
}

func (service *DepositService) withTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if service.txManager == nil {
		return fn(ctx)
	}
	return service.txManager.WithTx(ctx, fn)
}

func (service *DepositService) UpsertIncoming(ctx context.Context, request domain.IncomingDeposit) (*domain.Deposit, error) {
	log := zap.L()

	if err := service.validate.Struct(request); err != nil {
		log.Warn("incoming deposit validation failed", zap.Error(err))
		return nil, err
	}

	if request.AmountAtomic == nil || request.AmountAtomic.Cmp(big.NewInt(0)) <= 0 {
		return nil, domain.InvalidAmountError{}
	}

	log = log.With(
		zap.String("customer_id", request.CustomerID),
		zap.String("external_tx_id", request.ExternalTxID),
		zap.String("asset", string(request.Asset)),
		zap.Int("confirmations", request.Confirmations),
	)

	existing, err := service.deposits.GetByExternalTxID(ctx, request.ExternalTxID)
	if err != nil {
		var notFound domain.NotFoundError
		if !errors.As(err, &notFound) {
			log.Error("failed to check existing deposit", zap.Error(err))
			return nil, err
		}
	}

	if existing == nil {
		log.Info("creating new deposit")
		return service.createDeposit(ctx, request)
	}

	log.Info("updating existing deposit")
	return service.updateDeposit(ctx, existing, request)
}

func (service *DepositService) createDeposit(ctx context.Context, request domain.IncomingDeposit) (*domain.Deposit, error) {
	log := zap.L().With(
		zap.String("customer_id", request.CustomerID),
		zap.String("external_tx_id", request.ExternalTxID),
		zap.String("asset", string(request.Asset)),
	)

	deposit := &domain.Deposit{
		CustomerID:            request.CustomerID,
		AddressID:             request.AddressID,
		ExternalTxID:          request.ExternalTxID,
		TxHash:                request.TxHash,
		Asset:                 request.Asset,
		AmountAtomic:          new(big.Int).Set(request.AmountAtomic),
		Status:                nextDepositStatus(request.Confirmations, request.RequiredConfirmations),
		Confirmations:         request.Confirmations,
		RequiredConfirmations: request.RequiredConfirmations,
		TransactionMetadata:   request.TransactionMetadata,
	}

	err := service.withTx(ctx, func(ctx context.Context) error {
		deposits, events := service.reposFor(ctx)
		if err := deposits.Create(ctx, deposit); err != nil {
			return err
		}
		emitDepositCreated(ctx, events, deposit)
		return nil
	})
	if err != nil {
		log.Error("failed to create deposit", zap.Error(err))
		return nil, err
	}
	log.Info("deposit created successfully")
	return deposit, nil
}

func (service *DepositService) updateDeposit(ctx context.Context, existing *domain.Deposit, request domain.IncomingDeposit) (*domain.Deposit, error) {
	log := zap.L().With(
		zap.String("customer_id", request.CustomerID),
		zap.String("external_tx_id", request.ExternalTxID),
		zap.String("asset", string(request.Asset)),
	)

	updated, ok, err := service.applyIncomingUpdate(ctx, existing, request)
	if err != nil {
		var lockErr domain.OptimisticLockError
		if errors.As(err, &lockErr) {
			log.Warn("optimistic lock conflict on deposit update, retrying once", zap.Error(err))
			refetched, getErr := service.deposits.GetByExternalTxID(ctx, request.ExternalTxID)
			if getErr != nil {
				log.Error("failed to re-fetch deposit after lock conflict", zap.Error(getErr))
				return nil, getErr
			}
			updated, ok, err = service.applyIncomingUpdate(ctx, refetched, request)
			if err != nil {
				log.Error("failed to save deposit update after retry", zap.Error(err))
				return nil, err
			}
		} else {
			log.Error("failed to save deposit update", zap.Error(err))
			return nil, err
		}
	}
	if !ok {
		return existing, nil
	}
	log.Info("deposit updated successfully")
	return updated, nil
}

func (service *DepositService) applyIncomingUpdate(ctx context.Context, existing *domain.Deposit, request domain.IncomingDeposit) (*domain.Deposit, bool, error) {
	log := zap.L()

	updated := cloneDeposit(existing)
	prevStatus := existing.Status
	prevConfirmations := existing.Confirmations

	if request.TxHash != nil {
		updated.TxHash = request.TxHash
	}
	if request.TransactionMetadata != nil {
		updated.TransactionMetadata = request.TransactionMetadata
	}
	updated.Confirmations = max(existing.Confirmations, request.Confirmations)
	updated.RequiredConfirmations = request.RequiredConfirmations
	newStatus := nextDepositStatus(updated.Confirmations, updated.RequiredConfirmations)

	if !IsValidDepositTransition(existing.Status, newStatus) {
		log.Warn("invalid status transition ignored",
			zap.String("from", string(existing.Status)), zap.String("to", string(newStatus)))
		return nil, false, nil
	}

	updated.Status = newStatus

	err := service.withTx(ctx, func(ctx context.Context) error {
		deposits, events := service.reposFor(ctx)
		if err := deposits.Save(ctx, &updated); err != nil {
			return err
		}
		emitDepositEvents(ctx, events, &updated, prevStatus, prevConfirmations)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &updated, true, nil
}

func cloneDeposit(d *domain.Deposit) domain.Deposit {
	clone := *d
	if d.AmountAtomic != nil {
		clone.AmountAtomic = new(big.Int).Set(d.AmountAtomic)
	}
	if d.TxHash != nil {
		hash := *d.TxHash
		clone.TxHash = &hash
	}
	if d.TransactionMetadata != nil {
		clone.TransactionMetadata = make(map[string]any, len(d.TransactionMetadata))
		for k, v := range d.TransactionMetadata {
			clone.TransactionMetadata[k] = v
		}
	}
	return clone
}

func nextDepositStatus(confirmations, required int) domain.DepositStatus {
	if confirmations >= required {
		return domain.DepositStatusCompleted
	}
	if confirmations > 0 {
		return domain.DepositStatusConfirming
	}
	return domain.DepositStatusPending
}

func emitDepositCreated(ctx context.Context, events repository.DepositEventRepository, deposit *domain.Deposit) {
	if events == nil {
		return
	}
	event := domain.DepositEvent{
		DepositID: deposit.ID,
		EventType: "CREATED",
		ToStatus:  &deposit.Status,
		Metadata:  map[string]any{"external_tx_id": deposit.ExternalTxID},
		CreatedAt: time.Now(),
	}
	if err := events.Create(ctx, &event); err != nil {
		zap.L().Error("failed to create event", zap.Error(err))
	}
}

func emitDepositEvents(ctx context.Context, events repository.DepositEventRepository, deposit *domain.Deposit, prevStatus domain.DepositStatus, prevConfs int) {
	if events == nil {
		return
	}
	if deposit.Confirmations > prevConfs {
		event := domain.DepositEvent{
			DepositID: deposit.ID,
			EventType: "CONFIRMATION_UPDATED",
			Metadata:  map[string]any{"confirmations": deposit.Confirmations},
			CreatedAt: time.Now(),
		}
		if err := events.Create(ctx, &event); err != nil {
			zap.L().Error("failed to create confirmation event", zap.Error(err))
		}
	}
	if deposit.Status != prevStatus {
		event := domain.DepositEvent{
			DepositID:  deposit.ID,
			EventType:  "STATUS_CHANGED",
			FromStatus: &prevStatus,
			ToStatus:   &deposit.Status,
			CreatedAt:  time.Now(),
		}
		if err := events.Create(ctx, &event); err != nil {
			zap.L().Error("failed to create status change event", zap.Error(err))
		}
	}
}

func IsValidDepositTransition(from, to domain.DepositStatus) bool {
	if from == to {
		return true
	}

	switch from {
	case domain.DepositStatusCompleted, domain.DepositStatusFailed:
		return false

	case domain.DepositStatusPending:
		return to == domain.DepositStatusConfirming ||
			to == domain.DepositStatusCompleted ||
			to == domain.DepositStatusFailed

	case domain.DepositStatusConfirming:
		return to == domain.DepositStatusCompleted || to == domain.DepositStatusFailed

	default:
		return false
	}
}
