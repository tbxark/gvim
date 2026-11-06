#!/usr/bin/env bash
# Import-graph gate for ARCH-v4 section 2 (phase P0, WS-F).
#
# Reads the direct imports of every package under internal/ and of the main
# package (go list -deps), then checks the rules below. Prints IMPORT_RULES_OK
# when no rule is broken. Otherwise it prints every violation and exits 1.
#
#   R1  only internal/ui (and its sub-packages) and the test-only internal/uitest
#       may import charm.land/*.
#       Exempt as a source: internal/tui (legacy, retired in P4).
#   R2  internal/buffer imports no internal package and no charm.land or
#       github.com/charmbracelet package (no tea, lipgloss or bubbles).
#   R3  internal/editor does not import internal/ui, lsp, markdown, finder or fsops.
#   R4  internal/search does not import internal/editor or internal/ui.
#   R5  internal/ui does not import internal/app or internal/cli, and (ARCH-v4
#       section 2) not the concrete lsp, markdown, finder or fsops packages.
#   R6  internal/cli does not import internal/ui, editor, lsp or markdown.
#   R7  only main and internal/cli may import internal/app; only main may
#       import internal/cli.
#   R8  no production package (anything in the dependency tree of the main
#       package) imports internal/buffer/buffertest, the test-support sources.
#
# Packages that do not exist yet are skipped. Bash 3.2 compatible (macOS).

set -euo pipefail
cd "$(dirname "$0")/../.."

mod="github.com/tbxark/gvim"

if ! graph=$(go list -deps -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/... . 2>&1); then
  echo "import_rules_check: go list failed:" >&2
  echo "$graph" >&2
  exit 2
fi

# One "package<TAB>import" line per direct import of a package in this module.
edges=$(printf '%s\n' "$graph" | awk -v mod="$mod" -F': ' '
  {
    pkg = $1
    if (pkg != mod && index(pkg, mod "/") != 1) next
    n = split($2, imps, " ")
    for (i = 1; i <= n; i++) print pkg "\t" imps[i]
  }')

# Packages of the shipped binary, for R8.
if ! prod=$(go list -deps -f '{{.ImportPath}}' . 2>&1); then
  echo "import_rules_check: go list failed:" >&2
  echo "$prod" >&2
  exit 2
fi

count=0
report=""

add() {
  count=$((count + 1))
  report="${report}  [$1] $2 imports $3"$'\n'
}

# under PATH BASE: true when PATH is BASE or a sub-package of BASE.
under() {
  case "$1" in
    "$2" | "$2"/*) return 0 ;;
    *) return 1 ;;
  esac
}

while IFS=$'\t' read -r pkg imp; do
  [ -n "$pkg" ] || continue

  rel=${pkg#"$mod"}
  rel=${rel#/}
  ours=0
  irel=$imp
  case "$imp" in
    "$mod" | "$mod"/*)
      ours=1
      irel=${imp#"$mod"}
      irel=${irel#/}
      ;;
  esac

  # R1
  case "$imp" in
    charm.land/*)
      # Transitional exemptions, removed in P3 when ui owns the program start:
      # internal/app/run.go starts the Bubble Tea program until then.
      # internal/uitest is test-only support: it runs a Bubble Tea program in
      # process, so it may import charm.land/* like internal/ui does.
      if ! under "$rel" internal/ui && ! under "$rel" internal/uitest && [ "$rel" != internal/tui ]; then
        add R1 "$pkg" "$imp"
      fi
      ;;
  esac

  # R2
  if [ "$rel" = internal/buffer ]; then
    case "$imp" in
      "$mod" | "$mod"/* | charm.land/* | github.com/charmbracelet/*)
        add R2 "$pkg" "$imp"
        ;;
    esac
  fi

  # R3
  if [ "$rel" = internal/editor ] && [ "$ours" = 1 ]; then
    if under "$irel" internal/ui || under "$irel" internal/lsp ||
      under "$irel" internal/markdown || under "$irel" internal/finder ||
      under "$irel" internal/fsops; then
      add R3 "$pkg" "$imp"
    fi
  fi

  # R4
  if [ "$rel" = internal/search ] && [ "$ours" = 1 ]; then
    if under "$irel" internal/editor || under "$irel" internal/ui; then
      add R4 "$pkg" "$imp"
    fi
  fi

  # R5
  if under "$rel" internal/ui && [ "$ours" = 1 ]; then
    if under "$irel" internal/app || under "$irel" internal/cli ||
      under "$irel" internal/lsp || under "$irel" internal/markdown ||
      under "$irel" internal/finder || under "$irel" internal/fsops; then
      add R5 "$pkg" "$imp"
    fi
  fi

  # R6
  if [ "$rel" = internal/cli ] && [ "$ours" = 1 ]; then
    if under "$irel" internal/ui || under "$irel" internal/editor ||
      under "$irel" internal/lsp || under "$irel" internal/markdown; then
      add R6 "$pkg" "$imp"
    fi
  fi

  # R7
  if [ "$ours" = 1 ]; then
    if under "$irel" internal/app && [ -n "$rel" ] && [ "$rel" != internal/cli ]; then
      add R7 "$pkg" "$imp"
    fi
    if under "$irel" internal/cli && [ -n "$rel" ]; then
      add R7 "$pkg" "$imp"
    fi
  fi

  # R8
  if [ "$imp" = "$mod/internal/buffer/buffertest" ] && printf '%s\n' "$prod" | grep -qxF "$pkg"; then
    add R8 "$pkg" "$imp"
  fi
done <<<"$edges"

if [ "$count" -eq 0 ]; then
  echo "IMPORT_RULES_OK"
  exit 0
fi

printf 'import rule violations: %d\n%s' "$count" "$report"
echo "IMPORT_RULES_FAILED"
exit 1
