package worker

import (
	"context"
	"fmt"
	"os"
	"time"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/repository"
	"crypto-payment-service/internal/service"

	"go.uber.org/zap"
)

const (
	roundTimeout = 10 * time.Second
	batchSize    = 100

	// leaseTTL is how long a claimed row stays invisible to other workers.
	// It must comfortably outlive one processing round (a handful of external
	// API calls per batch) but expire fast enough that a crashed worker's
	// rows become claimable again promptly (ADR-0002).
	leaseTTL = 2 * time.Minute
)

// DefaultWorkerID identifies this replica in processing leases:
// hostname + PID is unique across replicas and stable for the process's life.
func DefaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

// workerFunctions is the claim/process seam the pipeline drives. Tests
// inject plain functions; production wires repository claims and processors.
type workerFunctions struct {
	claimApprovedWithdrawals     func(context.Context, int) ([]*domain.Withdrawal, error)
	claimConfirmingWithdrawals   func(context.Context, int) ([]*domain.Withdrawal, error)
	claimConfirmingDeposits      func(context.Context, int) ([]*domain.Deposit, error)
	broadcastApprovedWithdrawals func(context.Context, []*domain.Withdrawal)
	processConfirmingWithdrawals func(context.Context, []*domain.Withdrawal)
	processConfirmingDeposits    func(context.Context, []*domain.Deposit)
}

// Pipeline executes one claim-and-process round across the three work
// queues: approved withdrawals (broadcast), confirming withdrawals, and
// confirming deposits (confirmation polling).
type Pipeline struct {
	functions workerFunctions
}

func NewPipeline(
	depositRepo *repository.DepositRepo,
	withdrawalRepo *repository.WithdrawalRepo,
	depositService *service.DepositService,
	withdrawalService *service.WithdrawalService,
	broadcaster broadcast.Broadcaster,
	workerID string,
) *Pipeline {
	depositProcessor := service.NewDepositProcessor(depositService, broadcaster)
	withdrawalProcessor := service.NewWithdrawalProcessor(withdrawalService, broadcaster)

	// Confirmation polling is idempotent work: claim it with a lease so
	// replicas don't duplicate external provider calls (ADR-0002). The
	// APPROVED claim stays a state-transition claim because broadcast is
	// irreversible.
	return &Pipeline{functions: workerFunctions{
		claimApprovedWithdrawals: withdrawalRepo.ClaimForBroadcast,
		claimConfirmingWithdrawals: func(ctx context.Context, limit int) ([]*domain.Withdrawal, error) {
			return withdrawalRepo.LeaseClaimByStatus(ctx, domain.WithdrawalStatusConfirming, limit, workerID, leaseTTL)
		},
		claimConfirmingDeposits: func(ctx context.Context, limit int) ([]*domain.Deposit, error) {
			return depositRepo.LeaseClaimByStatus(ctx, domain.DepositStatusConfirming, limit, workerID, leaseTTL)
		},
		broadcastApprovedWithdrawals: withdrawalProcessor.BroadcastApproved,
		processConfirmingWithdrawals: withdrawalProcessor.ProcessConfirming,
		processConfirmingDeposits:    depositProcessor.ProcessConfirming,
	}}
}

func newPipelineForTest(functions workerFunctions) *Pipeline {
	return &Pipeline{functions: functions}
}

// ProcessAll runs one full round. The round shares one timeout so a slow
// provider cannot stretch a tick indefinitely.
func (p *Pipeline) ProcessAll(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, roundTimeout)
	defer cancel()

	p.broadcastApprovedWithdrawals(ctx)
	p.processConfirmingWithdrawals(ctx)
	p.processConfirmingDeposits(ctx)
}

func (p *Pipeline) broadcastApprovedWithdrawals(ctx context.Context) {
	approved, err := p.functions.claimApprovedWithdrawals(ctx, batchSize)
	if err != nil {
		zap.L().Error("failed to claim approved withdrawals", zap.Error(err))
		return
	}
	if len(approved) == 0 {
		return
	}

	zap.L().Info("broadcasting approved withdrawals", zap.Int("count", len(approved)))
	p.functions.broadcastApprovedWithdrawals(ctx, approved)
}

func (p *Pipeline) processConfirmingWithdrawals(ctx context.Context) {
	confirming, err := p.functions.claimConfirmingWithdrawals(ctx, batchSize)
	if err != nil {
		zap.L().Error("failed to claim confirming withdrawals", zap.Error(err))
		return
	}
	if len(confirming) == 0 {
		return
	}

	zap.L().Info("polling confirming withdrawals", zap.Int("count", len(confirming)))
	p.functions.processConfirmingWithdrawals(ctx, confirming)
}

func (p *Pipeline) processConfirmingDeposits(ctx context.Context) {
	confirming, err := p.functions.claimConfirmingDeposits(ctx, batchSize)
	if err != nil {
		zap.L().Error("failed to claim confirming deposits", zap.Error(err))
		return
	}
	if len(confirming) == 0 {
		return
	}

	zap.L().Info("polling confirming deposits", zap.Int("count", len(confirming)))
	p.functions.processConfirmingDeposits(ctx, confirming)
}

// Worker drives a Pipeline on a ticker.
type Worker struct {
	pipeline *Pipeline
	interval time.Duration
	stopped  chan struct{}
}

func NewWorker(
	depositRepo *repository.DepositRepo,
	withdrawalRepo *repository.WithdrawalRepo,
	depositService *service.DepositService,
	withdrawalService *service.WithdrawalService,
	broadcaster broadcast.Broadcaster,
	workerID string,
	interval time.Duration,
) *Worker {
	return &Worker{
		pipeline: NewPipeline(depositRepo, withdrawalRepo, depositService, withdrawalService, broadcaster, workerID),
		interval: interval,
		stopped:  make(chan struct{}),
	}
}

// Run ticks until done is closed, then finishes the round already in flight
// before returning. Callers that need a graceful shutdown wait on Stopped.
func (w *Worker) Run(done <-chan struct{}) {
	defer close(w.stopped)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log := zap.L().With(zap.String("category", "worker"))
	log.Info("worker started", zap.Duration("poll_interval", w.interval))

	for {
		select {
		case <-done:
			log.Info("worker stopping after current round")
			return
		case <-ticker.C:
			w.pipeline.ProcessAll(context.Background())
		}
	}
}

// Stopped is closed once Run has returned, including any in-flight round.
func (w *Worker) Stopped() <-chan struct{} {
	return w.stopped
}
