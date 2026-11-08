# Workspace trust

Status: design record of the workspace-trust feature (2026-11-08).

Opening a folder can expose the editor to code the repository chose: language servers,
formatters, plugin settings, build scripts. Workspace trust is the per-folder decision
that gates that code. This file records Fresh's model (`refs/fresh`,
`crates/fresh-editor/src/services/workspace_trust.rs`), the Go port of it in package
`trust`, what gvim enforces with it, and what it does not.

## 1. Fresh's model

A workspace is at one of three levels, shown as the `{trust}` status-bar pill:

| Level | What may run |
|-------|--------------|
| **Trusted** | Everything: language servers, build scripts, tasks, environment activation. |
| **Restricted** (default) | System tools found on `PATH` (`git`, `ripgrep`, the system `python`). An explicit executable path that resolves to a file inside the workspace is refused. Environment activation and language-server auto-start are gated on trust. |
| **Blocked** | Nothing at all. |

"Undecided" is not a level value. Fresh's store answers it with a predicate,
`TrustStore::is_decided`, and an undecided workspace reads the safe default,
Restricted. The open-time policy is:

1. a recorded decision is honored;
2. undecided and the folder has **no executable-content markers** (a plain text/docs
   folder): Fresh trusts it silently and *records* Trusted, so the question does not come
   back;
3. undecided and the folder **can execute code**: the workspace runs Restricted in memory
   (not persisted), and a mandatory modal asks. Its secondary button is **Quit**; leaving
   that way leaves the store undecided, so the prompt fires again next time. Any of the
   three concrete choices is recorded and ends the prompt.

A prompt opened voluntarily ("Workspace Trust…" from the palette, a click on the status
pill, activating another workspace) is *cancellable*: its secondary button is Cancel, and
dismissing it changes nothing.

The markers are Fresh's shallow, passive scan: env files (`.venv`, `venv`, `.envrc`,
`mise.toml`, `.mise.toml`, `.tool-versions`, `Pipfile`, `poetry.lock`), project
manifests whose language servers run project code (`Cargo.toml`, `go.mod`,
`package.json`, `pyproject.toml`, `pom.xml`, `build.gradle`, `build.gradle.kts`,
`CMakeLists.txt`, `compile_commands.json`, `Gemfile`, `composer.json`), a
dev-container config, and `.sln`/`.csproj`/`.fsproj` files. Nothing is executed to
detect them.

The store is one JSON file per project, `<data dir>/workspaces/<encoded canonical
root>/trust.json` holding `{"level": "trusted"}`, written atomically (pid-tagged temp
file and rename). A linked git worktree resolves to the repository's main worktree, so one
repository is one decision. A missing or corrupt file reads undecided; a repo can never
vouch for itself, since the store is in the user's data directory.

### What Fresh gates with it

- **Process spawns.** Every spawn (integrated terminal, LSP servers, plugin
  `spawnProcess`, formatters, find-in-files) is routed through the active authority's
  spawner, which consults the trust level: Trusted allows, Blocked denies, Restricted
  denies only a spawn whose explicit executable path resolves inside the workspace. A bare
  command name resolves through `PATH` and is allowed, because no environment is
  activated in a restricted workspace.
- **Language-server auto-start** is gated on trust; a manual start still works.
- **Environment activation** (venv/direnv/mise/pipenv/poetry) requires Trusted.
- **Project config is *not* trust-gated**: Fresh merges `.fresh/config.json` regardless
  of the level.

## 2. gvim's model

Package `trust` is the TUI-free port of the model: `Level` (`Trusted`, `Restricted`,
`Blocked`, plus the zero `Undecided`), `State` (effective level, whether decided, the
markers), the `Store`, and Fresh's open-time policy. The app resolves the state before it
loads the config, passes it to the TUI through `tui.Deps.Trust` (see
`internal/tui/trust_state.go`), and gates the config layer on it.

**Store path and format.** gvim keeps *one* store file,
`<config.Dir()>/trust.json`:

~~~json
{
  "version": 1,
  "workspaces": {
    "/Users/me/projects/app": "trusted",
    "/Users/me/projects/old": "blocked"
  }
}
~~~

`config.Dir()` is gvim's user config directory: `$XDG_CONFIG_HOME/gvim` or
`~/.config/gvim` on Unix (macOS included), and `os.UserConfigDir()/gvim` on Windows.
It is where `config.json`, the recent list and the plugins live. Keys are workspace roots
made canonical with `filepath.EvalSymlinks`, so a symlinked spelling and its target share
a decision. The store is written through `atomicfile` (temp file, sync, rename), so a
half-written file is never read; the directory is created on the first write. A missing
file, a corrupt file, an entry with an unknown level and a newer `version` all read as
*undecided*, and the next decision rewrites the file cleanly. The path is a parameter of
`trust.NewStore`, and the app builds the default through the package variable
`app.newTrustStore`, so tests never touch the real config directory.

Divergence from Fresh, and why: Fresh uses one file per project under its data directory,
so concurrent editors never contend and the encoding is a directory name. gvim already
keeps its persistent user state (config, recents, plugin state) in one config directory,
and a single small map file is simpler to inspect; the cost is that two gvim processes
deciding at the same instant keep the later write only. That is a rare user action and the
lost entry only asks again. The store is still outside the repository on every platform.

**Startup policy: identical to Fresh.** `trust.Store.Open` is the one place it lives:
a recorded decision is honored; an undecided plain folder is trusted silently and the
decision recorded; an undecided folder with executable content stays Restricted and
undecided, and the TUI opens the mandatory prompt. The config snapshot for the session is
built from the state that resolved, so an untrusted workspace starts configured by the
user's own file only. Language-server auto-start is gated on the same state: the config the
`lsphost` port is built from has `lsp_enabled` forced off for the session when the
workspace is not Trusted, which is exactly Fresh's `lsp_autostart_allowed` rule (a manual
Start/Restart LSP Server still works, and the user's file is not modified).

**Config keys.** Fresh has no configuration key for the trust policy: the store is the only
state, and the level is surfaced through the status bar element and the plugin snapshot.
gvim adds no key either. There is nothing to set: open the dialog and decide. (Fresh's
`env.detectors` markers feed its trust scan; gvim has no env activation, so its marker
list is fixed in `trust/markers.go`.)

## 3. Enforcement in gvim: the mapping

| Fresh gate | gvim | Status |
|---|---|---|
| Project config layer | Skipped entirely (`config.Options.Untrusted`) unless the workspace is Trusted. Covers `formatters`, `lsp` entries, `languages`, display settings, plugin enablement/settings. | **Enforced**, and **stricter than Fresh**, which merges the project config at any level. |
| Project plugins, plugin settings | Via the project layer skip: a project cannot turn a plugin off or set a project setting. gvim discovers no plugins from the workspace itself (plugins come from the binary, the user config dir and `--plugin`), so there is nothing else to gate. | **Enforced** via the config layer. |
| Explicit executables inside the workspace | Not enforced: gvim has no single process-spawn choke point. Terminals, shell commands, formatters, LSP server spawns, git/finder spawns and plugin process spawns are not consulted against the trust level. | **Not enforced** (gap). |
| LSP auto-start | `lsp_enabled` is forced off for the session when the workspace is not Trusted, so no server starts on its own (the LSP pill shows `LSP (off)`); Start/Restart LSP Server still starts one, as Fresh's manual start does in a restricted workspace. The user's config file is not changed. | **Enforced.** |
| Environment activation, devcontainers, tasks | gvim has none of these features. | Not applicable. |
| Blocked | Same restrictions as Restricted in gvim (project layer skipped, no language-server auto-start); the difference is the label and the notice. | **Partially enforced**: no project tooling, but processes are not blocked (same gap as above). |

Why the project layer is gated: it is the one place in gvim where a repository's file can
name a program to run. The project `.gvim/config.json` can set `formatters`, whose command
the editor runs on Format Buffer, and it can disable plugins and set project plugin
settings. gvim already refuses the most dangerous project keys unconditionally
(AMENDMENTS C11: project files cannot set keybindings/keymap, cannot name an LSP command,
can only *disable* a plugin and only *lower* `plugin_timeout_ms`). Trust is the logical
extension: the whole project layer waits for the user's decision. Under **Trusted** those
C11 restrictions still hold — trusting a folder opens its configuration, not the ability
to choose programs it should not.

The known gap is a process gate. Fresh's authority layer gives it one seam to wrap; gvim's
spawns are spread over `editor` shell jobs, formatters, `internal/lsphost`, the terminal
dock (`internal/terminal`) and `plugin/hostos`. A later round can add a spawn gate that
consults `State.Level` at those call sites (deny an explicit executable path inside the
workspace under Restricted, deny all spawns under Blocked) without changing this model.

**When a decision applies.** The trust state and the status pill change immediately when a
decision is taken. The *config-layer* restriction is a startup decision: the editor builds
its settings once, so a workspace trusted during a session applies its project config and
its project tooling at the next start. A decision is recorded at once and never asked
again. Switching the project with Switch Project loads the new root's decision and shows
its pill, but the running session keeps the config it started with (gvim does not hot-reload
the config; Fresh applies trust to live spawns instead).

**Config viewer.** `gvim config show` still merges the project layer: it is a read-only
diagnostic of the files, not a simulation of the trust gate. The editor itself follows the
gate.

## 4. UI

The dialog is `internal/tui/trustdialog.go`, a card in gvim's style (`docs/ui.md`):
68 cells wide, title on the top border, content padded by `space.PadX`, rounded border in
the theme's accent, buttons inside the card. The texts are Fresh's locale strings:

- title `⚠ SECURITY WARNING`;
- "This project folder can execute arbitrary code:", then `Path: <canonical root>` and,
  when markers were found, `Detected: <markers, comma separated>`;
- "How would you like to proceed?";
- three radio rows with descriptions — "Trust folder & Allow Tooling (T)", "Keep
  Restricted (Default) (K)", "Block All Execution (B)";
- `[ OK ]` and the secondary button: `[ Cancel (Esc) ]` for a cancellable prompt,
  `[ Quit (<the keymap's quit key>) ]` for the mandatory open-time one.

The card is sized to its content and shrinks the option descriptions when the frame is too
short for the whole text, so the buttons always stay on it. It is an exclusive modal: while
it is up it takes every key and mouse message, and nothing behind it is interactive.

Keys mirror Fresh's dialog:

| Key | Action |
|---|---|
| Up / Down | move the selection, wrapping around the three rows |
| T / K / B | select Trust / Restricted / Block |
| Enter, O | confirm the selected row (the two-step: selecting is not consenting) |
| Esc | cancel a prompt the user opened; inert on the mandatory one |
| the keymap's quit key (`Ctrl+Q`, `Cmd+Q`, `C-x C-c`) | quit the editor from the mandatory prompt |
| a click on a radio | select the row (a click is not consent either) |
| a click on a button | answer it |

The dialog opens from three places:

- **Workspace Trust…** in the command palette (`workspace_trust_prompt`, generated in
  `commands/commands_gen.go`), cancellable;
- a click on the status-bar trust pill (the status row paints `Trusted` /
  `Restricted` / `Blocked`; the label comes from `Model.trustStatus`, and the click
  calls `Model.openWorkspaceTrust`), cancellable;
- the open-time prompt, mandatory.

After a decision the status row shows Fresh's notice: "Workspace trusted — project tooling
may run processes", "Workspace restricted — repo-controlled execution is blocked", or
"Workspace blocked — no processes will run". A decision that could not be written to the
store still applies to the session and the row says it was not saved. A session that cannot
build a store (no user config directory) leaves trust unavailable: no port, no pill, no
prompt, nothing gated, and the command says "Workspace trust is unavailable in this
workspace" — an editor that cannot record a decision must not restrict itself with one.

## 5. Files

| File | Role |
|---|---|
| `trust/trust.go` | `Level`, `State`, `ParseLevel`, `Effective` and the trust predicates. |
| `trust/store.go` | `Store` (`LevelFor`, `Set`, `Open`), the canonical-root key and the JSON file. |
| `trust/markers.go` | Fresh's executable-content marker scan. |
| `internal/app/trust.go` | `openTrust` (startup resolution) and the `tui.TrustPort` implementation. |
| `internal/app/run.go` | Resolves the state before `config.Load`, sets `Options.Untrusted`, wires `Deps.Trust`. |
| `config/config.go` | `Options.Untrusted` skips the project layer; the C11 project rules are unchanged. |
| `internal/tui/trust.go` | The action, the trust port and messages, the notices, the project-switch load. |
| `internal/tui/trust_state.go` | `TrustPort`, the model's trust state, `trustStatus`, `trustLevel`. |
| `internal/tui/trustdialog.go` | The dialog overlay: layout, painting, keys, mouse. |

## 6. Tests

- `trust/trust_test.go`: levels, parsing, effective fallback, the state predicates.
- `trust/store_test.go`: canonical keys (symlinks), round trip, separate workspaces,
  corrupt/newer/unknown-level files, the three startup-policy cases, save failures,
  default path under the user config dir, file format.
- `trust/markers_test.go`: every marker class and the report order.
- `config/trust_test.go`: the project layer is read for a trusted caller and skipped for
  `Untrusted`, user layer included.
- `internal/app/trust_test.go`: startup policy, the unavailable-store fallback, the config
  gate end to end, the silent-decision write, the controller's decide/load/save-failure
  behavior.
- `internal/tui/trust_test.go`: the command opens the dialog, the drawn strings and
  markers, keyboard and mouse selection (without consenting), confirm/cancel/quit paths,
  the mandatory startup prompt and its inert Esc, the notices, the status label, and a
  project switch loading the new root's state.
