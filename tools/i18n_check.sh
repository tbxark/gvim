#!/usr/bin/env bash
# CI check for the i18n catalogue: embedded locale files are valid and the
# English catalogue is the source of truth.
set -euo pipefail
cd "$(dirname "$0")/.."

go test ./i18n -count=1 -run '^TestCatalogueHeapBudget$' -v
go test ./i18n -count=1 -skip '^TestCatalogueHeapBudget$'
