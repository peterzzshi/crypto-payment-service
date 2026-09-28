package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	atpostgres "ariga.io/atlas/sql/postgres"

	"crypto-payment-service/internal/migrate/seed"
	"crypto-payment-service/migrations"

	"go.uber.org/zap"
)

// advisoryLockKey is a fixed Postgres advisory lock ID guarding ApplyAll.
// Multiple instances (webhook-server, withdrawal-worker, replicas) may call
// ApplyAll concurrently on boot; the lock serializes them so only one
// actually runs migrations/seed at a time, while the others block and then
// see a fully-migrated, no-op state. This makes ApplyAll safe under
// concurrency, not just idempotent at the SQL/DB level.
const advisoryLockKey = 8817234659172645

// ApplyAll applies all pending Atlas-tracked schema migrations from
// migrations/, then applies the (unversioned, idempotent) seed data.
// Safe to call concurrently from multiple processes: an advisory lock
// serializes execution so a second caller waits for the first to finish
// rather than racing it.
func ApplyAll(ctx context.Context, db *sql.DB) error {
	log := zap.L()

	conn, err := db.Conn(ctx)
	if err != nil {
		log.Error("failed to acquire connection for migration lock", zap.Error(err))
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		log.Error("failed to acquire migration advisory lock", zap.Error(err))
		return err
	}
	defer conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey)

	dir := &embedDir{fs: migrations.Files}
	if err := atlas.Validate(dir); err != nil {
		log.Error("migration directory checksum validation failed", zap.Error(err))
		return err
	}

	drv, err := atpostgres.Open(db)
	if err != nil {
		log.Error("failed to open atlas postgres driver", zap.Error(err))
		return err
	}

	rrw := &pgRevisions{db: db}
	if err := rrw.init(ctx); err != nil {
		log.Error("failed to initialize revisions table", zap.Error(err))
		return err
	}

	ex, err := atlas.NewExecutor(drv, dir, rrw, atlas.WithAllowDirty(true))
	if err != nil {
		log.Error("failed to create migration executor", zap.Error(err))
		return err
	}

	log.Info("applying pending schema migrations")
	if err := ex.ExecuteN(ctx, 0); err != nil && !errors.Is(err, atlas.ErrNoPendingFiles) {
		log.Error("failed to apply schema migrations", zap.Error(err))
		return err
	}
	log.Info("schema migrations applied successfully")

	log.Info("applying seed data")
	seedSQL, err := seed.Files.ReadFile("seed.up.sql")
	if err != nil {
		log.Error("failed to read seed file", zap.Error(err))
		return err
	}
	if _, err := db.ExecContext(ctx, string(seedSQL)); err != nil {
		log.Error("failed to apply seed data", zap.Error(err))
		return err
	}
	log.Info("seed data applied successfully")

	return nil
}

// embedDir adapts an embed.FS containing migration files and an atlas.sum
// checksum file into an atlas migrate.Dir. It is read-only: WriteFile always
// fails, since these files are baked into the binary at build time.
type embedDir struct {
	fs embed.FS
}

func (d *embedDir) Open(name string) (fs.File, error) {
	return d.fs.Open(name)
}

func (d *embedDir) WriteFile(string, []byte) error {
	return errors.New("internal/migrate: embedded migration directory is read-only")
}

func (d *embedDir) Files() ([]atlas.File, error) {
	names, err := fs.Glob(d.fs, "*.sql")
	if err != nil {
		return nil, err
	}
	files := make([]atlas.File, 0, len(names))
	for _, n := range names {
		b, err := fs.ReadFile(d.fs, n)
		if err != nil {
			return nil, err
		}
		files = append(files, atlas.NewLocalFile(n, b))
	}
	return files, nil
}

func (d *embedDir) Checksum() (atlas.HashFile, error) {
	files, err := d.Files()
	if err != nil {
		return nil, err
	}
	return atlas.NewHashFile(files)
}

// revisionsTable is the name of the table atlas uses to track which
// migration versions have already been applied to this database.
const revisionsTable = "atlas_schema_revisions"

// pgRevisions is a minimal atlas migrate.RevisionReadWriter backed by a
// plain Postgres table, avoiding a dependency on the atlas CLI binary at
// runtime.
type pgRevisions struct {
	db *sql.DB
}

func (r *pgRevisions) init(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+revisionsTable+` (
			version          text PRIMARY KEY,
			description      text NOT NULL,
			type             bigint NOT NULL,
			applied          bigint NOT NULL,
			total            bigint NOT NULL,
			executed_at      timestamptz NOT NULL,
			execution_time   bigint NOT NULL,
			error            text NOT NULL DEFAULT '',
			error_stmt       text NOT NULL DEFAULT '',
			hash             text NOT NULL DEFAULT '',
			partial_hashes   text NOT NULL DEFAULT '',
			operator_version text NOT NULL DEFAULT ''
		)`)
	return err
}

func (r *pgRevisions) Ident() *atlas.TableIdent {
	return &atlas.TableIdent{Name: revisionsTable}
}

func (r *pgRevisions) ReadRevisions(ctx context.Context) ([]*atlas.Revision, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT version, description, type, applied, total, executed_at, execution_time,
		       error, error_stmt, hash, partial_hashes, operator_version
		FROM `+revisionsTable+` ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisions []*atlas.Revision
	for rows.Next() {
		rev, partialHashes, err := scanRevision(rows.Scan)
		if err != nil {
			return nil, err
		}
		rev.PartialHashes = splitPartialHashes(partialHashes)
		revisions = append(revisions, rev)
	}
	return revisions, rows.Err()
}

func (r *pgRevisions) ReadRevision(ctx context.Context, version string) (*atlas.Revision, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT version, description, type, applied, total, executed_at, execution_time,
		       error, error_stmt, hash, partial_hashes, operator_version
		FROM `+revisionsTable+` WHERE version = $1`, version)

	rev, partialHashes, err := scanRevision(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, atlas.ErrRevisionNotExist
	}
	if err != nil {
		return nil, err
	}
	rev.PartialHashes = splitPartialHashes(partialHashes)
	return rev, nil
}

func (r *pgRevisions) WriteRevision(ctx context.Context, rev *atlas.Revision) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO `+revisionsTable+` (
			version, description, type, applied, total, executed_at, execution_time,
			error, error_stmt, hash, partial_hashes, operator_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (version) DO UPDATE SET
			description = EXCLUDED.description,
			type = EXCLUDED.type,
			applied = EXCLUDED.applied,
			total = EXCLUDED.total,
			executed_at = EXCLUDED.executed_at,
			execution_time = EXCLUDED.execution_time,
			error = EXCLUDED.error,
			error_stmt = EXCLUDED.error_stmt,
			hash = EXCLUDED.hash,
			partial_hashes = EXCLUDED.partial_hashes,
			operator_version = EXCLUDED.operator_version`,
		rev.Version, rev.Description, int64(rev.Type), rev.Applied, rev.Total,
		rev.ExecutedAt, int64(rev.ExecutionTime), rev.Error, rev.ErrorStmt, rev.Hash,
		joinPartialHashes(rev.PartialHashes), rev.OperatorVersion,
	)
	return err
}

func (r *pgRevisions) DeleteRevision(ctx context.Context, version string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM `+revisionsTable+` WHERE version = $1`, version)
	return err
}

func scanRevision(scan func(...any) error) (*atlas.Revision, string, error) {
	rev := &atlas.Revision{}
	var (
		revType       int64
		executionTime int64
		partialHashes string
	)
	err := scan(
		&rev.Version, &rev.Description, &revType, &rev.Applied, &rev.Total,
		&rev.ExecutedAt, &executionTime, &rev.Error, &rev.ErrorStmt, &rev.Hash,
		&partialHashes, &rev.OperatorVersion,
	)
	rev.Type = atlas.RevisionType(revType)
	rev.ExecutionTime = time.Duration(executionTime)
	return rev, partialHashes, err
}

const partialHashSep = "\x1f"

func joinPartialHashes(hashes []string) string {
	s := ""
	for i, h := range hashes {
		if i > 0 {
			s += partialHashSep
		}
		s += h
	}
	return s
}

func splitPartialHashes(s string) []string {
	if s == "" {
		return nil
	}
	var hashes []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == partialHashSep[0] {
			hashes = append(hashes, s[start:i])
			start = i + 1
		}
	}
	hashes = append(hashes, s[start:])
	return hashes
}
