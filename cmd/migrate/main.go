package main

import (
	"context"
	"database/sql"
	"flag"
	"os"
	"time"

	"crypto-payment-service/internal/migrate"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

type config struct {
	DatabaseURL string
	Timeout     time.Duration
}

func main() {
	timeout := flag.Duration("timeout", 30*time.Second, "migration timeout")
	flag.Parse()

	base, err := zap.NewProduction()
	if err != nil {
		base = zap.NewNop()
	}
	zap.ReplaceGlobals(base)
	log := base.With(zap.String("category", "migrate"))
	ctx := context.Background()

	cfg := loadConfig(*timeout)

	if err := runMigrations(ctx, cfg); err != nil {
		log.Error("migration failed", zap.Error(err))
		os.Exit(1)
	}

	log.Info("migrations completed successfully")
}

func loadConfig(timeout time.Duration) config {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		zap.L().Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	return config{
		DatabaseURL: dsn,
		Timeout:     timeout,
	}
}

func runMigrations(ctx context.Context, cfg config) error {
	log := zap.L()

	log.Info("connecting to database")
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to open database", zap.Error(err))
		return err
	}
	defer func() { _ = db.Close() }()

	migrationCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	log.Info("pinging database")
	if err := db.PingContext(migrationCtx); err != nil {
		log.Error("database ping failed", zap.Error(err))
		return err
	}

	log.Info("starting migrations")
	return migrate.ApplyAll(migrationCtx, db)
}
