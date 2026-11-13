# Events

```text
gvim.on(event, fn, opts?) -> Subscription      -- raises on an unknown event name
gvim.once(event, fn, opts?) -> Subscription    -- unsubscribes itself after the first call
gvim.off(sub)   sub:off()   sub:active() -> boolean
gvim.emit(name, payload?) -> true | nil, err
```

```lua
gvim.on("after_file_save", function(ev)
  gvim.notify("saved " .. ev.path)
end)

gvim.on("buffer_changed", function(ev)
  if ev.overflow then rescan(ev.buf) else patch(ev.buf, ev.edits) end
end, { debounce = 300 })
```

* `opts.buf = Buffer` delivers only events about that buffer; `opts.debounce = ms` (1 to 60 000)
  delivers the *last* event after a pause of that long, trailing edge, merged (not on
  `before_file_save`); `opts.after_ms = ms` (default 1000, `idle` only) says how long the editor must be
  idle.
* Each handler call is a **task** and may wait (`before_file_save` and `shutdown` may not). The
  budget of an event handler is `min(50 ms, plugin_timeout_ms)`. A handler that raises is logged and
  does not stop other handlers; five failures in a row unsubscribe it.
* Handlers run in dispatch order: plugin load order, then subscription order. Plugins take turns:
  one plugin cannot use up the whole turn of the others.
* Payload keys are `snake_case`. `buf` is a `Buffer` handle, positions are `Pos` and `Range` (0-based
  lines, byte columns), `version` is a version token string.
* An edit a handler makes is delivered like any other (there is no "by" field). Compare
  `b:version()` if you must tell your own edits from the user's. A handler that edits yields exactly
  one `buffer_changed` after it returns, in the same or the next update.
* Subscribers can be at most 256 per plugin.

## Delivery rules

* **Hot events** (`cursor_moved`, `selection_changed`, `viewport_changed`, `buffer_changed`,
  `buffer_modified`, `pre_command`, `post_command`, `idle`) are computed only while some plugin
  subscribes. Under a storm they may be dropped (at most 8 rounds and 1000 handler calls per editor
  update; the rest of a storm is dropped and the plugin whose handler made it gets a strike). One
  subscription gets at most one of each hot event per buffer per update: `buffer_changed` payloads
  are **concatenated** (`edits` appended in arrival order, `undo` is the OR, `version` the latest;
  more than 256 records or 256 KiB becomes `overflow = true` with no `edits`); the other hot events
  keep the earliest `previous` and the latest state.
* **Lifecycle events** (everything else, including `plugins_loaded`, `ready`, `resize`,
  `after_file_open`, `buffer_activated`, `buffer_closed`, `after_file_save`, `diagnostics_updated`
  and your own `<plugin>:<name>`) are never dropped: what a cap leaves goes to the next update.
* Within one round: lifecycle events first, then text events, then cursor and viewport.
* **Replay.** A plugin that loads after the editor has been running (a reload, **Load Plugin from
  Buffer**, enabling it in the manager) is sent `after_file_open` and `buffer_activated` for the
  open buffers with `replay = true`, so it can initialise. The files given on the command line are
  opened before the plugins load, so they arrive the same way, after `plugins_loaded` and `ready`,
  with `replay = true`. A plugin that also scans
  `gvim.buf.list()` at load can see a buffer twice: make initialisation idempotent.

## The catalogue

| Event | Payload | Raised when |
|---|---|---|
| `plugins_loaded` | `{}` | every plugin that can finish loading has |
| `ready` | `{}` | the first window size after loading; held until `plugins_loaded` was delivered |
| `shutdown` | `{}` | gvim exits: `Runtime.Stop` runs the handlers, 200 ms in total for all plugins, 50 ms each, no waiting |
| `resize` | `{width, height}` | the terminal size changes (not for the first size) |
| `after_file_open` | `{buf, path}` | a tab with a file appears (any way of opening: explorer, palette, CLI, session restore, a plugin's `buf.open`) |
| `buffer_activated` | `{buf, previous}` | the active tab changes; `previous` is absent when the old tab closed |
| `buffer_deactivated` | `{buf, next}` | the previous active tab; not raised for a closed one |
| `buffer_closed` | `{id, path}` | a tab closed. `id` is the old id as a plain **number**: the handle is dead. `path` is `""` for an untitled buffer (`b:path()` is `nil` for it) |
| `path_changed` | `{buf, path, old_path}` | a buffer's path changed (save-as or a rename applied to it) |
| `buffer_group_closed` | `{group, name}` | a plugin buffer group closed (closed by the plugin or by unloading it); `group` is the group id |
| `before_file_save` | `{buf, path, source}` | before a save is planned; **vetoable** (below) |
| `after_file_save` | `{buf, path}` | a file was written, after the result is applied |
| `after_file_revert` | `{buf, path}` | a revert was applied |
| `buffer_modified` (hot) | `{buf, version, modified}` | the version or the modified flag changed |
| `buffer_changed` (hot) | `{buf, version, edits, undo, overflow}` | the text changed. `edits` is an array of `{range, text, removed}` |
| `cursor_moved` (hot) | `{buf, pos, previous}` | the primary caret of the active buffer moved |
| `selection_changed` (hot) | `{buf, range}` | the selection changed; `range` is absent when it was cleared |
| `viewport_changed` (hot) | `{buf, top_line, height, width}` | the focused pane of the active buffer scrolled or resized |
| `focus_gained` | `{}` | the editor got its keyboard back after a plugin panel, dock or overlay had it |
| `window_focused` | `{}` | the outer terminal window gained OS focus |
| `window_blurred` | `{}` | the outer terminal window lost OS focus |
| `active_window_changed` | `{previous, current}` | the focused split window changed; ids are plain numbers, absent/0 when there is no focused pane |
| `chrome_focus_changed` | `{region, plugin?, panel_id?}` | the chrome region with the keyboard changed: `editor`, `explorer`, `dock` or `section` (a plugin section names `plugin`/`panel_id`; the native Contents/Diagnostics sections carry neither); gvim's tab strip is the extension region `tabs`. Overlays and the menu are transient surfaces and do not raise it. |
| `key_release` | `{key, text?, ctrl, alt, shift, super}` | a key release the terminal backend reported, in the same shape as a plugin key capture. Legacy terminals report no releases and gvim requests no kitty keyboard enhancement flags, so this fires only on backends that deliver release events (for example the Windows console API); a release never edits or moves anything. |
| `language_changed` | `{buf, language}` | `Set Language`, or the path changed |
| `config_changed` | `{}` | the keymap changed |
| `theme_changed` | `{name, dark}` | a theme was selected or a plugin applied session color overrides |
| `env_changed` | `{detected, active, name, kind}` | the project environment was detected, activated or reloaded; `name`/`kind` are the active environment's, else the detected one's, else `""` |
| `trust_changed` | `{level}` | a workspace trust decision was recorded; `level` is `trusted`, `restricted` or `blocked` |
| `pre_command` / `post_command` (hot) | `{action, args}` / `{action, args, ok, err}` | a command the user started (palette, key, menu); not what a plugin runs with `command.run` |
| `idle` (hot) | `{ms, period}` | the editor was idle for `opts.after_ms`; once per idle period. `period` is a number that changes with each input |
| `mode_changed` | `{mode, plugin, previous, previous_plugin}` | the editor input mode changed; absent fields mean "none" |
| `diagnostics_updated` | `{buf, path, counts}` | the language server's diagnostics for a buffer changed; `counts = {error, warning, info, hint}` (all zero when they cleared) |
| `lsp_status_clicked` | `{}` | the LSP status pill in the status row was clicked (the server list popup opened) |
| `lsp_server_error` | `{error}` | a language-server call failed; `error` is the message the status row shows |
| `action_popup_result` | `{popup, action}` | an action popup opened with `gvim.ui.action_popup` closed; `popup` is its overlay id and `action` the chosen action id, or nil |
| `widget_event` | `{panel, event, widget?, index?, checked?, value?, cursor_byte?, included?, focus_key?, target_key?, target_index?, x?, y?, button?, wheel?}` | a mounted widget panel or sidebar section: `activate`, `toggle`, `select`, `expand`, `change`, `completion_accept`, `completion_dismiss`, `drag`, `drop`, `dragend`, `focus`, `focus_gained`, `focus_lost`, `cancel` and `mouse_click`/`mouse_move`/`mouse_wheel` (see [api-ui.md](api-ui.md#widget-panels-and-sidebar-sections)) |
| `<plugin>:<name>` | the payload of `gvim.emit` | a plugin emitted it |

Details worth knowing:

* `window_focused`/`window_blurred` report the host terminal window's OS focus, not which gvim
  surface holds the keyboard. They are raised once per real state change: the model starts
  host-focused, so startup and repeated `FocusMsg`/`BlurMsg` reports for the same state raise
  nothing. `focus_gained` remains the chrome/editor keyboard event (a plugin panel, dock or
  overlay handing the keyboard back) and is not raised by a terminal focus change. The
  events carry no payload and are queued through the normal plugin event pump after the
  dock's terminal focus reporting is updated; no plugin callback runs inside the focus
  message itself and the pump's budget bounds any Lua it drains.

* `chrome_focus_changed` fires once per change of the stable chrome region. It does not fire for
a menu or an overlay, and the initial editor focus raises nothing. The native Contents and
Diagnostics sections report `region=section` with no `plugin`/`panel_id`; a mounted plugin
section adds both. `tabs` is a gvim extension (Fresh has no tab strip region).
* `key_release` only exists on backends that report releases through Bubble Tea's
`tea.KeyReleaseMsg`. gvim deliberately does not request the kitty keyboard protocol's event
types (no fake kitty support and no fallback that would change key encoding), so a legacy
terminal never raises it; the Windows console backend can. It is independent of key capture:
a release is not a chord step and is never typed.
* `buffer_changed.edits` are in **sequential coordinates**: each record's `range` is expressed in the
  text *immediately before that record*; apply the records first to last to turn an old copy of the
  text into the new one. A replace is two records (a delete, then an insert at its start). `removed` is
  the text that went away (empty when it was over 64 KiB or in large mode). `overflow = true` (and no
  `edits`) means more than 256 records, a large file, or a buffer that was replaced as a whole:
  rescan. Decorations of such a buffer are dropped by the editor; set them again.
* `cursor_moved` and `selection_changed` cover the primary caret of the **active** buffer; switching
  tabs is `buffer_activated`, not a cursor move.
* `idle`: the editor sends growing `ms` values while idle on a ladder (every 100 ms to 1 s, 250 ms to
  10 s, every second to a minute, every 10 s to an hour) and your handler runs once per idle period, the
  first time `ms >= after_ms`. The timer runs only while some plugin subscribes to `idle`; a
  subscription made while the user is idle is picked up at the next editor message.
* `config_changed` is raised for a keymap switch only; `theme_changed` for `select_theme`.
* `diagnostics_updated` is raised from the editor's own diagnostics poll (about three times a second
  while a document is open); read the diagnostics with `gvim.lsp.diagnostics(ev.buf)`.

* `widget_event` is the input of the panels the plugin mounted: `panel` is the id `mount_widget_panel`/`mount_sidebar_section` returned, `event` says what happened, `widget` is the key of the widget it is about, `index` is the 1-based row, `checked`/`value`/`cursor_byte`/`included` carry the state after the event, `focus_key` is the widget that holds the panel focus afterwards, and the tree drag events add `target_key` (the row under the pointer) and `target_index`. The mouse events add the frame cell `x`/`y`, the `button` and the `wheel` delta. A text field raises `change` on every edit (with `value` and `cursor_byte`; accepting or dismissing a candidate of its completion list raises `completion_accept` / `completion_dismiss`); a dropdown, number or radio raises `change` with `value`/`index`; a tree raises `expand` with `checked` = expanded. See [api-ui.md](api-ui.md#widget-panels-and-sidebar-sections).

## before_file_save and the veto

`before_file_save` runs **before** the save is planned, so a handler may edit the buffer and the edit
is saved. It must not wait, has 25 ms, and a handler that waits or times out is ignored with a
warning and a strike.

```lua
gvim.on("before_file_save", function(ev)
  if ev.path:match "%.generated%.go$" then
    return false, "generated files are not saved by hand"   -- cancels the save
  end
end)
```

A handler that returns `false` (or `false, "reason"`) cancels the save and the reason is shown on the
status row. `source` says what started the save: `"manual"` (the user), `"autosave"` or `"api"`
(`b:save()`); use `cursor = "keep"` for edits when `source == "autosave"`. Save All and autosave
raise one event per modified buffer in tab order; a veto of any of them cancels the whole round (a
cancelled autosave is silent and tried again at the next tick). The unsaved-changes dialog's *Save* is
gated like any other save. See [trim_on_save.lua](examples/trim_on_save.lua).

## gvim.emit

`gvim.emit("<plugin>:<name>", payload?)` raises an event for the subscribers of that exact name in
**every** plugin. The prefix must be **your own plugin's name**: other prefixes, and the names of host
events, return `nil, "invalid: ..."`, so a plugin cannot speak for another. The payload is plain data
(at most 64 KiB) and at most 1000 of your events may be waiting at once (`limit`). Names are
`<plugin>:<name>` with `[a-z][a-z0-9_]*` parts.


## Focus announcer

The active window announcements are emitted by one announcer, and the buffer pair by the
buffer diff, which seeds the announcer from the same transition so the two paths never
report one change twice. The announcer diffs the focused (workspace, split window id, active
buffer id) and fires exactly one transition:

- active_window_changed with the previous and current split window ids (0 when there is no
  focused pane), and focus_gained on the no-window -> window transition.
- buffer_deactivated (only when the previous buffer still exists) and buffer_activated with
  the new buffer; a closed buffer is reported by buffer_closed and the following
  buffer_activated omits "previous". These are emitted by the buffer diff, which then seeds
  the announcer (so a split/window change does not repeat the pair).
- A workspace switch forces the entering workspace's announcement even when its numeric ids
  happen to equal the leaving workspace's. The window payload carries the workspace label in
  addition to its documented fields; the buffer pair carries it on that forced switch
  announcement (the ordinary buffer diff announces only the buffer fields).

Window and buffer ids are editor-global when local daemon workspaces share one plugin
runtime: the host packs a workspace sequence into the high bits, so a workspace that has
buffer 1 and split window 1 and another workspace with the same local numbers still expose
different ids, and an id from another (possibly closed/evicted) workspace is refused with a
CodeClosed error rather than resolving to the active workspace's colliding local id. Direct
launches have no shared namespace and keep their local ids. Widget focus is a separate
widget_event and is not part of this announcement.


## Editor-global ids

In a daemon whose local workspaces share one plugin runtime, every buffer and window id in
the plugin API and in event payloads is editor-global: the host packs the workspace's
sequence in the high bits and the workspace-local id in the low 40 bits. Two workspaces may
use the same local split/buffer numbers; their global ids differ. Passing an id that belongs
to another workspace (or to a workspace that was closed or evicted) returns CodeClosed
instead of silently acting on the active workspace's local id with the same number, and a
buffer that closed stops resolving even if the local editor later reuses the number. The
same applies to a mount's optional buf option, window labels/window state and the group,
decorations, line-target and language-server calls. Direct launches never share a runtime:
their ids stay the workspace-local numbers they always were.
