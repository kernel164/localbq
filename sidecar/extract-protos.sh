#!/bin/bash
# extract-protos.sh — Extract generated proto files from a Bazel build of googlesql.
#
# Usage: ./sidecar/extract-protos.sh [googlesql_source_dir] [output_dir]
#
# This script runs inside a Bazel-capable environment (Docker or CI) to:
# 1. Build the proto generation targets
# 2. Copy all proto files (static + generated) to the output directory
# 3. The output can then be used with protoc-gen-go to generate Go code
#
# The generated protos (resolved_ast.proto, parse_tree.proto, resolved_node_kind.proto)
# are produced by Bazel from .proto.template files and are not checked into the repo.

set -euo pipefail

GOOGLESQL_DIR="${1:-/tmp/googlesql-proto}"
OUTPUT_DIR="${2:-/tmp/googlesql-protos}"

echo "Extracting protos from ${GOOGLESQL_DIR} to ${OUTPUT_DIR}..."

# Build the proto targets to generate template-based protos
cd "${GOOGLESQL_DIR}"
bazel build \
  //googlesql/local_service:local_service_proto \
  //googlesql/resolved_ast:resolved_ast_proto \
  //googlesql/parser:parse_tree_proto \
  2>&1 | tail -5

# Find all proto files: static ones from the source tree + generated ones from bazel-bin
mkdir -p "${OUTPUT_DIR}"

# Copy static protos
find googlesql -name "*.proto" \
  -not -path "*/testdata/*" \
  -not -path "*/java/*" \
  -not -path "*/testing/*" \
  -not -name "*.proto.template" \
  -exec cp --parents {} "${OUTPUT_DIR}/" \;

# Copy generated protos from bazel output
for f in $(find bazel-bin/googlesql -name "*.proto" 2>/dev/null); do
  # Strip bazel-bin/ prefix
  rel="${f#bazel-bin/}"
  mkdir -p "${OUTPUT_DIR}/$(dirname "$rel")"
  cp "$f" "${OUTPUT_DIR}/${rel}"
done

echo "Proto extraction complete. Files:"
find "${OUTPUT_DIR}" -name "*.proto" | wc -l
echo "Key generated files:"
ls -la "${OUTPUT_DIR}/googlesql/resolved_ast/resolved_ast.proto" 2>/dev/null || echo "  resolved_ast.proto: NOT FOUND"
ls -la "${OUTPUT_DIR}/googlesql/parser/parse_tree.proto" 2>/dev/null || echo "  parse_tree.proto: NOT FOUND"
