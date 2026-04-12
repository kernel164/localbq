package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/slokam-ai/localbq/internal/api"
	"github.com/slokam-ai/localbq/internal/engine"
	"github.com/slokam-ai/localbq/internal/googlesql"
	"github.com/slokam-ai/localbq/internal/meta"
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

var daemon = flag.Bool("d", false, "run in background (daemon mode)")

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: localbq <command> [flags]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  up        Start the BigQuery emulator (add -d for background)\n")
	fmt.Fprintf(os.Stderr, "  stop      Stop a running background instance\n")
	fmt.Fprintf(os.Stderr, "  status    Check if LocalBQ is running\n")
	fmt.Fprintf(os.Stderr, "  load      Load data from Parquet/CSV/JSON into a table\n")
	fmt.Fprintf(os.Stderr, "  version   Print version\n")
	fmt.Fprintf(os.Stderr, "\nRun 'localbq <command> --help' for details.\n")
}

func main() {
	// Handle subcommands before flag parsing
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Printf("localbq %s\n", version)
			os.Exit(0)
		case "load":
			loadFlags := flag.NewFlagSet("load", flag.ExitOnError)
			loadDataDir := loadFlags.String("data-dir", defaultDataDir(), "data directory for DuckDB database")
			loadFlags.Parse(os.Args[2:])
			dataDir = loadDataDir
			runLoad(loadFlags.Args())
			return
		case "up":
			// Strip "up" from args so flag.Parse sees the flags
			os.Args = append(os.Args[:1], os.Args[2:]...)
		case "stop":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			flag.Parse()
			runStop()
			return
		case "status":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			flag.Parse()
			runStatus()
			return
		case "help", "--help", "-h":
			usage()
			os.Exit(0)
		}
	}

	flag.Parse()

	// Daemon mode: re-exec in background
	if *daemon {
		daemonStart()
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		slog.Error("failed to create data directory", "path", *dataDir, "error", err)
		os.Exit(1)
	}

	// GoogleSQL sidecar (optional — enhances SQL parsing if present)
	gsqlBinary, _ := googlesql.FindBinary()
	var sidecar *googlesql.Sidecar
	if gsqlBinary != "" {
		sidecar = googlesql.NewSidecar(gsqlBinary, *googlesqlTimeout)
		defer sidecar.Stop()
		slog.Info("googlesql sidecar found", "binary", gsqlBinary)
	}

	// Open DuckDB
	dbPath := filepath.Join(*dataDir, "localbq.duckdb")
	eng, err := engine.Open(dbPath)
	if err != nil {
		slog.Error("failed to open DuckDB", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer eng.Close()

	// Initialize INFORMATION_SCHEMA views
	if err := meta.InitInfoSchema(context.Background(), eng); err != nil {
		slog.Error("failed to initialize INFORMATION_SCHEMA", "error", err)
		os.Exit(1)
	}

	store := meta.NewStore(eng)
	mux := http.NewServeMux()

	// Register BigQuery API routes directly on mux
	api.New(mux, eng, store)

	// Status endpoint at root
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"kind":"localbq#status","version":"%s","status":"ok","duckdb":"%s"}`,
			version, dbPath)
	})

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
		fmt.Fprintf(os.Stderr, "DuckDB:   %s\n", dbPath)
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
