# LocalBQ

**Part of [LocalGCP](https://localgcp.com)** — a local BigQuery emulator powered by DuckDB.

Run BigQuery workloads on your laptop — same API, same SQL, zero cloud cost. Works with `bq` CLI, Python SDK, Go SDK, and any BigQuery REST API client.

> Full documentation at [localgcp.com/docs/bigquery](https://localgcp.com/docs/bigquery)

## Install

### With LocalGCP (recommended)

```bash
brew install slokam-ai/tap/localgcp
localgcp up --services=bigquery
```

### Docker (standalone)

```bash
docker run -p 9060:9060 ghcr.io/slokam-ai/localbq:latest
```

### From source (standalone)

```bash
go install github.com/slokam-ai/localbq/cmd/localbq@latest
```

## Quick Start

```bash
# Start
localbq up        # foreground
localbq up -d     # background (daemon mode)
localbq status    # check if running
localbq stop      # stop background instance

# Query with bq CLI — one env var, no other changes
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

## Connect Your Client

### bq CLI

```bash
export CLOUDSDK_API_ENDPOINT_OVERRIDES_BIGQUERY=http://localhost:9060/

bq --project_id=my-project query --use_legacy_sql=false 'SELECT 1 + 1 AS result'
bq --project_id=my-project mk my_dataset
bq --project_id=my-project ls
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

## Load Data

```bash
localbq load mydata.users users.parquet    # Parquet
localbq load mydata.events events.csv      # CSV
localbq load mydata.logs logs.jsonl        # JSON/NDJSON
```

If the table doesn't exist, it's created from the file schema. If it exists, data is appended.

Via SQL from any connected client:

```sql
LOAD DATA INTO mydata.users FROM '/path/to/users.parquet'
```

## Data Directory

DuckDB database lives at `~/.localbq/localbq.duckdb` by default. Override with `--data-dir`:

```bash
localbq up --data-dir /tmp/my-project-data
localbq load --data-dir /tmp/my-project-data mydata.users users.parquet
```

Data persists across restarts. Each `--data-dir` is an independent environment.

## Switching to Production

```bash
# Local development
export CLOUDSDK_API_ENDPOINT_OVERRIDES_BIGQUERY=http://localhost:9060/
bq query --use_legacy_sql=false 'SELECT 1'  # → hits LocalBQ

# Switch to real BigQuery
unset CLOUDSDK_API_ENDPOINT_OVERRIDES_BIGQUERY
bq query --use_legacy_sql=false 'SELECT 1'  # → hits Google Cloud
```

For Python/Go, use an env var check:

```python
import os
endpoint = os.getenv("LOCALBQ_ENDPOINT")
if endpoint:
    client = bigquery.Client(credentials=AnonymousCredentials(),
                             client_options={"api_endpoint": endpoint})
else:
    client = bigquery.Client()  # normal GCP auth
```

## Known Limitations

These GoogleSQL features are not yet supported:

- `MERGE` with `WHEN NOT MATCHED BY SOURCE`
- `QUALIFY`, `PIVOT` / `UNPIVOT`
- `SELECT AS STRUCT`, `SELECT AS VALUE`
- Complex `UNNEST` with explicit joins
- Scripting (`DECLARE`, stored procedures, JS UDFs)
- `GEOGRAPHY`, `BIGNUMERIC` beyond 38 digits
- Storage Read/Write API (gRPC)
- BigQuery ML / BI Engine

Standard SQL that DuckDB supports natively (joins, CTEs, window functions, aggregations, subqueries) works without any lowering.

## Development

```bash
go build ./cmd/localbq/
go test ./...
./localbq up --port 9060
```

## License

MIT — uses [DuckDB](https://github.com/duckdb/duckdb) (MIT) for query execution.
