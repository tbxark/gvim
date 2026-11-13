# Files gvim writes

Status: reference for the on-disk layout. The paths are the ones the code uses; the
tests cover the roots and the moves.

gvim writes to five roots. Only the first is configuration; everything else is state,
cache, per-workspace data or output the user asked for.

| Root | Unix (macOS included) | Windows | Override |
|---|---|---|---|
| Config | `$XDG_CONFIG_HOME/gvim`, else `~/.config/gvim` | `os.UserConfigDir()/gvim` | `--config PATH` (the user file only) |
| State | `$XDG_STATE_HOME/gvim`, else `~/.local/state/gvim` | `os.UserConfigDir()/gvim/state` | — |
| Cache | `os.UserCacheDir()/gvim` (`~/Library/Caches/gvim` on macOS) | same | — |
| Project | `<workspace root>/.gvim` | same | — |
| Daemon socket | `<cache>/daemon` | same | — |

`config.Dir()`, `config.StateDir()` and `config.CacheDir()` in `config/config.go` are
the only places that decide the roots.

## Config directory — what the user writes

| File | Written by | Notes |
|---|---|---|
| `config.json` | the Settings page (Edit ▸ Settings…, Save) | never created at startup; a missing file means the defaults |
| `init.lua` | **init: Edit init.lua** from the plugin template (when it does not exist) | created once, never overwritten |
| `plugins/` | the user | `<name>.lua` files and `<name>/` directories |
| `themes/` | the user | user themes; the registry only reads them |
| `plugin-data/<plugin>/state.json` | `gvim.state` of a **direct** launch | plugin data (not configuration); a daemon workspace keeps it in its session directory instead |

gvim writes nothing else here: the recent list, trust decisions, plugin toggles,
update stamps and the workspace registry live in the state directory.

## State directory — what gvim remembers

| File | Written when | Notes |
|---|---|---|
| `recent.json` | a workspace root is opened | the last 10 roots, newest first |
| `trust.json` | the user trusts, restricts or blocks a workspace | one file for every workspace, keyed by the canonical root; **never inside a project** |
| `plugin-state.json` | a plugin is enabled or disabled from the manager | direct launches; a daemon workspace keeps its own copy in its session directory |
| `update-check.json` | a successful update check | the debounce stamp |
| `workspaces/<encoded-root>.<stable-id>.json` | a daemon workspace is created, switched, captured or closed | the workspace registry (format version 2) |
| `workspaces/sessions/<stable-id>/` | a daemon workspace saves its session | same files as a project session below (only with `editor.save_session`) |

The workspace directory can be moved with `GVIM_WORKSPACE_DIR` (the tests use it).

## Cache directory — regenerable

| Path | Written when | Notes |
|---|---|---|
| `theme-cache/<sha256>.json` | a URL theme is fetched | the key is the sha256 of the trimmed URL |
| `slang-builtin/<module>-<sha256>.slang` | a slang LSP goto needs a synthetic source | content-addressed |
| `remote-sessions/<hash>/session/` | a remote workspace stages unsaved documents | only with `editor.save_session`; `GVIM_REMOTE_SESSION_DIR` overrides |
| `daemon/*.sock` | a daemon starts | the listening sockets |

## Project directory — `<root>/.gvim`

| Path | Written when | Notes |
|---|---|---|
| `config.json` | the Settings page saves the **project** layer | never created at startup; not touched by the session |
| `session/session.json` | the editor exits (or hot-exits) | tabs, cursors, MRU, split layout, explorer and sidebar state |
| `session/search.json` | the find/replace state changes | query, replacement and the option toggles |
| `session/terminal.json` | the terminal dock changes and the editor exits | terminal tabs, cwd, titles and the recorded screen |
| `session/stash-<random>.txt` | a document with unsaved text is saved | one file per dirty document |
| `session/recovery/` | every `editor.auto_recovery_save_interval_secs` while a document is dirty | `session.json` plus stash files; removed on a clean exit |

With `editor.restore_previous_session` off, a start restores only the hot-exit part of the
session: the tabs that carry unsaved text come back, and the saved active tab is active again
when it is one of them (otherwise the first restored tab is active).

`session/` exists only when `editor.save_session` is **on** (the default is off). With it
off, gvim reads and writes none of these files and does not create `.gvim` by itself; the
project `config.json` still works because the user writes it through Settings.

## What is not written by default

- A log file: only with `--log-file PATH`.
- A session or crash-recovery copy while `editor.save_session` is off.
- Anything inside a project from the trust store.
