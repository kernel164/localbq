# BigQuery Emulator Research Report (Claude)

Date: April 11, 2026

## Executive summary

There is no official Google BigQuery emulator. The only credible open-source project is `goccy/bigquery-emulator` — 1,077 stars, 169 forks, **185 open issues**, one maintainer working weekends. The community has been asking to replace its SQLite backend with DuckDB since February 2024 (issue #274) and the maintainer agrees.

Demand exists. The incumbent is maintenance-bottlenecked, not capability-bottlenecked. But **the pain in goccy is ~80% API-surface bugs unrelated to the storage engine** — swapping to DuckDB fixes native `STRUCT/ARRAY/GEOGRAPHY` and analytics perf but leaves `INFORMATION_SCHEMA`, MERGE, partitioning DDL, Storage Write API, and dbt auth bypass untouched. The DuckDB swap alone is not the moat.

The sobering data point for any "BigQuery emulator with DuckDB backend" pitch: **someone already shipped this for Snowflake three months ago.** `nnnkkk7/snowflake-emulator` (Go + DuckDB, transpiles Snowflake SQL) launched Dec 2025 and its Show HN got **6 points and 4 comments**. 33 stars total. The framing "stop burning cloud credits with a local emulator" does not self-sell.

The real unsolved job is **dbt-bigquery integration tests that run in under 10 seconds without a service account**. That's the wedge. The stealth competitor is SQLMesh + dbt-duckdb + SQLGlot, which already covers dbt unit tests for free via transpilation.

## Top three findings

1. **goccy/bigquery-emulator is one person's weekend project holding up ~1k downstream users.** README explicitly asks for sponsors. Last commit 2026-02-05. 185 open issues including `INFORMATION_SCHEMA` missing since 2022 (#48), `CREATE TABLE ... PARTITION BY` rejected (#152), MERGE statements broken (#128/163/299), Storage Write API unreliable (#246/342/352/364/398), `DECLARE` scripting unsupported (#344), TIMESTAMP returned as float (#390). ARM64 Docker is the highest-reacted issue at 24 reactions (#332).
2. **`nnnkkk7/snowflake-emulator` is the near-identical Snowflake version already shipped — and its GTM flopped.** 33 stars, last push 2026-01-19, Show HN = 6 points. Go + DuckDB, translates IFF→IF, NVL→COALESCE, DATEADD→INTERVAL, LISTAGG→STRING_AGG, FLATTEN→UNNEST. Explicitly does not support auth, clustering, Time Travel, zero-copy clone, streams/tasks/pipes.
3. **The "replace SQLite with DuckDB" ask is literally open as issue #274 on goccy's repo since February 2024.** The maintainer himself agrees: "DuckDB can be an excellent alternative... columnar, designed for analytics. GEOGRAPHY, STRUCT and ARRAY supported out of the box." Your hypothesis is consensus, not contrarian — which means the differentiation has to come from somewhere other than the backend swap.

## 1. The existing BigQuery emulator landscape

### Google's own emulator: doesn't exist
Google ships emulators for Bigtable, Firestore, Pub/Sub, Spanner, and Datastore — but not BigQuery. Internal Issue Tracker 129248927 has been open since 2019 with no ETA. BigQuery Sandbox (the hosted free tier) is not local, expires tables after 60 days, and blocks DML — useless for CI and for dbt's incremental model path.

### goccy/bigquery-emulator
The only serious player.

- **Stars/activity**: 1,077 stars, 169 forks, **185 open issues**, 14 watchers, last commit 2026-02-05. Commit cadence is sporadic: Feb 2026 → Jan 2026 → Oct 2025 → Dec 2024 → Sep 2024. Not dead, but throttled.
- **Architecture**: REST/gRPC server → `go-zetasqlite` (goccy's wrapper) → Google's real **ZetaSQL** parser/analyzer (C++, bound via CGO) → SQLite. Types BQ has but SQLite doesn't (ARRAY, STRUCT, TIMESTAMP) are encoded with type-tags and decoded via custom SQLite functions.
- **What works**: Jobs, Datasets, Tables CRUD, Query, Tabledata, Storage Read API (Avro+Arrow), 200+ functions, wildcard tables, JavaScript UDFs.
- **What's partial/broken**: Storage Write API (multiple bugs), MERGE statements, nested RECORD queries, JSON functions, TIMESTAMP serialization, Node.js and Java clients.
- **What's missing entirely**: `INFORMATION_SCHEMA`, `CREATE TABLE ... PARTITION BY`, materialized views, Model API, Connection API, row-level security, authorized views, time travel, BI Engine, sessions, scripting (`DECLARE`), stored procedures, IAM, `EXPORT DATA`, GEOGRAPHY (stub because SQLite).
- **Maintainer message**: the README explicitly says "this project is a personal project and I develop it on my days off and after work... if you want me to implement the features you want, please consider sponsoring me."

### nnnkkk7/snowflake-emulator — the most important prior art for this decision
- **Why it matters**: it is the single closest analog to "DuckDB-backed local emulator for a cloud DW" that has shipped and had its GTM tested.
- **Tech**: Go + DuckDB. Compatible with `gosnowflake` driver + REST API v2. Auto-translates Snowflake dialect to DuckDB (IFF→IF, NVL→COALESCE, DATEADD→INTERVAL, TO_VARIANT→CAST JSON, LISTAGG→STRING_AGG, FLATTEN→UNNEST). Supports DDL, DML, MERGE, COPY INTO, transactions.
- **What it excludes**: auth, clustering, Time Travel, Zero-Copy Clone, Streams/Tasks/Pipes, external stages, JS stored procs, UDFs.
- **Traction**: 33 stars, 3 open issues, last push 2026-01-19, created 2025-12-29. Show HN got **6 points / 4 comments**. The blog post that accompanied it ("Stop burning Snowflake credits...") got near-zero pickup on dev.to.

### Forks and wrappers of goccy
`kromiii/bigquery-emulator` (maintenance fork), `aerben/bigquery-emulator` (minor fixes), `filipecaixeta/bigquery-emulator-ui` (web UI), `bqemulatormanager` on PyPI (Python test harness). None are independent implementations.

### No commercial BigQuery emulator exists
LocalStack does not offer BigQuery. Datafold does data-diff, not emulation. MotherDuck positions itself as a BQ alternative to migrate to, not a local test harness.

## 2. Emulators for other data warehouses

| Warehouse | State |
|---|---|
| Redshift | LocalStack Pro emulates the SQL engine (paid). Community edition mocks only the control plane. No credible open-source standalone. Teams default to raw Postgres + hope. |
| Snowflake | Two things: **LocalStack for Snowflake** (GA 2025, paid, separate product — supports DDL/DML/Snowpark/warehouses/MVs/Row Access Policies/Polaris Iceberg as of 2026.03); **nnnkkk7/snowflake-emulator** (Go+DuckDB, 33 stars, see above). |
| Databricks | No runtime emulator. Databricks Connect runs pyspark + Delta locally but doesn't emulate clusters, Unity Catalog, Photon, or SQL Warehouses. |
| Azure Synapse | Nothing found. |
| ClickHouse | Not applicable — the same binary runs locally and in prod. This is the "gold standard" everyone else is trying to simulate. |

**Why the DW emulator landscape is thin**: ZetaSQL and Snowflake SQL are dialects with thousands of functions and proprietary semantics. Matching them is unbounded. Vendors have anti-incentive — emulators reduce bill-time. And DuckDB's rise has given people a good-enough alternative that sidesteps the whole dialect problem via transpilation.

## 3. What people actually want from a DW emulator

From the goccy issue tracker, dbt-bigquery #358, and HN/blog commentary, ranked by signal:

1. **dbt compatibility end-to-end.** goccy #429 + dbt-bigquery #358 together are the #1 blocker. `dbt-bigquery` rejects `AnonymousCredentials`, so teams cannot point dbt at the emulator without patching the adapter. dbt-bigquery #358 was closed without the feature being built.
2. **MERGE + incremental models** (#128, #163, #299). Most production BQ tables are incremental → rely on MERGE → broken in goccy.
3. **`INFORMATION_SCHEMA` exists at all** (#48, open since 2022). dbt uses it for source freshness, docs, and relation resolution. Without it, dbt breaks at parse time.
4. **`PARTITION BY` and `CLUSTER BY` DDL** (#152, #317). Every production BQ table uses these.
5. **ZetaSQL doesn't look like it's hanging** (#58). 20+ minute silent CGO builds, missing C++ headers, linux/amd64-only historically. The "it hangs" perception kills adoption.
6. **ARM64 Docker** (#332, 24 reactions — highest on the repo). Apple Silicon is now the default dev laptop.
7. **Storage Write API reliability** (#246, #342, #352, #364, #398). Every streaming Python/Java pipeline is affected.
8. **Determinism / run-twice-without-errors** (#237).
9. **TIMESTAMP serialization bugs** (#390).
10. **Performance on large projects** (#294) and **memory release** (#313).
11. **JSON functions correctness** (#286).
12. **Scripting / stored procedures** (#344).

What's **not** in the top complaints: BigQuery cloud cost. See §5.

## 4. Gaps and where the opportunity is

The actual gap is not SQL coverage — it's developer workflow:

- No canonical, well-maintained local BigQuery runtime with ARM64-first distribution.
- No dbt-bigquery integration that actually runs against a local target out of the box.
- No reliable Storage Write API for streaming ingestion testing.
- No credible `INFORMATION_SCHEMA` surface.
- No transparent compatibility matrix — users either trust blind or distrust everything.
- No "dbt integration tests in under 10 seconds" story anywhere in the ecosystem.

**The opportunity is not "another goccy with DuckDB."** The opportunity is **"the local dbt-BigQuery test harness that makes CI feedback loops go from minutes to seconds."** That narrower positioning is defensible and aligns with where dbt Labs is already pushing users (dbt 1.8 unit tests, May 2024).

## 5. Is there a cost-saving angle?

Weak. Honest answer.

- I could not find **a single public number** on what a dev team spends on BigQuery for dev/CI. All the $-focused blog posts (Shopify's $1M query, Bilt's $20k/mo, MixPanel's 80% cut) are production optimization stories, not dev-loop stories.
- dbt's own advice for dev cost control is the `--empty` flag and unit tests on mock data — which sidesteps the emulator entirely.
- The real pain per the dbt Discourse and GitHub issues is **CI feedback loop time** (minutes vs seconds) and **offline dev** (on a plane, on a flaky network), not the BQ bill.

**Pitching a new emulator as "stop burning cloud credits" is exactly the framing nnnkkk7 used for the Snowflake version, and it got 6 HN points.** Lead with feedback-loop speed and determinism, not cost.

## 6. What a new DuckDB-backed BigQuery emulator should look like

### The easy part: the storage engine
DuckDB is a genuine upgrade over SQLite for this job:
- Native `STRUCT`, `LIST`, `MAP`, `JSON` — no more type-tag encoding hacks
- Columnar, vectorized, designed for analytics → real perf on real tables
- Spatial extension → real GEOGRAPHY (WKT/WKB)
- Parquet/Arrow round-trip — matches BQ Storage API data format naturally
- MVCC transactions for `BEGIN/COMMIT`

### The hard parts (all unrelated to storage)

1. **SQL parsing and planning.** Two bad options:
   - Keep ZetaSQL via CGO (goccy's approach): inherit Google's real parser with correct semantics, but pay 20-minute builds, header-hunt hell, and platform friction.
   - Transpile via SQLGlot: cleaner build, but lossy — known issues with `SELECT AS STRUCT ARRAY(...)`, `ARRAY_AGG(DISTINCT struct)`, UDFs don't transpile at all, UUID type mismatch.
2. **`INFORMATION_SCHEMA`**: must be synthesized from your own metadata catalog, not DuckDB's. Separate subproject.
3. **Partitioning/clustering metadata**: DuckDB persists Hive-partitioned Parquet but doesn't track per-table partition metadata. `_PARTITIONTIME`, `_PARTITIONDATE`, partition pruning, `INFORMATION_SCHEMA.PARTITIONS` — all require a metadata layer on top.
4. **Storage Write API**: `_default` stream, PENDING/COMMITTED semantics, proto wrappers. This is where goccy's real pain lives.
5. **Jobs + async semantics**: BQ jobs are async with states; DuckDB queries are sync. Need a scheduler.
6. **Auth bypass that dbt-bigquery accepts**: either patch the adapter upstream or ship a custom credentials class.

### Minimum credible v0.1 scope

1. Single Go binary; ARM64 + x86_64 Docker day one
2. DuckDB backend with STRUCT/ARRAY/GEOGRAPHY first-class
3. Jobs/Datasets/Tables CRUD over REST — goccy-compatible API shape
4. `INFORMATION_SCHEMA.TABLES`, `.COLUMNS`, `.PARTITIONS`, `.JOBS` (enough for dbt)
5. `CREATE TABLE ... PARTITION BY DATE(col)` — even if partitioning is simulated
6. MERGE statement working against partitioned targets (the dbt incremental path)
7. A `dbt-bigquery-local` adapter shim that solves auth bypass
8. Headline benchmark: "run project X's dbt test suite in N seconds"

### Explicitly out of v0.1
Model API, BI Engine, real GEOGRAPHY, row-level security, authorized views, time travel, sessions, scripting, stored procedures, Storage Write API `_default` stream, materialized view refresh.

## 7. The stealth competitor nobody talks about

**SQLMesh + dbt-duckdb + SQLGlot** already covers dbt unit tests against in-memory DuckDB by transpiling BigQuery SQL. That's free, OSS, and working today for a real slice of users. MotherDuck actively promotes this pattern.

Any new emulator must be honest about what it does that transpilation fundamentally cannot: integration tests, `INFORMATION_SCHEMA`, realistic MERGE/partitioning behavior, Storage Write API, and the "same code path as prod" guarantee for pipelines that don't go through dbt at all.

## 8. Honest summary

- Demand exists (1k stars on a weekend project, 185 open issues).
- The incumbent is maintenance-bottlenecked, not capability-bottlenecked.
- The DuckDB swap alone does not move the needle — 80% of goccy's open issues are API-surface bugs.
- The dbt adapter story is the wedge, not the emulator itself. Ship both or don't ship.
- Cost is the wrong pitch. Lead with feedback-loop speed.
- The GTM is brutal. nnnkkk7's near-identical Snowflake project got 6 HN points.
- SQLMesh + dbt-duckdb + SQLGlot is already eating the unit-test slice of this market for free. Integration tests and full API fidelity are the defensible territory.

## Sources

- https://github.com/goccy/bigquery-emulator
- https://github.com/goccy/bigquery-emulator/issues/274 (Replace SQLite with DuckDB)
- https://github.com/goccy/bigquery-emulator/issues/48 (INFORMATION_SCHEMA)
- https://github.com/goccy/bigquery-emulator/issues/152 (PARTITION BY)
- https://github.com/goccy/bigquery-emulator/issues/332 (ARM64 Docker)
- https://github.com/goccy/bigquery-emulator/issues/429 (dbt integration)
- https://github.com/goccy/bigquery-emulator/issues/128 (MERGE broken)
- https://github.com/goccy/bigquery-emulator/issues/246 (Storage Write API)
- https://github.com/goccy/go-zetasql
- https://github.com/nnnkkk7/snowflake-emulator
- https://news.ycombinator.com/item?id=46471246 (Show HN snowflake-emulator, 6 points)
- https://dev.to/kurorr/stop-burning-snowflake-credits-build-a-local-emulator-with-go-and-duckdb-44lk
- https://github.com/dbt-labs/dbt-bigquery/issues/358 (emulator support, closed)
- https://docs.localstack.cloud/snowflake/
- https://blog.localstack.cloud/localstack-for-snowflake-1-0-release/
- https://cloud.google.com/bigquery/docs/sandbox
- https://issuetracker.google.com/issues/129248927
- https://duckdb.org/community_extensions/extensions/bigquery
- https://github.com/hafenkran/duckdb-bigquery
- https://deepwiki.com/tobymao/sqlglot/4.1-bigquery-and-duckdb-dialects
- https://github.com/tobymao/sqlglot/issues/1788
- https://sqlmesh.readthedocs.io/en/latest/integrations/engines/duckdb/
- https://github.com/duckdb/dbt-duckdb
- https://motherduck.com/learn-more/bigquery-alternative-motherduck/
- https://docs.getdbt.com/docs/build/unit-tests
- https://www.datafold.com/dbt
