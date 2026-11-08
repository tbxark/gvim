# User interface and decorations

## Prompts

```text
gvim.ui.input(opts) ⏳ -> string | nil
gvim.ui.confirm(msg, opts?) ⏳ -> boolean
gvim.ui.choose(msg, choices) ⏳ -> id | nil
gvim.ui.select(items, opts?) ⏳ -> item, index | nil
```

* **`input`**: opts `prompt`, `default`, `select_all = true`, `history = { "earlier", "answers" }` (oldest
  first, the last 100 strings; Up and Down in the prompt walk it, and Down past the newest brings back
  what was typed), `validate = fn(text) -> err|nil`
  (runs on every keystroke; it must be synchronous and cannot wait; return an error *string* to show
  it and refuse Enter) and `on_change = fn(text)` (runs as a callback task). A callback that
  raises is logged and does not trap the user. Returns the text, or `nil` when the user pressed Esc.
* **`confirm`**: a yes/no dialog. opts `yes = "Yes"`, `no = "No"`, `default = false`. Esc answers
  `false`.
* **`choose`**: `choices = { {id = "open", label = "Open"}, ... }`; returns the chosen `id`, or `nil`
  on Esc. It is a picker without filtering.
* **`select`**: a filtering picker over `items` (strings, or tables with `text`, `detail`,
  `keybinding`, `disabled`). opts `title`, `filter = "fuzzy"|"none"`, `initial_query`,
  `placeholder`, `footer` and `format = fn(item) -> string` (the text of a row; it is called for
  every item, strings included, so handle both kinds when you mix them, and return a string). It
  returns **your item** and its **1-based position** in `items`, or `nil` on Esc.
  Up to 50 000 items; the filtering runs in a background command, not in the update, and 100 rows
  are drawn.


A **prompt suggestion list** can be attached to an open prompt while the user types:
`gvim.ui.set_prompt_suggestions(items, selected?)` — called from the prompt's
`on_change = fn(text)` — replaces the list (`100` rows at most; strings or
`{id, text, description, value, disabled}`). Tab, Shift+Tab and Up/Down move the highlight and fill
the choice's `value` (the `text` when there is none) into the field; Enter still submits. An empty
list hides it. Without an open plugin prompt the call is `not_found`.

If the panic chord clears the prompt, or the plugin unloads, the call returns
`nil, "cancelled: ..."` instead of `nil`.

```lua
local name = gvim.ui.input { prompt = "Branch name:", validate = function(s)
  if s:find "%s" then return "no spaces" end
end }
if name then gvim.proc.run { "git", "switch", "-c", name } end
```

## Pickers

```lua
local p = gvim.ui.picker {
  title = "Files", items = files, filter = "fuzzy",
  on_query = function(query, picker) picker:set_items(search(query)) end,  -- makes it "live"
  on_select = function(item, index, picker) open_file(item) end,
  on_move = function(item, picker) preview(item) end,
  on_close = function() gvim.notify("closed") end,
  placeholder = "type to filter", footer = "enter opens", initial_query = "",
}
p:open()
```

`gvim.ui.picker` creates a closed picker; `p:open()` opens it (`busy` when already open).

```text
p:open() -> true | nil, err        p:close() -> true        p:is_open() -> boolean
p:set_items(items) -> true | nil, err
p:set_title(s)  p:set_footer(s)  p:set_status(s)
p:query() -> string                p:selected() -> item
```

* A picker with `on_query` is **live**: the query is passed to `on_query` after a 60 ms debounce, once
  when it opens (with `initial_query`), as a callback task that may wait and call `set_items`.
  Each query has a generation: `set_items` from an older query returns
  `nil, "stale: the query changed; these items are out of date"`.
* `on_select` runs when the user chooses a row; `on_move` when the highlight moves (a picker always
  asks the host for moves, so `p:selected()` is accurate without `on_move`). `on_close` runs when the
  user cancels (Esc, the panic chord), **not** after a selection and not after `p:close()`.
* `p:query()` follows what the user typed only for a **live** picker (the host reports query changes
  only then); for a plain fuzzy picker it returns the initial query.
* At most 8 live pickers and popups per plugin. Text of an item field is cut at 4096 bytes.

## Popups

```lua
local pop = gvim.ui.popup {
  title = "Commit", lines = { "line one", "line two" }, anchor = "center", width = 80,
  on_select = function(item, index) end, on_close = function() end,
}
pop:close()  pop:is_open()  pop:set_lines(lines)
```

A popup opens at once and shows either `lines` (text) or `items` (a list to choose from), not both.
`anchor` is `"cursor"` (default) or `"center"`, `width` 0 to 500 cells, at most 1000 lines.
`on_close` runs when the user dismisses it. There is no in-place update in the host, so
`set_lines` closes and shows the popup again.


### Action popups

```lua
local id = gvim.ui.action_popup {
  title = "Plugin update",
  message = "A new version is available.",
  actions = { { id = "install", label = "Install" }, { id = "later", label = "Later" } },
}
if id == "install" then
  gvim.notify("installing")
end
```

A message with up to 16 action buttons, as a centred card: the message wraps above the buttons,
Enter or a click chooses the highlighted one, Esc (or a click outside) closes it without a choice
and the call returns `nil`. At most 4 KiB of message and 200 bytes per label. When a choice is
made (or the popup closes) the editor also raises the `action_popup_result` event
(`{popup, action}`), so a plugin that opened it without waiting can still hear the outcome.

## Widget panels and sidebar sections

A plugin can describe a panel declaratively and let the editor lay it out, paint it and route
its input:

```text
gvim.ui.mount_widget_panel(spec, opts?) -> id | nil, err
gvim.ui.update_widget_panel(id, spec) -> true | nil, err
gvim.ui.unmount_widget_panel(id) -> true
gvim.ui.widget_command(id, action) -> true | nil, err
gvim.ui.widget_mutate(id, mutation) -> true | nil, err
gvim.ui.mount_sidebar_section(spec, opts?) -> id | nil, err
```

A **spec** is a widget tree: a node with a `kind` and, for a row, a col or a wrapper, `children`. A
panel spec may also be `{ root = <widget>, focus_key = "<key>" }`. The kinds are:

* layout: `row`, `col`, `spacer` (`cols` blank cells, or `flex = true`), `divider` (one-cell
  `text`, default `─`), `label` (text or styled `segments`/`runs` and `style`, all the
  [conventions](conventions.md#types) colour forms), `raw` (one row per `runs` entry), `hintBar`
  (a muted row of `runs`, entries separated by two spaces);
* display and behaviour: `button` (`label`, `intent`, `disabled`, initial `focused`), `toggle`
  (`label`, `checked`, `indeterminate`), `list` (`items`: strings or
  `{text, detail, key, checked, disabled}`, `selected_index` 1-based, `visible_rows`), `tree`
  (`nodes`: `{text, detail, key, depth, checked, expanded, leaf, draggable, flush, extra_lines}`,
  `checkable`, `selected_index`, `visible_rows`);
* interactive (tier 1b): `text` (an editable field), `number`, `dropdown`, `radio`, `dualList`;
* wrappers: `labeledSection`, `component`, `overlay` and `popup` (one child each; `title` names a
  labeledSection/popup box), and `windowEmbed` (draws placeholder rows: a panel cannot embed a
  live editor pane).

**Text fields.** `text` is the editable input. `text` (or `value`) is the seed content; `rows` > 1
makes it multi-line, `max_rows` (with `min_rows`) makes it grow with its content, `field_width`
limits the visible columns, `full_width` stretches it, `placeholder` shows while empty and
`read_only = true` keeps the caret and selection but refuses edits (the tier-1a read-only block
is now a read-only text field). `completions` is a candidate list the host draws under the field
when it is focused: Up/Down move the highlight, Tab/Enter accept, Esc dismisses. Every edit
raises a `change` widget event with `value` and `cursor_byte`. A text field needs a `key`.

**Number, dropdown, radio, dual list.** `number` has a seed `value` (also `number`), `min`,
`max`, `step`, `integer` and `percent`; Up/Down step, PageUp/PageDown step by ten, Home/End jump
to the bounds, and each change raises a `change` event with the formatted `value`. `dropdown` has
`options` and a 1-based `selected_index`; Enter opens the option list, Up/Down move the highlight,
Enter chooses (a `change` with `index` and `value`), Esc closes; closed, the arrows change the
value directly. `radio` renders one row per option and moves the selection with the arrows
(`change` on every move). `dualList` (the tier-1b simplification: one column, the included rows
marked `[x]`) moves with Up/Down and toggles inclusion with Space/Enter (`change` with `included`).

* `kind` is required on every node. `key` is the stable identity of a node across updates: the
  host keeps the focus, the selection, the checked state, the expanded tree nodes, the text and
  caret, the number, the included set and the rebuilt rows for matching keys. Key every widget
  whose state the user changes; the interactive kinds require it.
* A `list`'s and `tree`'s rows are windowed to the panel height (or `visible_rows`) and scroll the
  selection into view. A tree's expansion is the host's after the first draw; mutations change it.
* The colours are the [conventions](conventions.md#types) forms.

**Mount options.** `placement` is `"float"` (the default: a centred card, `width`/`height` as
percentages of the frame) or `"buffer"` (the panel is painted over the window that shows the
plugin's virtual buffer named by `buf`). `title` is the card title, `focus = true` gives the panel
the keyboard at once, `closable = true` (the default) makes Escape unmount it with a `cancel`
widget event, `page = true` makes the whole content one scrolling page (the arrows scroll it when
no focused widget takes them) and `auto_focus_first` (default `true`) says whether a panel
without `focus_key` starts on its first tabbable widget. `gvim.ui.mount_sidebar_section` takes
`title`, `rows` (body rows; 0 shares the column) and `closable`, and appends the panel as a
section under the file explorer.

**Actions** (`gvim.ui.widget_command`) go to the focused widget: `focusAdvance` (`delta` moves the
focus ring, Tab), `activate` (Enter/Space), `selectMove` (`delta` moves a list/tree/dropdown/radio/
dual-list selection), `textInputChar` (`text`), `textInputKey` (`key`: `backspace`, `delete`,
`left`, `right`, `home`, `end`, ...) and `key` (the generic form the host maps to the matching
smart key). A plugin mode's bindings dispatch into these.

**Mutations** (`gvim.ui.widget_mutate`) change one keyed widget without re-sending the spec, and
keep every other widget's state: `setValue` (`value`, optional `cursor_byte` for a text field),
`setCompletions` (`items`), `setChecked`, `setSelectedIndex` (1-based), `setNumber` (`number`),
`setDropdown` (`index`), `setDualIncluded` (`included`), `setItems` (`items`, `item_keys`),
`setExpandedKeys` (`keys`), `setCheckedKeys` (`keys`, `checked`), `appendTreeNodes` (`new_nodes`),
`setRawEntries` (`entries`), `setFocusKey` (`focus_key`), and the tree drag preview pair
`previewTreeMove` (`source_key`, `after_key`, `label`) and `endTreeMovePreview` (`commit`).

The host routes the panel's keys and mouse to the plugin as `widget_event` events (see
[events.md](events.md)): `activate`, `toggle`, `select`, `expand`, `change` (`value`,
`cursor_byte`, `index` or `included` depending on the widget), `completion_request`,
`completion_accept`, `completion_dismiss`, `drag`/`drop`/`dragend`, `focus`, `focus_gained`,
`focus_lost`, `close`, `cancel` and the mouse events; every event carries `focus_key`, the widget
that holds the panel focus afterwards. Plugin key layers (modes and key waits) see a key before
the host's smart dispatch, so a mode can turn typing into widget commands; the host's dispatch is
the fallback. A panel with the keyboard swallows keys its widgets do not use, so typing in a panel
never edits the buffer under it; Escape (or a click outside) gives the keyboard back.

**Caps and sanitisation.** A panel tree is at most 2000 widgets, nested at most 8 deep; a list
takes 2000 items, a dropdown/radio/dual list 2000 options, a text field 2000 completion
candidates; one string is 4 KiB, one key 128 bytes, a text field at most 200 editing rows; a
plugin mounts at most 8 panels (`limit`) and a plugin section asks for at most 100 body rows.
Text that reaches the grid is sanitised: control characters become spaces.

```lua
local id = gvim.ui.mount_widget_panel({
  root = { kind = "col", children = {
    { kind = "label", text = "Files" },
    { kind = "divider" },
    { kind = "list", key = "files", items = files, selected_index = 1 },
    { kind = "hintBar", runs = { { text = "Enter: open" }, { text = "Esc: close" } } },
  } },
}, { title = "Files", focus = true, width = 50, height = 60 })

gvim.on("widget_event", function(ev)
  if ev.panel ~= id then return end
  if ev.event == "activate" then
    gvim.notify("chose " .. tostring(ev.index))
  elseif ev.event == "cancel" then
    gvim.ui.unmount_widget_panel(id)
  end
end)

gvim.ui.widget_mutate(id, { kind = "setSelectedIndex", widget_key = "files", index = 2 })
```

**Caps and sanitisation.** A panel tree is at most 2000 widgets, nested at most 8 deep; a
list takes 2000 items, a tree 5000 nodes, one string 4 KiB and one key 128 bytes; a plugin
mounts at most 8 panels (`limit`), and a plugin section asks for at most 100 body rows. A
title is at most 120 bytes. Text that reaches the grid is sanitised: control characters
become spaces.


## Menu bar rows

A plugin can add its own rows to any menu bar dropdown:

```text
gvim.menu.add_item{menu, label, action, checkbox?, when?, after?, before?} -> true | nil, err
gvim.menu.remove_item{menu, action|label} -> true | nil, err
gvim.menu.clear() -> true
```

`menu` names the target menu by its stable id (`"File"`, `"Edit"`, `"View"`, `"Selection"`,
`"Go"`, `"LSP"`, `"Help"`) or by its label. `label` is the row text (a leading `%` makes it a
translation key); `action` is the action the row runs — normally your own command id
(`"<plugin>.<command>"`), which the menu bar dispatches like any palette action. `checkbox`
names a context key whose boolean state draws a checkmark (`gvim.context.set`); `when` gates
the row. `after`/`before` name a neighbouring row by its action or label to position the row;
a neighbour that matches nothing appends the row.

The set is whole-set replace per plugin: at most 32 rows in all, and a reload or an unload
drops the rows of the old generation.
## The status bar

```lua
local item = gvim.ui.statusbar.item { id = "branch", text = "main", align = "right", priority = 20,
                                      on_click = function() gvim.command.run("git_find_file.find") end }
item:set(text)  item:show()  item:hide()  item:remove()
```

`id` names the item inside your plugin (a second item with the same id is `conflict`). `align` is
`"left"` (between the position and the message) or `"right"` (between the language segment and the
diagnostics count). Higher `priority` comes first and is dropped last when the row is full. At most 8
items per plugin, each text at most 80 display cells, one line. `on_click` runs as a hidden command
with the command budget. A mode's `status` label is always the first left item.

## Other ui functions

```text
gvim.ui.focus(area) -> true | nil, err      -- "editor" | "explorer"
gvim.ui.size() -> { width, height }          -- the terminal size in cells
gvim.clipboard.set(text) -> true | nil, err  -- at most 8 MiB
```

Text you give to the interface (labels, items, popup lines, status text) is **sanitised**: control
characters become spaces, so a plugin cannot write escape sequences to the terminal.

## Decorations

A **decoration** is drawn over the document and is not part of it: it never becomes text and never
changes the buffer version. Its position is an **anchor** that follows the text: the editor moves it
as the document changes, so a position that was right when it was set stays on the same text, and a
decoration anchored to text that is gone (a deleted range or line) disappears.

```text
local ns = gvim.decor.namespace "blame"   -- at most 64 per plugin; the key is "<plugin>/<name>"
ns:set(buf, items, opts?) -> true | nil, err   -- replace EVERYTHING this namespace has on buf
ns:add(buf, item, opts?) -> id | nil, err
ns:remove(buf, id) -> true
ns:clear(buf?) -> true                          -- all buffers when buf is omitted
```

`opts = { version = tok }` (the token from `buf:version()`) makes the call fail with `stale` if the
buffer is not in that state, so positions computed against old text are never applied. Without it
the positions are taken as given and are right as long as you computed them from the current text.

Every item has a `kind` and an optional `priority` (default 0; higher wins):

| kind | fields | drawn as |
|---|---|---|
| `"highlight"` | `range`, `style` | a style over the cells of the range; `style.extend` paints to the end of the row |
| `"line"` | `line`, `style` | a style for the whole row |
| `"eol"` | `line`, `text`, `style` | virtual text after the last cell of the line; never changes columns; truncated at the pane edge; not drawn on windowed long lines of a large file |
| `"gutter"` | `line`, `text` (one cell), `style` | replaces the change-bar cell (the first gutter column); repeated on every wrapped row of its line |
| `"scroll"` | `line` or `range`, `color` | a scrollbar marker, merged with the built-in change marks |
| `"replace"` | `pos`, `text`, `style` | draws `text` over the cell at `pos`; width-preserving, so the caret and mouse hit mapping stay valid |

```lua
ns:set(buf, {
  { kind = "highlight", range = gvim.range(3, 0, 3, 5), style = { bg = "match" } },
  { kind = "gutter", line = 3, text = "+", style = { fg = "add" } },
  { kind = "eol", line = 3, text = "  alice, 3 days ago", style = { fg = "dim", italic = true } },
  { kind = "scroll", line = 3, color = "add" },
}, { version = buf:version() })
```

* **replace** covers the full display width of the cell at `pos`: the first grapheme of the label is
  drawn (a label wider than the cell is replaced by a space), then padded with spaces. A tab at
  `pos` replaces only its first display cell; a wide character is two cells and the label gets two.
* **Positions** are 0-based lines and byte columns, clamped, never an error. A column of 2^30 or
  more is the end of its line (for `eol`, `line` items and ranges that run to the end).
* **Colours**: the forms of [conventions.md](conventions.md#types) (`"#rrggbb"`, `{r, g, b}`, a theme
  key, a semantic name).
* **Caps**: at most 20 000 items per (plugin, namespace, buffer) and 50 000 per plugin (`limit`); at
  most 256 items of one kind are drawn on one line (the highest priority ones); the text of an `eol`,
  `gutter` or `replace` item is at most 1024 bytes.
* **What drops them.** A buffer that is replaced as a whole (revert, reload, encoding change) or a
  large file that is edited drops *every* decoration of that buffer; set them again when
  `buffer_changed` reports `overflow`. A change you made yourself shifts anchors like any other.
  **Undo does not restore** anchors the edit removed: set the decorations again on
  `buffer_changed`.
* Decorations of a closed buffer are freed at the next decoration call or when the plugin unloads.
  Unloading a plugin clears all of its namespaces.
* Large files cost nothing until painted: the editor asks per painted line.
* `Style.strike` is accepted and stored but not drawn.

### Virtual text surface (tier 1c-A)

Beyond the anchored kinds above, a namespace accepts the virtual-text kinds, and convenience
functions mirror Fresh's names (each takes a `Buffer` first and an `opts` table with
`namespace` — default `"default"` — `priority`, `version` and the kind-specific fields):

```text
gvim.decor.add_virtual_line(buf, line, text, opts?)       -- A display row above (default) the line.
gvim.decor.add_virtual_line_below(buf, line, text, opts?) -- A display row below the line.
gvim.decor.add_conceal(buf, from, to, replacement?, opts?) -- Hide the range's cells, draw replacement.
gvim.decor.add_soft_break(buf, pos, opts?)                -- Force a display row break at pos.
gvim.decor.set_cursor_line_overlay(buf, style, opts?)     -- Tint the caret's row.
gvim.decor.set_scrollbar_markers_in_range(buf, range, color, opts?) -- A ranged scrollbar mark.
gvim.decor.clear_decorations(buf?, range?, opts?)         -- Clear a namespace, or its items in a line range.
gvim.decor.refresh_lines(buf?)                            -- Kept for Fresh's shape; painting is per frame.
gvim.decor.add_fold(buf, start_line, end_line, opts?)     -- One namespace-scoped folding range.
gvim.decor.set_folding_ranges(buf, ranges, opts?)         -- Replace the namespace's folding ranges.
gvim.decor.clear_folds(buf?, opts?)                       -- Remove them.
```

The matching `ns:set` item kinds are `virtual_line_above` / `virtual_line_below` (`line`,
`text`, `style`, `gutter_glyph` one cell, `gutter_color`, `text_overlays = { {start=,
["end"]=, style=}, ... }` styled byte ranges inside the virtual line's text), `conceal`
(`range`, optional `replacement text`, `style`), `soft_break` (`pos`, `indent`, `prefix =
{text=, style=}`) and `cursor_line_overlay` (`style`). `activation` is `"always"` (default),
`"unless-cursor-in"` (skipped on the caret's line) or `"if-cursor-in"` (only there); `scope =
{start_line=, end_line=}` bounds where it applies; `compose_only` shows the item only in a
composing pane (the page view). Plugin folds are namespace-scoped, merged with the language
server's and the outline's ranges, start collapsed or expanded from `collapsed` and can be
toggled with the normal fold action; the placeholder shows on a collapsed range. Ordering:
within the rows above or below one line, ascending priority (ties by insertion order) top to
bottom; for the rows above, the last one is nearest the anchor line, and for the rows below the
first one is. Caps: 64 virtual rows and 64 soft breaks per line, 256 conceal spans per line,
2000 folding ranges per namespace, and the existing per-namespace/owner decoration quotas.

