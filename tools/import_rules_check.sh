#!/usr/bin/env bash
# Import-graph gate for the gvim package layering (ARCH-v5, docs/architecture.md).
#
# Reads the direct imports of every package of the module (go list), checks the
# rules below, and prints IMPORT_RULES_OK when none is broken. Otherwise every
# violation is printed and the script exits 1.
#
# Layout the rules describe:
#
#   main (module root), cmd/gvimdebug   executables (the only packages that import internal/)
#   internal/                    the executable shell, private:
#     app/                       composition root: the only package that imports the
#                                private host packages (uiadapter, pluginhost, lsphost,
#                                restore, keybindings, highlight) and the plugin runtime
#     uiadapter/                 the UI port adapters; the only package besides app that
#                                imports the TUI sub-packages
#     pluginhost/                the Lua plugin host: runtime, OS, editor, services
#     lsphost/                   the language-server port
#     restore/                   session restore and crash recovery
#     keybindings/               user keybindings, the editing store, plugin bindings
#     highlight/                 the syntax-span cache
#     apptest/                   test support only; never in a production tree
#     cli/                       cobra command tree (imports app for Options/Run, configview
#                                for config commands, syntax for the grammar list)
#     configview/                the `gvim config show/paths` backend (config + keybindings)
#     tui/ (+ sub-packages)      the Bubble Tea UI
#     terminal/  filewatch/      private backends of the shell
#     uitest/                    test-only in-process UI harness
#   top level (no internal/):    the TUI-free engine and shared libraries
#     buffer, document, editor, search, syntax, fold, diff, encoding, session,
#     decorations, textlayout, config, keymap, theme, workspace, explorer,
#     finder, fuzzy, fsops, gitstate, trust, lsp, pluginapi, plugin, commands,
#     outline, i18n
#
# Rules:
#   R1  no top-level (non-cmd, non-internal) package imports internal/.
#   R2  charm.land/* (Bubble Tea v2 and friends) may be imported only by the
#       TUI tree (internal/tui/...), the terminal backend and the test-only
#       UI harness (internal/uitest). The plain github.com/charmbracelet/x/*
#       utility libraries (ansi, term) are not the UI framework and stay free.
#   R3  leaf packages import no module package: buffer, textlayout, diff,
#       encoding, fuzzy, pluginapi, decorations, theme, fsops, gitstate, lsp,
#       lsp/lsptest, atomicfile, shellcmd, config, explorer, keymap, i18n,
#       plugin/bundled, plugin/pattern, outline. (lsp/manager sits above lsp and
#       may import it.)
#   R4  engine direction: document (the per-file model) imports only its leaf
#       packages (buffer, search, syntax, textlayout, fold, diff, encoding,
#       atomicfile, shellcmd), never editor, session or internal/; editor may
#       import document but must not import the UI, app, cli, plugin
#       runtime/contract, lsp, finder, fsops, workspace, keymap, theme or
#       config; search must not import editor or document; syntax imports only
#       theme among module packages; session imports only buffer, document and
#       atomicfile; commands imports only fuzzy and keymap; workspace imports
#       only config; trust imports only config and atomicfile; finder imports
#       only explorer and fuzzy.
#   R5  the TUI tree must not import internal/app, internal/cli, the plugin
#       runtime, lsp, finder or fsops; a tui sub-package must not import its
#       parent package (internal/tui).
#   R6  internal/cli imports only internal/app, internal/configview and syntax (plus
#       the standard library and cobra).
#   R7  only the root main package and internal/cli import internal/app; only
#       the root main package imports internal/cli; only internal/app,
#       internal/pluginhost and plugin/plugintest import the plugin runtime;
#       only internal/app and internal/uiadapter import the TUI sub-packages.
#   R8  no production package (the dependency tree of the root main package) imports test
#       support: buffer/buffertest, plugin/plugintest, lsp/lsptest,
#       internal/uitest, internal/apptest.
#   R9  pluginapi and decorations import no module package and no charm package.
#   R10 the plugin tree imports only the standard library, gopher-lua, hujson,
#       pluginapi, plugin/bundled, plugin/pattern and plugin/hostos (plugintest may
#       also import the plugin runtime and other plugin sub-packages). No charm. No
#       other module package.
#   R11 editor, buffer and decorations do not import pluginapi.
#   R12 tui sub-packages may import each other and the kits, but no package
#       outside internal/tui and internal/app may import them. The tier-1a
#       pluginpanel sub-package imports only grid, style, space and pluginapi.
#
# Bash 3.2 compatible (macOS).

set -euo pipefail
cd "$(dirname "$0")/.."

mod="github.com/tbxark/gvim"

if ! graph=$(go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... 2>&1); then
  echo "import_rules_check: go list failed:" >&2
  echo "$graph" >&2
  exit 2
fi

if ! prod=$(go list -deps -f '{{.ImportPath}}' . 2>&1); then
  echo "import_rules_check: go list failed:" >&2
  echo "$prod" >&2
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

# is_top REL: true for a top-level package (not internal/ or cmd/).
is_top() {
  case "$1" in
    internal | internal/* | cmd | cmd/* | "") return 1 ;;
    *) return 0 ;;
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

  # R1: the top-level engine never imports the executable shell.
  if [ "$ours" = 1 ] && is_top "$rel" && under "$irel" internal; then
    add R1 "$pkg" "$imp"
  fi

  # R2: Bubble Tea and lipgloss only in the UI tree, the terminal backend and uitest.
  case "$imp" in
    charm.land/*)
      if ! under "$rel" internal/tui && ! under "$rel" internal/terminal &&
        [ "$rel" != internal/uitest ]; then
        add R2 "$pkg" "$imp"
      fi
      ;;
  esac

  if [ "$ours" = 1 ]; then
    # R3: leaf packages have no module dependency at all.
    case "$rel" in
      buffer | textlayout | diff | encoding | fuzzy | pluginapi | decorations | theme | fsops | gitstate | lsp | lsp/lsptest | atomicfile | shellcmd | config | explorer | keymap | i18n | plugin/bundled | plugin/pattern | outline)
        add R3 "$pkg" "$imp"
        ;;
    esac

    # R4: engine direction.
    if [ "$rel" = document ]; then
      case "$irel" in
        buffer | search | syntax | textlayout | fold | diff | encoding | atomicfile | shellcmd) ;;
        *) add R4 "$pkg" "$imp" ;;
      esac
    fi
    if [ "$rel" = editor ]; then
      case "$irel" in
        internal/* | pluginapi | plugin | plugin/* | lsp | finder | fsops | workspace | keymap | theme | config | commands)
          add R4 "$pkg" "$imp"
          ;;
      esac
    fi
    if [ "$rel" = search ] && { under "$irel" editor || under "$irel" document || under "$irel" internal; }; then
      add R4 "$pkg" "$imp"
    fi
    if [ "$rel" = syntax ] && [ "$irel" != theme ]; then
      add R4 "$pkg" "$imp"
    fi
    if [ "$rel" = session ] && [ "$irel" != buffer ] && [ "$irel" != document ] && [ "$irel" != atomicfile ]; then
      add R4 "$pkg" "$imp"
    fi
    if [ "$rel" = commands ] && [ "$irel" != fuzzy ] && [ "$irel" != keymap ]; then
      add R4 "$pkg" "$imp"
    fi
    if [ "$rel" = workspace ] && [ "$irel" != config ]; then
      add R4 "$pkg" "$imp"
    fi
    if [ "$rel" = finder ] && [ "$irel" != explorer ] && [ "$irel" != fuzzy ]; then
      add R4 "$pkg" "$imp"
    fi
    if [ "$rel" = trust ] && [ "$irel" != config ] && [ "$irel" != atomicfile ]; then
      add R4 "$pkg" "$imp"
    fi

    # R5: the TUI tree does not reach the shell or concrete backends.
    if under "$rel" internal/tui; then
      case "$irel" in
        internal/app | internal/cli | plugin | lsp | finder | fsops)
          add R5 "$pkg" "$imp"
          ;;
      esac
      if [ "$rel" != internal/tui ] && [ "$irel" = internal/tui ]; then
        add R5 "$pkg" "$imp"
      fi
    fi

    # R6: cli is the flags layer; app holds the run, configview the config views.
    if [ "$rel" = internal/cli ]; then
      case "$irel" in
        internal/app | internal/configview | syntax) ;;
        *) add R6 "$pkg" "$imp" ;;
      esac
    fi

    # R7: the composition root and the executable.
    if [ -n "$rel" ] && [ "$rel" != internal/cli ] && under "$irel" internal/app; then
      add R7 "$pkg" "$imp"
    fi
    if [ -n "$rel" ] && [ "$irel" = internal/cli ]; then
      add R7 "$pkg" "$imp"
    fi
    if [ "$rel" != internal/app ] && [ "$rel" != internal/cli ] && [ "$irel" = internal/configview ]; then
      add R7 "$pkg" "$imp"
    fi
    if [ "$rel" != internal/app ] && [ "$rel" != internal/pluginhost ] && [ "$rel" != plugin/plugintest ] && under "$irel" plugin &&
      [ "$irel" != pluginapi ] && [ "$irel" != plugin/pattern ] && [ "$irel" != plugin/bundled ] &&
      [ "$irel" != plugin/hostos ]; then
      add R7 "$pkg" "$imp"
    fi
    if [ "$rel" != internal/tui ] && [ "$rel" != internal/app ] && [ "$rel" != internal/uiadapter ] && under "$irel" internal/tui &&
      [ "$irel" != internal/tui ]; then
      # Sub-packages of internal/tui may import the kits and their siblings.
      if ! under "$rel" internal/tui; then
        add R7 "$pkg" "$imp"
      fi
    fi

    # R10: the plugin tree.
    if under "$rel" plugin; then
      case "$irel" in
        pluginapi | plugin/bundled | plugin/pattern | plugin/hostos | plugin)
          if [ "$irel" = plugin ] && [ "$rel" != plugin/plugintest ] &&
            [ "$rel" != internal/app ]; then
            add R10 "$pkg" "$imp"
          fi
          ;;
        *)
          add R10 "$pkg" "$imp"
          ;;
      esac
      case "$imp" in
        charm.land/*)
          add R10 "$pkg" "$imp"
          ;;
      esac
    fi

    # R11: the engine does not know the plugin contract.
    if { [ "$rel" = editor ] || [ "$rel" = document ] || [ "$rel" = buffer ] || [ "$rel" = decorations ]; } &&
      [ "$irel" = pluginapi ]; then
      add R11 "$pkg" "$imp"
    fi

    # R12: only the tui root and the app import tui sub-packages.
    if under "$irel" internal/tui && [ "$irel" != internal/tui ] &&
      [ "$rel" != internal/tui ] && [ "$rel" != internal/app ] && [ "$rel" != internal/uiadapter ] &&
      ! under "$rel" internal/tui; then
      add R12 "$pkg" "$imp"
    fi

    # R14: the config-view backend is a config/keybindings leaf.
    if [ "$rel" = internal/configview ]; then
      case "$irel" in
        config | internal/keybindings | keymap | theme) ;;
        *) add R14 "$pkg" "$imp" ;;
      esac
    fi

    # R13: the private host packages. Only the composition root imports them; the
    # only sibling edges are uiadapter -> {highlight, keybindings, restore} and
    # pluginhost -> keybindings.
    case "$irel" in
      internal/uiadapter | internal/pluginhost | internal/lsphost)
        if [ "$rel" != internal/app ]; then
          add R13 "$pkg" "$imp"
        fi
        ;;
      internal/restore)
        if [ "$rel" != internal/app ] && [ "$rel" != internal/uiadapter ]; then
          add R13 "$pkg" "$imp"
        fi
        ;;
      internal/highlight)
        if [ "$rel" != internal/app ] && [ "$rel" != internal/uiadapter ]; then
          add R13 "$pkg" "$imp"
        fi
        ;;
      internal/keybindings)
        if [ "$rel" != internal/app ] && [ "$rel" != internal/uiadapter ] && [ "$rel" != internal/pluginhost ] &&
          [ "$rel" != internal/configview ]; then
          add R13 "$pkg" "$imp"
        fi
        ;;
    esac
  fi

  if [ "$ours" = 1 ] && [ "$irel" = internal/tui ]; then
    case "$rel" in
      internal/tui | internal/app | internal/uiadapter | internal/keybindings | internal/lsphost | internal/restore | internal/uitest) ;;
      *) add R15 "$pkg" "$imp" ;;
    esac
  fi

  # R8: test support never reaches a shipped binary.
  case "$imp" in
    "$mod/buffer/buffertest" | "$mod/plugin/plugintest" | "$mod/lsp/lsptest" | "$mod/internal/uitest" | "$mod/internal/apptest")
      if printf '%s\n' "$prod" | grep -qxF "$pkg"; then
        add R8 "$pkg" "$imp"
      fi
      ;;
  esac

  # R9: the plugin contract and the decoration store are leaf packages.
  if { [ "$rel" = pluginapi ] || [ "$rel" = decorations ]; } && [ "$ours" = 1 ]; then
    add R9 "$pkg" "$imp"
  fi
done <<<"$edges"


# R16: a top-level package's tests do not import internal/.
if ! testgraph=$(go list -test -f '{{.ImportPath}}: {{join .TestImports " "}}' ./... 2>&1); then
  echo "import_rules_check: go list -test failed:" >&2
  echo "$testgraph" >&2
  exit 2
fi
while IFS=: read -r tpkg timps; do
  [ -n "$tpkg" ] || continue
  # go list -test emits test variants as "pkg [pkg.test]"; use the package path.
  tpkg=${tpkg%% *}
  trel=${tpkg#"$mod"}
  trel=${trel#/}
  case "$trel" in
    internal | internal/* | cmd | cmd/* | "") continue ;;
  esac
  for timp in $timps; do
    case "$timp" in
      "$mod"/internal | "$mod"/internal/*)
        add R16 "$tpkg (test)" "$timp"
        ;;
    esac
  done
done <<<"$testgraph"

if [ "$count" -eq 0 ]; then
  echo "IMPORT_RULES_OK"
  exit 0
fi

printf 'import rule violations: %d\n%s' "$count" "$report"
echo "IMPORT_RULES_FAILED"
exit 1
