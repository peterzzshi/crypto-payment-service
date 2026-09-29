package repository

import (
	"database/sql"
	"os"
	"time"

	"crypto-payment-service/ent"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenEnt opens an ent client backed by the pgx driver. The pool is bounded
// deliberately: workers process claimed rows concurrently, and an unbounded
// pool would let a burst of goroutines exhaust database connections. Excess
// work queues in the pool instead of failing.
func OpenEnt(dsn string) (*ent.Client, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	if os.Getenv("LOG_LEVEL") == "debug" {
		client = client.Debug()
	}

	return client, nil
}
