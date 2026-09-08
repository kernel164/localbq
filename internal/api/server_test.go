package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/slokam-ai/localbq/internal/api"
	"github.com/slokam-ai/localbq/internal/engine"
	"github.com/slokam-ai/localbq/internal/meta"
)

func setup(t *testing.T) (*http.ServeMux, func()) {
	t.Helper()
	eng, err := engine.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.InitInfoSchema(context.Background(), eng); err != nil {
		eng.Close()
		t.Fatal(err)
	}
	store := meta.NewStore(eng)
	mux := http.NewServeMux()
	api.New(mux, eng, store)
	return mux, func() { eng.Close() }
}

func TestRoutes_UnsupportedEndpoint(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/bigquery/v2/projects/test/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var bqErr api.BQError
	if err := json.NewDecoder(resp.Body).Decode(&bqErr); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if bqErr.Error.Code != 400 {
		t.Errorf("error.code = %d, want 400", bqErr.Error.Code)
	}
	if len(bqErr.Error.Errors) == 0 {
		t.Fatal("expected at least one error detail")
	}
	if bqErr.Error.Errors[0].Reason != "notImplemented" {
		t.Errorf("error.reason = %q, want %q", bqErr.Error.Errors[0].Reason, "notImplemented")
	}
}

func TestRoutes_ContentType(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := `{"datasetReference":{"datasetId":"test_ds"}}`
	resp, err := http.Post(ts.URL+"/bigquery/v2/projects/test/datasets", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestQuery_SelectLiteral(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := `{"query":"SELECT 1 AS num, 'hello' AS greeting"}`
	resp, err := http.Post(ts.URL+"/bigquery/v2/projects/myproject/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var bqErr api.BQError
		json.NewDecoder(resp.Body).Decode(&bqErr)
		t.Fatalf("status = %d, error = %s", resp.StatusCode, bqErr.Error.Message)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	if result["kind"] != "bigquery#queryResponse" {
		t.Errorf("kind = %v, want bigquery#queryResponse", result["kind"])
	}
	if result["jobComplete"] != true {
		t.Errorf("jobComplete = %v, want true", result["jobComplete"])
	}
	if result["totalRows"] != "1" {
		t.Errorf("totalRows = %v, want 1", result["totalRows"])
	}

	// Verify f/v row encoding
	rows := result["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0].(map[string]any)
	f := row["f"].([]any)
	if len(f) != 2 {
		t.Fatalf("columns = %d, want 2", len(f))
	}

	v0 := f[0].(map[string]any)["v"]
	if v0 != "1" {
		t.Errorf("row[0].v = %v, want 1", v0)
	}
	v1 := f[1].(map[string]any)["v"]
	if v1 != "hello" {
		t.Errorf("row[1].v = %v, want hello", v1)
	}

	// Check schema
	schema := result["schema"].(map[string]any)
	fields := schema["fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("schema fields = %d, want 2", len(fields))
	}
}

func TestQuery_InvalidSQL(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := `{"query":"SELEC INVALID SQL"}`
	resp, err := http.Post(ts.URL+"/bigquery/v2/projects/p/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestQuery_EmptyQuery(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := `{"query":""}`
	resp, err := http.Post(ts.URL+"/bigquery/v2/projects/p/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestDataset_CRUD(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Create dataset
	body := `{"datasetReference":{"datasetId":"test_dataset"},"location":"US"}`
	resp, err := http.Post(base+"/datasets", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create dataset: status = %d", resp.StatusCode)
	}

	// Get dataset
	resp, err = http.Get(base + "/datasets/test_dataset")
	if err != nil {
		t.Fatal(err)
	}
	var ds map[string]any
	json.NewDecoder(resp.Body).Decode(&ds)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get dataset: status = %d", resp.StatusCode)
	}
	if ds["kind"] != "bigquery#dataset" {
		t.Errorf("kind = %v", ds["kind"])
	}
	ref := ds["datasetReference"].(map[string]any)
	if ref["datasetId"] != "test_dataset" {
		t.Errorf("datasetId = %v", ref["datasetId"])
	}

	// List datasets
	resp, err = http.Get(base + "/datasets")
	if err != nil {
		t.Fatal(err)
	}
	var listResp map[string]any
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()

	datasets := listResp["datasets"].([]any)
	if len(datasets) != 1 {
		t.Errorf("datasets count = %d, want 1", len(datasets))
	}

	// Delete dataset
	req, _ := http.NewRequest("DELETE", base+"/datasets/test_dataset?deleteContents=true", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete dataset: status = %d", resp.StatusCode)
	}

	// Verify deleted
	resp, err = http.Get(base + "/datasets/test_dataset")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get deleted dataset: status = %d, want 404", resp.StatusCode)
	}
}

func TestTable_CRUD(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Create dataset first
	body := `{"datasetReference":{"datasetId":"ds1"}}`
	resp, err := http.Post(base+"/datasets", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Create table
	body = `{"tableReference":{"tableId":"users"},"schema":{"fields":[{"name":"id","type":"INTEGER"},{"name":"name","type":"STRING"},{"name":"active","type":"BOOLEAN"}]}}`
	resp, err = http.Post(base+"/datasets/ds1/tables", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var tbl map[string]any
	json.NewDecoder(resp.Body).Decode(&tbl)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create table: status = %d, body = %v", resp.StatusCode, tbl)
	}
	if tbl["kind"] != "bigquery#table" {
		t.Errorf("kind = %v", tbl["kind"])
	}

	// Get table
	resp, err = http.Get(base + "/datasets/ds1/tables/users")
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(resp.Body).Decode(&tbl)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get table: status = %d", resp.StatusCode)
	}
	schema := tbl["schema"].(map[string]any)
	fields := schema["fields"].([]any)
	if len(fields) != 3 {
		t.Errorf("fields = %d, want 3", len(fields))
	}

	// List tables
	resp, err = http.Get(base + "/datasets/ds1/tables")
	if err != nil {
		t.Fatal(err)
	}
	var listResp map[string]any
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()

	tables := listResp["tables"].([]any)
	if len(tables) != 1 {
		t.Errorf("tables count = %d, want 1", len(tables))
	}

	// Delete table
	req, _ := http.NewRequest("DELETE", base+"/datasets/ds1/tables/users", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete table: status = %d", resp.StatusCode)
	}
}

func TestQuery_AgainstTable(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Create dataset + table
	http.Post(base+"/datasets", "application/json", bytes.NewBufferString(`{"datasetReference":{"datasetId":"ds1"}}`))
	http.Post(base+"/datasets/ds1/tables", "application/json", bytes.NewBufferString(
		`{"tableReference":{"tableId":"nums"},"schema":{"fields":[{"name":"val","type":"INTEGER"}]}}`))

	// Insert data via query
	body := `{"query":"INSERT INTO ds1.nums VALUES (1), (2), (3)"}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Query the data
	body = `{"query":"SELECT val FROM ds1.nums ORDER BY val"}`
	resp, err = http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query: status = %d", resp.StatusCode)
	}
	if result["totalRows"] != "3" {
		t.Errorf("totalRows = %v, want 3", result["totalRows"])
	}

	rows := result["rows"].([]any)
	vals := make([]string, len(rows))
	for i, row := range rows {
		f := row.(map[string]any)["f"].([]any)
		vals[i] = f[0].(map[string]any)["v"].(string)
	}
	if vals[0] != "1" || vals[1] != "2" || vals[2] != "3" {
		t.Errorf("values = %v, want [1 2 3]", vals)
	}
}

// TestQuery_NumericAndTimestampWireFormat pins down two formatValue bugs
// found by pointing invisibl-aether's real gcpbilling adapter at LocalBQ:
// a NUMERIC/DECIMAL column rendered its Go driver struct raw ("{18 3 4000}"
// instead of "4"), and a non-midnight TIMESTAMP rendered as a bare
// microseconds-since-epoch integer instead of BigQuery's own documented
// floating-point-seconds-since-epoch string.
func TestQuery_NumericAndTimestampWireFormat(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	http.Post(base+"/datasets", "application/json", bytes.NewBufferString(`{"datasetReference":{"datasetId":"ds1"}}`))
	body := `{"query":"CREATE TABLE ds1.t1 (id BIGINT, credits STRUCT(amount DOUBLE)[], ts TIMESTAMP)"}`
	http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))

	body = `{"query":"INSERT INTO ds1.t1 VALUES (1, [{'amount': 1.5}, {'amount': 2.5}], TIMESTAMP '2026-06-01 12:00:00.123456')"}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// The exact shape pkg/adapters/gcpbilling/plan.go's billingRowSelect uses
	// for credits_total, unmodified.
	body = `{"query":"SELECT ts, CAST(IFNULL((SELECT SUM(CAST(c.amount AS NUMERIC)) FROM UNNEST(credits) AS c), 0) AS NUMERIC) AS credits_total FROM ds1.t1"}`
	resp, err = http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query: status = %d, body = %v", resp.StatusCode, result)
	}
	rows := result["rows"].([]any)
	f := rows[0].(map[string]any)["f"].([]any)
	gotTS := f[0].(map[string]any)["v"].(string)
	gotCredits := f[1].(map[string]any)["v"].(string)

	if gotCredits != "4" {
		t.Errorf("credits_total = %q, want %q (was rendering the driver's Decimal struct raw)", gotCredits, "4")
	}
	seconds, err := strconv.ParseFloat(gotTS, 64)
	if err != nil {
		t.Fatalf("ts = %q, not a floating-point seconds-since-epoch string: %v", gotTS, err)
	}
	wantSeconds := float64(time.Date(2026, 6, 1, 12, 0, 0, 123456000, time.UTC).UnixNano()) / 1e9
	if diff := seconds - wantSeconds; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("ts = %q (%v seconds), want %v seconds (2026-06-01T12:00:00.123456Z) — was previously UnixMicro() rendered as a bare integer", gotTS, seconds, wantSeconds)
	}
}

func TestQuery_GetQueryResults(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Execute a query
	body := `{"query":"SELECT 42 AS answer"}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var queryResp map[string]any
	json.NewDecoder(resp.Body).Decode(&queryResp)
	resp.Body.Close()

	jobRef := queryResp["jobReference"].(map[string]any)
	jobID := jobRef["jobId"].(string)

	// Get query results
	resp, err = http.Get(base + "/queries/" + jobID)
	if err != nil {
		t.Fatal(err)
	}
	var resultResp map[string]any
	json.NewDecoder(resp.Body).Decode(&resultResp)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("getQueryResults: status = %d", resp.StatusCode)
	}
	if resultResp["jobComplete"] != true {
		t.Errorf("jobComplete = %v, want true", resultResp["jobComplete"])
	}
	if resultResp["totalRows"] != "1" {
		t.Errorf("totalRows = %v, want 1", resultResp["totalRows"])
	}
}

func TestInfoSchema_TablesView(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Create a dataset and table
	http.Post(base+"/datasets", "application/json", bytes.NewBufferString(`{"datasetReference":{"datasetId":"ds1"}}`))
	http.Post(base+"/datasets/ds1/tables", "application/json", bytes.NewBufferString(
		`{"tableReference":{"tableId":"users"},"schema":{"fields":[{"name":"id","type":"INTEGER"}]}}`))

	// Query INFORMATION_SCHEMA.TABLES (BQ-style with region prefix)
	body := `{"query":"SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = 'ds1'"}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, result)
	}
	if result["totalRows"] != "1" {
		t.Errorf("totalRows = %v, want 1", result["totalRows"])
	}
}

func TestInfoSchema_Columns(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	http.Post(base+"/datasets", "application/json", bytes.NewBufferString(`{"datasetReference":{"datasetId":"ds1"}}`))
	http.Post(base+"/datasets/ds1/tables", "application/json", bytes.NewBufferString(
		`{"tableReference":{"tableId":"users"},"schema":{"fields":[{"name":"id","type":"INTEGER"},{"name":"name","type":"STRING"}]}}`))

	body := `{"query":"SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'ds1' AND table_name = 'users' ORDER BY ordinal_position"}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, result)
	}
	if result["totalRows"] != "2" {
		t.Errorf("totalRows = %v, want 2", result["totalRows"])
	}
}

func TestInfoSchema_JobsByProject(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Run a query first to generate a job record
	http.Post(base+"/queries", "application/json", bytes.NewBufferString(`{"query":"SELECT 1"}`))

	// Query JOBS_BY_PROJECT
	body := `{"query":"SELECT job_id, state, statement_type FROM information_schema.\"JOBS_BY_PROJECT\" LIMIT 5"}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, result)
	}
	// Should have at least 1 job (the SELECT 1 we ran)
	rows, ok := result["rows"].([]any)
	if !ok || len(rows) == 0 {
		t.Error("expected at least 1 job in JOBS_BY_PROJECT")
	}
}

func TestQuery_LoweringPass(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	tests := []struct {
		name  string
		query string
		want  int // expected HTTP status
	}{
		{
			name:  "CURRENT_TIMESTAMP() with parens",
			query: "SELECT CURRENT_TIMESTAMP() AS ts",
			want:  200,
		},
		{
			name:  "COUNTIF lowered to count_if",
			query: "SELECT count_if(x > 1) FROM (VALUES (1),(2),(3)) t(x)",
			want:  200,
		},
		{
			name:  "FLOAT64 type in CAST",
			query: "SELECT CAST(1 AS FLOAT64)",
			want:  200,
		},
		{
			name:  "SAFE_CAST lowered to TRY_CAST",
			query: "SELECT SAFE_CAST('abc' AS INTEGER)",
			want:  200,
		},
		{
			name:  "TIMESTAMP_SUB lowered",
			query: "SELECT TIMESTAMP_SUB(TIMESTAMP '2024-01-15 12:00:00', INTERVAL 7 DAY)",
			want:  200,
		},
		{
			name:  "TIMESTAMP_TRUNC lowered",
			query: "SELECT TIMESTAMP_TRUNC(TIMESTAMP '2024-01-15 12:00:00', DAY)",
			want:  200,
		},
		{
			name:  "DATE function lowered",
			query: "SELECT DATE(TIMESTAMP '2024-01-15 12:00:00')",
			want:  200,
		},
		{
			name:  "backtick identifiers",
			query: "SELECT 1 AS `my col`",
			want:  200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"query":"` + tt.query + `"}`
			resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.want {
				var bqErr api.BQError
				json.NewDecoder(resp.Body).Decode(&bqErr)
				t.Errorf("status = %d, want %d, error = %s", resp.StatusCode, tt.want, bqErr.Error.Message)
			}
		})
	}
}

func TestAnonymousAuth(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// No auth header at all
	body := `{"query":"SELECT 1"}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("no-auth: status = %d, want 200", resp.StatusCode)
	}

	// Random bearer token
	req, _ := http.NewRequest("POST", base+"/queries", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer some-random-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("random-token: status = %d, want 200", resp.StatusCode)
	}

	// Google-style OAuth token
	req, _ = http.NewRequest("POST", base+"/queries", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ya29.fake-oauth-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("oauth-token: status = %d, want 200", resp.StatusCode)
	}
}

func TestDryRun_QueryEndpoint(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Create a table to validate against
	http.Post(base+"/datasets", "application/json", bytes.NewBufferString(`{"datasetReference":{"datasetId":"ds1"}}`))
	http.Post(base+"/datasets/ds1/tables", "application/json", bytes.NewBufferString(
		`{"tableReference":{"tableId":"t1"},"schema":{"fields":[{"name":"id","type":"INTEGER"},{"name":"name","type":"STRING"}]}}`))

	// Dry-run a valid SELECT
	body := `{"query":"SELECT id, name FROM ds1.t1 WHERE id > 0","dryRun":true}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dry-run status = %d, body = %v", resp.StatusCode, result)
	}
	if result["jobComplete"] != true {
		t.Errorf("jobComplete = %v, want true", result["jobComplete"])
	}
	if result["totalBytesProcessed"] != "0" {
		t.Errorf("totalBytesProcessed = %v, want 0", result["totalBytesProcessed"])
	}

	// Should have schema with 2 fields
	schema := result["schema"].(map[string]any)
	fields := schema["fields"].([]any)
	if len(fields) != 2 {
		t.Errorf("schema fields = %d, want 2", len(fields))
	}

	// Rows should NOT be present (dry-run doesn't execute)
	if _, hasRows := result["rows"]; hasRows {
		t.Error("dry-run should not return rows")
	}
}

func TestDryRun_InvalidQuery(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Dry-run an invalid query (table doesn't exist)
	body := `{"query":"SELECT * FROM nonexistent_table","dryRun":true}`
	resp, err := http.Post(base+"/queries", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("dry-run invalid: status = %d, want 400", resp.StatusCode)
	}
}

func TestDryRun_JobsInsert(t *testing.T) {
	mux, cleanup := setup(t)
	defer cleanup()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	base := ts.URL + "/bigquery/v2/projects/myproject"

	// Dry-run via jobs.insert (how Cascade calls EstimateCost)
	body := `{"configuration":{"query":{"query":"SELECT 1 AS num"},"dryRun":true}}`
	resp, err := http.Post(base+"/jobs", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dry-run jobs.insert: status = %d, body = %v", resp.StatusCode, result)
	}
	if result["kind"] != "bigquery#job" {
		t.Errorf("kind = %v, want bigquery#job", result["kind"])
	}

	stats := result["statistics"].(map[string]any)
	if stats["totalBytesProcessed"] != "0" {
		t.Errorf("totalBytesProcessed = %v, want 0", stats["totalBytesProcessed"])
	}
}
