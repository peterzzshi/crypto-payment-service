package main

import (
	"context"
	"os"
	"time"

	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/repository"
	"crypto-payment-service/internal/service"

	"go.uber.org/zap"
)

type config struct {
	DatabaseURL string
	Timeout     time.Duration
	BatchSize   int
}

type application struct {
	withdrawalRepo *repository.WithdrawalRepo
	processor      *service.WithdrawalProcessor
}

func main() {
	base, err := zap.NewProduction()
	if err != nil {
		base = zap.NewNop()
	}
	zap.ReplaceGlobals(base)
	log := base.With(zap.String("category", "withdrawal_cli"))
	ctx := context.Background()

	cfg := loadConfig()

	app, err := setupApplication(ctx, cfg)
	if err != nil {
		log.Error("failed to setup application", zap.Error(err))
		os.Exit(1)
	}

	if err := app.processWithdrawals(ctx, cfg); err != nil {
		log.Error("failed to process withdrawals", zap.Error(err))
		os.Exit(1)
	}
}

func loadConfig() config {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		zap.L().Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	return config{
		DatabaseURL: dsn,
		Timeout:     15 * time.Second,
		BatchSize:   100,
	}
}

func setupApplication(ctx context.Context, cfg config) (*application, error) {
	log := zap.L()

	log.Info("connecting to database")
	entClient, err := repository.OpenEnt(cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to open database", zap.Error(err))
		return nil, err
	}

	withdrawalRepo := repository.NewWithdrawalRepo(entClient)
	withdrawalEventRepo := repository.NewWithdrawalEventRepo(entClient)

	txManager := repository.NewEntTxManager(entClient)

	withdrawalService := service.NewWithdrawalService(withdrawalRepo, withdrawalEventRepo).
		WithTxManager(txManager)
	broadcaster := broadcast.NewStubBroadcaster()
	processor := service.NewWithdrawalProcessor(withdrawalService, broadcaster)

	return &application{
		withdrawalRepo: withdrawalRepo,
		processor:      processor,
	}, nil
}

func (app *application) processWithdrawals(ctx context.Context, cfg config) error {
	log := zap.L()

	processCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	log.Info("fetching pending withdrawals")
	pending, err := app.withdrawalRepo.ClaimByStatus(processCtx, domain.WithdrawalStatusPending, cfg.BatchSize)
	if err != nil {
		return err
	}

	log.Info("processing pending withdrawals", zap.Int("count", len(pending)))
	app.processor.ProcessPending(processCtx, pending)

	log.Info("fetching confirming withdrawals")
	confirming, err := app.withdrawalRepo.ClaimByStatus(processCtx, domain.WithdrawalStatusConfirming, cfg.BatchSize)
	if err != nil {
		return err
	}

	log.Info("processing confirming withdrawals", zap.Int("count", len(confirming)))
	app.processor.ProcessConfirming(processCtx, confirming)

	log.Info("withdrawal processing complete", zap.Int("pending", len(pending)), zap.Int("confirming", len(confirming)))
	return nil
}
