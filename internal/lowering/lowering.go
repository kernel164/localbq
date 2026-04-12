// Package lowering implements GoogleSQL → DuckDB SQL compatibility rewrites (Layer 2).
//
// This is a text-based lowering pass that handles the most common GoogleSQL patterns
// that DuckDB doesn't support natively. It will be upgraded to AST-based rewrites
// when the googlesql sidecar is integrated.
//
// Each rewrite is a function registered via init(). Rewrites are applied in order.
package lowering

import (
	"fmt"
	"regexp"
	"strings"
)

// rewrite is a single SQL transformation.
type rewrite struct {
	name string
	fn   func(string) string
}

var rewrites []rewrite

func register(name string, fn func(string) string) {
	rewrites = append(rewrites, rewrite{name: name, fn: fn})
}

// Lower applies all registered rewrites to transform GoogleSQL into DuckDB SQL.
func Lower(sql string) string {
	for _, r := range rewrites {
		sql = r.fn(sql)
	}
	return sql
}

func init() {
	// Order matters: some rewrites depend on prior transformations.

	// Backtick identifiers → double-quote identifiers.
	// Must run before function rewrites since backtick-quoted table refs
	// may appear inside function arguments.
	register("backticks", lowerBackticks)

	// Strip regional INFORMATION_SCHEMA prefix.
	// `region-us`.INFORMATION_SCHEMA.X → information_schema.X
	// region-us.INFORMATION_SCHEMA.X → information_schema.X
	register("regional_info_schema", lowerRegionalInfoSchema)

	// Strip project.dataset prefix from 3-part table references.
	// "project"."dataset"."table" → "dataset"."table"
	// project.dataset.table → dataset.table (when in INFORMATION_SCHEMA context)
	register("three_part_refs", lowerThreePartRefs)

	// TIMESTAMP_SUB(ts, INTERVAL n UNIT) → (ts::TIMESTAMP - INTERVAL 'n' UNIT)
	register("timestamp_sub", lowerTimestampSub)

	// TIMESTAMP_ADD(ts, INTERVAL n UNIT) → (ts::TIMESTAMP + INTERVAL 'n' UNIT)
	register("timestamp_add", lowerTimestampAdd)

	// TIMESTAMP_TRUNC(ts, UNIT) → date_trunc('UNIT', ts::TIMESTAMP)
	register("timestamp_trunc", lowerTimestampTrunc)

	// DATE_SUB(dt, INTERVAL n UNIT) → (dt - INTERVAL 'n' UNIT)
	register("date_sub", lowerDateSub)

	// DATE_ADD(dt, INTERVAL n UNIT) → (dt + INTERVAL 'n' UNIT)
	register("date_add", lowerDateAdd)

	// DATE(expr) → CAST(expr AS DATE) — but not DATE 'literal'
	register("date_func", lowerDateFunc)

	// COUNTIF(expr) → count_if(expr)
	register("countif", lowerCountIf)

	// SAFE_CAST(expr AS type) → TRY_CAST(expr AS type)
	register("safe_cast", lowerSafeCast)

	// CURRENT_TIMESTAMP() → CURRENT_TIMESTAMP (strip parens)
	register("current_timestamp_parens", lowerCurrentTimestampParens)

	// CURRENT_DATE() → CURRENT_DATE (strip parens)
	register("current_date_parens", lowerCurrentDateParens)

	// FLOAT64 → DOUBLE (in type contexts: CAST, column defs)
	register("float64", lowerFloat64)

	// BOOL → BOOLEAN (in type contexts)
	register("bool_type", lowerBoolType)

	// __TABLES__ → information_schema.tables (legacy BQ metadata)
	register("legacy_tables", lowerLegacyTables)

	// LOAD DATA INTO table FROM 'file' → INSERT INTO table SELECT * FROM read_parquet/csv/json('file')
	register("load_data", lowerLoadData)
}

// --- Rewrite implementations ---

func lowerBackticks(sql string) string {
	var result strings.Builder
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case ch == '\'' && !inDoubleQuote:
			inSingleQuote = !inSingleQuote
			result.WriteByte(ch)
		case ch == '"' && !inSingleQuote:
			inDoubleQuote = !inDoubleQuote
			result.WriteByte(ch)
		case ch == '`' && !inSingleQuote && !inDoubleQuote:
			// Find the closing backtick
			end := strings.IndexByte(sql[i+1:], '`')
			if end == -1 {
				result.WriteByte(ch) // unmatched, leave as-is
			} else {
				ident := sql[i+1 : i+1+end]
				result.WriteByte('"')
				result.WriteString(strings.ReplaceAll(ident, `"`, `""`))
				result.WriteByte('"')
				i += end + 1
			}
		default:
			result.WriteByte(ch)
		}
	}
	return result.String()
}

var reRegionalInfoSchema = regexp.MustCompile(
	`(?i)"?region-[a-z0-9-]+"?\s*\.\s*INFORMATION_SCHEMA\b`)

func lowerRegionalInfoSchema(sql string) string {
	return reRegionalInfoSchema.ReplaceAllStringFunc(sql, func(match string) string {
		return "information_schema"
	})
}

// Match 3-part dotted references: "a"."b"."c" or a.b.c
// In a BQ emulator context, strip the project (first part).
var reThreePartQuoted = regexp.MustCompile(
	`"[^"]+"\s*\.\s*"([^"]+)"\s*\.\s*"([^"]+)"`)

func lowerThreePartRefs(sql string) string {
	return reThreePartQuoted.ReplaceAllString(sql, `"$1"."$2"`)
}

// TIMESTAMP_SUB(expr, INTERVAL n UNIT)
var reTimestampSub = regexp.MustCompile(
	`(?i)\bTIMESTAMP_SUB\s*\(\s*((?:[^(),]+|\((?:[^()]*|\([^()]*\))*\))+?)\s*,\s*INTERVAL\s+(\w+)\s+(\w+)\s*\)`)

func lowerTimestampSub(sql string) string {
	return reTimestampSub.ReplaceAllString(sql, "($1::TIMESTAMP - INTERVAL '$2' $3)")
}

// TIMESTAMP_ADD(expr, INTERVAL n UNIT)
var reTimestampAdd = regexp.MustCompile(
	`(?i)\bTIMESTAMP_ADD\s*\(\s*((?:[^(),]+|\((?:[^()]*|\([^()]*\))*\))+?)\s*,\s*INTERVAL\s+(\w+)\s+(\w+)\s*\)`)

func lowerTimestampAdd(sql string) string {
	return reTimestampAdd.ReplaceAllString(sql, "($1::TIMESTAMP + INTERVAL '$2' $3)")
}

// TIMESTAMP_TRUNC(expr, UNIT)
var reTimestampTrunc = regexp.MustCompile(
	`(?i)\bTIMESTAMP_TRUNC\s*\(\s*((?:[^(),]+|\((?:[^()]*|\([^()]*\))*\))+?)\s*,\s*(\w+)\s*\)`)

func lowerTimestampTrunc(sql string) string {
	return reTimestampTrunc.ReplaceAllStringFunc(sql, func(match string) string {
		m := reTimestampTrunc.FindStringSubmatch(match)
		return "date_trunc('" + strings.ToLower(m[2]) + "', " + m[1] + "::TIMESTAMP)"
	})
}

// DATE_SUB(expr, INTERVAL n UNIT)
var reDateSub = regexp.MustCompile(
	`(?i)\bDATE_SUB\s*\(\s*((?:[^(),]+|\((?:[^()]*|\([^()]*\))*\))+?)\s*,\s*INTERVAL\s+(\w+)\s+(\w+)\s*\)`)

func lowerDateSub(sql string) string {
	return reDateSub.ReplaceAllString(sql, "($1 - INTERVAL '$2' $3)")
}

// DATE_ADD(expr, INTERVAL n UNIT)
var reDateAdd = regexp.MustCompile(
	`(?i)\bDATE_ADD\s*\(\s*((?:[^(),]+|\((?:[^()]*|\([^()]*\))*\))+?)\s*,\s*INTERVAL\s+(\w+)\s+(\w+)\s*\)`)

func lowerDateAdd(sql string) string {
	return reDateAdd.ReplaceAllString(sql, "($1 + INTERVAL '$2' $3)")
}

// DATE(expr) → CAST(expr AS DATE)
// But NOT: DATE 'literal', DATE_SUB, DATE_ADD, DATE_TRUNC, etc.
var reDateFunc = regexp.MustCompile(
	`(?i)\bDATE\s*\(\s*((?:[^(),]+|\((?:[^()]*|\([^()]*\))*\))+?)\s*\)`)

func lowerDateFunc(sql string) string {
	return reDateFunc.ReplaceAllStringFunc(sql, func(match string) string {
		m := reDateFunc.FindStringSubmatch(match)
		expr := strings.TrimSpace(m[1])
		// Don't rewrite if it's part of a compound function name
		// (those are caught by their own patterns first)
		return "CAST(" + expr + " AS DATE)"
	})
}

// COUNTIF(expr) → count_if(expr)
var reCountIf = regexp.MustCompile(`(?i)\bCOUNTIF\s*\(`)

func lowerCountIf(sql string) string {
	return reCountIf.ReplaceAllString(sql, "count_if(")
}

// SAFE_CAST(expr AS type) → TRY_CAST(expr AS type)
var reSafeCast = regexp.MustCompile(`(?i)\bSAFE_CAST\s*\(`)

func lowerSafeCast(sql string) string {
	return reSafeCast.ReplaceAllString(sql, "TRY_CAST(")
}

// CURRENT_TIMESTAMP() → CURRENT_TIMESTAMP
var reCurrentTimestampParens = regexp.MustCompile(`(?i)\bCURRENT_TIMESTAMP\s*\(\s*\)`)

func lowerCurrentTimestampParens(sql string) string {
	return reCurrentTimestampParens.ReplaceAllString(sql, "CURRENT_TIMESTAMP")
}

// CURRENT_DATE() → CURRENT_DATE
var reCurrentDateParens = regexp.MustCompile(`(?i)\bCURRENT_DATE\s*\(\s*\)`)

func lowerCurrentDateParens(sql string) string {
	return reCurrentDateParens.ReplaceAllString(sql, "CURRENT_DATE")
}

// FLOAT64 → DOUBLE in type contexts (after AS, in CAST, column defs)
var reFloat64 = regexp.MustCompile(`(?i)\bFLOAT64\b`)

func lowerFloat64(sql string) string {
	return reFloat64.ReplaceAllString(sql, "DOUBLE")
}

// BOOL → BOOLEAN (standalone, not BOOLEAN which is already valid)
var reBoolType = regexp.MustCompile(`(?i)\bBOOL\b`)

func lowerBoolType(sql string) string {
	return reBoolType.ReplaceAllStringFunc(sql, func(match string) string {
		// Don't replace if it's already part of BOOLEAN
		return "BOOLEAN"
	})
}

// Post-fix: BOOLEANEAN back to BOOLEAN (from BOOL→BOOLEAN when BOOLEAN was input)
func init() {
	register("bool_fix", func(sql string) string {
		return strings.ReplaceAll(sql, "BOOLEANEAN", "BOOLEAN")
	})
}

// __TABLES__ → information_schema.tables
var reLegacyTables = regexp.MustCompile(`(?i)\b__TABLES__\b`)

func lowerLegacyTables(sql string) string {
	return reLegacyTables.ReplaceAllString(sql, "information_schema.tables")
}

// LOAD DATA INTO <table> FROM '<file>'
// BigQuery syntax: LOAD DATA [OVERWRITE] INTO <table> FROM FILES (uris=['gs://...'], format='PARQUET')
// Simplified local syntax: LOAD DATA INTO <table> FROM '<local_file>'
var reLoadData = regexp.MustCompile(
	`(?i)^\s*LOAD\s+DATA\s+(?:OVERWRITE\s+)?INTO\s+(\S+)\s+FROM\s+'([^']+)'`)

func lowerLoadData(sql string) string {
	m := reLoadData.FindStringSubmatch(sql)
	if m == nil {
		return sql
	}

	table := m[1]
	filePath := m[2]

	ext := strings.ToLower(filePath)
	readFunc := "read_parquet"
	switch {
	case strings.HasSuffix(ext, ".csv") || strings.HasSuffix(ext, ".tsv"):
		readFunc = "read_csv"
	case strings.HasSuffix(ext, ".json") || strings.HasSuffix(ext, ".jsonl") || strings.HasSuffix(ext, ".ndjson"):
		readFunc = "read_json"
	}

	return fmt.Sprintf("INSERT INTO %s SELECT * FROM %s('%s')", table, readFunc, filePath)
}

