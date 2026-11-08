#!/usr/bin/env bash
# Pure-Go gate: the module must build with cgo disabled, and no package in the
# dependency graph may contain cgo files or import "C" or runtime/cgo.
set -euo pipefail
cd "$(dirname "$0")/.."
CGO_ENABLED=0 go build ./...
cgo_pkgs=$(CGO_ENABLED=0 go list -deps -f '{{if .CgoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v '^$' || true)
if [ -n "$cgo_pkgs" ]; then
  echo "cgo files found in dependency graph:" >&2
  echo "$cgo_pkgs" >&2
  exit 1
fi
if CGO_ENABLED=0 go list -deps ./... | grep -qx 'runtime/cgo'; then
  echo "runtime/cgo is in the default dependency graph" >&2
  exit 1
fi
echo "PURE_GO_OK"
