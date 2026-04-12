// Package lowering implements the GoogleSQL -> DuckDB SQL compatibility layer (Layer 2).
//
// Architecture: one file per rewrite pattern with a registry dispatcher.
// Each pattern file is independently testable with table-driven tests.
//
// Files: qualify.go, pivot.go, unnest.go, merge.go, array.go, etc.
// Each registers with the pattern registry via init().
//
// The lowering pass:
//  1. Receives a resolved AST from googlesql-go bindings
//  2. Walks the AST via the visitor pattern
//  3. Dispatches each node to the registered pattern handler
//  4. Emits DuckDB SQL + metadata deltas
//
// Namespace isolation: rejects any query referencing _meta.* schemas.
package lowering
