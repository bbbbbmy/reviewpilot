// Command reviewpilot is the entrypoint for the ReviewPilot server
// (and CLI).
//
// Server mode (default) — `reviewpilot server`:
//
//	GET /healthz                — liveness
//	GET /metrics                 — Prometheus
//	POST /webhook                — GitHub pull_request event
//
// CLI mode — `reviewpilot review <range|--stdin> --repo <id>`:
//
//	runs the review pipeline locally and writes JSON comments to
//	stdout or --out <file>.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/bbbbbmy/reviewpilot/internal/webdashboard"
)

// version is the build version. Overridable via the build flag
//
//	-ldflags "-X main.version=v0.3.0"
var version = "v0.3.0-dev"

func main() {
	root := &cobra.Command{
		Use:           "reviewpilot",
		Short:         "Self-hosted AI code review bot",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(reviewCmd)
	root.AddCommand(serverCmd)
	root.AddCommand(versionCmd)
	if err := root.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			fmt.Fprintln(os.Stderr, ee.msg)
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the build version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version)
	},
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run the HTTP server (default if no subcommand given)",
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(runServer())
	},
}

func runServer() int {
	addr := os.Getenv("RP_HTTP_LISTEN")
	if addr == "" {
		addr = ":8080"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	// Build webhook handler. If credentials are missing we run a
	// degraded server that returns 503 on /webhook but still serves
	// /healthz and /metrics. This is more useful than failing to boot.
	wh, whErr := buildWebhookHandler(logger)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", webdashboard.NewServer().Handler())
	if wh != nil {
		mux.HandleFunc("POST /webhook", wh.ServeHTTP)
	} else {
		mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
			logger.Warn("webhook disabled: credentials missing", "err", whErr)
			http.Error(w, "webhook disabled: "+whErr.Error(), http.StatusServiceUnavailable)
		})
	}

	httpsrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
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

// unused but kept for go vet on build tags etc.
var _ = context.Background

// exitError is defined in cli.go alongside the review subcommand.
