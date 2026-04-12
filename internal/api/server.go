// Package api implements the BigQuery REST API front door (Layer 1).
//
// Request flow:
//
//   Client (bq CLI, Go SDK, Python SDK)
//     │
//     ▼ HTTP REST
//   Server.mux (routing)
//     │
//     ├── POST /bigquery/v2/projects/{project}/queries     → handleQuery
//     ├── POST /bigquery/v2/projects/{project}/jobs         → handleInsertJob
//     ├── GET  /bigquery/v2/projects/{project}/jobs/{id}    → handleGetJob
//     ├── GET  /bigquery/v2/projects/{project}/jobs          → handleListJobs
//     ├── GET  /bigquery/v2/projects/{project}/queries/{id} → handleGetQueryResults
//     ├── POST /bigquery/v2/projects/{project}/jobs/{id}/cancel → handleCancelJob
//     ├── GET/POST/PUT/DELETE /bigquery/v2/projects/{project}/datasets/* → datasets
//     ├── GET/POST/PUT/DELETE /bigquery/v2/projects/{project}/datasets/{ds}/tables/* → tables
//     └── GET /bigquery/v2/projects/{project}/datasets/{ds}/tables/{t}/data → tabledata
//
// All unsupported endpoints return BigQuery-shaped errors:
//   {"error":{"code":400,"message":"...","errors":[{"reason":"UNSUPPORTED_FEATURE",...}]}}
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// Server is the BigQuery REST API server.
type Server struct {
	mux *http.ServeMux
}

// New creates a new API server and registers all routes.
func New() *Server {
	s := &Server{
		mux: http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// Handler returns the http.Handler for use with http.Server.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	// BigQuery API v2 routes
	prefix := "/bigquery/v2/projects/{project}"

	// Jobs
	s.mux.HandleFunc("POST "+prefix+"/queries", s.handleQuery)
	s.mux.HandleFunc("POST "+prefix+"/jobs", s.handleInsertJob)
	s.mux.HandleFunc("GET "+prefix+"/jobs/{jobId}", s.handleGetJob)
	s.mux.HandleFunc("GET "+prefix+"/jobs", s.handleListJobs)
	s.mux.HandleFunc("GET "+prefix+"/queries/{jobId}", s.handleGetQueryResults)
	s.mux.HandleFunc("POST "+prefix+"/jobs/{jobId}/cancel", s.handleCancelJob)

	// Datasets
	s.mux.HandleFunc("GET "+prefix+"/datasets", s.handleListDatasets)
	s.mux.HandleFunc("POST "+prefix+"/datasets", s.handleCreateDataset)
	s.mux.HandleFunc("GET "+prefix+"/datasets/{datasetId}", s.handleGetDataset)
	s.mux.HandleFunc("DELETE "+prefix+"/datasets/{datasetId}", s.handleDeleteDataset)

	// Tables
	tblPrefix := prefix + "/datasets/{datasetId}/tables"
	s.mux.HandleFunc("GET "+tblPrefix, s.handleListTables)
	s.mux.HandleFunc("POST "+tblPrefix, s.handleCreateTable)
	s.mux.HandleFunc("GET "+tblPrefix+"/{tableId}", s.handleGetTable)
	s.mux.HandleFunc("DELETE "+tblPrefix+"/{tableId}", s.handleDeleteTable)

	// Tabledata
	s.mux.HandleFunc("GET "+tblPrefix+"/{tableId}/data", s.handleListTableData)

	// Catch-all for unsupported endpoints
	s.mux.HandleFunc("/", s.handleUnsupported)
}

// --- Job handlers ---

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	slog.Debug("jobs.query", "project", project)
	// TODO: parse request body, send to googlesql sidecar, execute via DuckDB
	writeUnsupported(w, "jobs.query not yet implemented")
}

func (s *Server) handleInsertJob(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "jobs.insert not yet implemented")
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "jobs.get not yet implemented")
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "jobs.list not yet implemented")
}

func (s *Server) handleGetQueryResults(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "jobs.getQueryResults not yet implemented")
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "jobs.cancel not yet implemented")
}

// --- Dataset handlers ---

func (s *Server) handleListDatasets(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "datasets.list not yet implemented")
}

func (s *Server) handleCreateDataset(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "datasets.insert not yet implemented")
}

func (s *Server) handleGetDataset(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "datasets.get not yet implemented")
}

func (s *Server) handleDeleteDataset(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "datasets.delete not yet implemented")
}

// --- Table handlers ---

func (s *Server) handleListTables(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "tables.list not yet implemented")
}

func (s *Server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "tables.insert not yet implemented")
}

func (s *Server) handleGetTable(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "tables.get not yet implemented")
}

func (s *Server) handleDeleteTable(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "tables.delete not yet implemented")
}

func (s *Server) handleListTableData(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "tabledata.list not yet implemented")
}

// --- Catch-all ---

func (s *Server) handleUnsupported(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, fmt.Sprintf("endpoint not supported: %s %s", r.Method, r.URL.Path))
}

// BQError is a BigQuery-shaped error response.
type BQError struct {
	Error BQErrorBody `json:"error"`
}

// BQErrorBody is the body of a BigQuery error response.
type BQErrorBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Errors  []BQErrorDetail `json:"errors"`
}

// BQErrorDetail is a single error detail in a BigQuery error response.
type BQErrorDetail struct {
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

func writeUnsupported(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, "notImplemented", msg)
}

func writeError(w http.ResponseWriter, code int, reason, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(BQError{
		Error: BQErrorBody{
			Code:    code,
			Message: msg,
			Errors: []BQErrorDetail{
				{Reason: reason, Message: msg},
			},
		},
	})
}
