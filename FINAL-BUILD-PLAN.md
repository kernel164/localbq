# BigQuery Emulator — Build Plan

_A synthesis of three independent research reports: Claude, Codex, and Gemini._
_Date: April 11, 2026._

## The decision in one paragraph

**Build it as a new service inside LocalGCP, with Cascade as the dogfood customer.** Three independent reports converge on the same shape: `goccy/bigquery-emulator` is the only serious incumbent (1k stars, 185 open issues, one weekend maintainer); SQLite is the wrong backend and DuckDB is the right one (literally goccy issue #274, open since Feb 2024); and the hardest work is not the storage engine but the GoogleSQL compatibility layer, `INFORMATION_SCHEMA`, partition metadata, jobs lifecycle, and client-library fidelity. The project ships under the LocalGCP brand alongside Cloud Storage, Pub/Sub, Firestore, Secret Manager, Cloud Tasks, and Vertex AI — same single binary, same Homebrew tap, same multi-arch Docker. The GTM risk from the `nnnkkk7/snowflake-emulator` precedent (6 HN points on a near-identical Go+DuckDB pitch) is blunted because this isn't a standalone launch — it rides LocalGCP's existing distribution and audience, and its first customer is internal (Cascade's BigQuery integration). The product is positioned per RK's exact framing: **_"higher than a simple emulator and lower than actual BQ in production — DuckDB's engine with BigQuery's semantics."_** Not a test double. A legitimate local BigQuery runtime for teams whose primary warehouse is the BQ ecosystem.

## Direction from RK (2026-04-11)

These are decided. Everything downstream flows from them.

- **Name**: `BigQuery Emulator` — the direct name. Inside LocalGCP this is just "the BigQuery service," following the same naming as Pub/Sub, Firestore, etc. (Minor caveat: goccy's project has the same name. Inside LocalGCP's namespace it's fine; in external writing, disambiguate as "LocalGCP BigQuery" when needed.)
- **Distribution**: ships as a LocalGCP service, not a standalone repo. The `~/Projects/ideas/bq-emulator` folder is the research and drafting space; the actual code lives in `slokam-ai/localgcp`.
- **North star**: 100% BigQuery API compatibility. Aspirational but real — not "emulator-quality," production-path quality.
- **Positioning philosophy (RK's words)**: _"higher than a simple emulator and lower than actual BQ in production."_ The product should let users fully use DuckDB's power while writing BigQuery semantics against the BigQuery API.
- **Forcing function**: **Cascade is the first customer.** Cascade already has shipped BigQuery integration (Phase 2 of its roadmap). The emulator must be good enough for Cascade's BQ code path to run against it for dev, demo, CI, and customer-evaluation without a real GCP account. Cascade's Snowflake→BigQuery migration focus also compounds value — local BQ lets you validate migrated SQL before cloud costs. The feedback loop is internal, which collapses the validation cost dramatically versus "go talk to 5 dbt teams."
- **Motivation stack**: (1) enrich localgcp.com with another high-quality native emulator, (2) give Cascade a local BQ to build against. The internal use case funds the work; the external audience is upside.
- **dbt integration**: stays on the roadmap but as **second priority**, not the headline. Cascade-first.

## What all three reports agree on

1. **No official Google emulator exists.** `gcloud emulators` ships Bigtable, Firestore, Pub/Sub, Spanner, Datastore — not BigQuery. Internal issue 129248927 open since 2019.
2. **`goccy/bigquery-emulator` is the only credible incumbent** and is maintenance-bottlenecked. 1,077 stars. 185 open issues. One weekend maintainer. README explicitly asks for sponsors.
3. **DuckDB is the right backend.** Native `STRUCT/ARRAY/LIST/MAP/JSON`, columnar, vectorized, spatial extension, `httpfs` for S3/GCS, Parquet/Arrow round-trip. Goccy's own issue #274 agrees.
4. **DuckDB is not the product by itself.** The storage swap fixes ~20% of goccy's open issues (types, perf, GEOGRAPHY). The other ~80% is API surface, metadata, and dbt integration.
5. **Positioning: a local _development runtime_, not a full warehouse clone.** Target the inner loop. Target dbt + CI. Be honest about what's missing.
6. **A real GoogleSQL compatibility layer is the hard part.** Raw DuckDB SQL is not BigQuery SQL. Either bind ZetaSQL (what goccy does) or transpile via SQLGlot — both have real costs.
7. **Transparent compatibility matrices beat promises of full parity.** Users can live with gaps; they can't live with silent drift.
8. **dbt integration is table stakes.** None of this matters if `dbt-bigquery` can't be pointed at it.

## What each report added uniquely

### Claude's report added
- The **GTM reality check**: nnnkkk7's near-identical Snowflake project got 6 HN points. This is the single most important data point for positioning.
- A **concrete inventory of goccy issues ranked by signal**: MERGE broken (#128/163/299), `INFORMATION_SCHEMA` missing since 2022 (#48), `PARTITION BY` unsupported (#152), ARM64 Docker was the highest-reacted issue (#332, 24 reactions), Storage Write API unreliable (#246/342/352/364/398), TIMESTAMP returned as float (#390), `DECLARE` rejected (#344).
- **SQLMesh + dbt-duckdb + SQLGlot as the stealth competitor.** Already eating the dbt-unit-test slice for free, via transpilation, without any emulator at all.
- **Honest read on the cost angle**: no public numbers exist for dev-team BQ spend. The cost pitch flopped for Snowflake; don't repeat it.
- **dbt-bigquery issue #358** (add emulator support) was closed without the feature being built — the adapter-level blocker is real and unfixed upstream.

### Codex's report added
- **`novucs/local-bigquery`** as prior art — Python + DuckDB + SQLGlot, currently 0 stars, dormant since Sep 2025. Not a competitor, but a validity proof that the DuckDB+transpile path _works_ at MVP scope.
- **Three compatibility modes** as a product feature: strict / permissive / **contract-test**. Contract-test mode runs the same query locally and against real BigQuery for sampled validation during dev/CI. _This is the killer feature none of the existing emulators offer._
- A clean **four-layer architecture**: (1) compatibility front door, (2) GoogleSQL analyzer + rewriter, (3) DuckDB execution core, (4) metadata/control plane.
- Explicit **MVP exclusion list**: IAM, BQ ML, reservations, every `INFORMATION_SCHEMA` table, every DCL feature, exact geospatial parity, every scripting construct.
- **Snowpark Python local testing framework** as prior art for "even the vendor narrows scope to the highest-value developer loop."

### Gemini's report added
- **Concrete benchmark numbers**: DuckDB on a 50 GB dataset is ~10x faster than warm BigQuery median (2,775 ms → 284 ms). Simple aggregations 3x, filtered queries 6x. This is the headline you can write on the README.
- **FinOps math for engineering time**: a $150k engineer costs ~$75/hour; 4 hours/week waiting on cloud provisioning = **$15k/engineer/year lost**. This is the honest cost pitch that _does_ work.
- **The `BIGNUMERIC` precision gap**: BigQuery supports 76 digits; DuckDB's max `DECIMAL` is 38. This is a concrete, measurable compatibility wall to document up front.
- **The `GEOGRAPHY` gap**: BigQuery uses WGS84 spherical with geodesic edges; DuckDB spatial centers on planar `GEOMETRY`. Don't pretend to emulate GEOGRAPHY in v0.1.
- **Storage Read/Write API gap**: modern BQ clients use gRPC + Arrow; most emulators ship only the JSON REST path and hit a performance cliff on large tables.
- **Three unique advanced features** worth stealing into the roadmap: instant snapshots (single-file DuckDB format), cost projection (analyze local bytes scanned → project prod cost), and auto-mocking (query real BQ `INFORMATION_SCHEMA` → materialize local schemas automatically).
- **ClickHouse as the local-first gold standard** — the reference everyone is trying to hit.

## The honest risks (what could kill this)

1. **The GTM is brutal.** nnnkkk7's Go+DuckDB Snowflake emulator got 6 HN points and 33 stars. LocalGCP went unnoticed on HN per your own notes. "Local emulator for cloud X" is a low-virality pitch without a concrete, measurable, painful-to-solve-otherwise benchmark.
2. **goccy already has 2 years of REST/Storage API compatibility fixes.** Rebuilding from scratch means relearning every client-library edge case the hard way.
3. **ZetaSQL via CGO is a real distribution tax.** 20+ minute silent builds, missing headers, historical linux/amd64-only binaries. Any fork inherits this unless we ship pre-built binaries on day one.
4. **SQLGlot transpilation is lossy** in ways that hit real dbt macros: `SELECT AS STRUCT ARRAY(...)`, `ARRAY_AGG(DISTINCT struct)`, UUID type mismatch, UDFs don't transpile at all.
5. **SQLMesh + dbt-duckdb + SQLGlot already covers dbt unit tests for free.** If users adopt that pattern faster than we ship, the addressable slice shrinks to "integration tests only" — still real, but smaller than the all-dbt pitch.
6. **dbt-bigquery issue #358 was closed.** The upstream adapter team has already said no once. Any dbt story we ship has to be a side-loaded adapter or a fork, not a clean merge.
7. **`BIGNUMERIC`, `GEOGRAPHY`, scripting, and materialized views are all known-incomplete territory.** If a production team adopts us and hits any of these, it's a hard failure mode we can't paper over.

## The three architectural forks we must pick

### Fork 1: ZetaSQL (CGO) vs SQLGlot (transpile)

| | ZetaSQL (CGO) | SQLGlot |
|---|---|---|
| Correctness | Google's real parser, ~100% | Lossy on structs/arrays/UDFs/UUID |
| Build pain | 20-min silent builds, CGO hell | Clean Go or Python dependency |
| Distribution | Must ship pre-built binaries | Just go build |
| Long-tail function behavior | Correct by construction | Has to be fixed per-function |
| Maintenance burden | Track upstream ZetaSQL releases | Track SQLGlot issues + our own patches |

**Recommendation: ZetaSQL via CGO, with pre-built binaries for macOS-arm64, macOS-x64, linux-arm64, linux-x64 on day one.** The correctness advantage compounds; the distribution pain is a one-time fix. Use goccy's `go-zetasqlite` as the starting point — he's already done the CGO wrangling. Accept the build tax, pay it once, ship Homebrew and Docker artifacts so no user ever sees it.

Keep SQLGlot in reserve as a fallback for queries ZetaSQL can't handle, or as the path for a "lite" build target that omits CGO.

### Fork 2: Fork goccy vs greenfield vs port-the-pieces

With LocalGCP as the settled distribution home, the calculus shifts. We're not trying to get a PR merged upstream; we're building a LocalGCP service. The choice is: how much of goccy do we reuse, and as what?

**Recommendation: port, don't fork.** Take the two high-value pieces out of goccy — the REST/gRPC API layer (which encodes 2 years of client-library edge-case fixes) and `go-zetasqlite` (which handles the ZetaSQL CGO wiring) — and **rewire them into a new LocalGCP service with a DuckDB-backed execution and metadata layer**. Goccy is MIT-licensed, so this is legal and attribution is cheap. We don't inherit the SQLite-shaped internal model, but we inherit the hard-won parts.

**Still worth doing**: courtesy-contact goccy as a professional move. Let him know we're building the DuckDB backend under LocalGCP and ask whether he'd like to collaborate, cross-link, or stay independent. This is upside, not dependency.

**Default path**: port goccy's API layer + `go-zetasqlite` into a new `localgcp/services/bigquery` package, rewrite the storage/execution/metadata layer against DuckDB, keep the REST+gRPC surface stable.
**Upside path**: if goccy wants to collaborate, upstream our DuckDB backend as an optional driver to his repo too — everyone wins.

### Fork 3: Language

**Recommendation: Go.** Three reasons:
1. Matches goccy so code can be shared/upstreamed.
2. Single-binary distribution matches LocalGCP's existing pattern and your own muscle memory.
3. The one shipped Go+DuckDB DW emulator (nnnkkk7) proves the stack works.

Python (novucs's path) is easier to prototype but harder to distribute and forces a runtime dependency on users.

## The wedge — positioning

Per RK's direction, the positioning is not "an emulator" and not "dbt test harness." Ship it as:

> **"DuckDB's engine with BigQuery's semantics. Run real BigQuery workloads locally — same API, same SQL, same clients. Part of LocalGCP."**

Three claims. Each one is something nobody else credibly delivers today:

1. **Real BigQuery API surface** — point any official client (`bq`, `google-cloud-bigquery`, Go, Java, Node, dbt) at `localhost` and it works. Zero application code changes.
2. **Real DuckDB underneath** — columnar, vectorized, Parquet/Arrow native, spatial extension, `httpfs` for GCS/S3. You're not emulating; you're running a serious analytics engine that happens to speak BigQuery.
3. **Production-path quality, not test-double quality** — the internal quality bar is "Cascade (an AI agent for GCP data engineering) can run its full BigQuery integration against it without fallbacks." That's a higher bar than any existing BQ emulator.

**Secondary pitches** (for different audiences):
- For dbt teams: _"Run your dbt-BigQuery project locally. No service account. No cloud cost. Same SQL that runs in prod."_
- For Cascade-style tool builders: _"Build BigQuery-aware tools against a real BigQuery API without burning GCP credits or needing cloud access in CI."_
- For migrations (Snowflake→BQ, the Cascade focus): _"Validate migrated SQL locally on DuckDB speed before you hit cloud billing."_
- For cost-conscious teams: $15k/engineer/year in waiting time (Gemini's math), not query scan dollars.

**Never use** "stop burning cloud credits" as the headline. That framing flopped for nnnkkk7's Snowflake emulator (6 HN points). Lead with capability ("DuckDB-powered real BigQuery runtime") and let cost savings be a footnote.

## MVP scope — v0.1

Scope is chosen by one rule: **Cascade's existing BigQuery integration must run against it end-to-end.** Every feature below is justified by Cascade need, LocalGCP brand consistency, or the compatibility matrix we'll have to publish publicly. If a feature isn't needed by Cascade's shipped BQ code path, it's v0.2+.

### In scope — must-haves for Cascade
1. **LocalGCP service integration**: new `localgcp/services/bigquery` package, wired into the existing single-binary runtime, config system, welcome screen, Homebrew tap, and multi-arch Docker (ARM64 + x86_64 day one).
2. **Ported REST + Storage API surface from goccy** with `go-zetasqlite` for GoogleSQL parsing and analysis. Rewired onto a DuckDB execution and metadata layer.
3. **Jobs lifecycle**: `jobs.insert`, `jobs.query`, `jobs.get`, `jobs.list`, `jobs.getQueryResults` — all with realistic async states (PENDING → RUNNING → DONE). Cascade's UI depends on this.
4. **Dry-run cost estimation** (`jobs.query` with `dryRun: true` → returns bytes-scanned estimate). _This is Cascade's "cost-aware by default" principle and was a Gemini-suggested advanced feature. Promoted into v0.1 because it's an identity claim: "runs the same `bq query --dry_run` cost estimates you're used to."_
5. **`INFORMATION_SCHEMA`**: `TABLES`, `COLUMNS`, `PARTITIONS`, `JOBS`, `JOBS_BY_PROJECT` — enough for Cascade's schema discovery and for dbt macros.
6. **Dataset/Table CRUD** with real metadata: labels, descriptions, schema, partitioning spec, clustering spec, expiration.
7. **`CREATE TABLE ... PARTITION BY DATE(col)` and `CLUSTER BY`** — partitioning simulated via a metadata layer on top of DuckDB.
8. **MERGE statement** correct against partitioned targets (the dbt incremental path and also a migration-validation path for Cascade).
9. **Realistic BigQuery error shapes** (HTTP status codes, error reasons, `errorResult` payload structure). Cascade's error handling branches on these; if they drift from real BQ, Cascade breaks.
10. **Anonymous credentials mode** that plays cleanly with Cascade's two-plane auth model (GCP resource auth separated from model auth). `HTTPOptions.BaseURL` + `AnonymousCredentials` — the Vertex AI emulator's pattern.
11. **`dbt-bigquery-local` adapter shim** — side-loaded package solving the auth-bypass pain. Second-priority target but low additional cost once #10 is done.
12. **Fixture loading**: YAML manifests for projects/datasets/tables/schemas, Parquet/CSV/JSON seed files, reset command, deterministic job IDs in test mode.
13. **Contract-test mode** (from Codex): sample N% of queries, also run against real BigQuery, diff results, fail loudly on drift. This is the trust mechanism.
14. **Public compatibility matrix** at launch — strict/permissive/contract-test modes and an explicit list of unsupported features with BigQuery-shaped error responses.

### Explicitly out of scope for v0.1
BigQuery ML / Model API, Connection API, BI Engine, real `GEOGRAPHY` (ship with a loud warning + planar `GEOMETRY` fallback), row-level security, column-level security, authorized views, time travel / `FOR SYSTEM_TIME AS OF`, sessions, scripting (`DECLARE` / procedural constructs / stored procedures), Storage Write API `_default` stream + full PENDING/COMMITTED semantics, materialized view refresh semantics, `BIGNUMERIC` beyond 38 digits of precision, IAM API, reservations/capacity API.

### Advanced features on the v0.2+ roadmap
Instant snapshots leveraging DuckDB's single-file format (Gemini), auto-mocking from real BQ `INFORMATION_SCHEMA` (Gemini — particularly useful for Cascade: pull a prod schema, materialize it locally, fuzz data into it), full Storage Write API, full scripting, SQLMesh integration, real `GEOGRAPHY` via `h3` + careful sphere-vs-plane adapter, column-level masking for governance-aware Cascade flows, `BIGNUMERIC` via software-emulated arbitrary precision.

## The 30-day plan — validate before building

Spend the first 30 days de-risking. With Cascade as the forcing function, validation is internal and fast — no need for 5 dbt-team interviews before committing.

1. **Day 1-2: Cascade BQ feature audit.** Grep Cascade's codebase for every BigQuery API call it makes. Produce a spreadsheet: `method × HTTP verb × payload shape × frequency × criticality`. This is the authoritative v0.1 API surface list — every row becomes a must-have or explicit-gap. No feature ships in v0.1 unless Cascade uses it or LocalGCP brand requires it.
2. **Day 1: courtesy outreach to goccy.** GitHub issue or email: _"We're building a DuckDB backend for BigQuery emulation as a LocalGCP service. We'll port your REST/gRPC surface and `go-zetasqlite` with attribution under MIT. Happy to collaborate, cross-link, or upstream our DuckDB driver back to your repo if you'd find it useful."_ Not a dependency — a professional heads-up.
3. **Week 1: Spike A — `go-zetasqlite` with DuckDB backend.** Fork `go-zetasqlite` locally. Replace the SQLite driver with a DuckDB driver. Run goccy's test suite against it. Success metric: **how many of the top 20 goccy issues close automatically?** Expected answer: 5-10. Prove or disprove "the DuckDB swap moves the needle" with numbers.
4. **Week 2: Spike B — end-to-end Cascade dogfood.** Wire the Spike A prototype into a minimal LocalGCP service skeleton. Point Cascade's BigQuery client at it. Run Cascade's existing BQ test suite. Record every failure. This is the ground-truth v0.1 backlog — every failure becomes either a must-fix or a documented compatibility gap.
5. **Week 2-3: Spike C — build the conformance test harness.** A test runner that takes a SQL corpus, runs it against real BigQuery and against the prototype, diffs results. This harness is the product's soul and the contract-test mode MVP. Seed it with: all SQL Cascade emits, the BigQuery public documentation examples, and a curated corpus from goccy's open issues. Ship this as an independent OSS tool regardless of whether the emulator ships — it's valuable standalone.
6. **Week 3: Spike D — `dbt-bigquery` smoke test (lower priority).** Point real `dbt-bigquery` at the prototype with a minimal adapter patch from goccy #429. Run `dbt-labs/jaffle-shop`. Record failures. This becomes the v0.2 backlog, not v0.1 — but it's a cheap way to scope the "dbt-bigquery-local" shim that ships in v0.1.
7. **Week 3-4: optionally interview 2-3 Cascade users or dbt-on-BQ teams** you already know. Not a formal user research project — just pressure-test the positioning and ask "would you adopt this?" Skip if you don't have warm contacts.
8. **Week 4: decide.** Write a 1-page go/no-go memo with Spike A/B/C results and the Cascade feature-audit spreadsheet. The decision is yours (no Slokam gate unless you want one).

### Go criteria (all must be true)
- **Spike A**: ≥25% of goccy's top-20 open issues close automatically with the DuckDB swap (validates the backend-swap thesis).
- **Spike B**: Cascade's existing BQ test suite runs against the prototype with ≥60% pass rate on the first try, and the remaining failures are cataloged and bounded (validates that Cascade-dogfood is achievable, not open-ended).
- **Spike C**: conformance harness runs end-to-end against real BQ for at least a dozen representative queries and produces a clean diff report (validates that contract-test mode is buildable).
- The v0.1 API surface list from the Cascade feature audit fits inside a realistic 60-day build budget.

### Kill criteria (any one is sufficient)
- ZetaSQL→DuckDB lowering can't handle operators Cascade actually emits, without per-operator custom rewrites scaling worse than SQLGlot's lossiness.
- Cascade's test suite has a compatibility gap that would require implementing BI Engine, Model API, or scripting (the known v0.1 exclusions) — would force scope explosion.
- goccy announces he's shipping his own DuckDB backend imminently — pause, collaborate, don't compete.
- Cascade's actual BQ usage turns out to be so narrow that the emulator's effort is wildly disproportionate to the benefit (e.g., if Cascade only uses 4 API methods, build a tiny tool, not an emulator).

## The 90-day plan — to v0.1 ship (if go)

- **Month 1** (above): Cascade feature audit, validation spikes, go/no-go decision.
- **Month 2**: port goccy's API layer + `go-zetasqlite` into `localgcp/services/bigquery`. Wire up the DuckDB backend. Implement the Cascade-critical API methods from the audit spreadsheet. Build the metadata layer for partitioning/clustering. Stand up the conformance harness in LocalGCP's CI.
- **Month 3**: ship v0.1 as part of the next LocalGCP release. Cascade dogfood running in CI against the emulator. `dbt-bigquery-local` adapter shim as a side package. Public compatibility matrix. Headline: _"LocalGCP now ships BigQuery — DuckDB-powered, 100% API-compatible target for Cascade and other BigQuery-native tools."_ Benchmark against a Cascade workload: _"N queries, M seconds, zero cloud cost."_

## Open questions we must answer before writing production code

1. **Will `go-zetasqlite` + DuckDB lowering work for all operators Cascade actually emits?** The Cascade feature audit (Day 1 of the 30-day plan) answers the scoped version of this question. Bigger unknown: does the ZetaSQL AST contain enough context to lower idiomatically into DuckDB, or are there operators that force per-case custom rewrites?
2. **Can `INFORMATION_SCHEMA` be synthesized from our metadata layer without shipping the full BQ catalog surface?** Minimum set = what Cascade queries + what dbt macros touch. Probably `TABLES`, `COLUMNS`, `PARTITIONS`, `JOBS`, `JOBS_BY_PROJECT`.
3. **How does dry-run cost estimation work when the underlying data is DuckDB?** BigQuery's bytes-scanned depends on columnar physical layout. DuckDB has its own columnar physical layout. We can compute an analogous "bytes-projected-to-be-read" from the DuckDB query plan, but is that number close enough to real BQ dry-run that Cascade's cost-aware UI can trust it? Or do we also contract-test dry-run estimates against real BQ?
4. **What happens to contract-test mode when the query references tables that only exist locally?** Skip with `"skipped: local-only table"` status? Warn? Auto-mirror the schema to real BQ? This UX needs a decision.
5. **`BIGNUMERIC` beyond 38 digits** — silent truncation is a footgun. Options: loud error in strict mode, warning-with-truncation in permissive mode. How does Cascade handle it today?
6. **`GEOGRAPHY` semantics** — DuckDB spatial is planar, BQ is WGS84 geodesic. Options: (a) ship with planar fallback and a warning, (b) defer entirely and error out, (c) integrate `h3` or similar. How often does Cascade or its users touch GEOGRAPHY?
7. **Goccy collaboration posture** — collaborative, indifferent, or competitive? Doesn't block the project but changes the attribution + upstream story.
8. **What's the right LocalGCP service API for accessing this?** Does it live behind `http://localhost:9050` (goccy's default) or a LocalGCP-assigned port? Does it share a process with other LocalGCP services or run in its own subprocess for isolation (ZetaSQL CGO memory footprint is non-trivial)?

## GTM plan — how to avoid the 6-HN-points trap

The GTM risk is lower now because this ships as a LocalGCP service, not a standalone launch, and its first customer is internal (Cascade). Still, per the nnnkkk7 precedent and your own wiki note on LocalGCP's HN flop, **do not post this yourself on HN on launch day**. The distribution shape:

1. **Ship inside a LocalGCP minor release.** Not a separate launch. The release post enumerates services: "LocalGCP N.M adds BigQuery: DuckDB-powered, 100% API-compatible, runs Cascade's full BQ integration in CI." BigQuery is one of seven services, not THE product.
2. **Lead with Cascade as proof.** Blog post title: _"How we built a local BigQuery for Cascade with DuckDB under the hood."_ The tool is the punchline. Venue: Binary Yoga. The Cascade angle makes it a build-log, not a product pitch — and build-logs get shared more easily than product launches.
3. **Second-moment launch: the conformance test harness as a standalone tool.** Even if it ships same-day as v0.1, frame it as its own thing: _"We shipped a BigQuery↔local-emulator conformance diff tool. Use it to audit your own tests."_ Two launch moments beat one.
4. **Go direct to the dbt Slack and Discourse** when the dbt-bigquery-local shim is stable (not necessarily at v0.1). Targeted post in #local-development.
5. **Submit to `dbt-labs` adapter registry** if the shim is clean enough to list. Official listing = legitimacy.
6. **Wait for organic HN discovery.** Per your wiki note: "someone else needs to find my stuff." LocalGCP already has mindshare; the BigQuery service will surface naturally as people update.
7. **If Cascade itself becomes publicly interesting**, the emulator rides along as "the local BigQuery that Cascade uses" — reframe from "tool for testing dbt" to "local dev infrastructure for the next generation of BQ-native tools."

## Summary — the action-oriented shape

1. **Don't build yet.** Spend 30 days on the Cascade feature audit + 3 de-risking spikes.
2. **LocalGCP is the home.** No more debate about distribution or umbrella. The research folder at `~/Projects/ideas/bq-emulator` is scratch space; production code lives in `slokam-ai/localgcp`.
3. **Cascade is the forcing function.** v0.1 scope = what Cascade uses, plus LocalGCP brand-consistency must-haves. Everything else is v0.2+.
4. **Port, don't fork.** Take goccy's REST/gRPC surface and `go-zetasqlite` with MIT attribution; rewrite storage/execution/metadata onto DuckDB. Courtesy-contact goccy, but don't wait for him.
5. **Position as "DuckDB's engine with BigQuery's semantics"** — not an emulator, not a test harness. Higher than an emulator, lower than production BQ.
6. **Promote dry-run cost estimation into v0.1.** Cascade's "cost-aware by default" principle requires it, and it's an identity claim that distinguishes us from every other emulator.
7. **Contract-test mode stays as the trust mechanism.** Build the conformance harness in the spike phase; ship it as an independent tool alongside v0.1.
8. **GTM rides LocalGCP's release cycle**, not a standalone launch. Cascade dogfood is the story. Don't self-post to HN.

The upside if this works: LocalGCP becomes the default local development runtime for the GCP data engineering stack, Cascade has a world-class test backend, and the BigQuery service spins off as its own reference project for anyone building BQ-native tools. The downside if it doesn't: ~90 days and a validated "no," which frees up a high-quality slot for the next LocalGCP service (Dataflow? Composer? Logging?).

The question isn't "should we build a BigQuery emulator." It's **"can we ship a local BigQuery that Cascade will actually run its full integration against, under the LocalGCP brand, in 90 days?"** Answer the scoped version of that in 30 days with the Cascade feature audit + spikes, before writing a line of production code.
