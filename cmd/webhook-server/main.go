package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crypto-payment-service/internal/adapter/bitgo"
	"crypto-payment-service/internal/api"
	"crypto-payment-service/internal/broadcast"
	"crypto-payment-service/internal/domain"
	"crypto-payment-service/internal/migrate"
	"crypto-payment-service/internal/repository"
	"crypto-payment-service/internal/service"
	"crypto-payment-service/internal/webhook"
	"crypto-payment-service/internal/worker"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

type config struct {
	DatabaseURL    string
	Port           string
	WebhookSecret  string
	WorkerInterval time.Duration
}

type application struct {
	config            config
	depositRepo       *repository.DepositRepo
	withdrawalRepo    *repository.WithdrawalRepo
	depositService    *service.DepositService
	withdrawalService *service.WithdrawalService
	webhookHandler    *webhook.DepositHandler
	withdrawalHandler *api.WithdrawalHandler
}

func main() {
	base, err := zap.NewProduction()
	if err != nil {
		base = zap.NewNop()
	}
	zap.ReplaceGlobals(base)
	log := base.With(zap.String("category", "server"))
	ctx := context.Background()

	cfg := loadConfig()

	log.Info("starting webhook server", zap.String("port", cfg.Port))

	if err := runMigrations(ctx, cfg.DatabaseURL); err != nil {
		log.Error("migration failed", zap.Error(err))
		os.Exit(1)
	}

	app, err := setupApplication(cfg)
	if err != nil {
		log.Error("failed to setup application", zap.Error(err))
		os.Exit(1)
	}

	if err := app.run(ctx); err != nil {
		log.Error("server error", zap.Error(err))
		os.Exit(1)
	}
}

func loadConfig() config {
	log := zap.L()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	var workerInterval time.Duration
	if raw := os.Getenv("WITHDRAWAL_WORKER_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			log.Error("invalid WITHDRAWAL_WORKER_INTERVAL, worker disabled", zap.Error(err))
		} else {
			workerInterval = parsed
		}
	}

	return config{
		DatabaseURL:    dsn,
		Port:           port,
		WebhookSecret:  os.Getenv("WEBHOOK_SECRET"),
		WorkerInterval: workerInterval,
	}
}

func runMigrations(ctx context.Context, dsn string) error {
	log := zap.L()
	log.Info("starting database migrations")

	migrationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(migrationCtx); err != nil {
		log.Error("database ping failed", zap.Error(err))
		return err
	}
	log.Info("database ping successful")

	if err := migrate.ApplyAll(migrationCtx, db); err != nil {
		log.Error("migration apply failed", zap.Error(err))
		return err
	}

	log.Info("database migrations completed")
	return nil
}

func setupApplication(cfg config) (*application, error) {
	log := zap.L()

	entClient, err := repository.OpenEnt(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	log.Info("database connection established")

	depositRepo := repository.NewDepositRepo(entClient)
	withdrawalRepo := repository.NewWithdrawalRepo(entClient)
	addressRepo := repository.NewAddressRepo(entClient)
	depositEventRepo := repository.NewDepositEventRepo(entClient)
	withdrawalEventRepo := repository.NewWithdrawalEventRepo(entClient)

	txManager := repository.NewEntTxManager(entClient)

	depositService := service.NewDepositService(depositRepo, depositEventRepo).
		WithTxManager(txManager)
	withdrawalService := service.NewWithdrawalService(withdrawalRepo, withdrawalEventRepo).
		WithTxManager(txManager)

	registry := domain.DefaultRegistry()
	if err := domain.ValidateContractAddresses(registry); err != nil {
		return nil, err
	}
	bitgoAdapter := bitgo.NewBitGoAdapter(registry)
	bitgoIngestor := service.NewDepositIngestor(bitgoAdapter, depositService, addressRepo)

	webhookHandler := &webhook.DepositHandler{
		Adapters: map[string]webhook.DepositIngestor{
			"bitgo": bitgoIngestor,
		},
		Secret: cfg.WebhookSecret,
	}

	withdrawalHandler := api.NewWithdrawalHandler(withdrawalService)

	return &application{
		config:            cfg,
		depositRepo:       depositRepo,
		withdrawalRepo:    withdrawalRepo,
		depositService:    depositService,
		withdrawalService: withdrawalService,
		webhookHandler:    webhookHandler,
		withdrawalHandler: withdrawalHandler,
	}, nil
}

func (app *application) setupRoutes() http.Handler {
	mux := http.NewServeMux()
	// BitGo unified route (supports BTC, ETH, BNB, USDT/Ethereum, USDT/Tron, USDT/BSC)
	mux.Handle("/webhooks/bitgo", app.webhookHandler)
	mux.Handle("/withdrawals/", app.withdrawalHandler)
	return mux
}

func (app *application) run(ctx context.Context) error {
	log := zap.L()

	srv := &http.Server{
		Addr:              ":" + app.config.Port,
		Handler:           app.setupRoutes(),
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	serverErrors := make(chan error, 1)
	go func() {
		log.Info("webhook server listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	var w *worker.Worker
	workerDone := make(chan struct{})
	if app.config.WorkerInterval > 0 {
		w = worker.NewWorker(
			app.depositRepo,
			app.withdrawalRepo,
			app.depositService,
			app.withdrawalService,
			broadcast.MustNewBroadcasterFromEnv(),
			worker.DefaultWorkerID(),
			app.config.WorkerInterval,
		)
		go w.Run(workerDone)
	} else {
		log.Info("worker disabled (WITHDRAWAL_WORKER_INTERVAL not set)")
	}

	stopWorker := func() {
		if w == nil {
			return
		}
		close(workerDone)
		<-w.Stopped()
		log.Info("worker stopped")
	}

	select {
	case err := <-serverErrors:
		stopWorker()
		return err
	case <-shutdown:
		log.Info("shutdown signal received, initiating graceful shutdown")

		stopWorker()

		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("server shutdown error", zap.Error(err))
		} else {
			log.Info("http server stopped")
		}

		log.Info("shutdown complete")
	}

	return nil
}
