package api

import (
	"encoding/json"
	"net/http"
)

// registerDiscovery adds the API discovery endpoint that the bq CLI requires.
// The bq CLI fetches $discovery/rest?version=v2 before making any API calls.
func registerDiscovery(mux *http.ServeMux) {
	mux.HandleFunc("GET /$discovery/rest", handleDiscovery)
}

func handleDiscovery(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	rootURL := scheme + "://" + host + "/"
	baseURL := rootURL + "bigquery/v2/"

	doc := buildDiscoveryDoc(rootURL, baseURL)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

func buildDiscoveryDoc(rootURL, baseURL string) map[string]any {
	return map[string]any{
		"kind":             "discovery#restDescription",
		"discoveryVersion": "v1",
		"id":               "bigquery:v2",
		"name":             "bigquery",
		"version":          "v2",
		"title":            "BigQuery API (LocalBQ)",
		"description":      "Local BigQuery emulator powered by DuckDB",
		"rootUrl":          rootURL,
		"baseUrl":          baseURL,
		"basePath":         "/bigquery/v2/",
		"servicePath":      "bigquery/v2/",
		"batchPath":        "batch/bigquery/v2",
		"protocol":         "rest",
		"parameters":       discoveryGlobalParams(),
		"schemas":          discoverySchemas(),
		"resources":        discoveryResources(),
	}
}

func discoveryGlobalParams() map[string]any {
	return map[string]any{
		"alt": map[string]any{
			"type":        "string",
			"description": "Data format for the response.",
			"default":     "json",
			"location":    "query",
		},
		"prettyPrint": map[string]any{
			"type":        "boolean",
			"description": "Returns response with indentations and line breaks.",
			"default":     "true",
			"location":    "query",
		},
	}
}

func discoverySchemas() map[string]any {
	return map[string]any{
		"QueryRequest": map[string]any{
			"id":   "QueryRequest",
			"type": "object",
			"properties": map[string]any{
				"query":        map[string]any{"type": "string"},
				"useLegacySql": map[string]any{"type": "boolean"},
				"dryRun":       map[string]any{"type": "boolean"},
				"maxResults":   map[string]any{"type": "integer", "format": "uint32"},
			},
		},
		"QueryResponse": map[string]any{
			"id":   "QueryResponse",
			"type": "object",
			"properties": map[string]any{
				"kind":        map[string]any{"type": "string"},
				"schema":      map[string]any{"$ref": "TableSchema"},
				"rows":        map[string]any{"type": "array", "items": map[string]any{"$ref": "TableRow"}},
				"totalRows":   map[string]any{"type": "string", "format": "uint64"},
				"jobComplete": map[string]any{"type": "boolean"},
				"jobReference": map[string]any{"$ref": "JobReference"},
			},
		},
		"TableSchema": map[string]any{
			"id":   "TableSchema",
			"type": "object",
			"properties": map[string]any{
				"fields": map[string]any{"type": "array", "items": map[string]any{"$ref": "TableFieldSchema"}},
			},
		},
		"TableFieldSchema": map[string]any{
			"id":   "TableFieldSchema",
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"type": map[string]any{"type": "string"},
				"mode": map[string]any{"type": "string"},
			},
		},
		"TableRow": map[string]any{
			"id":   "TableRow",
			"type": "object",
			"properties": map[string]any{
				"f": map[string]any{"type": "array", "items": map[string]any{"$ref": "TableCell"}},
			},
		},
		"TableCell": map[string]any{
			"id":   "TableCell",
			"type": "object",
			"properties": map[string]any{
				"v": map[string]any{},
			},
		},
		"JobReference": map[string]any{
			"id":   "JobReference",
			"type": "object",
			"properties": map[string]any{
				"projectId": map[string]any{"type": "string"},
				"jobId":     map[string]any{"type": "string"},
				"location":  map[string]any{"type": "string"},
			},
		},
		"Job": map[string]any{
			"id":   "Job",
			"type": "object",
			"properties": map[string]any{
				"kind":          map[string]any{"type": "string"},
				"jobReference":  map[string]any{"$ref": "JobReference"},
				"status":        map[string]any{"$ref": "JobStatus"},
				"configuration": map[string]any{"$ref": "JobConfiguration"},
				"statistics":    map[string]any{"$ref": "JobStatistics"},
			},
		},
		"JobStatus": map[string]any{
			"id":   "JobStatus",
			"type": "object",
			"properties": map[string]any{
				"state":       map[string]any{"type": "string"},
				"errorResult": map[string]any{"$ref": "ErrorProto"},
			},
		},
		"ErrorProto": map[string]any{
			"id":   "ErrorProto",
			"type": "object",
			"properties": map[string]any{
				"reason":  map[string]any{"type": "string"},
				"message": map[string]any{"type": "string"},
			},
		},
		"JobConfiguration": map[string]any{
			"id":   "JobConfiguration",
			"type": "object",
			"properties": map[string]any{
				"query":  map[string]any{"$ref": "JobConfigurationQuery"},
				"dryRun": map[string]any{"type": "boolean"},
			},
		},
		"JobConfigurationQuery": map[string]any{
			"id":   "JobConfigurationQuery",
			"type": "object",
			"properties": map[string]any{
				"query":        map[string]any{"type": "string"},
				"useLegacySql": map[string]any{"type": "boolean"},
			},
		},
		"JobStatistics": map[string]any{
			"id":   "JobStatistics",
			"type": "object",
			"properties": map[string]any{
				"totalBytesProcessed": map[string]any{"type": "string", "format": "int64"},
			},
		},
		"Dataset": map[string]any{
			"id":   "Dataset",
			"type": "object",
			"properties": map[string]any{
				"kind":             map[string]any{"type": "string"},
				"datasetReference": map[string]any{"$ref": "DatasetReference"},
				"location":         map[string]any{"type": "string"},
			},
		},
		"DatasetReference": map[string]any{
			"id":   "DatasetReference",
			"type": "object",
			"properties": map[string]any{
				"projectId": map[string]any{"type": "string"},
				"datasetId": map[string]any{"type": "string"},
			},
		},
		"DatasetList": map[string]any{
			"id":   "DatasetList",
			"type": "object",
			"properties": map[string]any{
				"kind":     map[string]any{"type": "string"},
				"datasets": map[string]any{"type": "array", "items": map[string]any{"$ref": "Dataset"}},
			},
		},
		"Table": map[string]any{
			"id":   "Table",
			"type": "object",
			"properties": map[string]any{
				"kind":           map[string]any{"type": "string"},
				"tableReference": map[string]any{"$ref": "TableReference"},
				"schema":         map[string]any{"$ref": "TableSchema"},
			},
		},
		"TableReference": map[string]any{
			"id":   "TableReference",
			"type": "object",
			"properties": map[string]any{
				"projectId": map[string]any{"type": "string"},
				"datasetId": map[string]any{"type": "string"},
				"tableId":   map[string]any{"type": "string"},
			},
		},
		"TableList": map[string]any{
			"id":   "TableList",
			"type": "object",
			"properties": map[string]any{
				"kind":   map[string]any{"type": "string"},
				"tables": map[string]any{"type": "array", "items": map[string]any{"$ref": "Table"}},
			},
		},
		"JobList": map[string]any{
			"id":   "JobList",
			"type": "object",
			"properties": map[string]any{
				"kind": map[string]any{"type": "string"},
				"jobs": map[string]any{"type": "array", "items": map[string]any{"$ref": "Job"}},
			},
		},
	}
}

func discoveryResources() map[string]any {
	p := func(name, typ, loc string) map[string]any {
		return map[string]any{"type": typ, "location": loc, "required": true}
	}
	projectParam := p("projectId", "string", "path")
	datasetParam := p("datasetId", "string", "path")
	tableParam := p("tableId", "string", "path")
	jobParam := p("jobId", "string", "path")

	return map[string]any{
		"jobs": map[string]any{
			"methods": map[string]any{
				"query": map[string]any{
					"id":         "bigquery.jobs.query",
					"path":       "projects/{+projectId}/queries",
					"httpMethod": "POST",
					"parameters": map[string]any{"projectId": projectParam},
					"request":    map[string]any{"$ref": "QueryRequest"},
					"response":   map[string]any{"$ref": "QueryResponse"},
				},
				"insert": map[string]any{
					"id":         "bigquery.jobs.insert",
					"path":       "projects/{+projectId}/jobs",
					"httpMethod": "POST",
					"parameters": map[string]any{"projectId": projectParam},
					"request":    map[string]any{"$ref": "Job"},
					"response":   map[string]any{"$ref": "Job"},
					"mediaUpload": map[string]any{
						"accept":    []string{"*/*"},
						"protocols": map[string]any{
							"simple":    map[string]any{"multipart": true, "path": "/upload/bigquery/v2/projects/{+projectId}/jobs"},
							"resumable": map[string]any{"multipart": true, "path": "/resumable/upload/bigquery/v2/projects/{+projectId}/jobs"},
						},
					},
				},
				"get": map[string]any{
					"id":         "bigquery.jobs.get",
					"path":       "projects/{+projectId}/jobs/{+jobId}",
					"httpMethod": "GET",
					"parameters": map[string]any{
						"projectId": projectParam,
						"jobId":     jobParam,
						"location":  map[string]any{"type": "string", "location": "query"},
					},
					"response": map[string]any{"$ref": "Job"},
				},
				"list": map[string]any{
					"id":         "bigquery.jobs.list",
					"path":       "projects/{+projectId}/jobs",
					"httpMethod": "GET",
					"parameters": map[string]any{"projectId": projectParam},
					"response":   map[string]any{"$ref": "JobList"},
				},
				"getQueryResults": map[string]any{
					"id":         "bigquery.jobs.getQueryResults",
					"path":       "projects/{+projectId}/queries/{+jobId}",
					"httpMethod": "GET",
					"parameters": map[string]any{
						"projectId":  projectParam,
						"jobId":      jobParam,
						"location":   map[string]any{"type": "string", "location": "query"},
						"startIndex": map[string]any{"type": "string", "format": "uint64", "location": "query"},
						"maxResults": map[string]any{"type": "integer", "format": "uint32", "location": "query"},
						"timeoutMs":  map[string]any{"type": "integer", "format": "uint32", "location": "query"},
					},
					"response": map[string]any{"$ref": "QueryResponse"},
				},
				"cancel": map[string]any{
					"id":         "bigquery.jobs.cancel",
					"path":       "projects/{+projectId}/jobs/{+jobId}/cancel",
					"httpMethod": "POST",
					"parameters": map[string]any{"projectId": projectParam, "jobId": jobParam},
				},
			},
		},
		"datasets": map[string]any{
			"methods": map[string]any{
				"list": map[string]any{
					"id":         "bigquery.datasets.list",
					"path":       "projects/{+projectId}/datasets",
					"httpMethod": "GET",
					"parameters": map[string]any{"projectId": projectParam},
					"response":   map[string]any{"$ref": "DatasetList"},
				},
				"insert": map[string]any{
					"id":         "bigquery.datasets.insert",
					"path":       "projects/{+projectId}/datasets",
					"httpMethod": "POST",
					"parameters": map[string]any{"projectId": projectParam},
					"request":    map[string]any{"$ref": "Dataset"},
					"response":   map[string]any{"$ref": "Dataset"},
				},
				"get": map[string]any{
					"id":         "bigquery.datasets.get",
					"path":       "projects/{+projectId}/datasets/{+datasetId}",
					"httpMethod": "GET",
					"parameters": map[string]any{"projectId": projectParam, "datasetId": datasetParam},
					"response":   map[string]any{"$ref": "Dataset"},
				},
				"delete": map[string]any{
					"id":         "bigquery.datasets.delete",
					"path":       "projects/{+projectId}/datasets/{+datasetId}",
					"httpMethod": "DELETE",
					"parameters": map[string]any{"projectId": projectParam, "datasetId": datasetParam},
				},
			},
		},
		"tables": map[string]any{
			"methods": map[string]any{
				"list": map[string]any{
					"id":         "bigquery.tables.list",
					"path":       "projects/{+projectId}/datasets/{+datasetId}/tables",
					"httpMethod": "GET",
					"parameters": map[string]any{"projectId": projectParam, "datasetId": datasetParam},
					"response":   map[string]any{"$ref": "TableList"},
				},
				"insert": map[string]any{
					"id":         "bigquery.tables.insert",
					"path":       "projects/{+projectId}/datasets/{+datasetId}/tables",
					"httpMethod": "POST",
					"parameters": map[string]any{"projectId": projectParam, "datasetId": datasetParam},
					"request":    map[string]any{"$ref": "Table"},
					"response":   map[string]any{"$ref": "Table"},
				},
				"get": map[string]any{
					"id":         "bigquery.tables.get",
					"path":       "projects/{+projectId}/datasets/{+datasetId}/tables/{+tableId}",
					"httpMethod": "GET",
					"parameters": map[string]any{"projectId": projectParam, "datasetId": datasetParam, "tableId": tableParam},
					"response":   map[string]any{"$ref": "Table"},
				},
				"delete": map[string]any{
					"id":         "bigquery.tables.delete",
					"path":       "projects/{+projectId}/datasets/{+datasetId}/tables/{+tableId}",
					"httpMethod": "DELETE",
					"parameters": map[string]any{"projectId": projectParam, "datasetId": datasetParam, "tableId": tableParam},
				},
			},
		},
		"tabledata": map[string]any{
			"methods": map[string]any{
				"list": map[string]any{
					"id":         "bigquery.tabledata.list",
					"path":       "projects/{+projectId}/datasets/{+datasetId}/tables/{+tableId}/data",
					"httpMethod": "GET",
					"parameters": map[string]any{"projectId": projectParam, "datasetId": datasetParam, "tableId": tableParam},
				},
			},
		},
	}
}
