# BigQuery Emulator Research Report

Date: April 11, 2026

## Executive Summary

There is no official Google BigQuery emulator today. Google's documented `gcloud emulators` surface includes products such as Bigtable, Datastore, Firestore, Pub/Sub, and Spanner, but not BigQuery. In practice, the BigQuery emulator market is thin: one serious broad open-source emulator exists today, and a newer DuckDB-backed project is emerging. By contrast, Snowflake now has a meaningful local-emulation story, while Redshift still mostly pushes users toward live cloud development or API mocking rather than a faithful local warehouse clone.

The market opportunity is real, but it is not "people want a toy SQL parser." What teams actually want is a fast, deterministic, local-compatible substitute for the expensive and slow parts of the warehouse developer loop:

- run dbt and SQL transformations locally
- run CI without hitting cloud billing or quotas
- seed reproducible data fixtures
- exercise client SDKs and REST/job flows without changing application code
- catch most compatibility issues before cloud integration tests
- work offline or in isolated environments

The strongest opportunity is a **BigQuery-compatible local development runtime**, not a perfect full-service clone. If you build one on DuckDB, the differentiator will not be DuckDB itself. The differentiator will be the compatibility layer around GoogleSQL, BigQuery metadata, job semantics, API shape, and developer workflow.

## What People Look For In A Warehouse Emulator

Across the BigQuery, Snowflake, and Redshift-adjacent tools, the recurring demand pattern is consistent:

1. **Compatibility with existing clients**
   People want to point the official SDK, `bq`, dbt adapter, SQLAlchemy driver, BI tool, or orchestration tool at a local endpoint with minimal code changes.

2. **Fast feedback loops**
   Startup should be container-friendly, local, and cheap enough to run on every branch, every PR, and every laptop.

3. **Deterministic seeded data**
   Teams want YAML/SQL/CSV/Parquet fixture loading, repeatable test databases, and clean resets between test runs.

4. **Enough SQL parity to trust results**
   Exact parity is not required for every feature, but the emulator must be good enough that developers trust query results for the core analytic workload.

5. **Metadata and job semantics**
   SQL execution alone is not enough. Real users also need datasets, tables, jobs, query results polling, routines, external tables, and catalog behavior.

6. **Isolation from cloud failure modes**
   Developers want freedom from IAM setup, quotas, flaky network calls, cloud account sprawl, and accidental writes into shared environments.

7. **Tooling integration**
   The more it works with dbt, Airflow, Terraform, Testcontainers, CLI tools, IDEs, and notebooks, the more valuable it becomes.

This is why the winning products are usually not "database cores"; they are **developer workflow products**.

## Current Landscape

### BigQuery

#### Official Google position

Google does not currently offer a documented BigQuery emulator. The `gcloud beta emulators` docs list `bigtable`, `datastore`, `firestore`, `pubsub`, and `spanner`, but not BigQuery.

Implication: the market is still open enough that community tools define the category.

#### Existing BigQuery emulators and local substitutes

##### 1. `goccy/bigquery-emulator`

This is the most credible broad BigQuery emulator currently available.

What it does:

- local BigQuery server for testing and development
- works with the Go SDK, non-Go programs, and the `bq` command
- supports REST BigQuery APIs except IAM resource manipulation
- supports the BigQuery Storage API over gRPC with Avro and Arrow
- uses SQLite as the storage engine
- supports in-memory and file-backed persistence
- loads startup seeds from YAML
- supports a large slice of GoogleSQL through `go-zetasqlite`
- supports JavaScript UDFs, wildcard tables, and templated argument functions
- supports GCS-linked load/extract flows, though extract support is currently CSV and JSON only

What is important strategically:

- it aims to behave like BigQuery "from the BigQuery client's perspective"
- the maintainer explicitly lists missing parity work such as Model API, Connection API, and `INFORMATION_SCHEMA`
- it is still described as beta

Assessment:

- strongest current competitor on BigQuery API breadth
- strongest evidence that the real value is API plus metadata compatibility, not only SQL execution
- weakest point is likely long-tail parity and maintainer bandwidth rather than core concept

##### 2. `novucs/local-bigquery`

This is a newer local BigQuery implementation written in Python using SQLGlot for translation and DuckDB for execution.

What it does:

- runs as a local BigQuery-style server
- works with BigQuery clients over HTTP
- supports Docker usage
- supports Testcontainers-style workflows
- uses DuckDB as the execution core
- explicitly positions itself as a local BigQuery implementation

What it appears to support today:

- query and jobs flows
- datasets and tables basics
- `insertAll`
- local development with standard clients

What is clearly missing in the codebase:

- model APIs
- routine APIs
- row access policy APIs
- table get/patch/update APIs
- `tabledata.list`
- IAM policy and permission APIs

Assessment:

- strong proof that DuckDB is a viable backend for a BigQuery-like local runtime
- still much more of a query and metadata shim than a broad emulator
- useful evidence that a DuckDB-backed design is feasible, but the open space is still large

### Snowflake

Snowflake now has a two-part local story:

1. **first-party local testing** for Snowpark Python
2. **third-party broader emulation** via LocalStack

#### Snowflake first-party local testing

Snowflake documents a Snowpark Python local testing framework and explicitly calls it an emulator. It allows DataFrame operations locally without connecting to a Snowflake account.

Important limitation:

- `Session.sql(...)` is not supported
- some built-in functions are not supported
- Snowflake documents known limitations and behavior gaps, and states there are limitations it does not plan to address soon

Assessment:

- useful for unit-level DataFrame logic
- not a general Snowflake SQL or warehouse emulator
- good evidence that even Snowflake's own local strategy narrows scope to the highest-value developer loop

#### Snowflake via LocalStack

LocalStack now offers a Snowflake emulator with explicit documentation for:

- dbt
- Airflow
- DBeaver
- SnowSQL
- Terraform
- CI

Its own docs market this as local data development and testing, and its feature coverage matrix shows that some object families remain incomplete. Example gaps documented by LocalStack include:

- `EXTERNAL TABLE` operations marked unsupported
- `UNDROP` missing for several object families
- some catalog/integration surfaces only partially implemented

Assessment:

- strongest evidence that the category becomes compelling when it plugs into the real toolchain
- also strong evidence that feature coverage transparency matters more than promising perfect parity

### Redshift

I found no AWS first-party Redshift emulator.

AWS documentation still centers local development around:

- Redshift Query Editor v2
- sample data loading
- local-file upload into S3 followed by `COPY`
- live serverless or cluster-backed development

LocalStack documents Redshift support, but frames it as API-level support for mocking and testing rather than a full Redshift warehouse clone.

Assessment:

- Redshift still lacks a convincing local warehouse emulator story
- in practice, users either test against real Redshift, use small serverless environments, or accept partial substitutes
- this reinforces the opportunity for warehouses where the inner loop is still too cloud-dependent

## What Existing BigQuery Emulators Actually Do

Today, "BigQuery emulator" means one of two things:

### Category A: Broad client-facing emulation

`goccy/bigquery-emulator` is in this category.

Characteristics:

- exposes BigQuery-compatible APIs
- accepts official clients
- persists state
- models jobs and datasets
- handles enough GoogleSQL to be practically useful
- tries to feel like BigQuery, not just "SQL that looks kind of similar"

### Category B: Query server plus compatibility shim

`local-bigquery` is in this category.

Characteristics:

- translates BigQuery-ish SQL into a local execution engine
- exposes enough of the HTTP surface to make clients work
- is useful for integration tests and local development
- does not yet cover the deeper control-plane surface of BigQuery

## Gaps In The Current BigQuery Emulator Market

The gaps are fairly clear.

### 1. No official emulator

This means the ecosystem has no canonical reference runtime for local development.

### 2. SQL support exists, but full warehouse behavior does not

The hard part is not just parsing GoogleSQL. The hard part is:

- jobs lifecycle
- `INFORMATION_SCHEMA`
- model and connection APIs
- routines and UDFs lifecycle
- IAM-adjacent behavior
- external table behavior
- error shape and error codes
- client compatibility across languages
- metadata consistency

### 3. Weak emphasis on toolchain-first workflows

The most valuable use cases are:

- dbt local runs and tests
- application integration tests
- CI suites
- seeded data replay
- connector and SDK tests

The BigQuery market still lacks a clearly dominant product built around those workflows.

### 4. Missing explicit compatibility contracts

Users can tolerate incomplete support if the product is transparent. They cannot tolerate silent drift.

Opportunity:

- publish compatibility matrices
- provide "known differences from BigQuery"
- offer strict mode vs permissive mode
- ship conformance tests and cloud-vs-local diff tests

### 5. Performance is underused as a product wedge

DuckDB-class local execution can be dramatically faster than spinning up real warehouse-dependent dev loops for small and medium data slices. That speed itself is part of the product.

## Where The Opportunity Is

The best opportunity is **not** "full BigQuery replacement on a laptop."

The best opportunity is:

### A. Inner-loop BigQuery development runtime

Target users:

- analytics engineers using dbt
- backend teams with BigQuery-backed services
- data platform teams building connectors, ingestion services, or governance tooling
- CI systems that repeatedly run warehouse-dependent tests

Value proposition:

- run most development and CI locally
- reserve real BigQuery for compatibility gates and staging

### B. Contract-compatible local control plane

The real moat is not SQL execution alone. It is:

- HTTP API compatibility
- job metadata compatibility
- catalog compatibility
- reproducible BigQuery-like errors
- compatibility modes for clients and frameworks

### C. Toolchain-centric packaging

The product should win through:

- Docker image
- Testcontainers module
- dbt adapter guidance
- official examples for Go, Python, Java, Node, and `bq`
- CI recipes
- fixture loading and reset commands

### D. Compatibility transparency

A product that says "we support 80% of the common developer path and tell you exactly where the other 20% diverges" is much more credible than one that claims full parity and surprises users.

## Is There A Cost-Saving Angle?

Yes, but it needs to be framed correctly.

### The weak cost-saving thesis

"BigQuery is expensive, so a local emulator will save query spend."

This is only partly true.

Google already gives BigQuery a substantial free tier and sandbox path:

- BigQuery free tier includes the first 10 GiB of storage and the first 1 TiB of query data processed per month
- the BigQuery sandbox allows limited BigQuery use without a billing account
- dry runs are free and can estimate bytes processed before execution

For solo developers or tiny projects, raw cloud cost savings alone may not be enough to justify a new emulator.

### The strong cost-saving thesis

The stronger economic case is **team workflow cost**, not just warehouse spend:

1. **CI cost reduction**
   Repeated test queries, model builds, and connector tests can push teams beyond the free tier quickly.

2. **Fewer staging environments**
   Teams often keep shared warehouse environments alive just to support developer and PR testing.

3. **Lower failure cost**
   Local runs avoid quota failures, IAM misconfiguration, shared-dataset collisions, and accidental writes.

4. **Faster iteration**
   A developer loop that is 10x faster is often worth more than the direct query-cost savings.

5. **Safer experimentation**
   Engineers can test DDL, DML, and destructive transformations without touching real data.

### Practical conclusion

The cost angle is real, but the best positioning is:

> "Cut the cost and latency of the warehouse inner loop, not eliminate BigQuery."

That positioning is stronger and more defensible.

## How A Brand-New BigQuery Emulator Should Look

The right design is a **BigQuery-compatible local runtime** with four layers.

### 1. Compatibility front door

This layer should expose:

- BigQuery REST endpoints needed by common SDK and CLI workflows
- query/job submission APIs
- result polling APIs
- dataset/table metadata APIs
- selected Storage API behavior
- discovery endpoints where practical

Design goal:

- official clients should connect with only endpoint override and auth bypass

### 2. GoogleSQL compatibility layer

This is the heart of correctness.

Responsibilities:

- parse GoogleSQL
- analyze types and name resolution
- normalize BigQuery semantics
- rewrite supported queries to backend-native SQL
- reject unsupported constructs explicitly with BigQuery-like errors

You should not rely on raw DuckDB SQL compatibility alone. BigQuery and DuckDB overlap meaningfully, but they are not the same language or the same semantics.

A strong design would use:

- ZetaSQL or a GoogleSQL-compatible analyzer for parsing and semantic analysis
- an intermediate representation
- a lowering pass into DuckDB SQL

This is closer to the `goccy` approach philosophically, even if the backend changes from SQLite to DuckDB.

### 3. Execution engine

DuckDB is a strong choice here.

Why DuckDB fits:

- embedded, fast, zero-admin execution engine
- strong support for analytical SQL
- native nested/composite types including `LIST`, `STRUCT`, `MAP`, and `UNION`
- JSON support
- spatial extension
- `httpfs` extension for HTTP(S) and S3-style object storage access
- good local Parquet and CSV ergonomics

Why DuckDB alone is not enough:

- BigQuery `ARRAY` is variable-length, while DuckDB distinguishes fixed-length `ARRAY` and variable-length `LIST`; mapping must be intentional
- DuckDB `STRUCT` requires a consistent key layout per column, which is usually fine, but semantics still need normalization
- BigQuery `GEOGRAPHY` is defined on the WGS84 spheroid with geodesic edges, while DuckDB spatial centers on `GEOMETRY`; geospatial behavior will drift unless carefully adapted
- BigQuery JSON semantics and GoogleSQL function behavior differ from DuckDB JSON functions
- BigQuery scripting, job metadata, external connections, model APIs, and IAM behavior are outside DuckDB's scope

Conclusion:

- DuckDB is an excellent execution core
- DuckDB is not the product by itself

### 4. Metadata and control plane

This layer must track:

- projects
- datasets
- tables
- views
- routines
- jobs
- result handles
- load and extract operations
- compatibility flags
- seeded fixtures

Implementation options:

- persist metadata in SQLite or DuckDB catalog tables
- persist table data directly in DuckDB
- store job history and API objects as JSON records

## Recommended Product Shape For A DuckDB-Backed BigQuery Emulator

### Product principle

Optimize for **developer trust**, not maximum surface area on day one.

### MVP scope

An MVP should focus on the common inner loop:

- dataset and table CRUD
- query jobs
- job polling and result fetch
- views
- a practical GoogleSQL subset
- nested types that matter in analytics work
- load data from CSV, JSON, and Parquet
- local file and S3-compatible object store support
- deterministic seed loading
- Docker and Testcontainers support
- official client compatibility for Go and Python first

### Explicitly defer from MVP

- full IAM semantics
- BigQuery ML
- reservations and capacity APIs
- every `INFORMATION_SCHEMA` table
- every DCL and governance feature
- exact geospatial parity
- every scripting construct

### Compatibility modes

Ship with three modes:

1. **Strict mode**
   Reject unsupported constructs aggressively.

2. **Permissive mode**
   Translate when safe, warn on known semantic drift.

3. **Contract-test mode**
   Run the same query locally and in real BigQuery for sampled validation during development or CI.

This is a major product differentiator.

### Golden-path integrations

First-class support should exist for:

- `bq`
- `google-cloud-bigquery` for Python
- Go BigQuery client
- dbt via endpoint override guidance or dedicated adapter strategy
- SQLAlchemy BigQuery workflows where possible
- Docker Compose and Testcontainers

### Fixture system

A good emulator should treat fixture management as a first-class feature:

- YAML/SQL manifest describing projects, datasets, tables, schemas, and seed files
- reset command
- snapshot/restore command
- deterministic timestamps and job IDs in test mode

### Error model

A large share of trust comes from realistic errors:

- BigQuery-like HTTP status codes
- BigQuery-like error reasons and payload shape
- clear unsupported-feature errors

## Suggested Architecture

### Request flow

1. Client submits REST query/job request.
2. API layer validates payload and creates a job record.
3. GoogleSQL analyzer parses and type-checks the query.
4. Rewriter lowers supported constructs into DuckDB SQL.
5. Execution layer runs query in DuckDB.
6. Result mapper converts DuckDB values back into BigQuery-like row/value structures.
7. Job/result metadata is persisted and exposed through polling endpoints.

### Storage model

- DuckDB stores actual relational data
- control-plane metadata lives in dedicated internal schemas
- object-store abstraction targets local filesystem and S3-compatible endpoints
- seed loader converts fixture manifests into datasets, tables, and load jobs

### Extension points

- pluggable object-store connector
- pluggable auth shim for test environments
- pluggable conformance harness against real BigQuery
- optional Storage API emulation

## Key Technical Risks

### 1. GoogleSQL semantic drift

This is the main risk. If you skip a real compatibility layer and rely on ad hoc SQL translation, user trust will break quickly.

### 2. Geospatial mismatch

BigQuery `GEOGRAPHY` and DuckDB `GEOMETRY` are not the same abstraction.

### 3. Long-tail function behavior

Analytic SQL engines often agree on syntax and disagree on edge behavior. Nulls, coercions, JSON behavior, and timestamp handling all matter.

### 4. Metadata surface creep

Once users adopt the tool, they quickly ask for more of the warehouse control plane, not just more SQL.

### 5. Client compatibility over time

Official SDKs and dbt adapters evolve. The emulator must track those expectations.

## Strategic Recommendation

If you build this, do not market it as a "BigQuery clone."

Market it as:

> A local BigQuery-compatible development runtime for dbt, CI, and application integration tests.

That framing is more credible, more useful, and more aligned with where current competitors are weakest.

The most promising wedge is:

- DuckDB for fast local execution
- a serious GoogleSQL compatibility layer
- BigQuery-compatible REST/jobs surface
- best-in-class fixture and CI ergonomics
- transparent compatibility matrices

If executed well, that is enough to become the default inner-loop tool for teams that build on BigQuery.

## Source Notes

Primary sources used in this report:

- Google Cloud SDK emulator docs: <https://docs.cloud.google.com/sdk/gcloud/reference/beta/emulators>
- BigQuery pricing: <https://cloud.google.com/bigquery/pricing.html>
- BigQuery sandbox: <https://docs.cloud.google.com/bigquery/docs/sandbox>
- BigQuery dry-run cost estimation: <https://cloud.google.com/bigquery/docs/best-practices-costs#estimate-query-costs>
- BigQuery data types: <https://cloud.google.com/bigquery/docs/reference/standard-sql/data-types>
- BigQuery geospatial docs: <https://cloud.google.com/bigquery/docs/geospatial-data>
- BigQuery JSON docs: <https://cloud.google.com/bigquery/docs/json-data>
- `goccy/bigquery-emulator` README: <https://github.com/goccy/bigquery-emulator>
- `goccy/go-zetasqlite` README/status: <https://github.com/goccy/go-zetasqlite>
- `novucs/local-bigquery` README: <https://github.com/novucs/local-bigquery>
- Snowpark Python local testing framework: <https://docs.snowflake.com/en/developer-guide/snowpark/python/testing-locally>
- LocalStack for Snowflake overview: <https://docs.localstack.cloud/snowflake/>
- LocalStack Snowflake integrations overview: <https://docs.localstack.cloud/snowflake/integrations/>
- LocalStack Snowflake dbt docs: <https://docs.localstack.cloud/snowflake/integrations/dbt/>
- LocalStack Snowflake Airflow docs: <https://docs.localstack.cloud/snowflake/integrations/airflow/>
- LocalStack Snowflake DBeaver docs: <https://docs.localstack.cloud/snowflake/integrations/dbeaver/>
- LocalStack Snowflake SnowSQL docs: <https://docs.localstack.cloud/snowflake/integrations/snow-sql/>
- LocalStack Snowflake Terraform docs: <https://docs.localstack.cloud/snowflake/integrations/terraform/>
- LocalStack Snowflake feature coverage: <https://docs.localstack.cloud/snowflake/feature-coverage/>
- Amazon Redshift query editor v2: <https://docs.aws.amazon.com/redshift/latest/mgmt/query-editor-v2.html>
- Amazon Redshift loading docs: <https://docs.aws.amazon.com/redshift/latest/mgmt/query-editor-v2-loading.html>
- Amazon Redshift local file loading workflow: <https://docs.aws.amazon.com/redshift/latest/mgmt/query-editor-v2-loading-data-local.html>
- Amazon Redshift and PostgreSQL docs: <https://docs.aws.amazon.com/redshift/latest/dg/c_redshift-postgres-jdbc.html>
- LocalStack Redshift docs: <https://docs.localstack.cloud/user-guide/aws/redshift/>
- DuckDB data types overview: <https://duckdb.org/docs/stable/sql/data_types/overview>
- DuckDB struct type: <https://duckdb.org/docs/stable/sql/data_types/struct>
- DuckDB JSON overview: <https://duckdb.org/docs/stable/data/json/overview.html>
- DuckDB spatial extension: <https://duckdb.org/docs/stable/core_extensions/spatial/overview>
- DuckDB httpfs extension: <https://duckdb.org/docs/stable/core_extensions/httpfs/overview>
- DuckDB `CREATE MACRO`: <https://duckdb.org/docs/lts/sql/statements/create_macro.html>
