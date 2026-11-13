# Buffers and windows

A **buffer** is an open document (a tab). A **window** is a split pane that shows a buffer. The
cursor and the selection belong to the buffer, not to the window. Coordinates are 0-based lines
and 0-based *byte* columns ([conventions.md](conventions.md#bases-and-ranges)).

## gvim.buf

```text
gvim.buf.current() -> Buffer | nil
gvim.buf.get(id) -> Buffer | nil
gvim.buf.list() -> Buffer[]                          -- in tab order
gvim.buf.find(path) -> Buffer | nil
gvim.buf.open(path, opts?) ⏳ -> Buffer | nil, err
gvim.buf.new() -> Buffer | nil, err                  -- an untitled, editable tab
gvim.buf.create(opts) -> Buffer | nil, err           -- a virtual buffer, see below
```

A relative `path` is resolved against the workspace root (`gvim.workspace.root()`), which is also the
base of the paths `gvim.workspace.grep` and `gvim.workspace.files` return: pass those to `open` as
they are. (`gvim.fs` and `gvim.proc` still resolve relative paths against the process's working
directory.) A path that does not exist is not an error: `open` opens a new empty buffer named after
it, which is saved to that path (check with `gvim.fs.exists` when you want a miss).

`open` options: `pos = Pos` (where to put the cursor), `focus = true`, `preview = false`
(a preview tab, replaced by the next preview), `background = false` (open without switching to
it). Opening a large file that needs a full load (a non-UTF-8 large file) returns
`too_large: needs full load` and never prompts. A path that exists and is not a regular file or
a directory (a FIFO, a device, a socket) is refused with `io: ... not a regular file`, because
reading it can block for ever.

## The Buffer handle

`b.id` is the number that identifies the buffer for the whole session (never reused). Methods on a
closed buffer return `nil, "closed: ..."`.

```text
b:valid() -> boolean
b:info() -> { id, path, name, language, modified, read_only, virtual, large, preview, active,
              line_count, encoding, line_ending, version, mode }
b:path() b:name() b:language() b:is_modified() b:is_read_only() b:is_virtual() b:is_large()
b:version() -> string
b:line_count() -> integer | nil        -- nil while a large file is still being indexed
```

`path` is `nil` for an untitled buffer, `language` is the language set with **Set Language** or
the one detected from the file name (never from the content). A language is its id, the key of the
`languages` table of the configuration: `go`, `rust`, `python`, `javascript`, `typescript`, `c`, `cpp`,
`java`, `json`, `yaml`, `toml`, `markdown`, `makefile`, `dockerfile`, `ruby`, `bash` (for `.sh`, `.bash`
and `.zsh`; there is no `sh` or `zsh`), and so on, plus any language the user defined. `encoding` and `line_ending` are the
editor's labels (`"UTF-8"`, `"LF"`, ...).

### Reading

```text
b:line(n) -> string | nil                 -- no newline; nil past the end
b:lines(first?, count?) -> string[] | nil, err   -- first default 0; count nil or negative: to the end
b:text(range?) -> string | nil, err       -- the whole text, or a range; LF-normalised
b:byte_length() -> integer | nil, err
b:offset_of(pos) -> integer | nil, err    -- 0-based byte offset; refused on large buffers
b:pos_of(offset) -> Pos | nil, err
```

One read is limited to 200 000 lines and 16 MiB (`too_large`). On a large file only the indexed
part can be read. `offset_of` is O(lines) and cached per version.

### Editing

Every edit is synchronous, visible to the next read, and **one undo step** per call.

```text
b:insert(pos, text) -> true | nil, err
b:delete(range) -> true | nil, err
b:replace(range, text) -> true | nil, err
b:edit(edits, opts?) -> true | nil, err        -- atomic; opts {cursor = "map"|"keep"|"end", version = tok}
b:set_lines(first, last, lines) -> true | nil, err
b:set_text(text) -> true | nil, err            -- replace the whole content
b:append(text) -> true | nil, err
b:undo_group(fn) -> fn's results               -- all edits fn makes are one undo step; must not wait
b:undo() -> true | nil, err
b:redo() -> true | nil, err
```

* `b:edit` takes an array of `{range, text}`. All ranges are in the coordinates of the text
  **before** the call, in any order; overlapping ranges are `invalid: overlapping edits`. At most
  50 000 edits and 8 MiB of inserted text per call (`limit`). `cursor` says what happens to the
  caret: `"map"` (default) moves it with the text, `"keep"` leaves it where it is, `"end"` puts it
  after the last edit. `version` is a stale check (see [conventions](conventions.md#version-tokens)).
* Use `b:edit` for many changes: a loop of `b:insert` costs one undo step and one change
  notification each. For a rewrite of a whole file call `b:set_text` once.
* **Typing semantics.** `b:insert` inserts text; it does not auto-indent or auto-close brackets.
  To type like the user does, run the editor action:
  `gvim.command.run("insert_char", { text = "x" })`.
* Errors: `read_only`, `closed`, `too_large` (large-file mode refuses most edits),
  `conflict: unsaved changes are still being restored`, `stale`, `limit`.

### Cursor and selection

```text
b:cursor() -> Pos                                   -- the primary caret
b:cursors() -> { {pos = Pos, anchor = Pos|nil}, ... }   -- primary first
b:selection() -> Range | nil
b:selected_text() -> string | nil
b:set_cursor(pos, opts?) -> true | nil, err         -- opts {extend = false, reveal = true, center = false}
b:select(range) -> true | nil, err
```

### Searching

```text
b:find(pattern, opts?) -> Range[], truncated | nil, err
b:find_lines(pattern, opts?) -> lines, next | nil, err
```

`b:find` searches on the host side. Options: `regex = false`, `case_sensitive = false`,
`whole_word = false`, `max = 1000` (at most 100 000), `within = Range`. It returns the ranges and a
boolean, `truncated`, true when `max` was reached and the scan stopped early. A pattern is at most
4 KiB. A regex falls under the `re` work cap ([limits.md](limits.md#the-regex-work-cap)): a range too
big for it returns `limit`; search line by line with `find_lines` or in smaller ranges. Large
buffers refuse `find`.

`b:find_lines` is for big files. It scans **whole lines** (a pattern never matches across a
line break) and returns the 0-based numbers of the matching lines, and the first line it did *not*
scan, or `nil` when the range was fully scanned. It is time-boxed (20 ms by default,
`budget_ms` up to 100) so it can be called again from `next`:

```lua
local from = 0
while from do
  local lines, nxt = buf:find_lines("TODO", { from = from, max = 500 })
  for _, n in ipairs(lines) do handle(n) end
  from = nxt
  if from then gvim.timer.sleep(0) end   -- give the editor a turn between chunks
end
```

Options: `regex`, `case_sensitive`, `from` and `to` (a line range, `to` exclusive, default to the
end), `max` (default 10 000), `budget_ms`.

### Tabs and files

```text
b:save(path?) ⏳ -> true | nil, err       -- plan and write; resolves after the save; a save-as with path
b:revert() ⏳ -> true | nil, err
b:close(opts?) -> true | nil, err         -- opts {force = false}
b:show() -> true | nil, err               -- make it the active tab
b:move_tab(delta) -> true | nil, err
b:set_language(name) -> true | nil, err   -- name: a language name or alias, at most 32 bytes
b:diff_saved() ⏳ -> Hunk[] | nil, err    -- saved text against the current text
```

* `save` runs the whole save flow, including the `before_file_save` veto of other plugins; it returns
  `cancelled: <reason>` when a plugin vetoed it.
* `close` without `force` on a modified buffer returns `conflict: unsaved changes`; it never opens a
  dialog.
* `diff_saved` reads the buffer synchronously and diffs in the background; a buffer over 200 000
  lines or 16 MiB is `too_large`. See [`gvim.diff`](api-system.md#gvimdiff).

## Virtual buffers

A virtual buffer is a scratch tab a plugin owns: it has no file, is never saved, never
counts as "unsaved changes", never goes to the session or to a language server, and is closed
when its plugin unloads. A plugin may have 64 of them (256 in the editor), holding 64 MiB of
text (256 MiB in all).

```text
gvim.buf.create{ name = "Outline", text = "", language = "markdown", read_only = true,
                 mode = "my_mode", focus = false } -> Buffer | nil, err
b:set_content(entries) -> true | nil, err
b:props_at(pos?) -> table | nil
b:set_read_only(bool) -> true | nil, err
b:set_mode(name | nil) -> true | nil, err
b:set_option(name, value) -> true | nil, err     -- "wrap", "line_numbers" (boolean) and "tab_size" (number)
b:get_option(name) -> value | nil
```

* `name` is required and is the title of the tab.
* `set_content` replaces text, styles and properties in one step. `entries` is an array of
  `{ text = , style = Style, segments = { {text =, style =}, ... }, props = table }`, concatenated
  **verbatim** (add your own `"\n"`). An entry with `segments` uses the concatenated segments as
  its text and ignores `text`. Each content write gives the buffer a new generation, so it drops
  the decorations other plugins put on it.
* **Streaming into a read-only buffer.** `b:append` and the other edits fail with `read_only` on a
  read-only virtual buffer, and `set_content` replaces the whole text and every style, so calling it
  for each line of a process's output is quadratic (30 000 lines took about a minute of CPU). Append
  in batches instead (collect lines for a timer tick or a few hundred lines):
  `b:set_read_only(false); b:append(chunk); b:set_read_only(true)`. Use `set_content` for the
  rewrite of a whole view, not for each new line.
* `props_at(pos)` returns the merged `props` of the entries covering `pos` (default: the
  cursor), or `nil`. Props answer only for the state they were set for: after any edit they are gone
  (an undo back to that state brings them back).
* `set_mode(name)` makes `name` the buffer-local input mode (see
  [api-commands.md](api-commands.md#gvimmode)); the mode is found by its bare name among the modes
  plugins defined.
* `get_option` returns the buffer's own override, or `nil` when it follows the global setting.

## gvim.win

```text
gvim.win.current() -> Window | nil
gvim.win.list() -> Window[]
gvim.win.get(id) -> Window | nil
gvim.win.equalize() -> true
```

`w.id` is stable and never reused. A `Window` handle you hold after the window closed stays a
valid value: `w:valid()` is false and its methods return `nil, "closed: ..."`.

```text
w:valid() w:buffer() -> Buffer | nil
w:set_buffer(buf) -> true | nil, err
w:focus() -> true | nil, err
w:close() -> true | nil, err                 -- the last window cannot be closed (conflict)
w:split(opts?) -> Window | nil, err          -- opts {dir = "right"|"below", buf = Buffer, ratio = 0.5, before = false, focus = false}
w:size() -> { x, y, width, height }          -- the text area, in screen cells
w:view() -> { top_line, left_col, height, width }
w:visible() -> { {line, col_from, col_to, wrapped}, ... }
w:scroll_to(line, opts?) -> true | nil, err  -- opts {center = false}
```

* `height` is in **rows**, not lines: with soft wrap a window shows fewer lines than it has rows.
  Use `w:visible()` to learn which lines and which byte columns of them are on the screen.
  It returns the rows the painter draws, wrapped continuation rows included (`wrapped = true`),
  lines hidden by a fold left out, at most 500, with columns as bytes in `[col_from, col_to)`.
* `scroll_to(line)` puts `line` at the top; with `center = true` in the middle. The caret does not
  move.
* A plugin that splits a window owns it: when the plugin unloads or reloads the windows it opened
  and has not closed are closed (the buffers stay).
* A window that shows a compare view refuses `set_buffer` with `conflict`. The user's own windows
  are listed too.

```text
gvim.buf.text_properties_at_cursor(buf?) -> table | nil
```

The properties of the virtual-buffer content entry under the buffer's primary caret (the
current buffer by default), the equivalent of Fresh's `getTextPropertiesAtCursor`: the entry
that covers the caret position, or `nil` when none does. The same map a buffer's `props_at(pos)`
returns; read it to follow a click or Enter on a rendered row (a file path, a line number, a
search result).

## Buffer groups (group-lite)

```text
gvim.buf.create_group(name, layout_json) -> id | nil, err
gvim.buf.set_panel_content(id, panel, entries) -> true | nil, err
gvim.buf.set_group_title(id, segments) -> true | nil, err
gvim.buf.set_panel_visible(id, panel, visible) -> true | nil, err
gvim.buf.focus_panel(id, panel) -> true | nil, err
gvim.buf.close_group(id) -> true | nil, err
gvim.buf.group_panels(id) -> { {name, buf, window?, visible}, ... } | nil, err
```

A group is a layout of panels shown as splits of the current tab (gvim has no synthetic group
tab yet: tier 1c). The layout is Fresh's JSON: a leaf is
`{"type":"scrollable","id":"name"}` or `{"type":"fixed","id":"name","height":n}` (1 to 100
rows: the host gives the leaf that display height by setting the split ratio above it and
re-applies it on every layout change, so a resize keeps it), and a split is
`{"type":"split","direction":"h"|"v","ratio":0.5,"first":...,"second":...}`. A leaf may take
`"bufferId"` to show an existing buffer; otherwise the host makes a read-only panel buffer the
group owns and `set_panel_content` fills it with styled `entries` (the virtual-buffer content
shape: `{text, style?, segments?, props?}`; read the props back with `b:props_at` or
`gvim.buf.text_properties_at_cursor`). `set_panel_visible` hides a panel (closing its window and
keeping its buffer and content) and shows it again by splitting a visible panel.
`set_group_title` stores Fresh's title segments (a segment may name a panel); gvim keeps them
for the port APIs and does not paint a group tab. Closing the group closes the split windows it
created and the panel buffers it made; unloading the plugin does the same. Group, panel and
scroll-sync ids are global, so a call naming one another plugin made answers `closed`. Limits: at most 8
panels, a layout 4 deep, 64 KiB of layout JSON and 16 title segments. A split's
`ratio` is the FIRST child's share of it. Fixed-height leaves are approximate: the split
border can leave the leaf one row off, a fixed leaf inside a side-by-side split is only as
exact as the ratio, a height taller than the body is clamped, and a hidden fixed panel's
height is re-applied when it is shown again. git_log's height-1 toolbar now renders as one row.

### Scroll-sync groups

```text
gvim.win.create_scroll_sync(id, left, right) -> true | nil, err
gvim.win.set_scroll_sync_anchors(id, { {left_line, right_line}, ... }) -> true | nil, err
gvim.win.remove_scroll_sync(id) -> true | nil, err
```

Two split windows scroll together: the follower's top line follows the leader's through the
anchor pairs (linear interpolation between the nearest pairs; the ends extend with their
nearest pair's slope; no anchors is the identity). The editor notices a scroll of either window
and moves the other, so a plugin only sets the anchors — for a side-by-side diff, the matching
line pairs. A group is removed automatically when the plugin unloads; windows that close stop
syncing. `id` is the plugin's own positive id (per plugin namespace).

## Split labels, window state and split wrappers (tier 1c-C)

```text
gvim.win.set_split_label(win, label) -> true | nil, err   -- label a split; "" clears
gvim.win.get_split_by_label(label) -> Window | nil
gvim.win.clear_split_label(win) -> true | nil, err
gvim.buf.get_window_state(buf, key[, default]) -> value | default
gvim.buf.set_window_state(buf, key, value) -> true | nil, err   -- nil clears
gvim.buf.open_file_in_split(path, opts?) ⏳ -> Buffer | nil, err
gvim.buf.focus_split(win) -> true | nil, err
gvim.buf.close_split(win) -> true | nil, err
```

A split label names a window so a plugin can find it again after focus moves or splits; it
lives on the split (the window keeps it until it closes) and `gvim.win.get_split_by_label`
returns the window handle. Window state is a small JSON-scalar map per plugin and buffer
(`string`, `number`, `boolean`): it survives a plugin reload in-session, is cleared when the
buffer closes, and a `nil` value removes one key. `gvim.buf.open_file_in_split` splits the
focused window (`direction = "right"` by default, or `"below"`) and opens the file there,
completing asynchronously like `gvim.buf.open`; `focus_split`/`close_split` are the plain
wrappers over the window operations. Limits: a split label is at most 64 bytes; window state
keeps at most 256 keys per plugin and buffer, keys up to 128 bytes and strings up to 4 KiB.

## Line targets (tier 1c-B)

```text
gvim.buf.set_line_targets(buf, {{line=, path=, target=?, into=?}, ...}) -> true | nil, err
gvim.buf.set_line_targets(buf, {{line=, action=}, ...}) -> true | nil, err
gvim.buf.clear_line_targets(buf) -> true
```

A target makes a line of the buffer clickable: pressing Enter on it in Normal mode (count 1, no
selection, no extra carets) or a single left click runs it. A path target opens `path` at
0-based line `target` (the top when omitted). `into` names a split by its plugin label: a live
labelled window takes the target; otherwise the target opens beside the pane that holds the
targets, so a result list never replaces itself. `line` is the 0-based line in this buffer.

An action target has `action` instead of `path` (the two are mutually exclusive): Enter/click
runs that command id through the same dispatcher as a key or palette row. For safety it must be
a command the SETTING plugin registered (`<plugin>.<name>`); another plugin's command is
rejected with an invalid error, so a target can never dispatch someone else's private command.
Nothing is built from target data into a shell or argv — the id is only looked up in the
command registry; an action that was unregistered or unloaded makes the click a no-op after the
owned targets are dropped. The call replaces the buffer's previous targets; an empty list (or
`clear_line_targets`) clears them. Targets are dropped when the buffer closes and when the
owning plugin unloads; another plugin's later call replaces the set. Counts 2/3,
extend-selection clicks and multi-caret mode keep their normal editing behaviour. Caps:
10 000 targets per buffer, path 4096 bytes, split label 64 bytes, action 128 bytes.
