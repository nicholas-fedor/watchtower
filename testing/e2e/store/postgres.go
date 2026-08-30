package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runColumns is the INSERT/SELECT list for runs.
const runColumns = `
	id, label, created_at, started_at, finished_at, status,
	generator, seed, topic, filter, file_path, shard,
	offset_n, limit_n, workers, keep, passed, failed, skipped, error`

// caseColumns is the INSERT/SELECT list for cases.
const caseColumns = `
	run_id, case_id, status, factors, expect, argv, env, error, duration_ms,
	inspect_before, inspect_after, porcelain, http_details, started_at, finished_at`

// Postgres is the durable Store.
type Postgres struct {
	// pool is the pgx connection pool.
	pool *pgxpool.Pool
}

// OpenPostgres connects, pings, and migrates.
//
// Parameters:
//   - ctx: Cancellation.
//   - databaseURL: Postgres DSN.
//
// Returns:
//   - *Postgres: Ready store.
//   - error: Connect or migrate failure.
func OpenPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	if databaseURL == "" {
		return nil, ErrNotConfigured
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres pool: %w", err)
	}

	pingErr := pool.Ping(ctx)
	if pingErr != nil {
		pool.Close()

		return nil, fmt.Errorf("postgres ping: %w", pingErr)
	}

	migErr := Migrate(ctx, pool)
	if migErr != nil {
		pool.Close()

		return nil, fmt.Errorf("migrate: %w", migErr)
	}

	return &Postgres{pool: pool}, nil
}

// Ping connects and pings without running migrations.
//
// Parameters:
//   - ctx: Cancellation.
//   - databaseURL: Postgres DSN.
//
// Returns:
//   - error: Connect or ping failure.
func Ping(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return ErrNotConfigured
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	defer pool.Close()

	if pingErr := pool.Ping(ctx); pingErr != nil {
		return fmt.Errorf("postgres ping: %w", pingErr)
	}

	return nil
}

// Close releases the pool.
//
// Returns:
//   - error: Always nil.
func (p *Postgres) Close() error {
	p.pool.Close()

	return nil
}
