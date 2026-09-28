package repository

import (
	"database/sql"
	"os"

	"crypto-payment-service/ent"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenEnt opens an ent client backed by the pgx driver.
func OpenEnt(dsn string) (*ent.Client, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	if os.Getenv("LOG_LEVEL") == "debug" {
		client = client.Debug()
	}

	return client, nil
}
