package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slokam-ai/localbq/internal/api"
)

func TestRoutes_UnsupportedEndpoint(t *testing.T) {
	srv := api.New()
	ts := httptest.NewServer(srv.Handler())
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

func TestRoutes_QueryEndpoint(t *testing.T) {
	srv := api.New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/bigquery/v2/projects/myproject/queries", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should return 400 with notImplemented (stub)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var bqErr api.BQError
	if err := json.NewDecoder(resp.Body).Decode(&bqErr); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if bqErr.Error.Errors[0].Reason != "notImplemented" {
		t.Errorf("reason = %q, want %q", bqErr.Error.Errors[0].Reason, "notImplemented")
	}
}

func TestRoutes_ContentType(t *testing.T) {
	srv := api.New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/bigquery/v2/projects/test/datasets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}
