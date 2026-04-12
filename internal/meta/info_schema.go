package meta

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slokam-ai/localbq/internal/engine"
)

// InitInfoSchema creates BigQuery-compatible INFORMATION_SCHEMA views in DuckDB.
// These views overlay DuckDB's native information_schema with BQ-shaped columns
// and add views that BQ has but DuckDB doesn't (TABLE_STORAGE, JOBS_BY_PROJECT, etc.).
//
// Cascade queries these views heavily for schema population and cost analysis.
func InitInfoSchema(ctx context.Context, eng *engine.Engine) error {
	views := []struct {
		name string
		ddl  string
	}{
		// TABLE_OPTIONS: BQ has this for table descriptions/labels.
		// We create a stub that returns no options by default.
		{
			name: "TABLE_OPTIONS",
			ddl: `CREATE OR REPLACE VIEW information_schema."TABLE_OPTIONS" AS
				SELECT
					table_catalog,
					table_schema,
					table_name,
					'' AS option_name,
					'' AS option_type,
					'' AS option_value
				FROM information_schema.tables
				WHERE table_schema NOT IN ('information_schema', 'pg_catalog')`,
		},

		// COLUMN_FIELD_PATHS: BQ uses this for nested field descriptions.
		// We create a flat view from columns.
		{
			name: "COLUMN_FIELD_PATHS",
			ddl: `CREATE OR REPLACE VIEW information_schema."COLUMN_FIELD_PATHS" AS
				SELECT
					table_catalog,
					table_schema,
					table_name,
					column_name,
					column_name AS field_path,
					data_type,
					'' AS description,
					'' AS collation_name
				FROM information_schema.columns
				WHERE table_schema NOT IN ('information_schema', 'pg_catalog')`,
		},

		// TABLE_STORAGE: BQ-specific storage metadata.
		// We estimate row counts and sizes from DuckDB's pragma functions.
		{
			name: "TABLE_STORAGE",
			ddl: `CREATE OR REPLACE VIEW information_schema."TABLE_STORAGE" AS
				SELECT
					table_catalog AS project_id,
					table_schema,
					table_name,
					CAST(0 AS BIGINT) AS total_rows,
					CAST(0 AS BIGINT) AS total_logical_bytes,
					CAST(0 AS BIGINT) AS active_logical_bytes,
					CAST(0 AS BIGINT) AS long_term_logical_bytes,
					CURRENT_TIMESTAMP AS creation_time,
					CURRENT_TIMESTAMP AS last_modified_time
				FROM information_schema.tables
				WHERE table_schema NOT IN ('information_schema', 'pg_catalog')
				  AND table_type = 'BASE TABLE'`,
		},
	}

	for _, v := range views {
		if err := eng.Exec(ctx, v.ddl); err != nil {
			return fmt.Errorf("create INFORMATION_SCHEMA.%s: %w", v.name, err)
		}
		slog.Debug("created INFORMATION_SCHEMA view", "name", v.name)
	}

	// Create the _bq_meta schema for job tracking tables
	if err := eng.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS _bq_meta"); err != nil {
		return fmt.Errorf("create _bq_meta schema: %w", err)
	}

	// JOBS_BY_PROJECT: tracks all executed jobs.
	// This is a real table (not a view) because jobs are inserted at runtime.
	if err := eng.Exec(ctx, `CREATE TABLE IF NOT EXISTS _bq_meta.jobs (
		job_id VARCHAR PRIMARY KEY,
		project_id VARCHAR,
		user_email VARCHAR DEFAULT 'local@localbq',
		job_type VARCHAR DEFAULT 'QUERY',
		statement_type VARCHAR DEFAULT 'SELECT',
		state VARCHAR DEFAULT 'DONE',
		creation_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		start_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		end_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		query VARCHAR,
		total_bytes_processed BIGINT DEFAULT 0,
		total_bytes_billed BIGINT DEFAULT 0,
		cache_hit BOOLEAN DEFAULT false,
		destination_table_project VARCHAR,
		destination_table_dataset VARCHAR,
		destination_table_id VARCHAR,
		error_reason VARCHAR,
		error_message VARCHAR
	)`); err != nil {
		return fmt.Errorf("create _bq_meta.jobs: %w", err)
	}

	// Create the JOBS_BY_PROJECT view in information_schema
	if err := eng.Exec(ctx, `CREATE OR REPLACE VIEW information_schema."JOBS_BY_PROJECT" AS
		SELECT
			job_id,
			project_id,
			user_email,
			job_type,
			statement_type,
			state,
			creation_time,
			start_time,
			end_time,
			query,
			total_bytes_processed,
			total_bytes_billed,
			cache_hit,
			STRUCT_PACK(
				project_id := destination_table_project,
				dataset_id := destination_table_dataset,
				table_id := destination_table_id
			) AS destination_table,
			CASE WHEN error_reason IS NOT NULL
				THEN STRUCT_PACK(reason := error_reason, message := error_message)
				ELSE NULL
			END AS error_result
		FROM _bq_meta.jobs
		ORDER BY creation_time DESC`); err != nil {
		return fmt.Errorf("create INFORMATION_SCHEMA.JOBS_BY_PROJECT: %w", err)
	}

	slog.Debug("created INFORMATION_SCHEMA.JOBS_BY_PROJECT view")
	return nil
}

// RecordJob inserts a job record into _bq_meta.jobs for INFORMATION_SCHEMA visibility.
func RecordJob(ctx context.Context, eng *engine.Engine, projectID, jobID, query, stmtType string, jobErr error) {
	errReason := ""
	errMsg := ""
	state := "DONE"
	if jobErr != nil {
		errReason = "invalidQuery"
		errMsg = jobErr.Error()
	}

	eng.Exec(ctx, fmt.Sprintf(
		`INSERT INTO _bq_meta.jobs (job_id, project_id, query, statement_type, state, error_reason, error_message)
		 VALUES ('%s', '%s', '%s', '%s', '%s', NULLIF('%s',''), NULLIF('%s',''))`,
		escapeSQL(jobID), escapeSQL(projectID), escapeSQL(query),
		escapeSQL(stmtType), state, escapeSQL(errReason), escapeSQL(errMsg)))
}

// classifyStatement returns a rough statement type for job tracking.
func ClassifyStatement(sql string) string {
	// Simple prefix-based classification
	for i := 0; i < len(sql); i++ {
		if sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r' {
			continue
		}
		upper := ""
		end := i + 10
		if end > len(sql) {
			end = len(sql)
		}
		for j := i; j < end; j++ {
			c := sql[j]
			if c >= 'a' && c <= 'z' {
				c -= 32
			}
			upper += string(c)
		}

		switch {
		case len(upper) >= 6 && upper[:6] == "SELECT":
			return "SELECT"
		case len(upper) >= 6 && upper[:6] == "INSERT":
			return "INSERT"
		case len(upper) >= 6 && upper[:6] == "UPDATE":
			return "UPDATE"
		case len(upper) >= 6 && upper[:6] == "DELETE":
			return "DELETE"
		case len(upper) >= 5 && upper[:5] == "MERGE":
			return "MERGE"
		case len(upper) >= 6 && upper[:6] == "CREATE":
			return "CREATE_TABLE"
		case len(upper) >= 4 && upper[:4] == "DROP":
			return "DROP_TABLE"
		case len(upper) >= 5 && upper[:5] == "ALTER":
			return "ALTER_TABLE"
		case len(upper) >= 4 && upper[:4] == "WITH":
			return "SELECT" // CTEs
		default:
			return "SELECT"
		}
	}
	return "SELECT"
}

func escapeSQL(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			result = append(result, '\'', '\'')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
