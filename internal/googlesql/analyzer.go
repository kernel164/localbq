package googlesql

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// AnalyzeResult holds the output from GoogleSQL analysis.
type AnalyzeResult struct {
	// ResolvedAST is the text representation of the resolved AST tree.
	// Each line represents a node with indentation showing hierarchy.
	ResolvedAST string

	// SQL is the "unanalyzed" SQL regenerated from the resolved AST.
	// This is semantically equivalent to the input but uses resolved names.
	SQL string
}

// Analyzer wraps the googlesql execute_query binary to parse and analyze SQL.
//
// For the spike, this uses subprocess execution (execute_query --mode=analyze).
// In production, this will be replaced by gRPC calls to the sidecar.
type Analyzer struct {
	binaryPath string
	catalog    string // "none", "sample", "tpch"
	timeout    time.Duration
}

// NewAnalyzer creates an Analyzer that uses the given execute_query binary.
func NewAnalyzer(binaryPath string) *Analyzer {
	return &Analyzer{
		binaryPath: binaryPath,
		catalog:    "none",
		timeout:    10 * time.Second,
	}
}

// WithCatalog sets the catalog to use for analysis.
// Valid values: "none", "sample", "tpch".
func (a *Analyzer) WithCatalog(catalog string) *Analyzer {
	a.catalog = catalog
	return a
}

// Analyze parses and resolves a SQL statement, returning the resolved AST.
func (a *Analyzer) Analyze(ctx context.Context, sql string) (*AnalyzeResult, error) {
	ast, err := a.run(ctx, "analyze", sql)
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}
	return &AnalyzeResult{ResolvedAST: ast}, nil
}

// Unanalyze parses, resolves, then regenerates SQL from the resolved AST.
// This is the "lowering" operation: input GoogleSQL -> output resolved SQL.
func (a *Analyzer) Unanalyze(ctx context.Context, sql string) (*AnalyzeResult, error) {
	resolvedSQL, err := a.run(ctx, "unanalyze", sql)
	if err != nil {
		return nil, fmt.Errorf("unanalyze: %w", err)
	}

	// Also get the AST for inspection
	ast, err := a.run(ctx, "analyze", sql)
	if err != nil {
		return nil, fmt.Errorf("analyze (for AST): %w", err)
	}

	return &AnalyzeResult{
		ResolvedAST: ast,
		SQL:         resolvedSQL,
	}, nil
}

// Parse parses SQL without analysis (no catalog needed).
func (a *Analyzer) Parse(ctx context.Context, sql string) (string, error) {
	return a.run(ctx, "parse", sql)
}

func (a *Analyzer) run(ctx context.Context, mode, sql string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.binaryPath,
		"--mode="+mode,
		"--catalog="+a.catalog,
		"--product_mode=external", // BigQuery uses "external" mode
		sql,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", mode, errMsg)
	}

	output := strings.TrimSpace(stdout.String())

	// execute_query exits 0 even on errors, but prints ERROR: to stdout
	if strings.HasPrefix(output, "ERROR:") {
		return "", fmt.Errorf("%s: %s", mode, output)
	}

	return output, nil
}
