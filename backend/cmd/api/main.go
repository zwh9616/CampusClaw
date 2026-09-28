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
	"campusclaw/internal/seed"
	"campusclaw/internal/server"
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

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.New(cfg, handle),
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
