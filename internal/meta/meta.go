// Package meta implements the metadata and control plane (Layer 4).
//
// Manages: projects, datasets, tables, views, routines, jobs, result handles.
// Storage: DuckDB _meta.* schemas, file-backed persistence.
//
// Key behaviors:
//   - Result handles: 1-hour TTL, cleaned by background goroutine every 5 minutes
//   - Result handles are ephemeral (lost on restart)
//   - Metadata (projects, datasets, tables) persists across restarts
//   - Uses a read-only DuckDB connection (never blocks on query execution)
//   - INFORMATION_SCHEMA views: TABLES, COLUMNS, PARTITIONS, JOBS, JOBS_BY_PROJECT, ROUTINES, VIEWS
package meta
