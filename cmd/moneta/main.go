// Entry point: loads the config, migrates the database, then serves the HTTP API and runs the job worker until
// interrupted.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClementAGIL/moneta-api/internal/config"
	"github.com/ClementAGIL/moneta-api/internal/db"
	"github.com/ClementAGIL/moneta-api/internal/httpapi"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	kek, err := vault.ParseKEK(cfg.MasterKey)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	deps, worker := wire(pool, kek, cfg, logger)
	go worker.Run(ctx)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewRouter(deps),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return serve(ctx, srv, logger)
}

// serve runs srv until ctx is cancelled (Ctrl+C, service stop), then shuts it down gracefully.
func serve(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	errs := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr)
		errs <- srv.ListenAndServe()
	}()
	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
