#!/usr/bin/env bash
# CI check for the i18n catalogue: embedded locale files are valid, the English
# catalogue is the source of truth, mapped Fresh keys are all translated, and (when
# the reference checkout is present) the committed locale files match Fresh's import.
set -euo pipefail
cd "$(dirname "$0")/.."

go test ./i18n -count=1 -run TestCatalogueHeapBudget -v
go test ./i18n -count=1 \
  -run 'TestEmbeddedLocaleFiles|TestEmbeddedCataloguesValidate|TestEnglishIsTheSourceOfTruth|TestLocaleCoverage|TestCoverageFor|TestValidateCatalogue|TestPluralCategoriesPerLocale'

if [ -d refs/fresh/crates/fresh-editor-core/locales ]; then
  python3 tools/i18n_import.py --check
else
  echo "i18n: refs/fresh is not checked out; skipped the Fresh import drift check"
fi