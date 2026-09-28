package worker

import (
	"context"
	"time"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/repository"
	"crypto-payment-service/internal/service"

	"go.uber.org/zap"
)

const (
	workerTimeout = 10 * time.Second
	batchSize     = 100
)

type Worker struct {
	functions workerFunctions
	interval  time.Duration
}

type workerFunctions struct {
	claimDeposits                func(context.Context, domain.DepositStatus, int) ([]*domain.Deposit, error)
	claimWithdrawals             func(context.Context, domain.WithdrawalStatus, int) ([]*domain.Withdrawal, error)
	processConfirmingDeposits    func(context.Context, []*domain.Deposit)
	processPendingWithdrawals    func(context.Context, []*domain.Withdrawal)
	processConfirmingWithdrawals func(context.Context, []*domain.Withdrawal)
}

func NewWorker(
	depositRepo *repository.DepositRepo,
	withdrawalRepo *repository.WithdrawalRepo,
	depositService *service.DepositService,
	withdrawalService *service.WithdrawalService,
	interval time.Duration,
) *Worker {
	broadcaster := broadcast.NewStubBroadcaster()
	depositProcessor := service.NewDepositProcessor(depositService, broadcaster)
	withdrawalProcessor := service.NewWithdrawalProcessor(withdrawalService, broadcaster)

	return newWorker(interval, workerFunctions{
		claimDeposits:                depositRepo.ClaimByStatus,
		claimWithdrawals:             withdrawalRepo.ClaimByStatus,
		processConfirmingDeposits:    depositProcessor.ProcessConfirming,
		processPendingWithdrawals:    withdrawalProcessor.ProcessPending,
		processConfirmingWithdrawals: withdrawalProcessor.ProcessConfirming,
	})
}

func newWorker(interval time.Duration, functions workerFunctions) *Worker {
	return &Worker{functions: functions, interval: interval}
}

func (w *Worker) Run(done <-chan struct{}) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log := zap.L().With(zap.String("category", "worker"))
	ctx := context.Background()

	log.Info("worker started", zap.Duration("poll_interval", w.interval))

	for {
		select {
		case <-done:
			log.Info("worker stopping...")
			return
		case <-ticker.C:
			w.processAll(ctx)
		}
	}
}

func (w *Worker) processAll(parentCtx context.Context) {
	if parentCtx.Err() != nil {
		zap.L().Info("parent context cancelled, skipping processing")
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, workerTimeout)
	defer cancel()

	w.processPendingWithdrawals(ctx)
	w.processConfirmingWithdrawals(ctx)
	w.processConfirmingDeposits(ctx)
}

func (w *Worker) processPendingWithdrawals(ctx context.Context) {
	log := zap.L()
	approved, err := w.functions.claimWithdrawals(ctx, domain.WithdrawalStatusApproved, batchSize)
	if err != nil {
		log.Error("failed to claim approved withdrawals", zap.Error(err))
		return
	}
	if len(approved) == 0 {
		return
	}

	log.Info("processing approved withdrawals", zap.Int("count", len(approved)))
	w.functions.processPendingWithdrawals(ctx, approved)
	log.Info("successfully processed approved withdrawals", zap.Int("count", len(approved)))
}

func (w *Worker) processConfirmingWithdrawals(ctx context.Context) {
	log := zap.L()
	confirming, err := w.functions.claimWithdrawals(ctx, domain.WithdrawalStatusConfirming, batchSize)
	if err != nil {
		log.Error("failed to claim confirming withdrawals", zap.Error(err))
		return
	}
	if len(confirming) == 0 {
		return
	}

	log.Info("processing confirming withdrawals", zap.Int("count", len(confirming)))
	w.functions.processConfirmingWithdrawals(ctx, confirming)
	log.Info("successfully processed confirming withdrawals", zap.Int("count", len(confirming)))
}

func (w *Worker) processConfirmingDeposits(ctx context.Context) {
	log := zap.L()
	confirming, err := w.functions.claimDeposits(ctx, domain.DepositStatusConfirming, batchSize)
	if err != nil {
		log.Error("failed to claim confirming deposits", zap.Error(err))
		return
	}
	if len(confirming) == 0 {
		return
	}

	log.Info("processing confirming deposits", zap.Int("count", len(confirming)))
	w.functions.processConfirmingDeposits(ctx, confirming)
	log.Info("successfully processed confirming deposits", zap.Int("count", len(confirming)))
}
