// Package db owns the MySQL connection pool and the startup migration pass.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers the "mysql" driver

	"campusclaw/internal/config"
	"campusclaw/migrations"
)

const (
	maxOpenConns    = 10
	maxIdleConns    = 5
	connMaxLifetime = 3 * time.Minute
	retryInterval   = 250 * time.Millisecond
)

// Open returns the pool that serves requests, waiting until the server answers
// so a slow database start does not abort the boot.
func Open(ctx context.Context, cfg *config.Config) (*sql.DB, error) {
	handle, err := sql.Open("mysql", cfg.MySQL.DSN())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	handle.SetMaxOpenConns(maxOpenConns)
	handle.SetMaxIdleConns(maxIdleConns)
	handle.SetConnMaxLifetime(connMaxLifetime)

	if err := waitForReady(ctx, handle); err != nil {
		handle.Close()
		return nil, err
	}

	return handle, nil
}

// Migrate applies pending migrations over a separate single-connection handle
// that permits multi-statement scripts. The application pool never enables
// that option.
func Migrate(ctx context.Context, cfg *config.Config) error {
	handle, err := sql.Open("mysql", cfg.MySQL.MigrationDSN())
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer handle.Close()

	handle.SetMaxOpenConns(1)

	if err := waitForReady(ctx, handle); err != nil {
		return err
	}

	return migrations.Apply(ctx, handle)
}

func waitForReady(ctx context.Context, handle *sql.DB) error {
	for {
		pingErr := handle.PingContext(ctx)
		if pingErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("database never became reachable: %w", pingErr)
		case <-time.After(retryInterval):
		}
	}
}
