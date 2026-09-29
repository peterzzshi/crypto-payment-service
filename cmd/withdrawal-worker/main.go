package main

import (
	"context"
	"os"
	"time"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/repository"
	"crypto-payment-service/internal/service"
	"crypto-payment-service/internal/worker"

	"go.uber.org/zap"
)

const roundTimeout = 30 * time.Second

func main() {
	base, err := zap.NewProduction()
	if err != nil {
		base = zap.NewNop()
	}
	zap.ReplaceGlobals(base)
	log := base.With(zap.String("category", "withdrawal_cli"))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	log.Info("connecting to database")
	entClient, err := repository.OpenEnt(dsn)
	if err != nil {
		log.Error("failed to open database", zap.Error(err))
		os.Exit(1)
	}

	txManager := repository.NewEntTxManager(entClient)
	depositRepo := repository.NewDepositRepo(entClient)
	withdrawalRepo := repository.NewWithdrawalRepo(entClient)
	depositService := service.NewDepositService(depositRepo, repository.NewDepositEventRepo(entClient)).
		WithTxManager(txManager)
	withdrawalService := service.NewWithdrawalService(withdrawalRepo, repository.NewWithdrawalEventRepo(entClient)).
		WithTxManager(txManager)

	// The one-shot CLI drives the same pipeline as the long-running worker so
	// claim semantics can never drift between the two entry points.
	pipeline := worker.NewPipeline(
		depositRepo,
		withdrawalRepo,
		depositService,
		withdrawalService,
		broadcast.MustNewBroadcasterFromEnv(),
		worker.DefaultWorkerID(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), roundTimeout)
	defer cancel()

	pipeline.ProcessAll(ctx)
	log.Info("withdrawal processing complete")
}
