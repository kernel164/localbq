package googlesql_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/slokam-ai/localbq/internal/googlesql"
)

func findBinary(t *testing.T) string {
	t.Helper()

	// Check for the binary in common locations
	candidates := []string{
		os.Getenv("GOOGLESQL_SERVER_PATH"),
		"../../bin/execute_query_macos",
	}

	for _, c := range candidates {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}

	// Try FindBinary
	p, err := googlesql.FindBinary()
	if err == nil {
		return p
	}

	t.Skip("execute_query binary not found, skipping integration test")
	return ""
}

func TestAnalyzer_Parse(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary)

	result, err := a.Parse(context.Background(), "SELECT 1+1 AS result")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result, "QueryStatement") {
		t.Errorf("expected QueryStatement in parse output, got:\n%s", result)
	}
	if !strings.Contains(result, "SelectColumn") {
		t.Errorf("expected SelectColumn in parse output, got:\n%s", result)
	}
}

func TestAnalyzer_Analyze_SimpleQuery(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary)

	result, err := a.Analyze(context.Background(), "SELECT 1+1 AS result")
	if err != nil {
		t.Fatal(err)
	}

	ast := result.ResolvedAST
	if !strings.Contains(ast, "QueryStmt") {
		t.Errorf("expected QueryStmt, got:\n%s", ast)
	}
	if !strings.Contains(ast, "INT64") {
		t.Errorf("expected INT64 type annotation, got:\n%s", ast)
	}
	if !strings.Contains(ast, "$add") {
		t.Errorf("expected $add function call, got:\n%s", ast)
	}
}

func TestAnalyzer_Analyze_WithCatalog(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary).WithCatalog("sample")

	result, err := a.Analyze(context.Background(), "SELECT key, value FROM KeyValue WHERE key > 10")
	if err != nil {
		t.Fatal(err)
	}

	ast := result.ResolvedAST
	if !strings.Contains(ast, "TableScan") {
		t.Errorf("expected TableScan, got:\n%s", ast)
	}
	if !strings.Contains(ast, "KeyValue") {
		t.Errorf("expected KeyValue table reference, got:\n%s", ast)
	}
	if !strings.Contains(ast, "FilterScan") {
		t.Errorf("expected FilterScan (WHERE clause), got:\n%s", ast)
	}
	if !strings.Contains(ast, "$greater") {
		t.Errorf("expected $greater function (> operator), got:\n%s", ast)
	}
}

func TestAnalyzer_Analyze_Aggregation(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary).WithCatalog("sample")

	result, err := a.Analyze(context.Background(), "SELECT key, COUNT(*) AS cnt FROM KeyValue GROUP BY key")
	if err != nil {
		t.Fatal(err)
	}

	ast := result.ResolvedAST
	if !strings.Contains(ast, "AggregateScan") {
		t.Errorf("expected AggregateScan, got:\n%s", ast)
	}
	if !strings.Contains(ast, "$count_star") {
		t.Errorf("expected $count_star function, got:\n%s", ast)
	}
}

func TestAnalyzer_Analyze_MergeStatement(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary).WithCatalog("sample")

	sql := `MERGE INTO KeyValue AS target
		USING (SELECT 1 AS key, 'new' AS value) AS source
		ON target.Key = source.key
		WHEN MATCHED THEN UPDATE SET value = source.value
		WHEN NOT MATCHED THEN INSERT (Key, Value) VALUES (source.key, source.value)`

	result, err := a.Analyze(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}

	ast := result.ResolvedAST
	if !strings.Contains(ast, "MergeStmt") {
		t.Errorf("expected MergeStmt, got:\n%s", ast)
	}
	if !strings.Contains(ast, "MATCHED") {
		t.Errorf("expected MATCHED clause, got:\n%s", ast)
	}
}

func TestAnalyzer_Unanalyze_Roundtrip(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary).WithCatalog("sample")

	result, err := a.Unanalyze(context.Background(), "SELECT key, value FROM KeyValue WHERE key > 10 ORDER BY key")
	if err != nil {
		t.Fatal(err)
	}

	if result.SQL == "" {
		t.Fatal("expected non-empty SQL from unanalyze")
	}
	if result.ResolvedAST == "" {
		t.Fatal("expected non-empty AST from unanalyze")
	}

	// The unanalyzed SQL should be valid and contain the key elements
	sql := strings.ToUpper(result.SQL)
	if !strings.Contains(sql, "SELECT") {
		t.Errorf("expected SELECT in unanalyzed SQL, got:\n%s", result.SQL)
	}
	if !strings.Contains(sql, "ORDER BY") {
		t.Errorf("expected ORDER BY in unanalyzed SQL, got:\n%s", result.SQL)
	}
}

func TestAnalyzer_InvalidSQL(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary)

	_, err := a.Parse(context.Background(), "SELECTT INVALID GARBAGE")
	if err == nil {
		t.Fatal("expected error for invalid SQL")
	}
}

func TestAnalyzer_UnknownTable(t *testing.T) {
	binary := findBinary(t)
	a := googlesql.NewAnalyzer(binary).WithCatalog("none")

	_, err := a.Analyze(context.Background(), "SELECT * FROM nonexistent_table")
	if err == nil {
		t.Fatal("expected error for unknown table")
	}
}
