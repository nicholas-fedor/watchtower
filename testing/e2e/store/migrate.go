package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var migrations embed.FS

// Migrate applies embedded SQL files in lexical order.
//
// Parameters:
//   - ctx: Cancellation.
//   - pool: Open Postgres pool.
//
// Returns:
//   - error: Read or exec failure.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := fs.ReadDir(migrations, "sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		raw, readErr := migrations.ReadFile("sql/" + entry.Name())
		if readErr != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), readErr)
		}

		_, execErr := pool.Exec(ctx, string(raw))
		if execErr != nil {
			return fmt.Errorf("apply %s: %w", entry.Name(), execErr)
		}
	}

	return nil
}
