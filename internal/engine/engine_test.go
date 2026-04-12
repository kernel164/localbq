package engine_test

import (
	"context"
	"testing"

	"github.com/slokam-ai/localbq/internal/engine"
)

func TestOpen_InMemory(t *testing.T) {
	eng, err := engine.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
}

func TestQuery_SelectLiteral(t *testing.T) {
	eng, err := engine.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	result, err := eng.Query(context.Background(), "SELECT 1 AS num, 'hello' AS greeting, true AS flag")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Columns) != 3 {
		t.Fatalf("columns = %d, want 3", len(result.Columns))
	}
	if result.Columns[0].Name != "num" {
		t.Errorf("col[0].name = %q", result.Columns[0].Name)
	}
	if result.Columns[0].Type != engine.TypeInteger {
		t.Errorf("col[0].type = %q, want INTEGER", result.Columns[0].Type)
	}
	if result.Columns[1].Type != engine.TypeString {
		t.Errorf("col[1].type = %q, want STRING", result.Columns[1].Type)
	}
	if result.Columns[2].Type != engine.TypeBoolean {
		t.Errorf("col[2].type = %q, want BOOLEAN", result.Columns[2].Type)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
}

func TestSchema_CreateAndList(t *testing.T) {
	eng, err := engine.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ctx := context.Background()

	if err := eng.CreateSchema(ctx, "test_dataset"); err != nil {
		t.Fatal(err)
	}

	exists, err := eng.SchemaExists(ctx, "test_dataset")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("schema should exist")
	}

	schemas, err := eng.ListSchemas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range schemas {
		if s == "test_dataset" {
			found = true
		}
	}
	if !found {
		t.Errorf("test_dataset not found in schemas: %v", schemas)
	}

	if err := eng.DropSchema(ctx, "test_dataset", false); err != nil {
		t.Fatal(err)
	}

	exists, err = eng.SchemaExists(ctx, "test_dataset")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("schema should not exist after drop")
	}
}

func TestTable_CreateQueryDrop(t *testing.T) {
	eng, err := engine.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ctx := context.Background()

	eng.CreateSchema(ctx, "ds")
	eng.Exec(ctx, `CREATE TABLE ds.users (id BIGINT, name VARCHAR, active BOOLEAN)`)
	eng.Exec(ctx, `INSERT INTO ds.users VALUES (1, 'Alice', true), (2, 'Bob', false)`)

	// List tables
	tables, err := eng.ListTables(ctx, "ds")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0] != "users" {
		t.Errorf("tables = %v, want [users]", tables)
	}

	// Table columns
	cols, err := eng.TableColumns(ctx, "ds", "users")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 {
		t.Fatalf("columns = %d, want 3", len(cols))
	}
	if cols[0].Name != "id" || cols[0].Type != engine.TypeInteger {
		t.Errorf("col[0] = %+v", cols[0])
	}

	// Query
	result, err := eng.Query(ctx, "SELECT * FROM ds.users ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 {
		t.Errorf("rows = %d, want 2", len(result.Rows))
	}

	// Drop
	eng.DropTable(ctx, "ds", "users")
	exists, _ := eng.TableExists(ctx, "ds", "users")
	if exists {
		t.Error("table should not exist after drop")
	}
}
