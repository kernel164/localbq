package lowering

import (
	"testing"
)

func TestLower(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Backtick conversion
		{
			name:  "backtick to double quote",
			input: "SELECT `my col` FROM `my table`",
			want:  `SELECT "my col" FROM "my table"`,
		},
		{
			name:  "backtick with dot (project.dataset.table)",
			input: "SELECT * FROM `project.dataset.table`",
			want:  `SELECT * FROM "project.dataset.table"`,
		},
		{
			name:  "backtick inside string unchanged",
			input: "SELECT 'has `backtick` inside'",
			want:  "SELECT 'has `backtick` inside'",
		},

		// Regional INFORMATION_SCHEMA
		{
			name:  "region prefix stripped",
			input: "SELECT * FROM `region-us`.INFORMATION_SCHEMA.JOBS_BY_PROJECT",
			want:  `SELECT * FROM information_schema.JOBS_BY_PROJECT`,
		},
		{
			name:  "region prefix with east",
			input: "SELECT * FROM region-us-east1.INFORMATION_SCHEMA.TABLE_STORAGE",
			want:  `SELECT * FROM information_schema.TABLE_STORAGE`,
		},
		{
			name:  "quoted region prefix",
			input: `SELECT * FROM "region-us-east1".INFORMATION_SCHEMA.TABLES`,
			want:  `SELECT * FROM information_schema.TABLES`,
		},

		// TIMESTAMP_SUB
		{
			name:  "TIMESTAMP_SUB basic",
			input: "WHERE creation_time >= TIMESTAMP_SUB(CURRENT_TIMESTAMP, INTERVAL 7 DAY)",
			want:  "WHERE creation_time >= (CURRENT_TIMESTAMP::TIMESTAMP - INTERVAL '7' DAY)",
		},
		{
			name:  "TIMESTAMP_SUB with hours",
			input: "WHERE ts > TIMESTAMP_SUB(CURRENT_TIMESTAMP, INTERVAL 12 HOUR)",
			want:  "WHERE ts > (CURRENT_TIMESTAMP::TIMESTAMP - INTERVAL '12' HOUR)",
		},
		{
			name:  "TIMESTAMP_SUB case insensitive",
			input: "timestamp_sub(CURRENT_TIMESTAMP, INTERVAL 1 MINUTE)",
			want:  "(CURRENT_TIMESTAMP::TIMESTAMP - INTERVAL '1' MINUTE)",
		},

		// TIMESTAMP_ADD
		{
			name:  "TIMESTAMP_ADD",
			input: "TIMESTAMP_ADD(ts, INTERVAL 3 HOUR)",
			want:  "(ts::TIMESTAMP + INTERVAL '3' HOUR)",
		},

		// TIMESTAMP_TRUNC
		{
			name:  "TIMESTAMP_TRUNC to day",
			input: "TIMESTAMP_TRUNC(CURRENT_TIMESTAMP, DAY)",
			want:  "date_trunc('day', CURRENT_TIMESTAMP::TIMESTAMP)",
		},
		{
			name:  "TIMESTAMP_TRUNC to month",
			input: "WHERE ts >= TIMESTAMP_TRUNC(CURRENT_TIMESTAMP, MONTH)",
			want:  "WHERE ts >= date_trunc('month', CURRENT_TIMESTAMP::TIMESTAMP)",
		},

		// DATE_SUB
		{
			name:  "DATE_SUB",
			input: "DATE_SUB(CURRENT_DATE, INTERVAL 30 DAY)",
			want:  "(CURRENT_DATE - INTERVAL '30' DAY)",
		},

		// DATE_ADD
		{
			name:  "DATE_ADD",
			input: "DATE_ADD(d, INTERVAL 1 YEAR)",
			want:  "(d + INTERVAL '1' YEAR)",
		},

		// DATE()
		{
			name:  "DATE function",
			input: "SELECT DATE(creation_time) AS day",
			want:  "SELECT CAST(creation_time AS DATE) AS day",
		},

		// COUNTIF
		{
			name:  "COUNTIF",
			input: "SELECT COUNTIF(status = 'DONE')",
			want:  "SELECT count_if(status = 'DONE')",
		},
		{
			name:  "countif lowercase",
			input: "SELECT countif(x > 0)",
			want:  "SELECT count_if(x > 0)",
		},

		// SAFE_CAST
		{
			name:  "SAFE_CAST",
			input: "SELECT SAFE_CAST(x AS INTEGER)",
			want:  "SELECT TRY_CAST(x AS INTEGER)",
		},

		// CURRENT_TIMESTAMP()
		{
			name:  "CURRENT_TIMESTAMP() parens",
			input: "SELECT CURRENT_TIMESTAMP()",
			want:  "SELECT CURRENT_TIMESTAMP",
		},
		{
			name:  "CURRENT_DATE() parens",
			input: "SELECT CURRENT_DATE()",
			want:  "SELECT CURRENT_DATE",
		},

		// Type rewrites
		{
			name:  "FLOAT64 to DOUBLE",
			input: "SELECT CAST(x AS FLOAT64)",
			want:  "SELECT CAST(x AS DOUBLE)",
		},
		{
			name:  "BOOL to BOOLEAN",
			input: "SELECT CAST(x AS BOOL)",
			want:  "SELECT CAST(x AS BOOLEAN)",
		},
		{
			name:  "BOOLEAN unchanged",
			input: "SELECT CAST(x AS BOOLEAN)",
			want:  "SELECT CAST(x AS BOOLEAN)",
		},

		// __TABLES__
		{
			name:  "legacy __TABLES__",
			input: "SELECT * FROM dataset.__TABLES__",
			want:  "SELECT * FROM dataset.information_schema.tables",
		},

		// Three-part references
		{
			name:  "three-part quoted ref",
			input: `SELECT * FROM "myproject"."mydata"."users"`,
			want:  `SELECT * FROM "mydata"."users"`,
		},

		// Combined (Cascade-style query)
		{
			name: "cascade cost query",
			input: "SELECT DATE(creation_time) AS day, SUM(total_bytes_billed) / POW(1024, 4) * 6.25 AS cost_usd " +
				"FROM `region-us`.INFORMATION_SCHEMA.JOBS_BY_PROJECT " +
				"WHERE creation_time >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 7 DAY) " +
				"AND job_type = 'QUERY' AND state = 'DONE' GROUP BY day ORDER BY day",
			want: "SELECT CAST(creation_time AS DATE) AS day, SUM(total_bytes_billed) / POW(1024, 4) * 6.25 AS cost_usd " +
				"FROM information_schema.JOBS_BY_PROJECT " +
				"WHERE creation_time >= (CURRENT_TIMESTAMP::TIMESTAMP - INTERVAL '7' DAY) " +
				"AND job_type = 'QUERY' AND state = 'DONE' GROUP BY day ORDER BY day",
		},
		{
			name: "cascade failed jobs query",
			input: "SELECT job_id, error_result.reason FROM `region-us`.INFORMATION_SCHEMA.JOBS_BY_PROJECT " +
				"WHERE error_result IS NOT NULL AND creation_time > TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 12 HOUR)",
			want: "SELECT job_id, error_result.reason FROM information_schema.JOBS_BY_PROJECT " +
				"WHERE error_result IS NOT NULL AND creation_time > (CURRENT_TIMESTAMP::TIMESTAMP - INTERVAL '12' HOUR)",
		},

		// LOAD DATA
		{
			name:  "LOAD DATA parquet",
			input: "LOAD DATA INTO mydata.users FROM '/tmp/users.parquet'",
			want:  "INSERT INTO mydata.users SELECT * FROM read_parquet('/tmp/users.parquet')",
		},
		{
			name:  "LOAD DATA csv",
			input: "LOAD DATA INTO ds.events FROM '/data/events.csv'",
			want:  "INSERT INTO ds.events SELECT * FROM read_csv('/data/events.csv')",
		},
		{
			name:  "LOAD DATA json",
			input: "LOAD DATA INTO ds.logs FROM '/data/logs.jsonl'",
			want:  "INSERT INTO ds.logs SELECT * FROM read_json('/data/logs.jsonl')",
		},
		{
			name:  "LOAD DATA OVERWRITE",
			input: "LOAD DATA OVERWRITE INTO ds.t FROM '/tmp/data.parquet'",
			want:  "INSERT INTO ds.t SELECT * FROM read_parquet('/tmp/data.parquet')",
		},

		// UNNEST alias, in a correlated scalar subquery — DuckDB's binder
		// refuses "FROM UNNEST(x) AS c" directly ("Referenced table \"c\" not
		// found!"); wrapping it in a derived table's select list is the fix.
		{
			name:  "UNNEST alias in correlated subquery, real gcpbilling credits_total shape",
			input: "SELECT CAST(IFNULL((SELECT SUM(CAST(c.amount AS NUMERIC)) FROM UNNEST(credits) AS c), 0) AS NUMERIC) AS credits_total FROM t",
			want:  "SELECT CAST(IFNULL((SELECT SUM(CAST(c.amount AS NUMERIC)) FROM (SELECT UNNEST(credits) AS c)), 0) AS NUMERIC) AS credits_total FROM t",
		},
		{
			name:  "UNNEST alias on a qualified column",
			input: "SELECT (SELECT COUNT(*) FROM UNNEST(t.labels) AS l WHERE l.key = 'env') FROM t",
			want:  "SELECT (SELECT COUNT(*) FROM (SELECT UNNEST(t.labels) AS l) WHERE l.key = 'env') FROM t",
		},
		{
			name:  "UNNEST two-part alias left untouched",
			input: "SELECT * FROM UNNEST(credits) AS c(amount)",
			want:  "SELECT * FROM UNNEST(credits) AS c(amount)",
		},

		// Passthrough (already valid DuckDB SQL)
		{
			name:  "simple select passthrough",
			input: "SELECT 1 AS num",
			want:  "SELECT 1 AS num",
		},
		{
			name:  "standard functions passthrough",
			input: "SELECT COALESCE(NULL, 'x'), CONCAT('a', 'b'), POW(2, 10)",
			want:  "SELECT COALESCE(NULL, 'x'), CONCAT('a', 'b'), POW(2, 10)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Lower(tt.input)
			if got != tt.want {
				t.Errorf("\n  input: %s\n    got: %s\n   want: %s", tt.input, got, tt.want)
			}
		})
	}
}
