# LocalBQ

A local BigQuery emulator powered by DuckDB. Run BigQuery workloads on your laptop — same API, same SQL, zero cloud cost.

```
$ localbq up
LocalBQ dev — BigQuery emulator powered by DuckDB
REST API: http://localhost:9060
DuckDB:   ~/.localbq/localbq.duckdb
```

Point any BigQuery client at `localhost:9060` and it works. No service account. No GCP project. No scan costs.

## Quick Start

### Docker

```bash
docker run -p 9060:9060 ghcr.io/slokam-ai/localbq:latest
```

### From source

```bash
go install github.com/slokam-ai/localbq/cmd/localbq@latest
localbq up        # foreground
localbq up -d     # background (daemon mode)
localbq status    # check if running
localbq stop      # stop background instance
```

### Query it

```bash
# One env var — your existing bq CLI just works
export CLOUDSDK_API_ENDPOINT_OVERRIDES_BIGQUERY=http://localhost:9060/
bq --project_id=my-project query --use_legacy_sql=false 'SELECT 1 + 1 AS result'
```

```
+--------+
| result |
+--------+
|      2 |
+--------+
```

## Client SDKs

### bq CLI

No code changes needed. One env var:

```bash
export CLOUDSDK_API_ENDPOINT_OVERRIDES_BIGQUERY=http://localhost:9060/

bq --project_id=my-project query --use_legacy_sql=false 'SELECT 1 + 1 AS result'
bq --project_id=my-project ls                    # list datasets
bq --project_id=my-project mk my_dataset         # create dataset
```

### Python

```python
from google.cloud import bigquery
from google.auth.credentials import AnonymousCredentials

client = bigquery.Client(
    project="my-project",
    credentials=AnonymousCredentials(),
    client_options={"api_endpoint": "http://localhost:9060"},
)

for row in client.query("SELECT 42 AS answer").result():
    print(row.answer)  # 42
```

### Go

```go
client, _ := bigquery.NewClient(ctx, "my-project",
    option.WithEndpoint("http://localhost:9060/bigquery/v2/"),
    option.WithoutAuthentication(),
)

it, _ := client.Query("SELECT 42 AS answer").Read(ctx)
```

## Switching Between LocalBQ and Production

LocalBQ is designed for seamless switching. No config files to edit, no code to change.

### bq CLI

```bash
# Use LocalBQ
export CLOUDSDK_API_ENDPOINT_OVERRIDES_BIGQUERY=http://localhost:9060/
bq query --use_legacy_sql=false 'SELECT 1'  # → hits LocalBQ

# Switch back to real BigQuery
unset CLOUDSDK_API_ENDPOINT_OVERRIDES_BIGQUERY
bq query --use_legacy_sql=false 'SELECT 1'  # → hits Google Cloud
```

### Python / Go

Use an env var check so the same code works in both environments:

```python
import os
from google.cloud import bigquery
from google.auth.credentials import AnonymousCredentials

endpoint = os.getenv("LOCALBQ_ENDPOINT")
if endpoint:
    client = bigquery.Client(
        project="my-project",
        credentials=AnonymousCredentials(),
        client_options={"api_endpoint": endpoint},
    )
else:
    client = bigquery.Client()  # normal GCP auth + production endpoint
```

```go
if endpoint := os.Getenv("LOCALBQ_ENDPOINT"); endpoint != "" {
    client, _ = bigquery.NewClient(ctx, project,
        option.WithEndpoint(endpoint+"/bigquery/v2/"),
        option.WithoutAuthentication())
} else {
    client, _ = bigquery.NewClient(ctx, project)
}
```

## Load Data

```bash
# From Parquet
localbq load mydata.users users.parquet

# From CSV
localbq load mydata.events events.csv

# Via SQL (from any connected client)
LOAD DATA INTO mydata.users FROM '/path/to/users.parquet'
```

Supports Parquet, CSV, TSV, JSON, and NDJSON. If the table doesn't exist, it's created from the file schema. If it exists, data is appended.

## What Works

### API Endpoints

| Endpoint | Status |
|----------|--------|
| `jobs.query` | Working (with GoogleSQL lowering) |
| `jobs.insert` | Working (with dry-run support) |
| `jobs.get` | Working |
| `jobs.list` | Working |
| `jobs.getQueryResults` | Working |
| `datasets.*` (CRUD) | Working |
| `tables.*` (CRUD) | Working |
| `tabledata.list` | Working |
| `jobs.cancel` | Stub (no-op) |

### GoogleSQL Compatibility

LocalBQ translates GoogleSQL syntax to DuckDB SQL. These patterns are supported:

- `TIMESTAMP_SUB`, `TIMESTAMP_ADD`, `TIMESTAMP_TRUNC`
- `DATE_SUB`, `DATE_ADD`, `DATE()` function
- `CURRENT_TIMESTAMP()`, `CURRENT_DATE()` with parentheses
- `COUNTIF()` → `count_if()`
- `SAFE_CAST()` → `TRY_CAST()`
- `FLOAT64`, `BOOL` type names
- Backtick-quoted identifiers
- `INFORMATION_SCHEMA` with regional prefix (`region-us.INFORMATION_SCHEMA.*`)
- Three-part table references (project stripped)
- `LOAD DATA INTO ... FROM '...'`

### INFORMATION_SCHEMA Views

| View | Status |
|------|--------|
| `TABLES` | Working (native DuckDB) |
| `COLUMNS` | Working (native DuckDB) |
| `JOBS_BY_PROJECT` | Working (tracks all executed queries) |
| `TABLE_STORAGE` | Stub (zero-valued sizes) |
| `TABLE_OPTIONS` | Stub |
| `COLUMN_FIELD_PATHS` | Stub |

### Other Features

- **Dry-run** — `dryRun: true` validates queries without executing, returns 0 bytes processed
- **Anonymous auth** — accepts any bearer token or no token at all
- **Data persistence** — DuckDB file at `~/.localbq/localbq.duckdb`, survives restarts
- **Job tracking** — every query is recorded and visible in `INFORMATION_SCHEMA.JOBS_BY_PROJECT`

## Known Limitations

These GoogleSQL features are not yet supported and will return errors:

- `MERGE` with `WHEN NOT MATCHED BY SOURCE`
- `QUALIFY` clause
- `PIVOT` / `UNPIVOT`
- `SELECT AS STRUCT`, `SELECT AS VALUE`
- Complex `UNNEST` with explicit joins
- `ARRAY_AGG(DISTINCT struct)`
- Scripting (`DECLARE`, stored procedures, JS UDFs)
- `GEOGRAPHY` type (WGS84 geodesic)
- `BIGNUMERIC` beyond 38 digits
- Storage Read/Write API (gRPC)
- BigQuery ML / BI Engine

Standard SQL that DuckDB supports natively (joins, CTEs, window functions, aggregations, subqueries) works without any lowering.

## Architecture

```
 BigQuery Clients (Python SDK, Go SDK, bq CLI, dbt)
       |
       | HTTP REST (port 9060)
       v
 +---------------------------------------------+
 |             LocalBQ (Go binary)              |
 |                                              |
 |  REST API --> Lowering Pass --> DuckDB Engine |
 |                                              |
 |  GoogleSQL      15 rewrite      Columnar,    |
 |  patterns  -->  rules       --> vectorized,  |
 |  (text-based)                   Parquet/CSV  |
 |                                              |
 |  Metadata Store (INFORMATION_SCHEMA, jobs)   |
 +----------------------------------------------+
```

Single Go binary. DuckDB embedded via CGO. No external dependencies.

## Development

```bash
go build ./cmd/localbq/
go test ./...
./localbq --port 9060
```

## License

MIT

This project uses [DuckDB](https://github.com/duckdb/duckdb) (MIT) for query execution.
