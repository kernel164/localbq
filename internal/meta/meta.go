// Package meta implements the metadata and control plane (Layer 4).
//
// Manages: projects, datasets, tables, jobs.
// Datasets map to DuckDB schemas. Tables map to DuckDB tables within schemas.
// Jobs are tracked in-memory with a 1-hour TTL.
package meta

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/slokam-ai/localbq/internal/engine"
)

// DatasetRef identifies a dataset.
type DatasetRef struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
}

// Dataset holds BigQuery dataset metadata.
type Dataset struct {
	DatasetReference DatasetRef `json:"datasetReference"`
	Location         string     `json:"location,omitempty"`
	CreationTime     string     `json:"creationTime"`
	LastModifiedTime string     `json:"lastModifiedTime"`
}

// TableRef identifies a table.
type TableRef struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
	TableID   string `json:"tableId"`
}

// TableFieldSchema describes a column in a BigQuery table.
type TableFieldSchema struct {
	Name string           `json:"name"`
	Type engine.FieldType `json:"type"`
	Mode string           `json:"mode"`
}

// TableSchema is the schema of a BigQuery table.
type TableSchema struct {
	Fields []TableFieldSchema `json:"fields"`
}

// Table holds BigQuery table metadata.
type Table struct {
	TableReference   TableRef          `json:"tableReference"`
	Schema           TableSchema       `json:"schema"`
	NumRows          string            `json:"numRows"`
	CreationTime     string            `json:"creationTime"`
	Type             string            `json:"type"`
	TimePartitioning *TimePartitioning `json:"timePartitioning,omitempty"`
}

// TimePartitioning is the subset of BigQuery's own TimePartitioning wire
// shape (google.golang.org/api/bigquery/v2.TimePartitioning) this store
// round-trips: Type and Field, the two fields a client reads back to tell an
// ingestion-time partitioned table (Field empty, per that type's own doc:
// "If not set, the table is partitioned by pseudo column '_PARTITIONTIME'")
// from a field-partitioned one apart. DuckDB has no native partitioning
// concept, so this carries no engine behavior of its own -- Tables.insert's
// declared value is simply held here so a later Tables.get echoes back the
// same answer a real detailed billing export would, rather than always
// looking unpartitioned-by-ingestion-time the way every table did before
// this existed, which sent invisibl-aether's gcpbilling adapter down its
// _PARTITIONTIME branch even for a table field-partitioned on export_time.
type TimePartitioning struct {
	Type  string `json:"type,omitempty"`
	Field string `json:"field,omitempty"`
}

// JobState represents the state of a BigQuery job.
type JobState string

const (
	JobPending JobState = "PENDING"
	JobRunning JobState = "RUNNING"
	JobDone    JobState = "DONE"
)

// JobRef identifies a job.
type JobRef struct {
	ProjectID string `json:"projectId"`
	JobID     string `json:"jobId"`
}

// Job holds BigQuery job metadata.
type Job struct {
	JobReference JobRef        `json:"jobReference"`
	Status       JobStatus     `json:"status"`
	Config       JobConfig     `json:"configuration"`
	Result       *engine.Result `json:"-"`
	CreatedAt    time.Time     `json:"-"`
}

// JobStatus holds the current state of a job.
type JobStatus struct {
	State JobState `json:"state"`
	Error *string  `json:"errorResult,omitempty"`
}

// JobConfig holds job configuration.
type JobConfig struct {
	Query string `json:"query,omitempty"`
}

// Store manages metadata backed by a DuckDB engine.
type Store struct {
	engine   *engine.Engine
	mu       sync.RWMutex
	jobs     map[string]*Job
	datasets map[string]*Dataset // key: datasetId
	// partitioning holds each table's declared TimePartitioning, keyed by
	// "datasetId.tableId". Nothing else about a table is tracked here --
	// GetTable derives the rest fresh from DuckDB on every call -- because
	// TimePartitioning is the one property Tables.insert can declare that
	// DuckDB's own schema has no column for at all.
	partitioning map[string]*TimePartitioning
}

// NewStore creates a metadata store backed by the given engine.
func NewStore(eng *engine.Engine) *Store {
	return &Store{
		engine:       eng,
		jobs:         make(map[string]*Job),
		datasets:     make(map[string]*Dataset),
		partitioning: make(map[string]*TimePartitioning),
	}
}

// --- Dataset operations ---

// CreateDataset creates a new dataset (DuckDB schema).
func (s *Store) CreateDataset(ctx context.Context, projectID, datasetID, location string) (*Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.datasets[datasetID]; exists {
		return nil, fmt.Errorf("dataset %s already exists", datasetID)
	}

	if err := s.engine.CreateSchema(ctx, datasetID); err != nil {
		return nil, err
	}

	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	ds := &Dataset{
		DatasetReference: DatasetRef{
			ProjectID: projectID,
			DatasetID: datasetID,
		},
		Location:         location,
		CreationTime:     now,
		LastModifiedTime: now,
	}
	s.datasets[datasetID] = ds
	return ds, nil
}

// GetDataset returns a dataset by ID.
func (s *Store) GetDataset(datasetID string) (*Dataset, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, ok := s.datasets[datasetID]
	return ds, ok
}

// ListDatasets returns all datasets.
func (s *Store) ListDatasets(projectID string) []*Dataset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Dataset
	for _, ds := range s.datasets {
		result = append(result, ds)
	}
	return result
}

// DeleteDataset deletes a dataset and its schema.
func (s *Store) DeleteDataset(ctx context.Context, datasetID string, deleteContents bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.datasets[datasetID]; !exists {
		return fmt.Errorf("dataset %s not found", datasetID)
	}

	if err := s.engine.DropSchema(ctx, datasetID, deleteContents); err != nil {
		return err
	}

	delete(s.datasets, datasetID)
	return nil
}

// --- Table operations ---

// partitioningKey is the tablePartitioning map key for one table.
func partitioningKey(datasetID, tableID string) string {
	return datasetID + "." + tableID
}

// SetTablePartitioning records the TimePartitioning a Tables.insert call
// declared for one table, so a later GetTable echoes it back. A nil tp
// clears the entry, matching an unpartitioned table.
func (s *Store) SetTablePartitioning(datasetID, tableID string, tp *TimePartitioning) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := partitioningKey(datasetID, tableID)
	if tp == nil {
		delete(s.partitioning, key)
		return
	}
	s.partitioning[key] = tp
}

// GetTable returns table metadata by reading it from DuckDB.
func (s *Store) GetTable(ctx context.Context, projectID, datasetID, tableID string) (*Table, error) {
	exists, err := s.engine.TableExists(ctx, datasetID, tableID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("table %s.%s not found", datasetID, tableID)
	}

	cols, err := s.engine.TableColumns(ctx, datasetID, tableID)
	if err != nil {
		return nil, err
	}

	fields := make([]TableFieldSchema, len(cols))
	for i, c := range cols {
		fields[i] = TableFieldSchema{
			Name: c.Name,
			Type: c.Type,
			Mode: "NULLABLE",
		}
	}

	s.mu.RLock()
	tp := s.partitioning[partitioningKey(datasetID, tableID)]
	s.mu.RUnlock()

	return &Table{
		TableReference: TableRef{
			ProjectID: projectID,
			DatasetID: datasetID,
			TableID:   tableID,
		},
		Schema:           TableSchema{Fields: fields},
		CreationTime:     strconv.FormatInt(time.Now().UnixMilli(), 10),
		Type:             "TABLE",
		TimePartitioning: tp,
	}, nil
}

// ListTables returns all tables in a dataset.
func (s *Store) ListTables(ctx context.Context, projectID, datasetID string) ([]TableRef, error) {
	tables, err := s.engine.ListTables(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	refs := make([]TableRef, len(tables))
	for i, t := range tables {
		refs[i] = TableRef{
			ProjectID: projectID,
			DatasetID: datasetID,
			TableID:   t,
		}
	}
	return refs, nil
}

// DeleteTable deletes a table.
func (s *Store) DeleteTable(ctx context.Context, datasetID, tableID string) error {
	exists, err := s.engine.TableExists(ctx, datasetID, tableID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("table %s.%s not found", datasetID, tableID)
	}
	return s.engine.DropTable(ctx, datasetID, tableID)
}

// --- Job operations ---

// CreateJob creates a new job and stores it.
func (s *Store) CreateJob(projectID, jobID, query string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := &Job{
		JobReference: JobRef{
			ProjectID: projectID,
			JobID:     jobID,
		},
		Status: JobStatus{State: JobRunning},
		Config: JobConfig{Query: query},
		CreatedAt: time.Now(),
	}
	s.jobs[jobID] = job
	return job
}

// CompleteJob marks a job as done with its result.
func (s *Store) CompleteJob(jobID string, result *engine.Result, jobErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	job.Status.State = JobDone
	if jobErr != nil {
		msg := jobErr.Error()
		job.Status.Error = &msg
	} else {
		job.Result = result
	}
}

// GetJob returns a job by ID.
func (s *Store) GetJob(jobID string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	return job, ok
}

// ListJobs returns all jobs for a project.
func (s *Store) ListJobs(projectID string) []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Job
	for _, job := range s.jobs {
		if job.JobReference.ProjectID == projectID {
			result = append(result, job)
		}
	}
	return result
}
