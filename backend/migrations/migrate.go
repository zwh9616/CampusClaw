package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// createMigrationsTable is the migration tool's own bookkeeping table. It is
// the only table the design permits outside the five business tables.
const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    VARCHAR(64) NOT NULL,
    applied_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (version)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// Apply runs every migration not yet recorded, in filename order.
//
// It is safe to call on each start: applied versions are skipped and nothing is
// ever dropped or truncated. MySQL DDL is not transactional, so a failure part
// way through a file leaves the schema partially changed and the version
// unrecorded — the caller must refuse to serve rather than continue.
func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	names, err := availableNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		if applied[name] {
			continue
		}

		if err := applyOne(ctx, db, name); err != nil {
			return err
		}
	}

	return nil
}

func applyOne(ctx context.Context, db *sql.DB, name string) error {
	contents, err := files.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	if _, err := db.ExecContext(ctx, string(contents)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}

	if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}

	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)

	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}

	return applied, nil
}

// availableNames lists the embedded scripts in application order, which is the
// numeric filename prefix order.
func availableNames() ([]string, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}

	sort.Strings(names)
	return names, nil
}
