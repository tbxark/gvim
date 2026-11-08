# Language servers

gvim detects language servers the way Fresh does. It has a built-in table of servers, one for
each language below, and it starts a language's server when you open a file of that language and
the server's program is on `PATH`. There is nothing to configure for the common ones: install the
server and open a file.

Where gvim differs from Fresh: Fresh's shipped entries all carry `auto_start: false`, so a server
there waits to be started from the palette, while its documentation says it is used automatically.
gvim follows the documentation and starts a built-in server when a file opens. An entry with
`"auto_start": false` (see below) gives the Fresh behaviour for one language.

The status row has an `LSP` pill for the active file. Its states follow Fresh's LSP
indicator, in Fresh's priority order, with gvim's wording where Fresh's is less precise:

| Pill | Meaning |
|------|---------|
| `LSP` | the file is open on its language server |
| `LSP …` | the server is starting (Fresh shows a spinner in this state) |
| `LSP (off)` | a server is configured for the language, but none runs: switched off, dormant (`auto_start: false`), started by hand and stopped, or turned off for this buffer |
| `LSP (missing)` | the server's program is not installed (Fresh has no state for this: it reports `LSP (error)` only after a failed start) |
| `LSP (failed)` | the program started and failed its handshake (Fresh's `LSP (error)`) |
| `LSP (n/a)` | this buffer is skipped (a large file, or LSP off for the buffer) while the language's server runs for another open buffer (Fresh's `LSP (n/a)`) |

A language with no server shows no pill. Click it, or run **Show LSP Status**, for the
details. The states are Fresh's priority order: the per-buffer `(n/a)` first, then a failure,
then starting and running, and last the language-level `(off)`. gvim adds no fixed-width
padding to the pill (Fresh pads it to 11 cells so the bar does not shift): the right group is
right-aligned, and the blank slot before the count badge keeps the badge's columns for it.

Nothing is started when the program is missing. The first time a file of that language opens, the
status row says what is missing and how to get it, once per language and session:

```
gopls is not installed (the language server for go): go install golang.org/x/tools/gopls@latest
```

Run an LSP command on that language (hover, go to definition, rename, **Show LSP Status**) and the
same message comes back. After you install the server, run **Start/Restart LSP Server** and gvim
looks again.

The `PATH` search and every start run in a command, never in the update loop, so a slow disk or a
slow server cannot freeze typing.

## Built-in servers

The program, and how to get it. A language that gvim's syntax detection names differently (for
example `C++` for `cpp`) uses the same entry.

| Language | Program | Install |
|----------|---------|---------|
| rust | `rust-analyzer` | `rustup component add rust-analyzer` |
| go | `gopls` | `go install golang.org/x/tools/gopls@latest` |
| typescript, javascript, tsx, jsx | `typescript-language-server --stdio` | `npm install -g typescript-language-server typescript` |
| python | `pylsp` | `pip install python-lsp-server` |
| java | `jdtls` | `brew install jdtls` |
| zig | `zls` | `brew install zls` |
| latex | `texlab` | `brew install texlab` |
| markdown | `marksman server` | `brew install marksman` |
| c, cpp | `clangd` | `brew install llvm` |
| kotlin | `kotlin-lsp --stdio` | `brew install JetBrains/utils/kotlin-lsp` |
| html, css, json, jsonc | `vscode-*-language-server --stdio` | `npm install -g vscode-langservers-extracted` |
| yaml | `yaml-language-server --stdio` | `npm install -g yaml-language-server` |
| toml | `taplo lsp stdio` | `brew install taplo` |
| lua | `lua-language-server` | `brew install lua-language-server` |
| bash | `bash-language-server start` | `npm install -g bash-language-server` |
| csharp | `csharp-ls` | `dotnet tool install -g csharp-ls` |

The rest of Fresh's table is there too: ruby, php, dart, nushell, solidity, terraform, cmake,
protobuf, graphql, sql, vue, svelte, astro, tailwindcss, nix, swift, scala, elixir, erlang,
haskell, ocaml, clojure, r, julia, perl, nim, gleam, racket, fsharp, templ, typst, c3 and slang.
`lsp/manager/defaults.go` is the whole table, with the install hint of each.

Verilog and SystemVerilog use `svls` (`cargo install svls`), and the assembly languages (asm, gas,
NASM) use `asm-lsp` (`cargo install asm-lsp`).

A `.h` file is opened as C++, so clangd reads it as C++, when its directory holds a C++ source or
header: `.cc`, `.cpp`, `.cxx`, `.C`, `.c++`, `.hpp`, `.hh` or `.hxx`. That is Fresh's rule for the
sibling files. Fresh also looks for a C++ `compile_commands.json` in the parent directories, which
gvim does not, so a header with no C++ sibling is opened as C.

Two servers do not start on their own, as in Fresh: `ols` (odin), which most platforms have no
binary for, and the Godot server (gdscript), which needs a running Godot editor. Turn one on with
an entry that has no command, or start it by hand (see below):

```json
{ "lsp": { "odin": { "enabled": true } } }
```

### Project roots

Each built-in entry has Fresh's `root_markers`: the files that mark a project, such as
`Cargo.toml` for rust, `go.mod` for go and `package.json` for typescript. A server runs in the
nearest directory above the file that has one of them (the name, or a glob such as `*.csproj`), so
a crate in a subdirectory of the folder you opened gets its own server root. The walk stops at the
folder you opened, and a file with no marker above it uses that folder, as does a language with
no markers. An `lsp` entry may set `root_markers` for its language.

## Changing or turning servers off

Entries of the `lsp` key in `~/.config/gvim/config.json` are laid over the table. The key is the
language ID shown in the status row.

```json
{
  "lsp": {
    "python": { "command": "pyright-langserver", "args": ["--stdio"] },
    "go": { "command": "gopls", "args": ["-remote=auto"] },
    "rust": { "enabled": false }
  }
}
```

- A `command` replaces the program of the language, and `args` its arguments. `args` without a
  `command` keeps the built-in program.
- `"enabled": false` turns the server of one language off. gvim never looks for it.
- `"auto_start": false` keeps the server dormant: it does not start when a file opens, but
  **Start/Restart LSP Server** runs it. `"auto_start": true` (or `"enabled": true`) on a manual
  server such as `ols` starts it on open.
- A language that is not in the table needs a `command`, and then runs like the others.
- The other fields that Fresh's config writes for a server (`env`, `initialization_options`,
  `only_features`, `except_features`, `language_id_overrides`, `process_limits` and `name`) are
  accepted, so a Fresh config still loads, and then ignored: gvim does not pass them to the server.
- `"lsp_enabled": false` at the top level turns every language server off, the entries of `lsp`
  included. The LSP commands then say so.

A server that is off for any of these reasons can still be started by hand, as in Fresh: with the
active file of that language, run **Start/Restart LSP Server** from the palette, or **Restart Server** in the LSP menu. That
starts the server for the language for the rest of the session, whatever `lsp_enabled`,
`"enabled": false`, `"auto_start": false` or the manual servers say.

### Who may choose the program

Only your own config file, `~/.config/gvim/config.json`, can name a program to run. A project's
`.gvim/config.json` comes with the code you opened, and opening a cloned repository must not run
whatever it names. Fresh gates language servers behind a workspace-trust prompt; gvim takes the
simpler rule: in a project file the `lsp` key can switch a language off or turn a built-in server on
(`enabled`, `auto_start`), but a `command` or `args` there is refused with a message that says to
set it in the user config, and your own entries stay as they are. A project file's `lsp` entries
are added to yours; they never replace them. The programs of the built-in table are found on your
`PATH`, as Fresh does.

Workspace trust adds a second gate above this rule: the whole project layer, this key included, is
read only when the workspace is Trusted (see [workspace trust](trust.md)). An untrusted workspace
also starts no language server on its own — `lsp_enabled` is forced off for the session, so the
status row shows `LSP (off)`; **Start/Restart LSP Server** still starts one, as Fresh's manual
start does in a restricted workspace.

While a server is off or missing, the editor works as before: syntax colors, completion of the
words in the buffer and everything else do not depend on it.

## What the servers add

- **Diagnostics.** The status row counts the diagnostics of the active file next to the pill,
  as Fresh's diagnostics element does: `[E:1 W:2 I:3]`, errors, warnings, and info and hints
  together, each part only when its count is above zero. The counts are the stored diagnostics
  of the file, so they stay on the row while the pill says the server is missing, failed or off,
  and they go when the server is stopped, as Fresh's do. While the server runs with no count
  known yet the row keeps a blank seven-cell slot, so the right group does not jump when the
  first diagnostic arrives; on a narrow row the palette hint and then the badge are dropped,
  and the pill keeps its place.
  Each affected line has a dot in the gutter, in the color of its worst severity, and the
  offending text is underlined. **Jump to Next Error** and **Jump to Previous Error** move to the
  next and previous error or warning and show its message in the status row. They are `F8` and
  `Shift+F8`, as in Fresh; the macOS keymap uses `Ctrl+'` for the next error, and the emacs keymap
  has no key for them. The command palette runs them in every keymap, and **Keybinding Editor**
  changes the keys.
- **Hover** shows the diagnostics under the caret, with their source, above what the server says
  about the symbol. A line wider than the popup is wrapped over several rows, never cut.
- **Go to definition, implementation, references, completion, rename, code actions, signature
  help.** Rename edits every file the server returns. A file that is not open opens in a new tab,
  edited and left unsaved, like Fresh. Save it with Save All.
- **Format Buffer.** It formats with an external formatter when there is one, and otherwise asks
  the language server (`textDocument/formatting`). The caret stays on its text when the formatter
  adds or removes lines above it. With neither a formatter nor a server, the status row says which
  is missing, for example:
  `Format Buffer: no formatter is set for zig (set formatters.zig in config.json), and zls is not installed ...`.

  The external formatters are Fresh's table, used when the program is on `PATH`: `gofmt` for go,
  `rustfmt --edition 2021` for rust, `ruff format` for python, `clang-format` for c and c++ and
  `prettier` for javascript, typescript, json, jsonc and yaml. `gofmt` ends the file with a newline,
  which a plain save does not (Fresh's `ensure_final_newline_on_save` is off by default). The
  `formatters` key of your config replaces the default for a language, by name or by extension, and
  `$FILE` in its command is the quoted path of the buffer:

  ```json
  { "formatters": { "go": "gofumpt", ".md": "prettier --stdin-filepath $FILE" } }
  ```

## When a server fails

A server that starts but fails its handshake is reported with its name, the command line and the
last lines it wrote to its error output, instead of a bare `initialize: EOF`:

```
gopls (language server for go) failed to start: lsp: initialize: EOF [command: gopls serve] [it said: flag provided but not defined: -serve]
```

gvim does not start it again for that language until **Start/Restart LSP Server**.

Servers only start for files that have a name on disk, and not for files in large file mode.
