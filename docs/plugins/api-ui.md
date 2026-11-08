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
