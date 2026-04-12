// Package federation implements GCS/Parquet federation (Feature 1).
//
// Primary approach: duckdb-gcs extension with ADC credentials.
// Fallback: local signing proxy on 127.0.0.1:9070.
//
// External tables created via CREATE EXTERNAL TABLE map to
// BigQuery external table semantics. DuckDB reads real GCS
// Parquet/CSV files through the configured GCS access method.
package federation
