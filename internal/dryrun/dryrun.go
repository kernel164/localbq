// Package dryrun implements dry-run cost projection (Feature 2).
//
// When jobs.query is called with dryRun: true, computes bytes-projected
// from DuckDB statistics. Native tables only in v0.1.
// Ship gate: delta <=20% on >=80% of test corpus.
package dryrun
