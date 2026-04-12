// Package contract implements contract-test mode (Feature 3).
//
// Opt-in, default OFF. When enabled, samples queries against real
// BigQuery, diffs results, logs fidelity metrics.
//
// Diff engine normalization rules:
//   - Result sets compared as unordered multisets (unless ORDER BY present)
//   - Floats: epsilon = 1e-9 relative tolerance
//   - NULL: SQL NULL = SQL NULL for diff purposes
//   - STRUCT: field-wise, unordered field comparison
//   - ARRAY: respects order (BQ arrays are ordered)
//   - TIMESTAMP: normalized to UTC before comparison
//   - NUMERIC: compared at 38-digit precision
package contract
