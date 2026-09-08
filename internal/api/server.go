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
	"strconv"
	"sync"
	"time"

	duckdb "github.com/marcboeker/go-duckdb"

	"github.com/slokam-ai/localbq/internal/engine"
	"github.com/slokam-ai/localbq/internal/lowering"
	"github.com/slokam-ai/localbq/internal/meta"
)

// Server is the BigQuery REST API server.
type Server struct {
	eng   *engine.Engine
	store *meta.Store
}

// New creates a new API server with routes registered on the provided mux.
func New(mux *http.ServeMux, eng *engine.Engine, store *meta.Store) *Server {
	s := &Server{
		eng:   eng,
		store: store,
	}
	s.registerRoutes(mux)
	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// BigQuery API v2 routes
	prefix := "/bigquery/v2/projects/{project}"

	// Jobs
	mux.HandleFunc("POST "+prefix+"/queries", s.handleQuery)
	mux.HandleFunc("POST "+prefix+"/jobs", s.handleInsertJob)
	mux.HandleFunc("GET "+prefix+"/jobs/{jobId}", s.handleGetJob)
	mux.HandleFunc("GET "+prefix+"/jobs", s.handleListJobs)
	mux.HandleFunc("GET "+prefix+"/queries/{jobId}", s.handleGetQueryResults)
	mux.HandleFunc("POST "+prefix+"/jobs/{jobId}/cancel", s.handleCancelJob)

	// Datasets
	mux.HandleFunc("GET "+prefix+"/datasets", s.handleListDatasets)
	mux.HandleFunc("POST "+prefix+"/datasets", s.handleCreateDataset)
	mux.HandleFunc("GET "+prefix+"/datasets/{datasetId}", s.handleGetDataset)
	mux.HandleFunc("DELETE "+prefix+"/datasets/{datasetId}", s.handleDeleteDataset)

	// Tables
	tblPrefix := prefix + "/datasets/{datasetId}/tables"
	mux.HandleFunc("GET "+tblPrefix, s.handleListTables)
	mux.HandleFunc("POST "+tblPrefix, s.handleCreateTable)
	mux.HandleFunc("GET "+tblPrefix+"/{tableId}", s.handleGetTable)
	mux.HandleFunc("DELETE "+tblPrefix+"/{tableId}", s.handleDeleteTable)

	// Tabledata
	mux.HandleFunc("GET "+tblPrefix+"/{tableId}/data", s.handleListTableData)

	// Discovery endpoint for bq CLI compatibility
	registerDiscovery(mux)

	// Catch-all for unsupported BigQuery endpoints
	mux.HandleFunc("/bigquery/", s.handleUnsupported)
}

// --- Job handlers ---

// queryRequest is the body of POST /queries.
type queryRequest struct {
	Query          string          `json:"query"`
	UseLegacySql   *bool           `json:"useLegacySql,omitempty"`
	DefaultDataset *datasetRefJSON `json:"defaultDataset,omitempty"`
	MaxResults     *int            `json:"maxResults,omitempty"`
	DryRun         bool            `json:"dryRun,omitempty"`
}

type datasetRefJSON struct {
	ProjectID string `json:"projectId,omitempty"`
	DatasetID string `json:"datasetId,omitempty"`
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	slog.Debug("jobs.query", "project", project)

	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalidQuery", fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "invalidQuery", "query is required")
		return
	}

	// Legacy SQL is not supported
	if req.UseLegacySql != nil && *req.UseLegacySql {
		writeError(w, http.StatusBadRequest, "invalidQuery", "legacy SQL is not supported, set useLegacySql to false")
		return
	}

	// Lower GoogleSQL → DuckDB SQL
	duckSQL := lowering.Lower(req.Query)
	stmtType := meta.ClassifyStatement(req.Query)

	// Dry-run mode: validate the query without executing
	if req.DryRun {
		s.handleDryRun(w, r, project, req.Query, duckSQL, stmtType)
		return
	}

	// Generate a job ID
	jobID := newJobID()
	job := s.store.CreateJob(project, jobID, req.Query)

	// Execute the lowered query
	result, err := s.eng.Query(r.Context(), duckSQL)
	s.store.CompleteJob(jobID, result, err)

	// Record in INFORMATION_SCHEMA.JOBS_BY_PROJECT
	meta.RecordJob(r.Context(), s.eng, project, jobID, req.Query, stmtType, err)

	if err != nil {
		slog.Warn("query failed", "project", project, "jobId", jobID, "error", err)
		writeError(w, http.StatusBadRequest, "invalidQuery", err.Error())
		return
	}

	_ = job // job is tracked in the store

	// Build BigQuery QueryResponse
	resp := buildQueryResponse(project, jobID, result)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleDryRun validates a query without executing it. Returns schema and 0 bytes processed.
// This is used by BQ clients (including Cascade) for cost estimation.
func (s *Server) handleDryRun(w http.ResponseWriter, r *http.Request, project, originalSQL, duckSQL, stmtType string) {
	jobID := newJobID()

	result, err := s.eng.DryRun(r.Context(), duckSQL)
	if err != nil {
		slog.Warn("dry run failed", "project", project, "error", err)
		writeError(w, http.StatusBadRequest, "invalidQuery", err.Error())
		return
	}

	// Build schema fields from dry-run result
	fields := make([]map[string]any, len(result.Columns))
	for i, col := range result.Columns {
		fields[i] = map[string]any{
			"name": col.Name,
			"type": string(col.Type),
			"mode": "NULLABLE",
		}
	}

	resp := map[string]any{
		"kind": "bigquery#queryResponse",
		"jobReference": map[string]any{
			"projectId": project,
			"jobId":     jobID,
		},
		"jobComplete":         true,
		"totalBytesProcessed": "0",
		"schema": map[string]any{
			"fields": fields,
		},
	}

	// Also return job statistics in the format Cascade expects
	// (via job.LastStatus().Statistics.TotalBytesProcessed)
	resp["statistics"] = map[string]any{
		"totalBytesProcessed": "0",
		"query": map[string]any{
			"totalBytesProcessed": "0",
			"statementType":       stmtType,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// insertJobRequest is the body of POST /jobs (jobs.insert).
type insertJobRequest struct {
	JobReference *struct {
		ProjectId string `json:"projectId,omitempty"`
		JobId     string `json:"jobId,omitempty"`
	} `json:"jobReference,omitempty"`
	Configuration struct {
		Query struct {
			Query        string `json:"query"`
			UseLegacySql *bool  `json:"useLegacySql,omitempty"`
		} `json:"query"`
		DryRun bool `json:"dryRun,omitempty"`
	} `json:"configuration"`
}

func (s *Server) handleInsertJob(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")

	var req insertJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", fmt.Sprintf("invalid request body: %v", err))
		return
	}

	query := req.Configuration.Query.Query
	if query == "" {
		writeError(w, http.StatusBadRequest, "invalid", "configuration.query.query is required")
		return
	}

	duckSQL := lowering.Lower(query)
	stmtType := meta.ClassifyStatement(query)

	// Honors a caller-supplied JobReference.JobId (gcpbilling's own startQuery,
	// pkg/adapters/gcpbilling/plan.go, insists on this precisely so a retried
	// Fetch adopts the same job rather than starting a second one it pays for
	// twice — its own comment there calls this "the whole retry-safety story").
	// A repeat call naming a job that already completed is BigQuery's own
	// documented idempotent no-op: answered from the stored job, the query
	// never runs twice.
	var requestedJobID string
	if req.JobReference != nil {
		requestedJobID = req.JobReference.JobId
	}
	if requestedJobID != "" {
		if existing, ok := s.store.GetJob(requestedJobID); ok {
			writeJobResponse(w, existing)
			return
		}
	}
	jobID := requestedJobID
	if jobID == "" {
		jobID = newJobID()
	}

	if req.Configuration.DryRun {
		result, err := s.eng.DryRun(r.Context(), duckSQL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalidQuery", err.Error())
			return
		}

		fields := make([]map[string]any, len(result.Columns))
		for i, col := range result.Columns {
			fields[i] = map[string]any{
				"name": col.Name,
				"type": string(col.Type),
				"mode": "NULLABLE",
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"kind":         "bigquery#job",
			"jobReference": map[string]any{"projectId": project, "jobId": jobID},
			"status":       map[string]any{"state": "DONE"},
			"configuration": map[string]any{
				"query": map[string]any{"query": query},
				"dryRun": true,
			},
			"statistics": map[string]any{
				"totalBytesProcessed": "0",
				"query": map[string]any{
					"totalBytesProcessed": "0",
					"statementType":       stmtType,
				},
			},
		})
		return
	}

	// Non-dry-run: execute synchronously
	job := s.store.CreateJob(project, jobID, query)
	result, err := s.eng.Query(r.Context(), duckSQL)
	s.store.CompleteJob(jobID, result, err)
	meta.RecordJob(r.Context(), s.eng, project, jobID, query, stmtType, err)

	if err != nil {
		writeError(w, http.StatusBadRequest, "invalidQuery", err.Error())
		return
	}

	_ = job
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind":         "bigquery#job",
		"jobReference": map[string]any{"projectId": project, "jobId": jobID},
		"status":       map[string]any{"state": "DONE"},
		"configuration": map[string]any{
			"query": map[string]any{"query": query},
		},
		"statistics": map[string]any{
			"totalBytesProcessed": "0",
			"query": map[string]any{
				"totalBytesProcessed": "0",
				"statementType":       stmtType,
			},
		},
	})
}

// writeJobResponse answers a repeat Jobs.insert call naming an already
// completed job's own id, BigQuery's own documented idempotent no-op for a
// retried insert (gcpbilling's own startQuery, plan.go, is the caller relying
// on it — see its own doc). The query itself never runs a second time; this
// only replays job's already-recorded outcome, success or failure alike, the
// same outcome the first insert call already returned for this id.
func writeJobResponse(w http.ResponseWriter, job *meta.Job) {
	if job.Status.Error != nil {
		writeError(w, http.StatusBadRequest, "invalidQuery", *job.Status.Error)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind":         "bigquery#job",
		"jobReference": job.JobReference,
		"status":       map[string]any{"state": string(job.Status.State)},
		"configuration": map[string]any{
			"query": map[string]any{"query": job.Config.Query},
		},
		"statistics": map[string]any{
			"totalBytesProcessed": "0",
			"query": map[string]any{
				"totalBytesProcessed": "0",
				"statementType":       meta.ClassifyStatement(job.Config.Query),
			},
		},
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	jobID := r.PathValue("jobId")

	job, ok := s.store.GetJob(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "notFound", fmt.Sprintf("job %s not found", jobID))
		return
	}

	resp := map[string]any{
		"kind":         "bigquery#job",
		"jobReference": job.JobReference,
		"status": map[string]any{
			"state": job.Status.State,
		},
		"configuration": map[string]any{
			"query": map[string]any{
				"query": job.Config.Query,
			},
		},
	}
	_ = project
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")

	jobs := s.store.ListJobs(project)
	jobList := make([]any, len(jobs))
	for i, j := range jobs {
		jobList[i] = map[string]any{
			"kind":         "bigquery#job",
			"jobReference": j.JobReference,
			"status": map[string]any{
				"state": j.Status.State,
			},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind": "bigquery#jobList",
		"jobs": jobList,
	})
}

func (s *Server) handleGetQueryResults(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	jobID := r.PathValue("jobId")

	job, ok := s.store.GetJob(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "notFound", fmt.Sprintf("job %s not found", jobID))
		return
	}

	if job.Status.State != meta.JobDone {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"kind":        "bigquery#getQueryResultsResponse",
			"jobReference": job.JobReference,
			"jobComplete": false,
		})
		return
	}

	if job.Status.Error != nil {
		writeError(w, http.StatusBadRequest, "invalidQuery", *job.Status.Error)
		return
	}

	resp := buildQueryResponse(project, jobID, job.Result)
	resp["kind"] = "bigquery#getQueryResultsResponse"

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, "jobs.cancel not yet implemented")
}

// --- Dataset handlers ---

type createDatasetRequest struct {
	DatasetReference datasetRefJSON `json:"datasetReference"`
	Location         string         `json:"location,omitempty"`
}

func (s *Server) handleListDatasets(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")

	datasets := s.store.ListDatasets(project)
	dsList := make([]any, len(datasets))
	for i, ds := range datasets {
		dsList[i] = map[string]any{
			"kind":             "bigquery#dataset",
			"datasetReference": ds.DatasetReference,
			"location":         ds.Location,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind":     "bigquery#datasetList",
		"datasets": dsList,
	})
}

func (s *Server) handleCreateDataset(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")

	var req createDatasetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", fmt.Sprintf("invalid request body: %v", err))
		return
	}

	datasetID := req.DatasetReference.DatasetID
	if datasetID == "" {
		writeError(w, http.StatusBadRequest, "invalid", "datasetReference.datasetId is required")
		return
	}

	location := req.Location
	if location == "" {
		location = "US"
	}

	ds, err := s.store.CreateDataset(r.Context(), project, datasetID, location)
	if err != nil {
		writeError(w, http.StatusConflict, "duplicate", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"kind":             "bigquery#dataset",
		"datasetReference": ds.DatasetReference,
		"location":         ds.Location,
		"creationTime":     ds.CreationTime,
		"lastModifiedTime": ds.LastModifiedTime,
	})
}

func (s *Server) handleGetDataset(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	datasetID := r.PathValue("datasetId")
	_ = project

	ds, ok := s.store.GetDataset(datasetID)
	if !ok {
		writeError(w, http.StatusNotFound, "notFound", fmt.Sprintf("dataset %s not found", datasetID))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind":             "bigquery#dataset",
		"datasetReference": ds.DatasetReference,
		"location":         ds.Location,
		"creationTime":     ds.CreationTime,
		"lastModifiedTime": ds.LastModifiedTime,
	})
}

func (s *Server) handleDeleteDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := r.PathValue("datasetId")

	deleteContents := r.URL.Query().Get("deleteContents") == "true"

	if err := s.store.DeleteDataset(r.Context(), datasetID, deleteContents); err != nil {
		writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Table handlers ---

type createTableRequest struct {
	TableReference struct {
		TableID string `json:"tableId"`
	} `json:"tableReference"`
	Schema struct {
		Fields []struct {
			Name string           `json:"name"`
			Type engine.FieldType `json:"type"`
			Mode string           `json:"mode,omitempty"`
		} `json:"fields"`
	} `json:"schema"`
	TimePartitioning *meta.TimePartitioning `json:"timePartitioning,omitempty"`
}

func (s *Server) handleListTables(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	datasetID := r.PathValue("datasetId")

	refs, err := s.store.ListTables(r.Context(), project, datasetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}

	tblList := make([]any, len(refs))
	for i, ref := range refs {
		tblList[i] = map[string]any{
			"kind":           "bigquery#table",
			"tableReference": ref,
			"type":           "TABLE",
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind":   "bigquery#tableList",
		"tables": tblList,
	})
}

func (s *Server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	datasetID := r.PathValue("datasetId")

	var req createTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", fmt.Sprintf("invalid request body: %v", err))
		return
	}

	tableID := req.TableReference.TableID
	if tableID == "" {
		writeError(w, http.StatusBadRequest, "invalid", "tableReference.tableId is required")
		return
	}

	if len(req.Schema.Fields) == 0 {
		writeError(w, http.StatusBadRequest, "invalid", "schema.fields is required")
		return
	}

	// Recorded before the DDL attempt below, not after: a caller's Tables.insert
	// names its intended TimePartitioning every time it calls, including a
	// repeat call against a table that already physically exists (gcpseed's
	// own ensureTable calls Tables.insert on every pass and tolerates the
	// "already exists" CREATE TABLE failure that follows, by design -- see
	// tools/internal/gcpdemoestate.TableInsertMetadata's own doc). Gating this
	// on the DDL succeeding would silently drop the caller's declared
	// partitioning on every call after the table's first, physical creation.
	s.store.SetTablePartitioning(datasetID, tableID, req.TimePartitioning)

	// Build CREATE TABLE DDL
	ddl := buildCreateTableDDL(datasetID, tableID, req.Schema.Fields)
	if err := s.eng.Exec(r.Context(), ddl); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}

	tbl, err := s.store.GetTable(r.Context(), project, datasetID, tableID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	resp := map[string]any{
		"kind":           "bigquery#table",
		"tableReference": tbl.TableReference,
		"schema":         tbl.Schema,
		"type":           tbl.Type,
		"creationTime":   tbl.CreationTime,
	}
	if tbl.TimePartitioning != nil {
		resp["timePartitioning"] = tbl.TimePartitioning
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleGetTable(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	datasetID := r.PathValue("datasetId")
	tableID := r.PathValue("tableId")

	tbl, err := s.store.GetTable(r.Context(), project, datasetID, tableID)
	if err != nil {
		writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}

	resp := map[string]any{
		"kind":           "bigquery#table",
		"tableReference": tbl.TableReference,
		"schema":         tbl.Schema,
		"type":           tbl.Type,
		"numRows":        tbl.NumRows,
		"creationTime":   tbl.CreationTime,
	}
	if tbl.TimePartitioning != nil {
		resp["timePartitioning"] = tbl.TimePartitioning
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDeleteTable(w http.ResponseWriter, r *http.Request) {
	datasetID := r.PathValue("datasetId")
	tableID := r.PathValue("tableId")

	if err := s.store.DeleteTable(r.Context(), datasetID, tableID); err != nil {
		writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListTableData(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	datasetID := r.PathValue("datasetId")
	tableID := r.PathValue("tableId")

	query := fmt.Sprintf("SELECT * FROM %s.%s", quoteIdent(datasetID), quoteIdent(tableID))

	if limitStr := r.URL.Query().Get("maxResults"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			query += fmt.Sprintf(" LIMIT %d", limit)
		}
	}

	result, err := s.eng.Query(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}

	resp := buildQueryResponse(project, "", result)
	resp["kind"] = "bigquery#tableDataList"
	delete(resp, "jobReference")
	delete(resp, "jobComplete")
	delete(resp, "cacheHit")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- Catch-all ---

func (s *Server) handleUnsupported(w http.ResponseWriter, r *http.Request) {
	writeUnsupported(w, fmt.Sprintf("endpoint not supported: %s %s", r.Method, r.URL.Path))
}

// --- Response builders ---

// buildQueryResponse builds a BigQuery QueryResponse from an engine.Result.
func buildQueryResponse(project, jobID string, result *engine.Result) map[string]any {
	// Build schema
	fields := make([]map[string]any, len(result.Columns))
	for i, col := range result.Columns {
		fields[i] = map[string]any{
			"name": col.Name,
			"type": string(col.Type),
			"mode": "NULLABLE",
		}
	}

	// Build rows in BigQuery f/v format
	rows := make([]map[string]any, len(result.Rows))
	for i, row := range result.Rows {
		fVals := make([]map[string]any, len(row))
		for j, val := range row {
			fVals[j] = map[string]any{"v": formatValue(val)}
		}
		rows[i] = map[string]any{"f": fVals}
	}

	resp := map[string]any{
		"kind": "bigquery#queryResponse",
		"schema": map[string]any{
			"fields": fields,
		},
		"totalRows":          strconv.Itoa(len(result.Rows)),
		"jobComplete":        true,
		"cacheHit":           false,
		"totalBytesProcessed": "0",
	}

	if jobID != "" {
		resp["jobReference"] = map[string]any{
			"projectId": project,
			"jobId":     jobID,
		}
	}

	if len(rows) > 0 {
		resp["rows"] = rows
	}

	return resp
}

// formatValue converts a Go value to a BigQuery string representation.
// BQ API encodes all values as strings in the f/v row format.
func formatValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(val, 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case []byte:
		return string(val)
	case time.Time:
		// BigQuery's REST API encodes TIMESTAMP as a floating-point number of
		// seconds since epoch (a string, like every other field this handler
		// emits) and DATE as "YYYY-MM-DD". This used to emit UnixMicro() as a
		// bare integer string, which is not that convention (nor any other
		// documented BigQuery wire shape) and silently produced a wildly wrong
		// date in any downstream client that parsed it as seconds, the way
		// invisibl-aether's gcpbilling.parseBillingTime's numeric fallback does.
		if val.Hour() == 0 && val.Minute() == 0 && val.Second() == 0 && val.Nanosecond() == 0 {
			return val.Format("2006-01-02")
		}
		seconds := float64(val.UnixNano()) / 1e9
		return strconv.FormatFloat(seconds, 'f', 6, 64)
	case duckdb.Decimal:
		// go-duckdb's Decimal carries Width/Scale/Value (a *big.Int) and only
		// renders correctly through its own String(), which has a pointer
		// receiver — fmt's default formatting of a non-addressable interface
		// value falls back to printing the struct fields raw (e.g. "{18 3
		// 4000}" for 4.000), which is not a valid BigQuery NUMERIC/BIGNUMERIC
		// wire value. val is a local copy here, so it is addressable.
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

// buildCreateTableDDL generates a CREATE TABLE statement from BQ field specs.
func buildCreateTableDDL(schema, table string, fields []struct {
	Name string           `json:"name"`
	Type engine.FieldType `json:"type"`
	Mode string           `json:"mode,omitempty"`
}) string {
	ddl := fmt.Sprintf("CREATE TABLE %s.%s (", quoteIdent(schema), quoteIdent(table))
	for i, f := range fields {
		if i > 0 {
			ddl += ", "
		}
		ddl += quoteIdent(f.Name) + " " + bqTypeToDuckDB(f.Type)
	}
	ddl += ")"
	return ddl
}

// bqTypeToDuckDB maps BigQuery types to DuckDB types for CREATE TABLE.
func bqTypeToDuckDB(bqType engine.FieldType) string {
	switch bqType {
	case engine.TypeString:
		return "VARCHAR"
	case engine.TypeBytes:
		return "BLOB"
	case engine.TypeInteger:
		return "BIGINT"
	case engine.TypeFloat:
		return "DOUBLE"
	case engine.TypeBoolean:
		return "BOOLEAN"
	case engine.TypeTimestamp:
		return "TIMESTAMP"
	case engine.TypeDate:
		return "DATE"
	case engine.TypeTime:
		return "TIME"
	case engine.TypeDatetime:
		return "TIMESTAMP"
	case engine.TypeNumeric:
		return "DECIMAL(38,9)"
	case engine.TypeJSON:
		return "JSON"
	default:
		return "VARCHAR"
	}
}

func quoteIdent(name string) string {
	return `"` + name + `"`
}

// --- Helpers ---

var jobCounter int64
var jobMu sync.Mutex

func newJobID() string {
	jobMu.Lock()
	defer jobMu.Unlock()
	jobCounter++
	return fmt.Sprintf("job_%d", jobCounter)
}

// --- Error responses ---

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
