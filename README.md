# gvim (agent notes)

> Written for coding agents developing this project, not for end users. It will be
> replaced by a user-facing README once the project is finished.

## What this is

gvim is a terminal editor written in Go. It is a port of the Fresh TUI (Rust, reference
checkout at `refs/fresh`) on Bubble Tea v2. Rendering is a custom cell grid
(`internal/tui/grid`), not bubbles components.

- `CLAUDE.md` holds the behavior rules and an architecture summary.
- `docs/architecture.md` is the design record for the package layout, package roles and
  import rules.
- `migration/ARCH-v4.md` is the earlier design record, kept for history; it is superseded.

## Layout

The module has three layers.

**Entry and executable shell (private):**

- `cmd/gvim/main.go` → `internal/cli` (cobra flags and arguments) → `internal/app.Run`.
- `internal/` holds the executable shell. Nothing outside this module can import it, and
  no top-level package of the module imports it either.
  - `internal/app`: the composition root (`Options`, `Run`, `buildRuntime` in `run.go`);
    the only package that imports the host packages and the plugin runtime.
  - host packages: `internal/uiadapter` (engine → TUI ports), `internal/pluginhost` (the
    Lua runtime host), `internal/lsphost` (language-server port), `internal/restore`
    (session + crash recovery), `internal/keybindings` (keymap adapter and user
    bindings), `internal/highlight` (syntax-span cache); `internal/apptest` is test-only.
  - `internal/configview`: the backend of `gvim config show` / `config paths`.
  - `internal/cli`: the cobra command tree; hands `app.Options` to a `Runner`.
  - `internal/tui`: the single Bubble Tea root model (`ports.go` defines its interfaces;
    `internal/tui/frame` is the geometry source for painting and hit testing). Sub-packages:
    `overlay`, `list` (the shared overlay contract and list/box kit), `frame`, `split`,
    `tabstrip` (layout/hit map, split tree, tab strip), `dock` (the terminal dock),
    `editorpane`, `menubar`, `explorer`, `palette`, `find`, `prompt`, `browser`, `picker`,
    `popup`, `ctxmenu`, `dialog`, `style`, `grid`, `icons`, `space`.
  - `internal/terminal`: runs a program in a pseudo-terminal and keeps a model of its
    screen (the dock backend).
  - `internal/filewatch`: the fsnotify watcher behind the file tree and reloads.
  - `internal/uitest`: an in-process test harness for the real UI (see Debugging).
- `cmd/gvimdebug`: the debug page. It runs gvim in a pseudo-terminal and serves its
  screen over HTTP (see Debugging).

**Engine and shared libraries (top level, TUI-free):**

- Text: `buffer` (text model and its large-file mode), `document` (one open file: text,
  carets, motions, folds, save planning, large-file mode) and `editor` (the tab/action
  session over documents), `search`, `syntax`, `fold`, `diff`, `encoding`, `session`,
  `decorations`, `textlayout`; `atomicfile` (atomic file replace) and `shellcmd` (run one
  program with a timeout) are the shared leaves.
- Project and files: `workspace`, `explorer`, `finder`, `fuzzy`, `fsops`, `gitstate`.
- Settings: `config`, `keymap`, `theme`.
- Services: `lsp` (+`lsp/manager`, `lsp/lsptest`), `pluginapi` (plugin contract), `plugin`
  (Lua runtime, with `plugin/hostos`, `plugin/bundled`, `plugin/plugintest`,
  `plugin/pattern`), `commands` (the command
  registry that feeds the command palette).
- They must not import `internal/` and must not import bubbletea, bubbles or lipgloss.
  Engine packages do not import the UI, the app or the plugin runtime.
- `tools/import_rules_check.sh` (R1–R16) enforces the direction; see
  `docs/architecture.md`.

**Data and tools:**

- `tools/`: `purity_check.sh` and `import_rules_check.sh` are the architectural gates;
  `gen_menus.py` regenerates the menu bar; `lint_version.txt` pins the linter.

- `docs/plugins/`: the plugin author documentation and the `gvim.d.lua` type stubs.
- `refs/`: read-only reference checkouts. Do not edit or import them. `_attic/` is the
  retired TUI and is ignored by `./...`.
- No Makefile, no `.sphere/`, no proto or ent generation.

## Build and run

Requirements: the Go toolchain named in `go.mod`, `golangci-lint` v2 for lint, and
Python 3 for two generators. Google Chrome and `playwright-core` are only needed for
scripted screenshots of the debug page.

```sh
go build ./...                              # compile all packages
go build -o /tmp/gvim ./cmd/gvim                     # build the binary outside the repo
go run ./cmd/gvim [DIR] [FILE[:LINE[:COL]]]...       # run the TUI
go run ./cmd/gvim --help                             # full flag list
go run ./cmd/gvimdebug -- "$PWD" main.go    # run the TUI behind the debug page
```

Build the binary outside the repo root. A `gvim` binary there is not in `.gitignore`.

The shipped binary must stay pure Go:

```sh
CGO_ENABLED=0 go build ./...
bash tools/purity_check.sh        # prints PURE_GO_OK
bash tools/import_rules_check.sh  # prints IMPORT_RULES_OK
```

Gates to pass before calling work done:

```sh
go vet ./...
golangci-lint run ./...                     # 0 issues at the time of writing
go test ./...                               # all pass at the time of writing
go test ./internal/tui -run TestName         # one test while iterating
go test -race ./...                         # test-only; needs cgo
```

Generated files. Do not edit them by hand:

- `commands/commands_gen.go`: `python3 commands/gen/generate.py refs/fresh`
- `internal/tui/menubar/menu_table_gen.go`: `python3 tools/gen_menus.py`
  (`--check` reports drift)

## Debugging the TUI

Use the lowest layer that shows the bug.

### 1. Unit tests that drive `Update`

Most UI logic can be tested without a terminal. Build the model, send messages, and
assert on the model state, the emitted `tea.Cmd`s, layout rectangles and hit-map results.
Examples: `internal/tui/footer_test.go`, `internal/tui/hit_test.go`. To check rendering,
assert a few specific cells of the grid, not the whole frame.

### 2. `uitest`: the real program in memory

`uitest.Start(t, model, width, height)` runs the program in memory. It parses the output
with the `charmbracelet/x/vt` emulator and returns a session.

```go
s := uitest.Start(t, m, 100, 30)
s.Key("ctrl+p")                  // also s.Type("abc"), s.Click(x, y), s.Wheel(x, y, true)
s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Palette") })
sc := s.Screen()                 // sc.Cell(x, y), sc.Row(y), sc.Find(text)
s.Screenshot("/tmp/frame.png")
```

Key and mouse calls only queue messages. Call `WaitFor` or `Settle` before reading the
screen, or you may read a stale frame.

Key spec: a single character is that key. Otherwise, join the modifiers `ctrl`, `alt`
and `shift` to a key name with `+`, for example `ctrl+s` or `shift+down`. Key names:
`enter esc tab backspace delete space up down left right home end pgup pgdown f1`–`f12`.

Read `internal/uitest/session.go` before writing a new harness.

### 3. Screenshot sweep

```sh
GVIM_SHOTS=/tmp/gvim-shots go test -run TestWriteScreenshots -count=1 ./internal/app/
```

This writes 10 PNG frames from `internal/app/screenshots_test.go`: welcome, one file,
explorer, menu, palette, command palette, find, save-as, unsaved changes, and twelve tabs.
Open them with an image-capable read tool. They are for looking at, not for assertions.
The test skips without `GVIM_SHOTS`.

### 4. Debug page: `cmd/gvimdebug`

This command runs gvim in a pseudo-terminal, using `internal/terminal`, and serves its screen
on a local web page. It is a debug view, not a copy of your terminal (see Fidelity below).
The arguments after the flags go to gvim unchanged, so `--` ends the
debug flags:

```sh
go run ./cmd/gvimdebug -- "$PWD" main.go
go run ./cmd/gvimdebug -addr 127.0.0.1:9000 -- --diff old.go new.go
```

Flags:

- `-addr`: where the page listens. Default `127.0.0.1:8765`.
- `-cols`, `-rows`: the terminal size. Default 140×40.
- `-bin`: a prebuilt gvim binary to run. By default each start builds this module into a
  temp directory, so it must be run from inside the module.
- `-real-home`: keep the user's HOME and config. By default gvim gets a temp HOME, so a
  debug run does not read or write the user's session, recent files or config.
- `-font PATH`: a font file for the screen. The server serves it at `/font`, and the page
  loads it with `FontFace`. Use it when the browser cannot find the installed font by name.
  Safari did not find JetBrains Mono Nerd Font by name, and showed boxes for the icons.
  Example: `-font ~/Library/Fonts/JetBrainsMonoNerdFontMono-Regular.ttf`.

The page at `/` shows the screen. The cursor cell is inverted. Keys typed while the focus
is outside a field go to gvim. The page is split into `index.html` (layout), `app.css`
(styles) and `app.js` (behaviour), all embedded in the binary. The layout has:

- A top bar with the connection (`live`, `reconnecting`), gvim's status and a Restart
  button. The status is one of `running`, `exited: …`, `building`, `build failed`,
  `start failed` or `stopped`. A failed build keeps the page up. The build output goes to
  the terminal that runs the command, not to the page.
- The screen on the left, and a panel on the right with a Size card, a key field,
  quick-key buttons, a text area and mouse help. The Size card sets gvim's columns and
  rows, has a "Fit to window" switch, and changes the font size with A− and A+. The screen
  uses JetBrainsMono NFM, the family name written in the font file, then JetBrainsMono
  Nerd Font Mono, Symbols Nerd Font Mono and Menlo. These are looked up by name, and each
  browser matches names in its own way. With `-font`, the page loads the file instead. A
  terminal's font setting needs the family name `JetBrainsMono NFM` for this font file.
- A bottom bar with the terminal size, the cursor position and the last error.

Mouse input on the screen: click, right-click and scroll send a mouse event to the cell
under the pointer. The page updates as soon as gvim draws, because the server pushes each
change over `/events`.

The HTTP API:

| Method | Path       | Body                                                  | Response |
|--------|------------|-------------------------------------------------------|----------|
| GET    | `/`        |                                                       | the page; `/app.css` and `/app.js` are served beside it |
| GET    | `/events`  |                                                       | server-sent events, one JSON state on connect and after every change: `{"status","frame","cols","rows","cursor":{"row","col","visible"}}` |
| GET    | `/frame`   |                                                       | the screen as HTML spans; empty when gvim is not running |
| GET    | `/status`  |                                                       | the status line |
| POST   | `/key`     | one key spec, as in `uitest` above                     | 204; 400 for an unknown key; 409 when gvim is not running |
| POST   | `/text`    | text; each character is typed, `\n` sends Enter        | 204 |
| POST   | `/mouse`   | `{"action":"click","button":"left","x":3,"y":7}`      | 204; 400 for a bad action or button |
| POST   | `/restart` |                                                       | 204; 500 with the build error |
| POST   | `/resize`  | `{"cols":120,"rows":30}`                              | 204; 400 outside 10–400 columns or 4–200 rows. A running gvim is resized at once, and the next start keeps the size |

Mouse actions are `click`, `press`, `release`, `move`, `wheel-up` and `wheel-down`. Buttons
are `left`, `middle` and `right`. Coordinates are zero-based screen cells.

```sh
curl -d 'ctrl+e' 127.0.0.1:8765/key        # focus the file explorer
curl -d 'down' 127.0.0.1:8765/key
curl -d '{"action":"click","button":"left","x":3,"y":7}' \
     -H 'Content-Type: application/json' 127.0.0.1:8765/mouse
curl -X POST 127.0.0.1:8765/restart
curl 127.0.0.1:8765/frame > /tmp/frame.html
```

### Automating the page

Use the page from Playwright rather than from curl when you need to wait for the result of
an action. Every control has a `data-testid`, and each quick-key button has a `data-key`:

| Selector | What it is |
|----------|------------|
| `[data-testid=screen]` | the screen; its text is `innerText` |
| `[data-testid=status]` | the status chip; `data-state` is `running`, `building`, `stopped`, `exited` or `failed` |
| `[data-testid=connection]` | the stream chip; `data-state` is `connecting`, `live` or `reconnecting` |
| `[data-testid=size]`, `[data-testid=cursor]` | the terminal size and the cursor position |
| `[data-testid=message]` | the last input error; empty when the last input was accepted |
| `[data-testid=key-input]`, `[data-testid=key-send]` | the key field and its button |
| `[data-testid=text-input]`, `[data-testid=text-send]` | the text area and its button |
| `[data-testid=restart]` | the Restart button |
| `[data-testid=cols-input]`, `[data-testid=rows-input]`, `[data-testid=resize-apply]` | the size fields and their Apply button |
| `[data-testid=fit-toggle]` | the "Fit to window" checkbox |
| `[data-testid=font-smaller]`, `[data-testid=font-larger]`, `[data-testid=font-size]` | the font size buttons and the current size |
| `[data-testid=screen-card]` | the box around the screen, which the fit measures |
| `[data-key="ctrl+e"]` | a quick-key button; the attribute is the key spec |

`window.gvimdebug` drives the page from script:

- `key(spec)`, `text(s)`, `mouse({action, button, x, y})`, `resize(cols, rows)` and
  `restart()` each return a promise. It resolves when the server accepts the input, and
  rejects with the server's message when it does not.
- `frames` is the number of states drawn so far. Read it before an action, then call
  `waitForFrame(before)` to wait for the next state that gvim pushed.
- `screenText()` returns the screen as plain text.

```js
const before = await page.evaluate(() => window.gvimdebug.frames);
await page.evaluate(() => window.gvimdebug.key('ctrl+e'));
await page.evaluate(n => window.gvimdebug.waitForFrame(n), before);
const text = await page.evaluate(() => window.gvimdebug.screenText());
```

To screenshot the page, use Playwright. `playwright-core` with
`chromium.launch({ channel: 'chrome' })` drives the installed Chrome, so no browser download
is needed. Load the URL, wait for `[data-testid=status][data-state=running]`, then call
`page.screenshot`. The script used so far lives outside the repo at
`/tmp/gvim-shot/tools/`. Recreate it if it is gone.

The key and mouse encodings are in `cmd/gvimdebug/keys.go`, with table-driven tests in
`keys_test.go`.

### Fidelity: what the page is and is not

The page is a debug view, not a copy of the terminal you use. gvim's output goes through a
terminal emulator (`charmbracelet/x/vt`, via `internal/terminal`), and each cell is drawn as
text in the browser. Use the page to check layout, colours and input behaviour. Check the
final look in a real terminal.

Differences to keep in mind:

- **Emulator.** The screen is what `x/vt` makes of gvim's output, not what your terminal
  app would show. An escape sequence the emulator does not handle is shown differently,
  or not at all.
- **Font and metrics.** Glyphs come from the font the page loads (`-font`, or the installed
  font names), not from your terminal's font. Line height is 1.25 em, and the cell width is
  the font's advance. So the same `-cols` can fit a different width in the browser.
  Nerd Font icons take the width the font gives them.
- **Attributes.** Only foreground, background, bold, underline and reverse are drawn.
  Italic, faint, strikethrough, blink and styled underlines are not drawn.
- **Default colours.** A cell without a colour uses fixed dark defaults (`#d0d0d0` on
  `#101012`), not your terminal theme.
- **Cursor.** The cursor is drawn as an inverted cell while it is visible. Its shape (block,
  bar, underline) and blinking are not shown.
- **Keys and mouse.** Keys are encoded by a fixed table (`cmd/gvimdebug/keys.go`), so a key
  combination outside it is rejected or sent differently. The page sends a left click, a
  right click and the wheel. Middle clicks, modifier-clicks and drags are not sent from the
  page. The API also takes `press`, `release` and `move`.
- **Not shown.** Selection and copy, scrollback, the bell, the window title and image
  protocols. The page has none of them.

Known limits:

- The screen is the only output. Model internals are not exposed.
- Wide characters are not aligned. The second cell of a wide character is drawn as a space.
- The size starts at `-cols` and `-rows`. The Size card changes it while gvim runs, and
  "Fit to window" follows the browser window. Turning fit off keeps the last size. The
  next restart keeps the size too.
- Icons need gvim's config to ask for them. The debug run uses a temp config, so gvim
  shows plain text. Use `-real-home` to read your own `ui_icons` setting.
- The browser takes some shortcuts (Ctrl+T, Ctrl+W, Cmd+*). Use the quick-key buttons or
  `curl` for those.
- Every code change needs a restart, which rebuilds gvim and resets the editor state. There
  is no hot reload.
- The page listens on loopback only and has no authentication.

## Language servers

gvim starts a language server by itself, as Fresh does: it has a built-in table of servers, and a
file of a language opens its server when the program is on `PATH` (`gopls`, `rust-analyzer`,
`pylsp`, `typescript-language-server`, `clangd` and about fifty more). When the program is missing
the status row says which one and how to install it, once per language. Override or turn off a
server per language, or all of them, in `~/.config/gvim/config.json` or the project's
`.gvim/config.json`:

```json
{ "lsp": { "python": { "command": "pyright-langserver", "args": ["--stdio"] }, "rust": { "enabled": false } } }
```

`"lsp_enabled": false` turns every server off. See `docs/lsp.md` for the table, the messages and what
the servers add (diagnostics, Format Buffer through the server, and the rest).

Rename (F2) edits every file the server returns. Files that are not open are opened in new tabs, edited
and left unsaved, like the open ones. Save them with Save All.

## Lua plugins

gvim runs plugins written in Lua 5.1 on `github.com/yuin/gopher-lua` (pure Go). The old
JavaScript system (goja, esbuild) is gone. Users and plugin authors read `docs/plugins/README.md`;
the design record is `migration/ARCH-v4.md` (package graph and the plugin files by package).

- `pluginapi`: the contract. Plain types, the `Host` and `Runtime` interfaces, error
  codes, `Unsupported`. Stdlib only.
- `plugin`: the runtime (sandbox, tasks and the yield protocol, budgets, strikes, the
  loader, every `gvim.*` area in `api_*.go`). It imports no ui, editor or buffer package.
  `plugin/bundled` embeds the bundled plugins (`plugins/`) and Lua modules (`lib/`);
  `plugin/plugintest` is the fake host, fake OS, manual clock and `Harness` that plugin tests use.
- `plugin/pattern`: the Lua pattern matcher with a step budget that replaces `string.find`,
  `match`, `gmatch` and `gsub`. `decorations`: the decoration store.
- `internal/tui/plugin_*.go` is the host side (pump, events, prompts, keys, status, decorations,
  windows, virtual buffers, manager, init.lua, LSP). `internal/app/plugin*.go` wires the runtime to the real editor
  (`pluginedit.go`, `pluginos*.go`, `pluginservices.go`, `plugins.go`). Update does no IO: plugin
  work runs in `tea.Cmd`s as `pluginapi.Work`.
- All Lua runs on the Bubble Tea update goroutine. A host method runs inside a Lua call and must
  never call into the runtime; it validates and defers (`m.plug.later`).

Plugin gates, besides the usual ones:

```sh
go test ./plugin/... ./plugin/pattern ./pluginapi ./decorations
go test -run 'TestStubsMatchRuntime|TestDocs' ./plugin   # docs/plugins vs the runtime
go test -run TestBundledPluginsIdleHeap -v ./internal/app          # memory gate, prints the numbers
go test -race ./plugin/... ./plugin/pattern ./internal/tui/... ./internal/app
```

- `docs/plugins/gvim.d.lua` must declare exactly the functions a live plugin state has, and every
  function must appear in the Markdown (`TestStubsMatchRuntime`, `TestDocsCoverTheAPI`). Adding
  an API function means adding it to `gvim.d.lua` and to the right page of `docs/plugins/`.
  Every file in `docs/plugins/examples/` is loaded and run by `TestDocsExamples`.
- The default-enabled bundled plugins may keep at most 8 MiB of heap while idle
  (`TestBundledPluginsIdleHeap`; about 6.5 MiB today). When a new bundled plugin breaks it, set
  `default_enabled: false` in its `plugin.json` (largest first) and update `docs/plugins/bundled.md`.
- Tests that depend on time budgets assert on step counts or budget errors, with at most one
  generous wall-clock sanity bound. Do not add a tight wall-clock threshold.
- gopher-lua v1.1.2 miscompiles `a, b = b, a` on two local variables. Do not write it in Lua
  plugins (bundled or tests).
- `.js` and `.ts` plugins are never run.

## Testing rules

From `CLAUDE.md`:

- Plain Go unit tests: table-driven, asserting values and behavior.
- No golden or snapshot tests for the TUI, no full-frame string comparisons, and no new
  golden-screen scenarios.
- Fix a bug with a regression test that fails first.
- Keep the existing `testdata` fixtures. Do not regenerate them to make a test pass.
- `TestWriteScreenshots` is a debugging aid. It is gated by an environment variable, skips
  by default, and asserts nothing, so `go test ./...` stays green.

## Working rules for agents

- Keep diffs surgical. Match the surrounding style, and do not refactor adjacent code.
- Do not commit or push unless asked. Do not run commands with outside side effects
  (publishing, tagging, deploying).
- Do not edit or import `refs/` or `_attic/`.
- Before finishing, run `go vet ./...`, `golangci-lint run ./...`, both purity and import
  gates, and `go test ./...`.
