// Command api serves the CampusClaw HTTP API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campusclaw/internal/config"
	"campusclaw/internal/db"
	"campusclaw/internal/materials"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/seed"
	"campusclaw/internal/server"
)

// Indexing constants for startup work.
const (
	// collectionRetry is how long boot waits for the vector store to answer
	// before carrying on without it.
	collectionRetry = 30 * time.Second
	collectionEvery = time.Second
	// backfillBudget bounds one startup backfill pass.
	backfillBudget = 10 * time.Minute
)

const (
	readHeaderTimeout = 10 * time.Second
	// writeTimeout is the API half of the budget shared with Nginx's
	// proxy_read_timeout, leaving room for parse plus transaction.
	writeTimeout = 30 * time.Second
	idleTimeout  = 60 * time.Second
	// startupTimeout bounds how long boot waits for the database to appear.
	startupTimeout = 60 * time.Second
	shutdownGrace  = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		// Configuration and connection errors name variables but never values.
		log.Fatalf("api: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Refuse to serve without the document parsers rather than reporting every
	// PDF and DOCX as corrupt.
	if err := materials.VerifyParserTools(); err != nil {
		return err
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()

	if err := db.Migrate(startupCtx, cfg); err != nil {
		return err
	}

	handle, err := db.Open(startupCtx, cfg)
	if err != nil {
		return err
	}
	defer handle.Close()

	seeded, err := seed.Run(startupCtx, handle, cfg)
	if err != nil {
		return err
	}
	// Counts only: never passwords, tokens or DSNs.
	log.Printf("api: seed complete (created %d classes, %d accounts)", seeded.CreatedClasses, seeded.CreatedUsers)

	service := retrieval.NewFromConfig(cfg, handle)

	// A collection that is not ready is not fatal: the keyword path needs no
	// vector store, and the vector paths report their own outage. Failing to
	// start would turn a slow dependency into an unavailable service.
	if err := prepareCollection(startupCtx, service); err != nil {
		log.Printf("api: the vector collection is not ready yet: %v", err)
	}

	go backfill(service)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.New(cfg, handle, server.WithRetrieval(service)),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("api: listening on %s", httpServer.Addr)
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case sig := <-shutdownSignals:
		log.Printf("api: received %s, shutting down", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	// The background serve goroutine reports ErrServerClosed here.
	return httpServer.Shutdown(shutdownCtx)
}

// prepareCollection waits for the vector store to answer and makes sure the
// collection matches the configured embedding width.
//
// The wait covers the ordinary case of Compose starting the store a moment
// after the API; the caller treats a final failure as a degraded start rather
// than a fatal one.
func prepareCollection(ctx context.Context, service *retrieval.Service) error {
	deadline := time.Now().Add(collectionRetry)

	for {
		err := service.PrepareCollection(ctx)
		if err == nil {
			return nil
		}

		if time.Now().After(deadline) || ctx.Err() != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return err
		case <-time.After(collectionEvery):
		}
	}
}

// backfill indexes every material that has no usable index.
//
// It runs once at startup on its own context, so a long pass neither delays
// serving nor is cut short by the startup deadline. A run that is interrupted
// is simply repeated on the next start: the operation is idempotent.
func backfill(service *retrieval.Service) {
	ctx, cancel := context.WithTimeout(context.Background(), backfillBudget)
	defer cancel()

	indexed, err := service.Indexer().Backfill(ctx)
	if err != nil {
		log.Printf("api: index backfill stopped: %v", err)
	}

	if indexed > 0 {
		log.Printf("api: index backfill gave %d material(s) a usable index", indexed)
	}
}
