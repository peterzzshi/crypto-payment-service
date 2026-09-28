package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	atlas "ariga.io/atlas/sql/migrate"
	_ "ariga.io/atlas/sql/postgres"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"

	"crypto-payment-service/ent/migrate"
)

func init() {
	// Atlas's postgres opener calls sql.Open("postgres", dsn) internally.
	// The codebase only registers pgx under "pgx"/"pgx/v5", so register it
	// under "postgres" too for this tool.
	sql.Register("postgres", stdlib.GetDefaultDriver())
}

func main() {
	ctx := context.Background()

	dir, err := atlas.NewLocalDir("migrations")
	if err != nil {
		log.Fatalf("failed creating atlas migration directory: %v", err)
	}

	devURL := os.Getenv("DEV_DATABASE_URL")
	if devURL == "" {
		log.Fatal("DEV_DATABASE_URL environment variable is required")
	}

	name := "init"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}

	opts := []schema.MigrateOption{
		schema.WithDir(dir),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect("postgres"),
		schema.WithFormatter(atlas.DefaultFormatter),
	}

	if err := migrate.NamedDiff(ctx, devURL, name, opts...); err != nil {
		log.Fatalf("failed generating migration: %v", err)
	}
}
