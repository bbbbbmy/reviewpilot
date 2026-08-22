// Command reviewpilot is the entrypoint for the ReviewPilot server.
//
// In v0.0.1 it exposes only `GET /healthz` to verify the HTTP plumbing,
// packaging, and CI scaffolding. Real review pipeline / LLM / GitHub
// integrations land in subsequent releases — see
// https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/implement-reviewpilot-mvp
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

	"github.com/bbbbbmy/reviewpilot/internal/webdashboard"
)

// version is the build version. Overridable via the build flag
//
//	-ldflags "-X main.version=v0.1.0"
var version = "v0.0.1-dev"

func main() {
	os.Exit(run())
}

func run() int {
	addr := os.Getenv("RP_HTTP_LISTEN")
	if addr == "" {
		addr = ":8080"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	srv := webdashboard.NewServer()
	httpsrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("reviewpilot server starting",
			"version", version,
			"addr", addr,
		)
		if err := httpsrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("reviewpilot server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpsrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "err", err)
			return 1
		}
		return 0
	case err := <-errCh:
		logger.Error("reviewpilot server failed", "err", err)
		return 1
	}
}
