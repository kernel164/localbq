# LocalBQ

A local BigQuery emulator powered by DuckDB. Run BigQuery workloads on your laptop, same API, same SQL, zero cloud cost.

```
$ localbq up
LocalBQ v0.1.0 — BigQuery emulator powered by DuckDB
REST API: http://localhost:9060
```

Point any BigQuery client at `localhost:9060` and it works. No service account. No GCP project. No scan costs.

## Why

There is no official Google BigQuery emulator. The only alternative ([goccy/bigquery-emulator](https://github.com/goccy/bigquery-emulator)) is SQLite-backed, one-person-maintained, and has 185+ open issues.

LocalBQ is different:

- **DuckDB underneath.** Columnar, vectorized, Parquet/Arrow native. The right engine for analytics workloads.
- **Real GoogleSQL parsing.** Uses Google's own [GoogleSQL](https://github.com/google/googlesql) analyzer (the same parser BigQuery uses) for correct SQL semantics.
- **BigQuery REST API.** `jobs.query`, `jobs.insert`, `jobs.get`, `datasets.*`, `tables.*`, and more. Official clients work without code changes.

## Architecture

```
 BigQuery Clients (bq CLI, Python SDK, Go SDK, dbt)
       │
       │ HTTP REST (port 9060)
       ▼
 ┌─────────────────────────────────────────────┐
 │             LocalBQ (Go binary)             │
 │                                             │
 │  ┌─────────────┐   ┌────────────────────┐   │
 │  │  REST API    │   │  Metadata Store    │   │
 │  │  (Layer 1)   │   │  (Layer 4)         │   │
 │  │              │   │  DuckDB _meta.*    │   │
 │  │  Routes BQ   │   │  schemas, jobs,    │   │
 │  │  endpoints   │   │  result handles    │   │
 │  └──────┬───────┘   └────────┬───────────┘   │
 │         │                    │               │
 │  ┌──────▼───────┐   ┌───────▼───────────┐   │
 │  │  Lowering    │   │  DuckDB Engine    │   │
 │  │  Pass        │   │  (Layer 3)        │   │
 │  │  (Layer 2)   │   │                   │   │
 │  │              │   │  Executes SQL,    │   │
 │  │  GoogleSQL   │   │  stores data,    │   │
 │  │  AST →       │   │  Parquet/CSV     │   │
 │  │  DuckDB SQL  │   │  ingestion       │   │
 │  └──────┬───────┘   └───────────────────┘   │
 │         │ gRPC                               │
 └─────────┼───────────────────────────────────┘
           │
           ▼
 ┌───────────────────┐
 │  GoogleSQL        │
 │  Sidecar          │
 │                   │
 │  Parse, Analyze,  │
 │  type resolution  │
 │  (C++ binary)     │
 └───────────────────┘
```

**Four layers:**

1. **API Front Door** — Go HTTP server implementing the BigQuery REST API subset. Routes requests, returns BigQuery-shaped JSON responses and errors.

2. **Lowering Pass** — Takes a resolved AST from GoogleSQL (with full type information, function signatures, column references) and emits DuckDB SQL. One rewrite rule per file, visitor pattern. Handles the differences between GoogleSQL and DuckDB: `QUALIFY`, `PIVOT`, `MERGE`, `UNNEST`, array subscripts.

3. **DuckDB Engine** — Executes the lowered SQL. Embedded in-process via [duckdb-go](https://github.com/duckdb/duckdb-go). Handles data storage, Parquet/CSV ingestion, query execution.

4. **Metadata Store** — Tracks projects, datasets, tables, jobs, and result handles in DuckDB internal schemas. Provides `INFORMATION_SCHEMA` views. Persists across restarts.

**GoogleSQL Sidecar** — A separate process running Google's [GoogleSQL](https://github.com/google/googlesql) analyzer. LocalBQ starts it automatically on the first query and communicates via gRPC. This is the same approach Google's Java bindings use. It gives us correct GoogleSQL parsing without CGO or vendoring 558K lines of C++.

## Features

### Core (v0.1)

- **BigQuery REST API** — `jobs.query`, `jobs.insert`, `jobs.get`, `jobs.list`, `jobs.getQueryResults`, `jobs.cancel`, `datasets.*`, `tables.*`, `tabledata.list`
- **GoogleSQL compatibility** — Parsed and analyzed by Google's own SQL engine. Not a regex parser, not SQLGlot transpilation.
- **DuckDB execution** — Native `STRUCT`, `ARRAY`, `JSON`, `MAP`. Columnar, vectorized.
- **Data ingestion** — `LOAD DATA` DDL and `localbq load` CLI for Parquet and CSV
- **INFORMATION_SCHEMA** — `TABLES`, `COLUMNS`, `PARTITIONS`, `JOBS`, `JOBS_BY_PROJECT`, `ROUTINES`, `VIEWS`
- **Partitioning** — `PARTITION BY DATE(col)` accepted (logical). `_PARTITIONTIME` pseudo-columns.
- **Anonymous auth** — Accepts any bearer token. Works with `BIGQUERY_EMULATOR_HOST` env var.

### Differentiators (v0.1)

- **GCS/Parquet federation** — `CREATE EXTERNAL TABLE` queries real GCS Parquet files via DuckDB's native GCS extension with ADC credentials.
- **Dry-run cost projection** — `jobs.query` with `dryRun: true` returns estimated bytes scanned from DuckDB statistics.
- **Contract-test mode** — Opt-in. Diffs emulator results against real BigQuery for fidelity validation.

## Usage

### Standalone

```bash
# Install (macOS)
brew install slokam-ai/tap/localbq

# Or download from GitHub Releases
curl -fsSL https://github.com/slokam-ai/localbq/releases/latest/download/localbq_darwin_arm64.tar.gz | tar xz

# Start
localbq up

# In another terminal
export BIGQUERY_EMULATOR_HOST=localhost:9060
bq query --use_legacy_sql=false 'SELECT 1+1 AS result'
```

### With LocalGCP

```bash
localgcp up --services=bigquery
# BigQuery available at :9060 via Docker proxy
```

### Docker

```bash
docker run -p 9060:9060 ghcr.io/slokam-ai/localbq:latest
```

### Load data

```bash
# Via CLI
localbq load myproject.mydataset.mytable data.parquet

# Via SQL
# In any BigQuery client connected to localbq:
# LOAD DATA INTO myproject.mydataset.mytable FROM 'file:///path/to/data.parquet'
```

## Supported API Surface

| Endpoint | Status |
|----------|--------|
| `jobs.query` | v0.1 |
| `jobs.insert` | v0.1 |
| `jobs.get` | v0.1 |
| `jobs.list` | v0.1 |
| `jobs.getQueryResults` | v0.1 |
| `jobs.cancel` | v0.1 (no-op) |
| `datasets.*` (CRUD) | v0.1 |
| `tables.*` (CRUD) | v0.1 |
| `tabledata.list` | v0.1 |
| `routines.*` (SQL UDFs) | v0.1 |
| Storage Read/Write API | v0.2 |
| BigQuery ML / BI Engine | Not planned |

## Known Limitations

Things that work differently from real BigQuery:

- **Serialized execution.** Queries run one at a time behind a mutex. Fine for dev/CI. Concurrent execution is planned for v1.0.
- **BIGNUMERIC.** DuckDB supports 38-digit `DECIMAL`. Values exceeding this return a clear error.
- **GEOGRAPHY.** DuckDB spatial uses planar coordinates; BigQuery uses WGS84 geodesic. Returns `UNSUPPORTED_FEATURE`.
- **Scripting.** `DECLARE`, stored procedures, JS UDFs are not supported.
- **Clustering.** `CLUSTER BY` is accepted in DDL but has no execution impact.

Unsupported features return BigQuery-shaped error responses with reason `UNSUPPORTED_FEATURE`.

## Development

```bash
# Prerequisites
go 1.24+
# GoogleSQL binary (downloaded automatically or from google/googlesql releases)

# Build
go build ./cmd/localbq/

# Test
go test ./...

# Run
./localbq up --port=9060
```

## How It Works

LocalBQ uses a novel architecture: Google's own [GoogleSQL](https://github.com/google/googlesql) C++ analyzer runs as a sidecar process, communicating with the Go binary via gRPC. This gives us:

- **Correct parsing.** The same parser BigQuery uses in production. Not an approximation.
- **Resolved AST.** Full type information, function signatures, column references. The lowering pass knows exactly what it's transforming.
- **No CGO for SQL.** The sidecar is a pre-built binary. The Go project only uses CGO for DuckDB (via the official Go driver).

The lowering pass walks the resolved AST and emits DuckDB-compatible SQL. Where BigQuery and DuckDB agree (~80% of queries), the SQL passes through unchanged. Where they diverge (QUALIFY, PIVOT, MERGE, UNNEST), documented rewrite rules transform the AST.

## License

Apache-2.0

This project uses:
- [GoogleSQL](https://github.com/google/googlesql) (Apache-2.0) for SQL parsing and analysis
- [DuckDB](https://github.com/duckdb/duckdb) (MIT) for query execution
