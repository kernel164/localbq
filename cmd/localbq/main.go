package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slokam-ai/localbq/internal/api"
	"github.com/slokam-ai/localbq/internal/googlesql"
)

var (
	version          = "dev"
	port             = flag.Int("port", 9060, "REST API port")
	dataDir          = flag.String("data-dir", defaultDataDir(), "data directory for DuckDB database")
	googlesqlTimeout = flag.Duration("googlesql-timeout", 5*time.Second, "timeout for googlesql sidecar startup (increase for slow CI runners)")
)

func defaultDataDir() string {
	home, _ := os.UserHomeDir()
	return home + "/.localbq"
}

func main() {
	flag.Parse()

	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("localbq %s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		slog.Error("failed to create data directory", "path", *dataDir, "error", err)
		os.Exit(1)
	}

	// Find and prepare the googlesql sidecar (started lazily on first query)
	gsqlBinary, err := googlesql.FindBinary()
	if err != nil {
		slog.Warn("googlesql sidecar binary not found, SQL parsing will be unavailable", "error", err)
	}
	var sidecar *googlesql.Sidecar
	if gsqlBinary != "" {
		sidecar = googlesql.NewSidecar(gsqlBinary, *googlesqlTimeout)
		defer sidecar.Stop()
	}

	apiServer := api.New()
	mux := http.NewServeMux()

	// Status endpoint at root
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Fall through to API server for non-root paths
			apiServer.Handler().ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		sidecarStatus := "not_configured"
		if sidecar != nil {
			if sidecar.Port() > 0 {
				sidecarStatus = fmt.Sprintf("running (port %d)", sidecar.Port())
			} else {
				sidecarStatus = "ready (starts on first query)"
			}
		}
		fmt.Fprintf(w, `{"kind":"localbq#status","version":"%s","status":"ok","googlesql":"%s"}`, version, sidecarStatus)
	})

	// BigQuery API routes
	mux.Handle("/bigquery/", apiServer.Handler())

	addr := fmt.Sprintf(":%d", *port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Fprintf(os.Stderr, "LocalBQ %s — BigQuery emulator powered by DuckDB\n", version)
		fmt.Fprintf(os.Stderr, "REST API: http://localhost%s\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}
