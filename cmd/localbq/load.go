package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/slokam-ai/localbq/internal/engine"
	"github.com/slokam-ai/localbq/internal/meta"
)

// runLoad handles: localbq load <dataset.table> <file> [--data-dir=...]
func runLoad(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: localbq load <dataset.table> <file.parquet|file.csv|file.json>\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  localbq load mydata.users users.parquet\n")
		fmt.Fprintf(os.Stderr, "  localbq load mydata.events events.csv\n")
		os.Exit(1)
	}

	target := args[0]
	filePath := args[1]

	// Parse dataset.table
	parts := strings.SplitN(target, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		fmt.Fprintf(os.Stderr, "Error: target must be dataset.table (e.g., mydata.users)\n")
		os.Exit(1)
	}
	dataset, table := parts[0], parts[1]

	// Resolve file path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid file path: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(absPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: file not found: %s\n", absPath)
		os.Exit(1)
	}

	// Detect format
	readFunc, err := readFuncForFile(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create data directory %s: %v\n", *dataDir, err)
		os.Exit(1)
	}

	// Open DuckDB
	dbPath := filepath.Join(*dataDir, "localbq.duckdb")
	eng, err := engine.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to open DuckDB at %s: %v\n", dbPath, err)
		os.Exit(1)
	}
	defer eng.Close()

	ctx := context.Background()

	// Initialize INFORMATION_SCHEMA (needed for metadata consistency)
	if err := meta.InitInfoSchema(ctx, eng); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize metadata: %v\n", err)
		os.Exit(1)
	}

	// Ensure dataset (schema) exists
	if err := eng.CreateSchema(ctx, dataset); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create dataset %s: %v\n", dataset, err)
		os.Exit(1)
	}

	// Check if table exists
	exists, err := eng.TableExists(ctx, dataset, table)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	quotedTable := fmt.Sprintf(`"%s"."%s"`, dataset, table)

	if exists {
		// Table exists — INSERT INTO
		query := fmt.Sprintf("INSERT INTO %s SELECT * FROM %s('%s')", quotedTable, readFunc, escapePath(absPath))
		if err := eng.Exec(ctx, query); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to load data: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Table doesn't exist — CREATE TABLE AS
		query := fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s('%s')", quotedTable, readFunc, escapePath(absPath))
		if err := eng.Exec(ctx, query); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create table from file: %v\n", err)
			os.Exit(1)
		}
	}

	// Report results
	result, err := eng.Query(ctx, fmt.Sprintf("SELECT count(*) FROM %s", quotedTable))
	if err != nil {
		slog.Warn("failed to count rows", "error", err)
	}
	rowCount := "unknown"
	if result != nil && len(result.Rows) > 0 {
		rowCount = fmt.Sprintf("%v", result.Rows[0][0])
	}

	action := "Created"
	if exists {
		action = "Loaded into"
	}
	fmt.Fprintf(os.Stderr, "%s %s.%s (%s rows) from %s\n", action, dataset, table, rowCount, filepath.Base(filePath))
}

// readFuncForFile returns the DuckDB read function for the given file extension.
func readFuncForFile(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".parquet":
		return "read_parquet", nil
	case ".csv", ".tsv":
		return "read_csv", nil
	case ".json", ".jsonl", ".ndjson":
		return "read_json", nil
	default:
		return "", fmt.Errorf("unsupported file format %q (supported: .parquet, .csv, .tsv, .json, .jsonl, .ndjson)", ext)
	}
}

// escapePath escapes single quotes in file paths for SQL.
func escapePath(path string) string {
	return strings.ReplaceAll(path, "'", "''")
}
