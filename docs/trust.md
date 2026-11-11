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
`<config.StateDir()>/trust.json`:

~~~json
{
  "version": 1,
  "workspaces": {
    "/Users/me/projects/app": "trusted",
    "/Users/me/projects/old": "blocked"
  }
}
~~~

`config.StateDir()` is gvim's per-user state directory: `$XDG_STATE_HOME/gvim` or
`~/.local/state/gvim` on Unix (macOS included), and `os.UserConfigDir()/gvim/state` on Windows.
It holds the files gvim writes for itself (this store, the recent list, the plugin toggles and
the update stamp), never the configuration: `config.Dir()` keeps what the user writes
(`config.json`, `init.lua`, `plugins`, `themes`). A store left in the config directory by an
older build is moved there once; when the move fails the store keeps reading and writing the
legacy file in place (the next launch retries the move), so a recorded decision is never lost.
The store stays a per-user file and is never written inside a workspace, because a repository
must not be able to vouch for itself. Keys are workspace roots
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
| Explicit executables inside the workspace | Enforced by the spawn gate (`trust.DecideSpawn`, wired through the workspace authority's one-shots/long-running children/terminal, the plugin OS and the LSP manager): an executable that resolves inside the root is refused, lexically and through symlinks, and a bare name is resolved against the child's effective PATH first. See §3.1. | **Enforced.** |
| LSP auto-start | `lsp_enabled` is forced off for the session when the workspace is not Trusted, so no server starts on its own (the LSP pill shows `LSP (off)`); Start/Restart LSP Server still starts one, as Fresh's manual start does in a restricted workspace. The user's config file is not changed. | **Enforced.** |
| Environment activation, devcontainers, tasks | gvim has none of these features. | Not applicable. |
| Blocked | Blocked now denies every spawn (terminals, shell/formatter jobs, LSP starts, git/find spawns, plugin processes) in addition to the Restricted restrictions. | **Enforced** by the spawn gate. |

Why the project layer is gated: it is the one place in gvim where a repository's file can
name a program to run. The project `.gvim/config.json` can set `formatters`, whose command
the editor runs on Format Buffer, and it can disable plugins and set project plugin
settings. gvim already refuses the most dangerous project keys unconditionally
(AMENDMENTS C11: project files cannot set keybindings/keymap, cannot name an LSP command,
can only *disable* a plugin and only *lower* `plugin_timeout_ms`). Trust is the logical
extension: the whole project layer waits for the user's decision. Under **Trusted** those
C11 restrictions still hold — trusting a folder opens its configuration, not the ability
to choose programs it should not.

The spawn gate (below) closes the process gap this page used to record, without changing
the model: it consults the live level at launch time, so a decision taken during the
session applies to the next spawn.

### 3.1 The spawn gate

One decision function, `trust.DecideSpawn(level, root, cwd, argv, pathValue)`, runs before
every launch. `pathValue` is the child's effective `PATH` (parent environment, then the
project environment overlay, then the launch's own environment), so a bare command name is
judged by the program that would actually run, not by an assumption that `PATH` holds only
system tools. Fresh's gate judges the explicit executable path only and lets a bare name through because a restricted workspace activates no environment, so no project `bin/` is on `PATH`; gvim resolves the bare name through the child's actual `PATH` as the Lead-directed strengthening (a user `PATH` entry pointing into the project, an empty entry — cwd — or a relative entry is caught), and checks the first token of the shell runner's `-c` string, which Fresh's gate does not see. Those are the deliberate differences; everything else follows Fresh's three-level policy.

The rule matrix:

| Level | Terminal (shell supplied separately) | Explicit path (relative/absolute) | Bare name |
|---|---|---|---|
| Trusted | allowed | allowed | allowed |
| Restricted, Undecided (effective Restricted) | allowed — the shell is checked like any command | refused when the path is root or inside it, lexically **or through symlinks** | resolved through `pathValue` in order (an empty entry is cwd, a relative entry is relative to cwd); refused when the first existing executable is inside the root; allowed when it resolves nowhere (the exec then fails) or outside the root |
| Blocked | refused | refused | refused |

The root check compares the cleaned path and `filepath.EvalSymlinks` of both the root and
the candidate, so a symlinked workspace and a symlinked executable both stay inside the
rule. For the shell runner (one `sh -c` per job/format) the shell is judged like any
command and the **first token** of the command string is checked the same way, which
catches a project-relative or PATH-resolved formatter/shell command
(`./.venv/bin/python -m black`). Refusals are the `*trust.SpawnRefusal` error: the level,
the refused spelling and "trust the folder to allow project tooling" (Blocked says no
processes may run). Each surface shows it with its existing notice/status path; nothing
falls back to a shell, and a configured formatter or plugin environment cannot bypass it
because the gate runs at the launch, after the configuration resolved.

Where it runs: the workspace authority (`authority/local.go`: `Run`, `Start` and the
dock terminal's `Launch`, with the environment-aware hook) covers git/find one-shots, the
editor shell/formatter runner and the integrated terminal; `internal/pluginhost`'s OS runs
it before every plugin `gvim.proc.run/spawn`; `lsp/manager` runs it in `launch` before
`lsp.Start`, so auto-start, manual Start/Restart and universal servers are all covered.
Only the local backend is gated: a remote launch (`internal/authority` SSH) keeps the
coarse rule in the app (untrusted remote → remote commands denied) because a remote trust
store is deferred to #21; its helper filesystem runs commands on the remote host, where a
remote path policy would be a different decision.

**Resolution of the executed program.** A gate decision is only as good as the binary that
actually starts, and Go's `exec.Command` resolves a bare name with the *parent* environment
before `cmd.Dir` or the child environment apply. The three seams therefore resolve the
program themselves with the same cwd and child `PATH` the gate used, and execute the
resulting absolute path: `pluginOS.command` (all plugin processes, and the authority's
local OS beneath the shell/formatter/git one-shots), `authority`'s local `Launch` (the dock
shell is returned resolved) and `lsp/manager`'s `launch` (the server command is resolved
before `lsp.Start`'s own lookup, which passes an absolute path through). Any future spawn
site must do the same (`trust.ResolveSpawnPath`, then exec the result); a call site that
re-runs `exec.LookPath` in the parent environment would reintroduce the mismatch. The
agent-discovery runner follows the same pattern when it pre-resolves its `tmux` argv.

Residuals, recorded rather than claimed solved: the `-c` command string is inspected only
for its first token (compound/quoted/variable-built commands are not parsed; the shell
runner gap is reduced, not contained); Windows `PATH` resolution does not implement
`PATHEXT` (an exact file name is required) and path comparisons are not
drive-letter-case-normalized; a `PATH` value the gate cannot see (a per-launch environment
injected below the authority/plugin OS/LSP layers) would not be resolved, though all three
seams pass their child environment and resolve through it; and the gate decides, it does
not kill an already-running process when the level drops. The open-time prompt and the trust dialog are
unchanged: a spawn refusal does not open a dialog, it names the level and the command, and
the user changes the decision through the existing trust UI.

Tests: `trust/gate_test.go` (level matrix, explicit paths, PATH order/empty/relative
entries, symlinks both ways, shell first token, refusal shape), `authority/local_gate_test.go`
(env-aware hook for Run/Start/Launch, plain-gate fallback), `internal/pluginhost/pluginos_gate_test.go`,
`lsp/manager/gate_test.go` and `internal/app/spawn_gate_test.go` (restricted/blocked/trusted
through the real authority and the wired LSP port).

**When a decision applies.** The trust state and the status pill change immediately when a
decision is taken. The *config-layer* restriction is a startup decision: the editor builds
its settings once, so a workspace trusted during a session applies its project config and
its project tooling at the next start. A decision is recorded at once and never asked
again. Switching the project with Switch Project loads the new root's decision and shows
its pill, but the running session keeps the config it started with (gvim does not hot-reload
the config; Fresh applies trust to live spawns instead).

**Config viewer.** `gvim config show` reads the same trust store the editor uses and gates
the project layer on the workspace's recorded level. It is read-only in the strict sense: it
does not call the open-time policy that would auto-trust and record a decision for a clean
undecided workspace, so an undecided workspace shows untrusted (what the editor would use until
the user decides at open) and nothing is ever written.
A trusted workspace merges the project file exactly as the editor does; an untrusted or
undecided workspace omits the project layer and prints a warning naming the file it skipped
("project config layer skipped: … (the workspace is not trusted; start gvim in it to decide
trust)"). The command never prompts, never records a decision and never changes a file.
`gvim config paths` still lists both locations and whether each exists: a path listing
contains no config, so it is not gated.

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

## Environments

Trust also gates project-environment activation. Detection is passive and runs at
workspace open: `env.detectors` (user config only, a project file cannot set it)
lists `{name, markers, kind, snippet, require}` entries. The built-ins are Fresh's:
`.venv`/`venv` (path-only, requiring an interpreter inside), direnv (`.envrc`),
mise (`mise.toml`, `.mise.toml`, `.tool-versions`), pipenv (`Pipfile`) and
poetry (`poetry.lock`) (shell). The first matching detector wins; a bare `.venv`
without an interpreter does not activate. Detection only stats markers.

Activation is host-gated on the **Trusted** level: a snippet runs project shell code
(or sources a repo-controlled activation script for a virtualenv), so
`gvim.env.set` refuses in Restricted/Blocked. The `env-manager` bundled plugin
(auto-activates path-only environments when trusted, asks the core trust dialog for
shell environments) is off by default; the detected environment and trust state are
also visible in Settings > Environment. An active environment's captured variables
are applied to every spawn the editor owns: shell commands and formatters, language
servers, terminal shells, plugin processes and app git-grep runs.

Reductions from Fresh: one local environment (no SSH/container authority), the
overlay is set-only (activation unsets are not applied to spawns), no persisted
recipe across sessions, no devcontainer deferral, and detectors are read-only in the
Settings UI (edit `env.detectors` in the user config JSON).
