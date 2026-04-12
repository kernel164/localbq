// Package engine wraps DuckDB as the query execution layer (Layer 3).
//
// Responsibilities:
//   - Open/close the DuckDB database
//   - Execute SQL queries and return typed results
//   - Map DuckDB column types to BigQuery field types
//   - Schema (dataset) management via DuckDB schemas
package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "github.com/marcboeker/go-duckdb"
)

// FieldType represents a BigQuery field type.
type FieldType string

const (
	TypeString    FieldType = "STRING"
	TypeBytes     FieldType = "BYTES"
	TypeInteger   FieldType = "INTEGER"
	TypeFloat     FieldType = "FLOAT"
	TypeBoolean   FieldType = "BOOLEAN"
	TypeTimestamp FieldType = "TIMESTAMP"
	TypeDate      FieldType = "DATE"
	TypeTime      FieldType = "TIME"
	TypeDatetime  FieldType = "DATETIME"
	TypeNumeric   FieldType = "NUMERIC"
	TypeJSON      FieldType = "JSON"
	TypeRecord    FieldType = "RECORD"
)

// Column describes a result column.
type Column struct {
	Name string
	Type FieldType
}

// Result holds query execution results.
type Result struct {
	Columns []Column
	Rows    [][]any
}

// Engine wraps a DuckDB database for query execution.
type Engine struct {
	db *sql.DB
	mu sync.Mutex // serialized execution for v0.1
}

// Open creates a new Engine backed by a DuckDB database at the given path.
// Pass ":memory:" for an in-memory database (useful for tests).
func Open(dbPath string) (*Engine, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	return &Engine{db: db}, nil
}

// Close closes the underlying DuckDB connection.
func (e *Engine) Close() error {
	return e.db.Close()
}

// DryRun validates a query without executing it, returning column metadata.
// Uses EXPLAIN to validate syntax and schema references.
func (e *Engine) DryRun(ctx context.Context, query string) (*Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rows, err := e.db.QueryContext(ctx, "EXPLAIN "+query)
	if err != nil {
		return nil, fmt.Errorf("dry run: %w", err)
	}
	rows.Close()

	// For SELECT queries, run with LIMIT 0 to get column types without data
	result := &Result{}
	probe, err := e.db.QueryContext(ctx, query+" LIMIT 0")
	if err != nil {
		// Query validates but LIMIT 0 fails (e.g. DML) — return empty result
		return result, nil
	}
	defer probe.Close()

	cols, err := probe.ColumnTypes()
	if err != nil {
		return result, nil
	}
	result.Columns = make([]Column, len(cols))
	for i, col := range cols {
		result.Columns[i] = Column{
			Name: col.Name(),
			Type: mapDuckDBType(col.DatabaseTypeName()),
		}
	}
	return result, nil
}

// Query executes a SQL query and returns the results.
func (e *Engine) Query(ctx context.Context, query string) (*Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rows, err := e.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("column types: %w", err)
	}

	result := &Result{
		Columns: make([]Column, len(cols)),
	}
	for i, col := range cols {
		result.Columns[i] = Column{
			Name: col.Name(),
			Type: mapDuckDBType(col.DatabaseTypeName()),
		}
	}

	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		result.Rows = append(result.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return result, nil
}

// Exec executes a SQL statement that doesn't return rows (DDL, DML).
func (e *Engine) Exec(ctx context.Context, query string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, err := e.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	return nil
}

// CreateSchema creates a DuckDB schema (maps to a BigQuery dataset).
func (e *Engine) CreateSchema(ctx context.Context, name string) error {
	return e.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quoteIdent(name)))
}

// DropSchema drops a DuckDB schema.
func (e *Engine) DropSchema(ctx context.Context, name string, cascade bool) error {
	q := fmt.Sprintf("DROP SCHEMA IF EXISTS %s", quoteIdent(name))
	if cascade {
		q += " CASCADE"
	}
	return e.Exec(ctx, q)
}

// SchemaExists checks if a schema exists.
func (e *Engine) SchemaExists(ctx context.Context, name string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var count int
	err := e.db.QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.schemata WHERE schema_name = ?", name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListSchemas returns all user-created schemas (excludes DuckDB internals).
func (e *Engine) ListSchemas(ctx context.Context) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rows, err := e.db.QueryContext(ctx,
		"SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema', 'main', 'pg_catalog') ORDER BY schema_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		schemas = append(schemas, name)
	}
	return schemas, rows.Err()
}

// ListTables returns all tables in a schema.
func (e *Engine) ListTables(ctx context.Context, schema string) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rows, err := e.db.QueryContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = ? ORDER BY table_name", schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// TableColumns returns the columns for a table in a schema.
func (e *Engine) TableColumns(ctx context.Context, schema, table string) ([]Column, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rows, err := e.db.QueryContext(ctx,
		`SELECT column_name, data_type FROM information_schema.columns
		 WHERE table_schema = ? AND table_name = ?
		 ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var name, dtype string
		if err := rows.Scan(&name, &dtype); err != nil {
			return nil, err
		}
		cols = append(cols, Column{Name: name, Type: mapDuckDBType(dtype)})
	}
	return cols, rows.Err()
}

// TableExists checks if a table exists in a schema.
func (e *Engine) TableExists(ctx context.Context, schema, table string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var count int
	err := e.db.QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		schema, table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DropTable drops a table from a schema.
func (e *Engine) DropTable(ctx context.Context, schema, table string) error {
	return e.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", quoteIdent(schema), quoteIdent(table)))
}

// mapDuckDBType maps a DuckDB type name to a BigQuery field type.
func mapDuckDBType(duckType string) FieldType {
	upper := strings.ToUpper(duckType)

	switch {
	case strings.Contains(upper, "INT"):
		return TypeInteger
	case upper == "FLOAT" || upper == "DOUBLE" || strings.Contains(upper, "REAL"):
		return TypeFloat
	case upper == "BOOLEAN" || upper == "BOOL":
		return TypeBoolean
	case upper == "DATE":
		return TypeDate
	case upper == "TIME":
		return TypeTime
	case strings.Contains(upper, "TIMESTAMP"):
		return TypeTimestamp
	case strings.Contains(upper, "DECIMAL") || strings.Contains(upper, "NUMERIC"):
		return TypeNumeric
	case upper == "BLOB" || upper == "BYTEA":
		return TypeBytes
	case upper == "JSON":
		return TypeJSON
	case upper == "STRUCT" || strings.HasPrefix(upper, "STRUCT"):
		return TypeRecord
	default:
		return TypeString
	}
}

// quoteIdent quotes a SQL identifier.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
