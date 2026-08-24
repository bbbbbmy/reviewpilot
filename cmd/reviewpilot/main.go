// Command reviewpilot is the entrypoint for the ReviewPilot server
// (and CLI).
//
// Server mode (default) — `reviewpilot server`:
//
//	GET /healthz                — liveness (no auth)
//	GET /metrics                 — Prometheus (no auth)
//	POST /webhook                — GitHub pull_request event
//	GET  /                       — redirect to /reviews
//	GET  /reviews, /repos        — HTML dashboards (Basic Auth)
//	GET  /api/v1/*               — JSON API (Basic Auth)
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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/bbbbbmy/reviewpilot/internal/webdashboard"
)

// version is the build version. Overridable via the build flag
//
//	-ldflags "-X main.version=v0.4.0"
var version = "v0.4.0-dev"

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

	// Load admin creds from env. Server refuses to start without
	// them — except when RP_DEV_MODE=1, in which case auth is skipped.
	devMode := os.Getenv("RP_DEV_MODE") == "1"
	if !devMode {
		if _, _, err := webdashboard.LoadAdminFromEnv(); err != nil {
			fmt.Fprintln(os.Stderr, "server: "+err.Error()+" (or set RP_DEV_MODE=1 to skip)")
			return 1
		}
	}

	// Open SQLite. We share one connection per process; the dashboard
	// uses it for both reads (UI) and writes (webhook handler).
	dbPath := os.Getenv("RP_DB_PATH")
	if dbPath == "" {
		dbPath = "/var/lib/reviewpilot/review.db"
	}
	store, err := webdashboard.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db: "+err.Error())
		return 1
	}
	defer store.Close()

	// Web dashboard server (handles /, /repos, /reviews, /api/v1/*,
	// /healthz, /static/*).
	dash, err := webdashboard.NewServerWithStore(store)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dashboard: "+err.Error())
		return 1
	}

	// Prometheus registry shared with the webhook handler (so
	// review-pipeline metrics land on /metrics).
	registry := prometheus.NewRegistry()

	// Webhook handler (degraded if creds missing — returns 503).
	wh, whErr := buildWebhookHandlerWithRegistry(logger, registry)

	// Single mux.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	if wh != nil {
		mux.HandleFunc("POST /webhook", wh.ServeHTTP)
	} else {
		mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, _ *http.Request) {
			logger.Warn("webhook disabled: credentials missing", "err", whErr)
			http.Error(w, "webhook disabled: "+whErr.Error(), http.StatusServiceUnavailable)
		})
	}
	// Mount the dashboard's authenticated mux at "/" — it owns its
	// own /healthz handler but the bare /healthz we registered above
	// takes precedence (no auth on the bare path).
	mux.Handle("/", dash.Handler())

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
