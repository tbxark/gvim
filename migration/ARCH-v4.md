# ARCH-v4: gvim architecture after the v4 owner directives

Status: proposal, synthesized from three analyst reports (bubbles, cli, libs) and one new scratch check. Not implemented. The repo was not modified except this file. Where this file and BLUEPRINT*.md disagree on TUI or CLI structure, this file wins; BLUEPRINT*.md was not re-read in this pass.

Evidence labels: [V] = verified by a command in this pass, [A] = taken from an analyst report and not re-run, [P] = proposal.

## 1. Decisions

1. **TUI on Charm v2 with bubbles as parts, not as a whole screen.** Pins: `charm.land/bubbletea/v2` v2.1.1, `charm.land/bubbles/v2` v2.2.1, `charm.land/lipgloss/v2` v2.0.6. Bubbles components are used only where they fit: `textinput`, `list` (rendering and cursor only), `spinner`, `help`, `key`. The editor pane, tab strip, menu bar, dropdowns, explorer tree, scrollbar, status bar and mouse hit map stay custom. [V] for the pins and builds, [A] for the component fit.
2. **Reference structure.** Use `refs/crush` for the Bubble Tea v2 structure: one root `tea.Model`, sub-components with imperative methods, no IO in `Update`, and `ansi.*` helpers. Use `refs/fresh` only for behavior and feature scope. The TUI is not reproduced 1:1.
3. **Bubbles components not adopted:** `filepicker` (its default `Back` binding includes `esc`, it has no `..`/`/`/`~` rows, no size or mtime sort, and it reads with its own `os.ReadDir`), `textarea` (it owns the text, which conflicts with `editor.Document`, multi-cursor and per-line spans), `viewport` (it needs the whole buffer as one string per frame), `table` and `paginator` (not needed through M4; `table` may serve the M10 keybinding editor).
4. **List filtering is not done by `bubbles/list`.** `list.SetFilterText` runs the filter synchronously inside `Update` [A, `list/list.go:280-292`]. Call `SetFilteringEnabled` on the list pointer (it has a pointer receiver [V]) with `false`, filter in the controller with the shared fuzzy scorer or the async finder, and then call `SetItems` with at most 100 entries (the finder cap from GAP-v3 item 11).
5. **CLI: `github.com/spf13/cobra` v1.10.2.** Real subcommands, not Fresh's `--cmd` string dispatcher. No package globals and no `init()` flag registration. Command construction takes its dependencies as arguments. Errors are returned, and `main` prints them once. `fang` is not adopted yet.
6. **Markdown: `charm.land/glamour/v2` v2.0.1.** Not `github.com/charmbracelet/glamour` v1.0.0 and not `glow`. Glow is a `package main` app; its one importable package, `ui`, is a Bubble Tea v1 program [A]. Markdown is not required by M1–M10 (GAP-v3 §3), so the interface is defined now and the implementation waits for a feature that needs it.
7. **LSP: `go.lsp.dev/protocol` v1.0.1 and `go.lsp.dev/jsonrpc2` v1.0.1.** The owner's `github.com/go-language-server/...` paths do not resolve. The modules declare `go.lsp.dev/...` [A, verbatim error in the libs report]. Implementation waits for M8.
8. **Pure-Go gate.** Every new module must pass in a scratch module under `/tmp` first. The repo gate is `migration/tools/purity_check.sh` (`CGO_ENABLED=0 go build ./...` plus an empty cgo file list in `go list -deps`). Note the `-race` conflict in section 7 and risk R3.
9. **Parity policy change.** Legacy goldens are now a similarity signal, not a 1:1 acceptance gate. Keep the 26/26 suite (`migration/parity/golden/S01.json`–`S26.json`) as a regression floor for the editor text pane only, with chrome rows masked. Retired as acceptance gates, kept as inputs to the similarity report:
   - `migration/parity/golden_breakpoints/` (433 files)
   - `migration/parity/golden_sizes/` (16 files)
   - `migration/parity/golden_mouse/` (27 files)
   - `internal/tui/golden_test.go`: `goldenFrames()` and `goldenWant` (the four legacy full screens)

   The similarity report is informational. It prints a per-screen cell match ratio for the full frame and for the text pane, with no threshold. Thresholds are set later by the owner.
10. **Composition root and dependency direction.** `internal/app` is the only package that imports concrete adapters. `internal/ui` depends on interfaces it defines itself. Domain packages do not import `bubbletea`, `bubbles` or `lipgloss`.
11. **Fuzzy scorer moves to `internal/fuzzy`.** It is extracted from `internal/palette/fuzzy.go`, so `internal/finder` does not import the palette model [P]. Scores follow GAP-v3 item 11 (weights from Fresh's `input/fuzzy/mod.rs`).
12. **Rendering surface stays custom.** Keep a cell grid (`internal/ui/grid`) built on `github.com/charmbracelet/x/ansi` v0.11.9. Do not adopt `ultraviolet` in this refactor, so the surface changes only once [P]. Crush uses ultraviolet (`refs/crush/go.mod:16`, indirect in gvim).
13. **`internal/tui` is retired** after phase P4. New code goes to `internal/ui`.
14. **`gvim workspace open DIR` is not shipped in v4.** It duplicates the positional `gvim DIR`. The owner may add it later [P].

## 2. Package layout

```
main.go                       thin: signal context, cli.Execute, os.Exit
internal/cli/                 cobra commands, flag and arg parsing (no tea, no domain logic)
internal/app/                 composition root: Options, Run, adapters
internal/ui/                  bubbletea v2 root model; consumer-defined interfaces
  editorpane/                 text pane: visible-window render, gutter, cursor, selection, spans, scrollbar
  menubar/                    menu bar and dropdowns; generated menu table (menu_table_gen.go moves here)
  explorer/                   explorer panel (tree rendering, key handling)
  palette/                    palette controller (file, >, :, #), textinput + list.Model
  find/                       find bar and replace prompts (textinput + toggle row)
  prompt/                     goto, save-as, explorer name prompts, confirm dialog
  browser/                    file browser (Ctrl+O) and folder picker, on explorer.ListDir
  statusbar/                  status line, notices, spinner
  style/                      lipgloss styles built from internal/theme (the only lipgloss user besides components)
  grid/                       cell surface (from internal/tui/grid.go)
internal/buffer/              text storage, Pos
internal/editor/              Editor, Document, Tab, Action, Result
internal/search/              find and replace over buffer lines (stdlib regexp)
internal/fsops/               create, mkdir, rename, move, copy, trash with conflict policy
internal/workspace/           root and project switching, recents
internal/finder/              async file index, ranking adapter
internal/fuzzy/               scorer (moved from palette)
internal/palette/             model only: command registry, mode parsing, selection state
internal/keymap/              presets, chord parsing, resolve, bindings
internal/theme/               theme data (colors, syntax keys); no lipgloss
internal/syntax/              chroma lexer resolution, token spans per visible range
internal/explorer/            model only: Tree, ListDir, gitignore
internal/config/              (new) JSONC via tailscale/hujson, layered user and project config
internal/lsp/                 (new, M8) go.lsp.dev client
internal/markdown/            (new, when needed) glamour/v2 renderer
```

| Package | Public surface | May import | Must NOT be imported by |
|---|---|---|---|
| `main` | `main()` | `internal/cli`, `internal/app`, stdlib | nothing |
| `internal/cli` | `Execute(ctx, args, stdout, stderr, run Runner) int`, `NewRootCommand(run Runner) *cobra.Command`, `ParseTargets(args, stat) (Targets, error)`; `Runner func(ctx, app.Options) error` | cobra, pflag, `internal/app` (Options type only), stdlib | `ui`, `editor`, `lsp`, `markdown`, bubbletea |
| `internal/app` | `Options`, `FileTarget`, `(Options).Validate()`, `Run(ctx, Options) error`, adapters | every domain and ui package, `internal/lsp`, `internal/markdown`, `internal/config`, stdlib | domain packages, `internal/ui` and its sub-packages, `internal/lsp`, `internal/markdown`, `internal/cli` (cli imports app, so app must not import cli), cobra |
| `internal/ui` | `New(Deps) *Model` (tea.Model), `Deps`, the interfaces in section 3 | bubbletea, bubbles, lipgloss, `internal/ui/*`, value types from `editor`, `explorer`, `keymap`, `theme` | `app`, `cli`, `lsp` (concrete), `markdown` (concrete), `finder` (concrete), `fsops` |
| `internal/ui/editorpane` | `Pane`, `Document` interface | `buffer`, `syntax` (Span type), `grid`, `style`, lipgloss | `ui` root, `app` |
| `internal/ui/palette`, `find`, `prompt`, `browser`, `explorer`, `menubar`, `statusbar` | sub-models with `Update(msg) (Model, tea.Cmd)` and `View` | bubbles, `style`, `grid`, domain model packages | `ui` root, `app` (sub-packages never import their parent) |
| `internal/buffer` | `Pos`, buffer ops | stdlib | nothing in this repo may be imported from `buffer`; it imports no internal package and no tea or lipgloss |
| `internal/editor` | `New(Options) *Editor`, `Active`, `Tabs`, `Status`, `Do`, `SetViewport`, `OpenFile`, `OpenPreview`, `SaveAs`, `RenamePath`, `CloseByPath`, `GotoLine`, `ExtendSelection`, `SetCursor`, `SelectRange`, `ScrollBy` [V, names read from the source with grep, not from `go doc`] | `buffer`, `search`, stdlib | `ui`, `lsp`, `markdown`, `finder`, `fsops` (app orchestrates renames across both), tea, lipgloss |
| `internal/search` | find, replace, expand (planned) | `buffer`, stdlib | `editor`, `ui` (editor imports search) |
| `internal/fsops` | `CreateFile`, `Mkdir`, `Rename`, `Move`, `Copy(ConflictPolicy)`, `Trash` (planned, GAP item 15) | stdlib, `hymkor/trash-go` | `editor`, `ui`, `app` |
| `internal/workspace` | root and project switching, recents store | stdlib, `config` | `ui`, `editor` |
| `internal/finder` | `Start(ctx, root) *Index`, `Index.Snapshot`, `Loading`, `Truncated`, `Done`, `Err`, `Close`; `Enumerate`; `Score` (planned) [V, names from `finder/finder.go`] | `fuzzy`, `explorer` (gitignore matcher), `os/exec` for git, stdlib | `ui`, `palette`, tea |
| `internal/fuzzy` | `Score(query, target string) (score int, indexes []int, ok bool)` (planned) | stdlib | everything else |
| `internal/palette` | model only: command list, `ModeFor(query) Mode` (no prefix = file, `>` command, `:` line, `#` buffer), `Palette` selection (`SetSize`, `Select`, `ScrollBy`, `Selected`, `Window`) [V] | `fuzzy`, `keymap` (command metadata), stdlib | `ui`, bubbles, tea |
| `internal/keymap` | `Default`, `Resolve`, `Primary`, `Bindings`, chord parsing [V, from test names] | stdlib, `config` (inherits) | `ui` (ui adapts to `key.Binding`) |
| `internal/theme` | theme data, `builtin://` loader (planned) | stdlib | `ui`, lipgloss (ui/style converts) |
| `internal/syntax` | `Lexer` resolution (filename, then Analyse, then Fallback), `Spans(lines, first, last)` (planned) | `chroma/v2`, `theme` | `ui`, `editor` |
| `internal/explorer` | `Open(dir) (*Tree, error)`, `Tree` methods, `ListDir(dir, ListOptions) ([]Entry, error)` [V] | stdlib | `ui`, `fsops`, `finder`, tea |
| `internal/config` | `Load(paths) (Config, error)`, `Paths()` (planned, GAP item 27) | `hujson`, stdlib | `ui`, `editor` |
| `internal/lsp` (M8) | `Start`, `Initialize`, `DidOpen`, `DidChange`, `DidClose`, `Shutdown`, `Close`, `Diagnostics() <-chan DiagnosticsEvent` (see section 6) | `go.lsp.dev/protocol`, `go.lsp.dev/jsonrpc2`, stdlib | `ui`, `editor`, `app` (app adapts) |
| `internal/markdown` (when needed) | `New(style string, opts ...Option) (*Renderer, error)`, `Render(ctx, src, width)` | `charm.land/glamour/v2`, stdlib | `ui`, `app` (app adapts) |

## 3. Consumer-defined interfaces

Each interface lives in the package that calls it. Concrete types satisfy them through small adapters in `internal/app`. Test fakes implement these interfaces directly.

```go
// package ui

// EditorState is what the root model needs from the editor. The app adapter
// wraps *editor.Editor and maps editor.Tab to TabInfo.
type EditorState interface {
	SetViewport(rows, cols int)
	Do(a editor.Action, args map[string]any) editor.Result
	Active() editorpane.Document // nil when no tab is open
	Tabs() []TabInfo
	Status() string
}

type TabInfo struct {
	Name     string
	Modified bool
	Preview  bool
	Active   bool
}

// ExplorerTree is the subset of *explorer.Tree used by the explorer panel.
type ExplorerTree interface {
	Root() string
	Rows() []explorer.Row
	Selected() (explorer.Row, bool)
	Move(delta int)
	Toggle(path string) error
	Reveal(path string) error
	Reload() error
	ScrollBy(n int)
	ScrollOffset() int
	ShowHidden() bool
	SetShowHidden(v bool)
}

// FileIndex is the async file list from internal/finder.
type FileIndex interface {
	Snapshot() (files []string, loading bool)
	Done() <-chan struct{}
	Close()
}

// FileRanker scores paths for the palette and the browser filter.
// The finder package provides it (planned, see fuzzy).
type FileRanker interface {
	Rank(query string, files []string, limit int) []Ranked
}

type Ranked struct {
	Path    string
	Indexes []int // matched rune offsets, for highlighting
}

// FileOps is the file-operation surface (fsops, planned).
type FileOps interface {
	CreateFile(ctx context.Context, dir, name string) (string, error)
	Rename(ctx context.Context, path, newName string) (string, error)
	Trash(ctx context.Context, path string) error
}

// LSPSession is the subset of an LSP client the UI uses (M8).
type LSPSession interface {
	DidOpen(ctx context.Context, uri, languageID string, version int32, text string) error
	DidChange(ctx context.Context, uri string, version int32, text string) error
	DidClose(ctx context.Context, uri string) error
	Diagnostics() <-chan DiagnosticsEvent
}

type DiagnosticsEvent struct {
	URI     string
	Version int32
	Items   []Diagnostic // empty sets are skipped by the UI
}

type Diagnostic struct {
	Line, Col int
	Severity  int
	Message   string
}

// MarkdownRenderer renders a markdown preview at a fixed width (planned).
type MarkdownRenderer interface {
	Render(ctx context.Context, src string, width int) (string, error)
}

// Deps is the constructor input. Nil optional fields disable their features.
type Deps struct {
	Editor   EditorState
	Explorer ExplorerTree
	Files    FileIndex
	Ranker   FileRanker
	Ops      FileOps
	LSP      LSPSession        // nil until M8
	Markdown MarkdownRenderer  // nil until needed
	Keys     keymap.Keymap
	Theme    theme.Theme
}

// package editorpane

// Document is the read-only view the text pane renders.
type Document interface {
	Name() string
	Modified() bool
	LineCount() int
	Line(i int) string
	Cursor() buffer.Pos
	Selection() (anchor, head buffer.Pos, ok bool)
	LineSpans(i int) []syntax.Span // nil when no lexer
}
```

Notes on these signatures:
- `*explorer.Tree` already has `Root`, `Rows`, `Selected`, `Move`, `Toggle`, `Reveal`, `Reload`, `ScrollBy`, `ScrollOffset`, `ShowHidden`, `SetShowHidden` [V, from `explorer/explorer.go`]. Only the `ShowHidden` names need a final check against the source in P3.
- `*finder.Index` has `Snapshot`, `Done`, `Close` [V]. `FileRanker` and `Ranked` are planned, not existing code.
- `Document` method names are proposed. `Name` and `Modified` exist on `*editor.Document` [V]. The rest must be checked against `document.go` in P3.
- `DiagnosticsEvent` is defined in `ui`, not in `editor`. The libs report put the interface in the editor layer. This file puts it in `ui`, because `ui` is the only consumer, which keeps `editor` free of LSP imports.
- `MarkdownRenderer.Render` takes a `ctx`. The libs report had no `ctx`. The glamour call itself does not take one, so the adapter checks `ctx.Err()` before and after the call.

## 4. Component mapping (bubbles analyst, condensed)

| Surface | Implementation | Reason | Current gvim source | Target |
|---|---|---|---|---|
| Menu bar, dropdowns (separators, submenu headers, `when` rows) | custom | No bubbles menu; `list` has no separators or disabled-row skip | `menu_ui.go`, `menu_table_gen.go`, `chrome.go:257` | `ui/menubar` |
| Tab strip | custom | No tab component; needs overflow, preview style, dirty marks, click targets | `layout.go:277`, `chrome.go:70` | `ui` (tabs.go) |
| Editor pane | custom lazy render | `viewport` cannot express cursor or multi-cursor; render only visible lines, as crush's `list` does | `chrome.go:131`, `frame.go` | `ui/editorpane` |
| Vertical scrollbar | custom; reference `refs/crush/internal/ui/common/scrollbar.go:11` | No bubbles scrollbar | `chrome.go:133` | `ui/editorpane` |
| Explorer tree | custom render; state in `internal/explorer` | Expand, collapse, multi-select, cut dimming, reveal | `explorer_ui.go`, `chrome.go:82` | `ui/explorer` |
| Status bar, notices | custom; `spinner` for "Loading files…" | No bubbles status bar | `layout_panels.go:49` | `ui/statusbar` |
| Command palette (file, `>`, `:`, `#`) | `textinput` + `list.Model` (filtering off) + controller | `list` gives cursor and paging; the controller owns modes and the finder feed | `palette_ui.go`, `palette/palette.go`, `finder/score.go` | `ui/palette` |
| Find bar (Case, Whole, Regex toggles) | `textinput` + custom toggle row | Toggles and "Match N of M" are custom | `find_ui.go`, `chrome.go:224` | `ui/find` |
| Replace and query-replace (y/n/!/q) | `textinput` + custom key handling | Key semantics are custom | `find_ui.go:305-405` | `ui/find` |
| Goto (`N`, `N:COL`) | `textinput` + custom parse | | `prompt_ui.go:85` | `ui/prompt` |
| Save-as | `textinput` (optional `SetSuggestions`) | | `prompt_ui.go:106` | `ui/prompt` |
| Explorer name prompts (new, rename) | `textinput` | | `prompt_ui.go:160-196` | `ui/prompt` |
| Confirm (delete, overwrite/skip) | custom model with `key.Binding` | Crush `dialog/quit.go` is the pattern | `explorer_ops.go:78`, `prompt_ui.go:230` | `ui/prompt` |
| File browser (Ctrl+O) | custom on `explorer.ListDir`; `list` + `textinput` for filter | Needs `..`, `/`, `~`, `path:line:col`, Show Hidden, Name/Size/Modified sort | `browse_ui.go` | `ui/browser` |
| Folder picker | same browser with `folderOnly` | One `ListDir` implementation | `browse_ui.go`, `explorer.go` | `ui/browser` |
| Keybinding footer | `bubbles/help` + adapter over `internal/keymap` | Fits directly | `keymap/keymap.go` | `ui` (footer) |
| Mouse hit testing | custom layout-based hit map | No bubbles hit map | `hit.go`, `mouse.go` | `ui` (hit.go) |
| Cell surface | custom `grid` on `x/ansi` (decision 12) | Keeps the surface stable through the refactor | `grid.go` | `ui/grid` |

Rules for the components:
- A sub-model is a value type with `Update(msg) (Model, tea.Cmd)` and `View() string`, following the bubbles pattern.
- `Update` does no IO. Filesystem and finder work runs in `tea.Cmd` and returns a message.
- Bubbles key bindings are created from `internal/keymap` through one adapter, so keymap stays the single source for chords.
- Esc cancels in every overlay. `filepicker` is not used, so its Back-on-Esc default never applies.

## 5. CLI command tree and flags (cli analyst, verified in prototype)

```
gvim [DIR] [FILE[:LINE[:COL]]]...        launch the TUI (root, cobra.ArbitraryArgs, at most one DIR)
gvim config show                         print effective config (stub until M4 item 27)
gvim config paths                        print config, data and log paths
gvim grammar list                        list chroma lexers (lexers.Names(false), sorted)
gvim completion bash|zsh|fish|powershell cobra built-in (InitDefaultCompletionCmd)
gvim help [cmd]                          cobra built-in
```

Root persistent flags:

| Flag | Type | Default | Notes |
|---|---|---|---|
| `--config` | string | `""` | resolved per platform in `internal/config` |
| `--keymap` | string | `""` | built-in preset; validated in `app.Options.Validate` |
| `--theme` | string | `""` (resolves to `dark`) | GAP item 61: `main.go:49` hard-codes dark today |
| `--log-file` | string | `""` | no file log when empty |
| `-v, --version` | bool | | cobra built-in; prints `gvim version <Version>` |
| `-h, --help` | bool | | cobra built-in |

Behavior:
- `RunE` reads flags with `cmd.Flags().GetString` and calls `run(ctx, opts)`. It does not touch globals.
- `SilenceErrors` and `SilenceUsage` are set on the root. `main` prints the error once. Exit codes: 0 success, 1 error, 130 on interrupt (matches crush `root.go:199-240`).
- Directory-or-file classification uses `os.Stat` through an injected `stat` function in `internal/cli/args.go`, so tests do not touch the disk.
- A file named `config`, `grammar`, `completion` or `help` must be written `./name`. This is a cobra rule, observed in the prototype [V].
- `workspace open DIR` is omitted (decision 14).
- Fresh flags not adopted: `--cmd`, `--update`, `--skill`, `--attach`, `--stdin`, `--wait`, `--no-plugins`, `--safe`, `--event-log`, `--restore`, `--simple`, `--diff`, `--locale`, `--gui`, `--web`. Revisit `--stdin` and `--wait` after M2.

Cobra API names (cobra analyst, `go doc`, not re-run here): `ExecuteContext`, `InitDefaultCompletionCmd`, `GenZshCompletion`, `GenBashCompletionV2`, `RegisterFlagCompletionFunc`, `MarkFlagsMutuallyExclusive`, `RangeArgs`, `ExactValidArgs`, `OnlyValidArgs` [A, from `go doc`]. Mutual exclusion and flag completion are not run.

## 6. LSP and markdown plan

### LSP (M8, not before)

- Module: `go.lsp.dev/protocol` v1.0.1, `go.lsp.dev/jsonrpc2` v1.0.1, transitive `go.lsp.dev/uri` v1.0.1 [V, resolved in the scratch module at `/tmp/arch5/final`].
- Transport: spawn the server with `os/exec`, wrap stdin and stdout as one `io.ReadWriteCloser`, and call `jsonrpc2.NewConn(jsonrpc2.NewHeaderStream(rwc))`, then `conn.Go(ctx, handler)` [A, probe at `/tmp/arch4/lsp/main.go` runs in-process over `io.Pipe`, exit 0].
- Client API: `Start(ctx, ServerConfig) (*Client, error)`, `Initialize(ctx, rootURI)`, `DidOpen`, `DidChange` (full-text sync), `DidClose`, `Shutdown` (followed by `Exit`), `Close`.
- Use `protocol.ServerDispatcher(conn)` for the client side and `protocol.ClientHandler(client, fallback)` for server-to-client notifications [A].
- Diagnostics: the internal `protocol.Client` implementation pushes `PublishDiagnostics` into a buffered channel with a non-blocking send, because handlers run inline on the read goroutine [A].
- Use `jsonrpc2.Async` for any handler that issues a server-to-client `Call`. Otherwise the read loop deadlocks [A, per the `Conn.Go` docs].
- Implement these server-to-client methods explicitly: `RegisterCapability`, `WorkDoneProgressCreate`, `Configuration`, `WorkspaceFolders`, `ShowMessage`, `LogMessage`. Do not embed a nil interface, because a missing method then panics [A].
- Known gotchas [A]: `Diagnostic.Message` is a union type, so use `protocol.String("...")`; `TextDocumentContentChangeEvent` needs a pointer to `TextDocumentContentChangeWholeDocument`; `Version` is `Optional[int32]`, read with `.Get()`; skip empty `PublishDiagnostics`.
- Dependency weight: `go list -m all` lists about 254 modules in the scratch module [V] (the libs report counted about 300). The cause is the `tool (golangci-lint ...)` block in protocol's go.mod. The build list is small, and `go mod tidy -diff` must stay clean in P0.

### Markdown (deferred)

- Module: `charm.land/glamour/v2` v2.0.1, the same pin as crush (`refs/crush/go.mod:11`). Its transitive set includes `charm.land/lipgloss/v2` v2.0.6 and `alecthomas/chroma/v2`.
- Verified API [V, `go doc` in `/tmp/arch5/final`]: `NewTermRenderer(...TermRendererOption) (*TermRenderer, error)`, `(*TermRenderer).Render(in string) (string, error)`, `Close`, and options `WithStandardStyle`, `WithStylePath`, `WithStyles`, `WithStylesFromJSONBytes`, `WithStylesFromJSONFile`, `WithWordWrap`, `WithChromaFormatter`, `WithEmoji`, `WithPreservedNewLines`, `WithTableWrap`, `WithInlineTableLinks`, `WithBaseURL`, `WithEnvironmentConfig`, `WithOptions`.
- Not in v2 [V]: `WithColorProfile` and `WithAutoStyle`, which exist in glamour v1. The v1 TTY-query problem (OSC 11 and DSR written during `WithAutoStyle`) therefore does not apply directly. Whether v2 queries the TTY at construction is not verified, so pass `WithStandardStyle` chosen from config.
- Width is fixed at construction (`WithWordWrap`). Cache one renderer per width, as crush does (`refs/crush/internal/ui/common/markdown.go:137-143`). Render inside a `tea.Cmd`, never in `Update`.
- Choice rejected: glamour v1 (`github.com/charmbracelet/glamour` v1.0.0). The libs report needed `x/cellbuf` v0.0.15 pinned against `x/ansi` v0.11.9 for it to build [A]. v2 is on the same Charm v2 stack and needs no such pin in the scratch build [V].

## 7. Implementation phases, owners, gates

Workstreams (owners) are named, not people: WS-A (cli and app), WS-B (domain packages), WS-C (ui core), WS-D (ui overlays), WS-E (lsp and markdown), WS-F (parity and gates). Each file has one owner at a time. Dependency order: P0 then P1 and P2 in parallel, then P3, then P4, then P5 and P6, then P7, then P8 and P9, then P10.

| Phase | Scope | Owner | Files owned | Depends on | Exit |
|---|---|---|---|---|---|
| P0 | Baseline and gate. Record the 26-scenario floor on the current build. Add `require` lines in `go.mod` with the first importing code (`go mod tidy` drops unused requires). Add `migration/tools/import_rules_check.sh` (`go list -deps` checks against section 2). | WS-F | `go.mod`, `go.sum`, `migration/tools/import_rules_check.sh` | none | purity gate and tidy clean; floor recorded |
| P1 | CLI and app skeleton. `main.go` becomes thin; `parseArgs` moves to `internal/cli/args.go`; the existing TUI run is called through an adapter in `app`. | WS-A | `main.go`, `internal/cli/*`, `internal/app/*` | P0 | `gvim --help` lists the tree in section 5; arg-parsing table tests pass |
| P2 | Domain isolation. Move the scorer to `internal/fuzzy`. Add `internal/config` (JSONC). Add `ListDir` options for explorer (GAP item 9). | WS-B | `internal/fuzzy/*`, `internal/config/*`, `internal/palette/*` (scorer removed), `internal/explorer/listdir.go` | P0 | import-rules check passes; palette tests pass after the move |
| P3 | UI core. Root model, editor pane, tabs, menu bar (generated table moves), status bar, footer, grid, style, hit map. | WS-C | `internal/ui/{model,layout,hit,footer,tabs}.go`, `internal/ui/editorpane/*`, `internal/ui/menubar/*`, `internal/ui/statusbar/*`, `internal/ui/grid/*`, `internal/ui/style/*` | P1, P2 | parity floor S01–S26 passes on the text-pane region; `ui` unit tests pass |
| P4 | Overlays. Palette (textinput + list, modes), find bar, prompts (goto, save-as, names), confirm, browser (Ctrl+O and folder mode). Then delete `internal/tui`. | WS-D | `internal/ui/{palette,find,prompt,browser}/*`, `internal/ui/explorer/*` | P3 | M1 and M2 exit criteria in GAP-v3 pass; `internal/tui` deleted |
| P5 | Async finder. `finder.Start` in `app`, `FileRanker` adapter over `fuzzy`, palette fed with `SetItems` (≤100). | WS-B and WS-D | `internal/finder/*`, `internal/app/adapters.go` | P2, P4 | GAP-v3 items 10–14 pass; no synchronous filter in `Update` (test with 50,000 fake paths) |
| P6 | Explorer and file ops. `fsops` (create, rename, move, copy, trash). `app` orchestrates `fsops` then `editor.RenamePath`. | WS-B and WS-C | `internal/fsops/*`, `internal/ui/explorer/*` | P4 | GAP-v3 items 15–17, 31–36 pass |
| P7 | Syntax. `internal/syntax` spans per visible line; `editorpane` reads `LineSpans`. | WS-B | `internal/syntax/*`, `internal/ui/editorpane/spans.go` | P3 | GAP-v3 items 18–20 pass |
| P8 | Markdown (only when a feature needs it; not in M1–M10). | WS-E | `internal/markdown/*` | P1 | renderer tests with fixed width; no cache growth beyond the width set |
| P9 | LSP (M8). | WS-E | `internal/lsp/*`, `internal/app/adapters.go` (LSP part), `internal/ui/diagnostics.go` | P5 | GAP-v3 items 50–52 pass against a fake server |
| P10 | Retire `internal/tui`; final parity similarity report. | WS-F | `migration/parity/*` (report only) | P4 | no import of `internal/tui` remains |

Acceptance gates, run at the end of every phase:

1. `go build ./...` with default cgo settings.
2. `CGO_ENABLED=0 go build ./...`.
3. `go vet ./...`.
4. `golangci-lint run ./...` (version pinned in P0; not run in this pass).
5. `go test -race ./...` (see R3: needs cgo at test time, so it is a test-only gate).
6. `go fix -diff ./...` produces no output (not run in this pass).
7. `go mod tidy -diff` produces no output.
8. `migration/tools/purity_check.sh` prints `PURE_GO_OK`.
9. `migration/tools/import_rules_check.sh` passes (added in P0).
10. Parity: the 26-scenario floor (text pane only) passes. The similarity report is generated and attached, with no threshold.

Verification commands run in this pass (scratch only, `/tmp/arch5/final`):

```
go mod init scratch
go get charm.land/bubbles/v2@v2.2.1 charm.land/bubbletea/v2@v2.1.1 charm.land/lipgloss/v2@v2.0.6 \
       charm.land/glamour/v2@v2.0.1 github.com/spf13/cobra@v1.10.2 \
       go.lsp.dev/protocol@v1.0.1 go.lsp.dev/jsonrpc2@v1.0.1
go mod tidy
CGO_ENABLED=0 go build ./...                 -> exit 0
CGO_ENABLED=0 go vet ./...                   -> exit 0
go run .                                     -> "true <nil>"
CGO_ENABLED=0 go list -deps -f '{{if .CgoFiles}}{{.ImportPath}}{{end}}' ./...   -> (empty)
go list -deps ./... | grep -x runtime/cgo    -> 0 lines
GOOS=windows CGO_ENABLED=0 go build ./...    -> exit 0
GOOS=linux CGO_ENABLED=0 go build ./...      -> exit 0
go list -m all | wc -l                       -> 254
```

Resolved versions in that module [V]: bubbles v2.2.1, bubbletea v2.1.1, lipgloss v2.0.6, glamour v2.0.1, cobra v1.10.2, pflag v1.0.10, protocol v1.0.1, jsonrpc2 v1.0.1, uri v1.0.1, chroma/v2 v2.24.1 (glamour's choice). gvim's own `require` of `chroma/v2` v2.27.0 (GAP-v3 §3) wins under MVS when added directly, but that was not checked in the repo.

Verified in `refs/crush` and `refs/fresh` this pass: directory existence for `refs/crush/internal/ui/{model,dialog,common,list,styles,completions}`, `refs/crush/internal/ui/common/markdown.go` (lines 137–143 cache per width). The line numbers in the analyst reports for `ui.go`, `dialog.go`, `filepicker.go` and `scrollbar.go` were not re-read.

## 8. Parity policy

Keep it concrete so the gates are not argued each time:

- **Floor (gate):** `migration/parity/golden/S01.json`–`S26.json`, text-pane region only. Chrome rows (menu, tabs, status, explorer) are masked. The masked report mode already exists (`migration/parity/report-final-masked.json`) [V, file exists]. Scenario contents were not reviewed in this pass; WS-F confirms in P0 that all 26 are text-pane-only or masks them.
- **Retired gates (similarity only):** `golden_breakpoints`, `golden_sizes`, `golden_mouse`, `internal/tui/golden_test.go` four-screen expectations. They feed the similarity report. The inputs are `migration/tui_parity.py` and `migration/bench_tui.py` [A, not run here].
- **Similarity report (not a gate):** per screen, the fraction of identical cells, for the full frame and for the text pane. No threshold until the owner sets one. A drop of more than 5 points against the previous report needs a note in the phase exit. That 5-point figure is a proposal.
- **Intentional differences:** any screen that deliberately departs from Fresh (for example the palette without a forced `>` prefix, GAP-v3 item 12) is listed in the report's known-differences file, not treated as a failure.

## 9. Discrepancies between the analysts and the choices made

| Topic | Analyst position | Choice in this file | Basis |
|---|---|---|---|
| Glamour | libs: `github.com/charmbracelet/glamour` v1.0.0, with a cellbuf pin | `charm.land/glamour/v2` v2.0.1 | bubbles and crush agree; v2 builds in the combined scratch module with no pin [V] |
| LSP import paths | owner directive: `github.com/go-language-server/...` | `go.lsp.dev/...` | the owner's paths fail to resolve; module declares `go.lsp.dev` [A] |
| List filtering | bubbles: `Filter` adapter over `internal/finder` | filtering disabled in `list`; controller filters and feeds `SetItems` | `list.Model` filter runs synchronously in `Update` [A] |
| Markdown renderer signature | libs: `Render(src, width)` | `Render(ctx, src, width)` | cancellation and crush-like style |
| Diagnostics interface location | libs: editor layer | `ui` | keeps `editor` free of LSP imports |
| Surface | bubbles: custom `grid` or ultraviolet | custom `grid` | one surface change at a time (decision 12) |
| Chroma version | GAP-v3: v2.27.0 | v2.27.0 is the gvim pin; glamour's transitive chroma is v2.24.1 | MVS picks the higher direct pin; not re-checked in repo |

## 10. Risks and what was not verified

- **R1. Bubbles rendering inside gvim is not verified.** The bubbles analyst did not render `textinput` or `list` inside gvim's theme, and the textinput styling against gvim's theme keys is unchecked. Verify in P4 with a screenshot test that does not use the retired goldens.
- **R2. Glamour v2 TTY behavior is not verified.** Whether `NewTermRenderer` queries the terminal, and how color profile is chosen without `WithColorProfile`, was not tested. Use `WithStandardStyle` only, until tested.
- **R3. `-race` needs cgo at test time.** The Go race detector links a C runtime. `CGO_ENABLED=0` is therefore for shipped builds and the purity gate only. Run `go test -race` with cgo enabled as a test-only step and record that the shipped binary still builds with `CGO_ENABLED=0`. This needs an owner decision; the hard rule talks about the build, not the race detector.
- **R4. Import paths and module proxy drift.** The LSP and bubbles pins were checked against the proxy at the time of the reports. Re-check with `go list -m -versions` in P0.
- **R5. go.lsp.dev dependency weight.** `go list -m all` is about 254 modules because of protocol's `tool` block. `go mod tidy -diff` must stay clean, and the owner should accept the weight.
- **R6. The owner's go-language-server path.** The directive names a path that does not resolve. This file uses `go.lsp.dev`. If the owner wants `github.com/go-language-server/...`, it needs a fork or a `replace` directive, and that is an owner decision.
- **R7. Golden retirement is an owner decision.** Sections 1.9 and 8 retire four golden sets as gates. This file records that as the directive, not as a gate-less pass. The 26-scenario floor still gates the text pane.
- **R8. Not run in this pass:** `golangci-lint`, `go fix -diff`, `go test` in the gvim repo, the repo's `purity_check.sh`, `import_rules_check.sh` (it does not exist yet), cobra mutual exclusion and flag completion, and the planned `fuzzy`, `fsops`, `config` and `syntax` APIs. Those names are proposals.
- **R9. slowjs C lineage (GAP-v3 §4).** The plugin runtime decision is out of scope here, but its C-derived engine remains an open owner decision.
- **R10. Interfaces are not compiled.** The Go blocks in section 3 are signatures for review. They were not compiled against the domain packages in this pass.
- **R11. Clipboard, directory picker, and encoding (`x/text`)** were not evaluated for v4, and this file does not decide them.
- **R12. The repo was not run.** Only the scratch module at `/tmp/arch5/final` was built in this pass. The analyst scratch modules under `/tmp/arch4/*` were cited, not rebuilt. No commits were made.
