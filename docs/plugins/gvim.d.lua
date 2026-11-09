---@meta
-- Type stubs for the gvim Lua plugin API (api_version 2), for editor completion with the
-- Lua language server (LuaLS): put this file where the server can see it, for example in
-- the `workspace.library` setting. It is never loaded by gvim.
--
-- The stubs and the runtime are kept equal by a test (TestStubsMatchRuntime): every
-- function declared here exists in a live plugin state, and every function of a live state is
-- declared here. Read docs/plugins/*.md for what the functions do. Functions marked ⏳ in
-- the docs may suspend the calling task; they only work inside a task (a command, an event
-- handler, a callback, the main chunk or gvim.async.spawn).
--
-- Conventions: line and col are 0-based and col is a BYTE offset into the line text;
-- Lua arrays and string positions are 1-based. An expected failure returns nil and an error
-- string "<code>: <message>"; gvim.err_code(err) extracts the code.

---@alias ErrCode
---| "read_only" | "closed" | "too_large" | "invalid" | "not_found" | "busy" | "timeout"
---| "cancelled" | "io" | "exec" | "unsupported" | "conflict" | "limit" | "stale"

---@class Pos
---@field line integer 0-based line
---@field col integer 0-based byte column

---@class Range
---@field from Pos
---@field to Pos end, exclusive

---@class Edit
---@field range Range
---@field text string

---@alias Color string|number[]|{r:integer,g:integer,b:integer} "#rrggbb", {r,g,b}, a theme key ("editor.selection_bg") or a semantic name ("error", "warning", "info", "hint", "add", "change", "delete", "match", "accent", "dim")

---@class Style
---@field fg? Color
---@field bg? Color
---@field bold? boolean
---@field italic? boolean
---@field underline? boolean
---@field strike? boolean accepted and stored; the terminal grid does not draw it yet
---@field reverse? boolean
---@field extend? boolean paint to the end of the row

---@class Key
---@field key string
---@field text? string
---@field ctrl boolean
---@field alt boolean
---@field shift boolean
---@field super boolean

---@class Item
---@field text string
---@field detail? string
---@field keybinding? string
---@field disabled? boolean

---@class Hunk
---@field kind "insert"|"delete"|"change"
---@field old_start integer 0-based
---@field old_count integer
---@field new_start integer 0-based
---@field new_count integer

---@class ProcResult
---@field code integer
---@field signal? string
---@field stdout string
---@field stderr string
---@field ok boolean code == 0, no signal, no timeout
---@field timed_out boolean
---@field truncated boolean

---@class Diagnostic
---@field range Range
---@field severity "error"|"warning"|"info"|"hint"
---@field message string
---@field source? string

---@class BufferInfo
---@field id integer
---@field path? string
---@field name string
---@field language? string
---@field modified boolean
---@field read_only boolean
---@field virtual boolean
---@field large boolean
---@field preview boolean
---@field active boolean
---@field line_count? integer nil while a large file is still being indexed
---@field encoding string
---@field line_ending string
---@field version string opaque token, compare for equality only
---@field mode? string

-- ---------- handles ----------

---@class Buffer
---@field id integer
local Buffer = {}
---@return boolean
function Buffer:valid() end
---@return BufferInfo
function Buffer:info() end
---@return string|nil
function Buffer:path() end
---@return string
function Buffer:name() end
---@return string|nil
function Buffer:language() end
---@return boolean
function Buffer:is_modified() end
---@return boolean
function Buffer:is_read_only() end
---@return boolean
function Buffer:is_virtual() end
---@return boolean
function Buffer:is_large() end
---@return string
function Buffer:version() end
---@return integer|nil
function Buffer:line_count() end
---@param n integer 0-based
---@return string|nil
function Buffer:line(n) end
---@param first? integer 0-based, default 0
---@param count? integer default and negative: to the end
---@return string[]|nil, string?
function Buffer:lines(first, count) end
---@param range? Range
---@return string|nil, string?
function Buffer:text(range) end
---@return integer|nil, string?
function Buffer:byte_length() end
---@param pos Pos
---@return integer|nil, string?
function Buffer:offset_of(pos) end
---@param offset integer
---@return Pos|nil, string?
function Buffer:pos_of(offset) end
---@param pos Pos
---@param text string
---@return true|nil, string?
function Buffer:insert(pos, text) end
---@param range Range
---@return true|nil, string?
function Buffer:delete(range) end
---@param range Range
---@param text string
---@return true|nil, string?
function Buffer:replace(range, text) end
---@param edits Edit[] ranges are in the coordinates of the text before the call
---@param opts? {cursor?: "map"|"keep"|"end", version?: string}
---@return true|nil, string?
function Buffer:edit(edits, opts) end
---@param first integer
---@param last integer
---@param lines string[]
---@return true|nil, string?
function Buffer:set_lines(first, last, lines) end
---@param text string
---@return true|nil, string?
function Buffer:set_text(text) end
---@param text string
---@return true|nil, string?
function Buffer:append(text) end
---@generic T
---@param fn fun(): T
---@return T
function Buffer:undo_group(fn) end
---@return true|nil, string?
function Buffer:undo() end
---@return true|nil, string?
function Buffer:redo() end
---@return Pos
function Buffer:cursor() end
---@return {pos: Pos, anchor?: Pos}[]
function Buffer:cursors() end
---@return Range|nil
function Buffer:selection() end
---@return string|nil
function Buffer:selected_text() end
---@param pos Pos
---@param opts? {extend?: boolean, reveal?: boolean, center?: boolean}
---@return true|nil, string?
function Buffer:set_cursor(pos, opts) end
---@param range Range
---@return true|nil, string?
function Buffer:select(range) end
---@param pattern string
---@param opts? {regex?: boolean, case_sensitive?: boolean, whole_word?: boolean, max?: integer, within?: Range}
---@return Range[]|nil, boolean|string
function Buffer:find(pattern, opts) end
---@param pattern string
---@param opts? {regex?: boolean, case_sensitive?: boolean, from?: integer, to?: integer, max?: integer, budget_ms?: integer}
---@return integer[]|nil, integer|string|nil
function Buffer:find_lines(pattern, opts) end
---@param path? string
---@return true|nil, string?
function Buffer:save(path) end
---@return true|nil, string?
function Buffer:revert() end
---@param opts? {force?: boolean}
---@return true|nil, string?
function Buffer:close(opts) end
---@return true|nil, string?
function Buffer:show() end
---@param delta integer
---@return true|nil, string?
function Buffer:move_tab(delta) end
---@param name string
---@return true|nil, string?
function Buffer:set_language(name) end
---@return Hunk[]|nil, string?
function Buffer:diff_saved() end
---@param entries {text?: string, style?: Style, segments?: {text: string, style?: Style}[], props?: table}[]
---@return true|nil, string?
function Buffer:set_content(entries) end
---@param pos? Pos
---@return table|nil
function Buffer:props_at(pos) end
---@param ro boolean
---@return true|nil, string?
function Buffer:set_read_only(ro) end
---@param name string|nil
---@return true|nil, string?
function Buffer:set_mode(name) end
---@param name "wrap"|"line_numbers"|"tab_size"
---@param value any
---@return true|nil, string?
function Buffer:set_option(name, value) end
---@param name "wrap"|"line_numbers"|"tab_size"
---@return any
function Buffer:get_option(name) end

---@class Window
---@field id integer
local Window = {}
---@return boolean
function Window:valid() end
---@return Buffer|nil
function Window:buffer() end
---@param buf Buffer
---@return true|nil, string?
function Window:set_buffer(buf) end
---@return true|nil, string?
function Window:focus() end
---@return true|nil, string?
function Window:close() end
---@param opts? {dir?: "right"|"below", buf?: Buffer, ratio?: number, before?: boolean, focus?: boolean}
---@return Window|nil, string?
function Window:split(opts) end
---@return {x: integer, y: integer, width: integer, height: integer}
function Window:size() end
---@return {top_line: integer, left_col: integer, height: integer, width: integer}
function Window:view() end
---@return {line: integer, col_from: integer, col_to: integer, wrapped: boolean}[]
function Window:visible() end
---@param line integer
---@param opts? {center?: boolean}
---@return true|nil, string?
function Window:scroll_to(line, opts) end

---@class Subscription
local Subscription = {}
---@return true
function Subscription:off() end
---@return boolean
function Subscription:active() end

---@class Command
local Command = {}
---@return true|nil, string?
function Command:unregister() end

---@class Binding
local Binding = {}
---@return boolean
function Binding:active() end
---@return true|nil, string?
function Binding:del() end

---@class Mode
local Mode = {}
---@return string
function Mode:name() end
---@return true|nil, string?
function Mode:enter() end

---@class Timer
local Timer = {}
---@return true
function Timer:cancel() end
---@return boolean
function Timer:active() end

---@class Watcher
local Watcher = {}
---@return true
function Watcher:stop() end
---@return boolean
function Watcher:active() end

---@class Process
---@field pid integer?
local Process = {}
---@param data string
---@return true|nil, string?
function Process:write(data) end
---@return true
function Process:close_stdin() end
---@param signal? "TERM"|"KILL"|"INT"|"HUP"
---@return true|nil, string?
function Process:kill(signal) end
---@return boolean
function Process:is_running() end
---@return ProcResult|nil, string?
function Process:wait() end
---@return integer|nil, string?
function Process:wait_started() end

---@class Task
local Task = {}
---@return true
function Task:cancel() end
---@return boolean
function Task:done() end
---@param opts? {timeout_ms?: integer}
---@return any ...
function Task:wait(opts) end

---@class Namespace
local Namespace = {}
---@param buf Buffer
---@param items table[]
---@param opts? {version?: string}
---@return true|nil, string?
function Namespace:set(buf, items, opts) end
---@param buf Buffer
---@param item table
---@param opts? {version?: string}
---@return integer|nil, string?
function Namespace:add(buf, item, opts) end
---@param buf Buffer
---@param id integer
---@return true
function Namespace:remove(buf, id) end
---@param buf? Buffer
---@return true
function Namespace:clear(buf) end

---@class Picker
local Picker = {}
---@return true|nil, string?
function Picker:open() end
---@param items (string|Item)[]
---@return true|nil, string?
function Picker:set_items(items) end
---@param s string
---@return true
function Picker:set_title(s) end
---@param s string
---@return true
function Picker:set_footer(s) end
---@param s string
---@return true
function Picker:set_status(s) end
---@return string
function Picker:query() end
---@return any
function Picker:selected() end
---@return true
function Picker:close() end
---@return boolean
function Picker:is_open() end

---@class Popup
local Popup = {}
---@return true
function Popup:close() end
---@return boolean
function Popup:is_open() end
---@param lines string[]
---@return true|nil, string?
function Popup:set_lines(lines) end

---@class StatusItem
local StatusItem = {}
---@param text string
---@return true
function StatusItem:set(text) end
---@return true
function StatusItem:show() end
---@return true
function StatusItem:hide() end
---@return true
function StatusItem:remove() end

---@class Regex
local Regex = {}
---@param s string
---@param init? integer
---@return integer|nil, integer|string?, ...
function Regex:find(s, init) end
---@param s string
---@param init? integer
---@return string|false|nil, ...
function Regex:match(s, init) end
---@param s string
---@return boolean
function Regex:test(s) end
---@param s string
---@return fun(): string|false|nil, ...
function Regex:gmatch(s) end
---@param s string
---@param repl string|table|fun(...): string
---@param n? integer
---@return string, integer
function Regex:gsub(s, repl, n) end
---@param s string
---@param n? integer
---@return string[]
function Regex:split(s, n) end

-- ---------- the gvim module ----------

---@class gvim
gvim = {}

gvim.api_version = 2
gvim.version = ""
gvim.platform = ""

---@param feature "decor"|"windows"|"virtual_buffers"|"input"|"lsp"|"git"|string
---@return boolean
function gvim.has(feature) end
---@param err any
---@return ErrCode|nil
function gvim.err_code(err) end
---@param msg string
---@param level? "info"|"warn"|"error"
---@return true
function gvim.notify(msg, level) end
---@param msg string
---@return true
function gvim.status(msg) end
---@param line integer
---@param col? integer
---@return Pos
function gvim.pos(line, col) end
---@param l1 integer
---@param c1 integer
---@param l2 integer
---@param c2 integer
---@return Range
function gvim.range(l1, c1, l2, c2) end
---@param a Pos
---@param b Pos
---@return -1|0|1
function gvim.pos_cmp(a, b) end
---@param a Range
---@param b Range
---@return boolean
function gvim.range_eq(a, b) end

gvim.log = {}
---@param fmt any
---@param ... any
function gvim.log.debug(fmt, ...) end
---@param fmt any
---@param ... any
function gvim.log.info(fmt, ...) end
---@param fmt any
---@param ... any
function gvim.log.warn(fmt, ...) end
---@param fmt any
---@param ... any
function gvim.log.error(fmt, ...) end

gvim.plugin = {}
gvim.plugin.name = ""
gvim.plugin.dir = ""
gvim.plugin.version = ""
gvim.plugin.source = ""
---@param fn fun()
---@return true|nil, string?
function gvim.plugin.on_unload(fn) end

gvim.settings = {}
---@param schema table<string, {type: "boolean"|"integer"|"number"|"string"|"enum"|"string_list", default?: any, description?: string, minimum?: number, maximum?: number, values?: string[], project?: boolean}>
---@return table
function gvim.settings.define(schema) end
---@return table
function gvim.settings.get() end
gvim.config = {}
---@param key? string
---@return any
function gvim.config.get(key) end
gvim.state = {}
---@param key string
---@param default? any
---@return any
function gvim.state.get(key, default) end
---@param key string
---@param value any
---@return true|nil, string?
function gvim.state.set(key, value) end
---@param key string
---@return true
function gvim.state.delete(key) end
---@return string[]
function gvim.state.keys() end
gvim.dirs = {}
---@return string
function gvim.dirs.config() end
---@return string
function gvim.dirs.data() end
---@return string
function gvim.dirs.plugin() end
---@return string
function gvim.dirs.root() end
---@return string
function gvim.dirs.cwd() end
---@return string
function gvim.dirs.home() end
---@return string
function gvim.dirs.temp() end
gvim.env = {}
---@param name string
---@return string|nil
function gvim.env.get(name) end
---@return table|nil {name: string, kind: "path-only"|"shell", snippet: string}
function gvim.env.detected() end
---@return table|nil
function gvim.env.active() end
---@return "trusted"|"restricted"|"blocked"
function gvim.env.trust_level() end
---@param snippet string
---@param opts? {dir?: string, kind?: "path-only"|"shell"}
---@return boolean|nil, string?
function gvim.env.set(snippet, opts) end
---@return boolean|nil, string?
function gvim.env.clear() end

-- buffers and windows

gvim.buf = {}
---@return Buffer|nil
function gvim.buf.current() end
---@param id integer
---@return Buffer|nil
function gvim.buf.get(id) end
---@return Buffer[]
function gvim.buf.list() end
---@param path string
---@return Buffer|nil
function gvim.buf.find(path) end
---@param path string
---@param opts? {pos?: Pos, focus?: boolean, preview?: boolean, background?: boolean}
---@return Buffer|nil, string?
function gvim.buf.open(path, opts) end
---@return Buffer|nil, string?
function gvim.buf.new() end
---@param opts {name?: string, text?: string, language?: string, read_only?: boolean, mode?: string, focus?: boolean}
---@return Buffer|nil, string?
function gvim.buf.create(opts) end
---@param name string
---@param layout string JSON layout: a leaf {type:"scrollable"|"fixed", id=, bufferId?} or
---  {type="split", direction = "h"|"v", ratio=, first=, second=}
---@return integer|nil group id, string?
function gvim.buf.create_group(name, layout) end
---@param id integer
---@param panel string
---@param entries {text: string, style?: Style, segments?: Segment[], props?: table}[]
---@return true|nil, string?
function gvim.buf.set_panel_content(id, panel, entries) end
---@param id integer
---@param segments {text?: string, panel?: string}[]
---@return true|nil, string?
function gvim.buf.set_group_title(id, segments) end
---@param id integer
---@param panel string
---@param visible boolean
---@return true|nil, string?
function gvim.buf.set_panel_visible(id, panel, visible) end
---@param id integer
---@param panel string
---@return true|nil, string?
function gvim.buf.focus_panel(id, panel) end
---@param id integer
---@return true
function gvim.buf.close_group(id) end
---@param id integer
---@return {name: string, buf: Buffer, window?: Window, visible: boolean}[]|nil, string?
function gvim.buf.group_panels(id) end
---@param buf Buffer
---@param key string
---@param default? any
---@return any|nil value, or default when the key is not set
function gvim.buf.get_window_state(buf, key, default) end
---@param buf Buffer
---@param key string
---@param value string|number|boolean|nil (nil clears the key)
---@return true|nil, string?
function gvim.buf.set_window_state(buf, key, value) end
---@param path string
---@param opts? {direction?: "right"|"below", focus?: boolean, preview?: boolean, background?: boolean, pos?: Pos}
---@return true, Buffer|nil, string?
function gvim.buf.open_file_in_split(path, opts) end
---@param win Window
---@return true|nil, string?
function gvim.buf.focus_split(win) end
---@param win Window
---@return true|nil, string?
function gvim.buf.close_split(win) end
---@param buf Buffer
---@param targets {line: integer, path?: string, target?: integer, into?: string, action?: string}[]
---@return true|nil, string?
function gvim.buf.set_line_targets(buf, targets) end
---@param buf Buffer
---@return true
function gvim.buf.clear_line_targets(buf) end




gvim.win = {}
---@return Window|nil
function gvim.win.current() end
---@return Window[]
function gvim.win.list() end
---@param id integer
---@return Window|nil
function gvim.win.get(id) end
---@return true
function gvim.win.equalize() end
---@param win Window
---@param label string at most 64 bytes; "" clears it
---@return true|nil, string?
function gvim.win.set_split_label(win, label) end
---@param label string
---@return Window|nil
function gvim.win.get_split_by_label(label) end
---@param win Window
---@return true|nil, string?
function gvim.win.clear_split_label(win) end
---@param id integer a plugin-chosen id
---@param left Window the leader window
---@param right Window the follower window
---@return true|nil, string?
function gvim.win.create_scroll_sync(id, left, right) end
---@param id integer
---@param anchors {left: integer, right: integer}[] 0-based line pairs
---@return true|nil, string?
function gvim.win.set_scroll_sync_anchors(id, anchors) end
---@param id integer
---@return true
function gvim.win.remove_scroll_sync(id) end


-- commands, keys, modes, events

gvim.command = {}
---@param spec {name: string, title?: string, description?: string, when?: string[], keys?: string[], record?: boolean, hidden?: boolean, run: fun(ctx: {args: table, count: integer, buf: Buffer|nil, source: string})}
---@return Command|nil, string?
function gvim.command.register(spec) end
---@param name string
---@return true|nil, string?
function gvim.command.unregister(name) end
---@param filter? string|{query?: string, plugin?: string, builtin?: boolean}
---@return {id: string, title: string, plugin?: string, builtin: boolean}[]
function gvim.command.list(filter) end
---@param action string
---@param args? table
---@return true|nil, string?
function gvim.command.run(action, args) end
---@param steps (string|{action: string, args?: table, count?: integer}|{[1]: string, [2]: table})[]
---@return true|nil, string?
function gvim.command.run_all(steps) end
---@return string[]
function gvim.command.actions() end
---@param query? string
---@return true
function gvim.command.palette(query) end

gvim.keymap = {}
---@param chord string
---@param action string|fun(ctx: table)
---@param opts? {mode?: string, desc?: string, args?: table, when?: string[]}
---@return Binding|nil, string?
function gvim.keymap.set(chord, action, opts) end
---@param chord string
---@param opts? {mode?: string}
---@return true|nil, string?
function gvim.keymap.del(chord, opts) end
---@param action string
---@return string|nil
function gvim.keymap.label(action) end

gvim.mode = {}
---@param spec {name: string, bindings?: table<string, string|fun(ctx: table)>, text?: "pass"|"swallow"|fun(ctx: table), paste?: "pass"|"swallow"|"text", fallthrough?: boolean, status?: string}
---@return Mode|nil, string?
function gvim.mode.define(spec) end
---@param name string
---@return true|nil, string?
function gvim.mode.enter(name) end
---@return true
function gvim.mode.leave() end
---@return string|nil name
---@return string|nil plugin the plugin that owns the mode
function gvim.mode.current() end

gvim.context = {}
---@param name string
---@param on boolean
---@return true|nil, string?
function gvim.context.set(name, on) end
---@param name string
---@return boolean
function gvim.context.get(name) end

gvim.input = {}
---@param opts? {timeout_ms?: integer}
---@return Key|nil, string?
function gvim.input.next_key(opts) end

---@param event string
---@param fn fun(ev: table): boolean|nil, string?
---@param opts? {buf?: Buffer, debounce?: integer, after_ms?: integer}
---@return Subscription
function gvim.on(event, fn, opts) end
---@param event string
---@param fn fun(ev: table): boolean|nil, string?
---@param opts? {buf?: Buffer, debounce?: integer, after_ms?: integer}
---@return Subscription
function gvim.once(event, fn, opts) end
---@param sub Subscription
---@return true
function gvim.off(sub) end
---@param name string "<plugin>:<name>", your own plugin's name
---@param payload? table
---@return true|nil, string?
function gvim.emit(name, payload) end

-- ui, clipboard, decorations

---@class WidgetRun
---@field text string
---@field fg? Color
---@field bg? Color
---@field bold? boolean
---@field italic? boolean
---@field underline? boolean
---@field reverse? boolean

---@class WidgetItem
---@field text string
---@field detail? string
---@field key? string
---@field checked? boolean
---@field disabled? boolean

---@class WidgetNode
---@field text string
---@field detail? string
---@field key? string
---@field depth? integer 0 for a root row
---@field checked? boolean
---@field expanded? boolean seed of the node's children; the host owns it after the first draw
---@field leaf? boolean the node has no children even when the next node is deeper
---@field draggable? boolean the row is pickable; drag/drop/dragend widget_events follow
---@field flush? boolean a leaf's text starts at its indent, not after a disclosure gutter
---@field extra_lines? WidgetRun[] extra rows under a card node

---@class Widget
---@field kind "row"|"col"|"label"|"text"|"raw"|"hintBar"|"spacer"|"divider"|"button"|"toggle"|"list"|"tree"|"dropdown"|"number"|"radio"|"dualList"|"labeledSection"|"component"|"overlay"|"popup"|"windowEmbed"
---@field key? string stable identity across updates; required for state a mutation changes
---@field children? Widget[] row, col and the one-child wrappers (labeledSection/component/overlay/popup)
---@field text? string label text, the editable text field's seed/read-only block, the divider glyph
---@field label? string button and toggle text, number label
---@field value? string (number) seed value; the text field reads "text"
---@field runs? WidgetRun[] label segments; raw rows; hintBar entries
---@field style? Style
---@field checked? boolean toggle
---@field indeterminate? boolean toggle: neither on nor off
---@field focused? boolean initial focus hint
---@field disabled? boolean button: muted, not tabbable
---@field intent? "primary"|"secondary" button look
---@field items? (string|WidgetItem)[] list
---@field nodes? WidgetNode[] tree
---@field selected_index? integer 1-based index of the selected list/tree/dropdown/radio/dual row; absent means none
---@field visible_rows? integer list/tree window; 0 shares the panel height
---@field checkable? boolean tree: draw the checked seeds
---@field cols? integer spacer width in a row (default 1)
---@field flex? boolean spacer: take the free width of a row
---@field options? string[] dropdown, radio and dualList: the selectable values
---@field number? number number: seed value
---@field min? number
---@field max? number
---@field step? number default 1
---@field integer? boolean
---@field percent? boolean
---@field combo? boolean dropdown: also accept typed text
---@field placeholder? string text field: shown while empty
---@field field_width? integer text field: visible columns (0 auto)
---@field full_width? boolean text field: stretch to the enclosing width
---@field rows? integer text field: editing rows (0/1 single line, >=2 multi-line)
---@field min_rows? integer
---@field max_rows? integer text field: grow with its text up to this many rows
---@field read_only? boolean text field: caret/selection only
---@field markdown? boolean text field: accepted, rendered as plain text in this slice
---@field open? boolean dropdown: the option list starts expanded
---@field scroll_offset? integer dropdown: first visible option
---@field completions? string[] text field: host-drawn candidate list
---@field included? string[] dualList: the values initially included
---@field title? string labeledSection/popup: the box legend
---@field width_pct? integer labeledSection in a row: percentage of the row width
---@field width_cols? integer labeledSection in a row: exact columns (wins over width_pct)
---@field window? integer windowEmbed: the window id (0 draws placeholders)

gvim.ui = {}
---@param opts? {prompt?: string, default?: string, select_all?: boolean, history?: string[], validate?: fun(text: string): string|nil, on_change?: fun(text: string)}
---@return string|nil, string?
function gvim.ui.input(opts) end
---@param msg string
---@param opts? {yes?: string, no?: string, default?: boolean}
---@return boolean|nil, string?
function gvim.ui.confirm(msg, opts) end
---@param msg string
---@param choices {id: any, label?: string}[]
---@return any|nil, string?
function gvim.ui.choose(msg, choices) end
---@generic T
---@param items T[]
---@param opts? {title?: string, filter?: "fuzzy"|"none", initial_query?: string, placeholder?: string, footer?: string, format?: fun(item: T): string}
---@return T|nil, integer|string?
function gvim.ui.select(items, opts) end
---@param spec {title?: string, items?: (string|Item)[], filter?: "fuzzy"|"none", on_query?: fun(query: string, picker: Picker), on_select?: fun(item: any, index: integer, picker: Picker), on_move?: fun(item: any, picker: Picker), on_close?: fun(), placeholder?: string, footer?: string, initial_query?: string}
---@return Picker
function gvim.ui.picker(spec) end
---@param spec {title?: string, lines?: string[], items?: (string|Item)[], anchor?: "cursor"|"center", width?: integer, on_select?: fun(item: any, index: integer), on_close?: fun()}
---@return Popup
function gvim.ui.popup(spec) end
---@param opts {title?: string, message?: string, actions: {id: string, label: string}[]}
---@return string|nil chosen action id, string?
function gvim.ui.action_popup(opts) end
---@param items (string|{id?: string, text?: string, description?: string, value?: string, disabled?: boolean})[]
---@param selected? integer 1-based highlighted row
---@return true|nil, string?
function gvim.ui.set_prompt_suggestions(items, selected) end
---@param area "editor"|"explorer"
---@return true|nil, string?
function gvim.ui.focus(area) end
---@return {width: integer, height: integer}
function gvim.ui.size() end
---@param spec Widget|{root: Widget, focus_key?: string}
---@param opts? {placement?: "float"|"buffer", buf?: Buffer, scope?: "window"|"editor"|"buffer", title?: string, width?: integer, height?: integer, focus?: boolean, closable?: boolean, page?: boolean, auto_focus_first?: boolean}
---@return integer|nil panel id, string?
function gvim.ui.mount_widget_panel(spec, opts) end
---@param id integer
---@param spec Widget|{root: Widget, focus_key?: string}
---@return true|nil, string?
function gvim.ui.update_widget_panel(id, spec) end
---@param id integer
---@return true
function gvim.ui.unmount_widget_panel(id) end
---@param id integer
---@param action {kind: "focusAdvance"|"activate"|"selectMove"|"textInputKey"|"textInputChar"|"key", delta?: integer, key?: string, text?: string}
---@return true|nil, string?
function gvim.ui.widget_command(id, action) end
---@param id integer
---@param mutation {kind: "setChecked"|"setSelectedIndex"|"setValue"|"setNumber"|"setDropdown"|"setCompletions"|"setDualIncluded"|"setItems"|"setExpandedKeys"|"setCheckedKeys"|"appendTreeNodes"|"setRawEntries"|"setFocusKey"|"previewTreeMove"|"endTreeMovePreview", widget_key?: string, focus_key?: string, checked?: boolean, index?: integer, value?: string, cursor_byte?: integer, number?: number, included?: string[], items?: (string|WidgetItem)[], item_keys?: string[], keys?: string[], new_nodes?: WidgetNode[], entries?: WidgetRun[], source_key?: string, after_key?: string, label?: string, commit?: boolean}
---@return true|nil, string?
function gvim.ui.widget_mutate(id, mutation) end
---@param spec Widget|{root: Widget, focus_key?: string}
---@param opts? {title?: string, rows?: integer, focus?: boolean, closable?: boolean, scope?: "window"|"editor"|"buffer", buf?: Buffer}
---@return integer|nil panel id, string?
function gvim.ui.mount_sidebar_section(spec, opts) end
gvim.ui.statusbar = {}
---@param spec {id: string, text?: string, align?: "left"|"right", priority?: integer, on_click?: fun()}
---@return StatusItem|nil, string?
function gvim.ui.statusbar.item(spec) end
-- tier-1c-A decoration surface: convenience wrappers over the namespace items above.
---@param buf Buffer
---@param line integer 0-based anchor line
---@param text string
---@param opts? {above?: boolean, style?: Style, gutter_glyph?: string, gutter_style?: Style, gutter_color?: Color, priority?: integer, compose_only?: boolean, activation?: "always"|"unless-cursor-in"|"if-cursor-in", scope?: {start_line?: integer, end_line?: integer}, text_overlays?: {start: integer, end: integer, style: Style}[], namespace?: string, version?: string}
---@return integer|nil id, string?
function gvim.decor.add_virtual_line(buf, line, text, opts) end
---@param buf Buffer
---@param line integer
---@param text string
---@param opts? table same as add_virtual_line, above is forced false
---@return integer|nil, string?
function gvim.decor.add_virtual_line_below(buf, line, text, opts) end
---@param buf Buffer
---@param from Pos
---@param to Pos
---@param replacement? string
---@param opts? {style?: Style, activation?: "always"|"unless-cursor-in"|"if-cursor-in", scope?: {start_line?: integer, end_line?: integer}, compose_only?: boolean, priority?: integer, namespace?: string, version?: string}
---@return integer|nil, string?
function gvim.decor.add_conceal(buf, from, to, replacement, opts) end
---@param buf Buffer
---@param pos Pos
---@param opts? {indent?: integer, prefix?: {text?: string, style?: Style}, activation?: string, scope?: table, compose_only?: boolean, priority?: integer, namespace?: string, version?: string}
---@return integer|nil, string?
function gvim.decor.add_soft_break(buf, pos, opts) end
---@param buf Buffer
---@param style Style|nil
---@param opts? {namespace?: string, priority?: integer, version?: string}
---@return integer|nil, string?
function gvim.decor.set_cursor_line_overlay(buf, style, opts) end
---@param buf Buffer
---@param range Range half-open line range
---@param color Color
---@param opts? {namespace?: string, priority?: integer, version?: string}
---@return integer|nil, string?
function gvim.decor.set_scrollbar_markers_in_range(buf, range, color, opts) end
---@param buf? Buffer
---@param range? Range
---@param opts? {namespace?: string}
---@return true
function gvim.decor.clear_decorations(buf, range, opts) end
---@param buf? Buffer
---@return true
function gvim.decor.refresh_lines(buf) end
---@param buf Buffer
---@param start_line integer
---@param end_line integer
---@param opts? {placeholder?: string, collapsed?: boolean, fold_kind?: string, namespace?: string, version?: string}
---@return true|nil, string?
function gvim.decor.add_fold(buf, start_line, end_line, opts) end
---@param buf Buffer
---@param ranges {start_line: integer, end_line: integer, kind?: string, collapsed?: boolean, placeholder?: string}[]
---@param opts? {namespace?: string, version?: string}
---@return true|nil, string?
function gvim.decor.set_folding_ranges(buf, ranges, opts) end
---@param buf? Buffer
---@param opts? {namespace?: string}
---@return true
function gvim.decor.clear_folds(buf, opts) end

-- file explorer decorations and slots (per plugin namespace)
gvim.explorer = {}
---@param namespace string 1 to 128 bytes; namespaces are isolated per plugin
---@param decorations {path: string, symbol: string, color?: Color, priority?: integer}[]
---@return true|nil, string?
function gvim.explorer.set_decorations(namespace, decorations) end
---@param namespace string
---@return true
function gvim.explorer.clear_decorations(namespace) end
---@param namespace string
---@param slots {path: string, leading?: {text?: string, color?: Color, min_width?: integer}, suppress_leading?: boolean, trailing?: {text?: string, color?: Color}, suppress_trailing?: boolean, name_color?: Color, suppress_name_color?: boolean, priority?: integer}[]
---@return true|nil, string?
function gvim.explorer.set_slots(namespace, slots) end
---@param namespace string
---@return true
function gvim.explorer.clear_slots(namespace) end

gvim.clipboard = {}
---@param text string
---@return true|nil, string?
function gvim.clipboard.set(text) end

gvim.decor = {}
---@param name string
---@return Namespace|nil, string?
function gvim.decor.namespace(name) end

-- paths, files, processes, timers, async

gvim.path = {}
gvim.path.sep = "/"
---@param ... string
---@return string
function gvim.path.join(...) end
---@param p string
---@return string
function gvim.path.dirname(p) end
---@param p string
---@return string
function gvim.path.basename(p) end
---@param p string
---@return string
function gvim.path.extname(p) end
---@param p string
---@return boolean
function gvim.path.is_absolute(p) end
---@param p string
---@return string
function gvim.path.normalize(p) end
---@param base string
---@param p string
---@return string|nil, string?
function gvim.path.relative(base, p) end
---@param p string
---@return string, string
function gvim.path.split(p) end
---@param p string
---@return string
function gvim.path.to_uri(p) end
---@param u string
---@return string|nil, string?
function gvim.path.from_uri(u) end

gvim.fs = {}
---@param path string
---@param opts? {max?: integer}
---@return string|nil, string?
function gvim.fs.read(path, opts) end
---@param path string
---@param data string
---@param opts? {atomic?: boolean, append?: boolean, mode?: integer, parents?: boolean, exclusive?: boolean}
---@return true|nil, string?
function gvim.fs.write(path, data, opts) end
---@param path string
---@param opts? {hidden?: boolean}
---@return {name: string, kind: "file"|"dir"|"symlink", size: integer, mtime: number, target_kind?: string}[]|nil, string?
function gvim.fs.list(path, opts) end
---@param path string
---@return {kind: string, size: integer, mtime: number, mode: integer}|nil, string?
function gvim.fs.stat(path) end
---@param path string
---@return boolean
function gvim.fs.exists(path) end
---@param path string
---@param opts? {parents?: boolean}
---@return true|nil, string?
function gvim.fs.mkdir(path, opts) end
---@param path string
---@param opts? {recursive?: boolean}
---@return true|nil, string?
function gvim.fs.remove(path, opts) end
---@param from string
---@param to string
---@return true|nil, string?
function gvim.fs.rename(from, to) end
---@param from string
---@param to string
---@return true|nil, string?
function gvim.fs.copy(from, to) end
---@param path string
---@return string|nil, string?
function gvim.fs.realpath(path) end
---@param root string
---@param opts? {skip_dirs?: string[], hidden?: boolean, max_depth?: integer, max_entries?: integer}
---@return {entries: {path: string, rel: string, kind: string, size: integer, mtime: number}[], truncated: boolean}|nil, string?
function gvim.fs.walk(root, opts) end
---@param path string
---@param fn fun(ev: {path: string, kind: "create"|"write"|"remove"|"rename"|"error", err?: string})
---@param opts? {debounce_ms?: integer}
---@return Watcher|nil, string?
function gvim.fs.watch(path, fn, opts) end

gvim.url = {}
---@param url string
---@return true|nil, string?
function gvim.url.open(url) end

gvim.http = {}
---@param url string
---@param opts? {timeout_ms?: integer}
---@return {status: integer, body: string, headers: table<string, string>}|nil, string?
function gvim.http.get(url, opts) end

gvim.proc = {}
---@param argv string[]
---@param opts? {cwd?: string, env?: table<string, string>, stdin?: string, timeout_ms?: integer, max_output?: integer, merge_stderr?: boolean}
---@return ProcResult|nil, string?
function gvim.proc.run(argv, opts) end
---@param argv string[]
---@param opts? {cwd?: string, env?: table<string, string>, stdin?: string|boolean, timeout_ms?: integer, max_output?: integer, merge_stderr?: boolean, lines?: boolean, on_start?: fun(pid: integer), on_stdout?: fun(chunk: string), on_stderr?: fun(chunk: string), on_exit?: fun(result: ProcResult)}
---@return Process|nil, string?
function gvim.proc.spawn(argv, opts) end
---@param name string
---@return string|nil
function gvim.proc.which(name) end

gvim.timer = {}
---@param ms integer
---@param fn fun()
---@return Timer|nil, string?
function gvim.timer.after(ms, fn) end
---@param ms integer
---@param fn fun()
---@return Timer|nil, string?
function gvim.timer.every(ms, fn) end
---@param ms integer
---@return true
function gvim.timer.sleep(ms) end
---@param ms integer
---@param fn fun(...)
---@return fun(...)
function gvim.timer.debounce(ms, fn) end

gvim.async = {}
---@param fn fun(...)
---@param ... any
---@return Task|nil, string?
function gvim.async.spawn(fn, ...) end
---@param fns (fun(): any)[]
---@return table[]
function gvim.async.all(fns) end
---@param fn fun(resume: fun(...))
---@param opts? {timeout_ms?: integer}
---@return any ...
function gvim.async.suspend(fn, opts) end

-- workspace, git, lsp, diff, other plugins

gvim.workspace = {}
---@return string
function gvim.workspace.root() end
---@param opts? {limit?: integer}
---@return {files: string[], loading: boolean} -- paths relative to the workspace root
function gvim.workspace.files(opts) end
---@param query string
---@param opts? {regex?: boolean, case_sensitive?: boolean, max?: integer}
---@return {path: string, line: integer, col: integer, text: string}[]|nil, string? -- path is relative to the workspace root
function gvim.workspace.grep(query, opts) end
---@param dir string
---@return string|nil
function gvim.workspace.git_root(dir) end
---@param root string
---@return string|nil, string?
function gvim.workspace.git_branch(root) end

gvim.git = {}
---@return boolean
function gvim.git.available() end
---@param args string[] arguments after "git"
---@param opts? {cwd?: string, stdin?: string, timeout_ms?: integer, max_output?: integer, env?: table<string, string>}
---@return ProcResult|nil, string?
function gvim.git.run(args, opts) end
---@param path string
---@return {root: string, head: string}|nil, string?
function gvim.git.repo(path) end
---@param path string
---@return string|nil, string?
function gvim.git.root(path) end
---@param root? string
---@return string|nil, boolean|string?
function gvim.git.branch(root) end
---@param path string
---@param opts? {rev?: string, for_buffer?: Buffer}
---@return string|nil, string?
function gvim.git.show(path, opts) end
---@param root? string
---@return string[]|nil, boolean|string?
function gvim.git.ls_files(root) end
---@param path string
---@param opts? {from?: integer, to?: integer, contents?: string, rev?: string}
---@return {line: integer, commit: string, author: string, time: integer, summary: string}[]|nil, string?
function gvim.git.blame(path, opts) end
---@param dir string
---@param hash string
---@return string[]|nil, string?
function gvim.git.commit(dir, hash) end

gvim.lsp = {}
---@param buf? Buffer the buffer whose primary caret's properties are read (default: current)
---@return table|nil the properties of the entry under the caret, string?
function gvim.buf.text_properties_at_cursor(buf) end
---@param buf? Buffer
---@return Diagnostic[]|nil, string?
function gvim.lsp.diagnostics(buf) end
---@return {path: string, diagnostics: Diagnostic[]}[]|nil, string?
function gvim.lsp.all_diagnostics() end
---@param method string
---@param params? table
---@param opts? {buf?: Buffer}
---@return any|nil, string?
function gvim.lsp.request(method, params, opts) end

gvim.diff = {}
---@param a string[]
---@param b string[]
---@return Hunk[]|nil, string?
function gvim.diff.lines(a, b) end
---@param a string
---@param b string
---@param opts? {ignore_eol?: boolean}
---@return Hunk[]|nil, string?
function gvim.diff.text(a, b, opts) end
---@param buf Buffer
---@param text string the old side
---@param opts? {ignore_eol?: boolean}
---@return Hunk[]|nil, string?
function gvim.diff.buffer(buf, text, opts) end
---@param buf Buffer
---@return Hunk[]|nil, string?
function gvim.diff.saved(buf) end
---@param hunks Hunk[]
---@param line integer
---@return Hunk|nil
function gvim.diff.next_hunk(hunks, line) end
---@param hunks Hunk[]
---@param line integer
---@return Hunk|nil
function gvim.diff.prev_hunk(hunks, line) end
---@class CompareSpan
---@field start integer
---@field end integer
---@class CompareSide
---@field start integer
---@field count integer
---@field from integer
---@field to integer
---@field jump integer
---@field lines [integer, integer][]
---@field spans (CompareSpan|false)[]
---@class CompareHunk
---@field left CompareSide
---@field right CompareSide
---@field eol_only boolean
---@class CompareResult
---@field hunks CompareHunk[]
---@field left_lines integer
---@field right_lines integer
---@field left_len integer
---@field right_len integer
---@field left_version string
---@field right_version string
---@param left Buffer
---@param right Buffer
---@return CompareResult|nil, string?
function gvim.diff.compare_buffers(left, right) end
---@param buf Buffer
---@param start integer
---@param end integer
---@param text string
---@param expected_version? string
---@return boolean|nil, string?
function gvim.diff.replace_range(buf, start, end, text, expected_version) end

gvim.dashboard = {}
---@param name string
---@param refresh fun(ctx: {width: integer, plugin: string, name: string}): table|nil, string?
---@param opts? {title?: string, ttl_ms?: integer, timeout_ms?: integer, priority?: integer}
---@return integer|nil, string?
function gvim.dashboard.register_section(name, refresh, opts) end
---@param id_or_name integer|string
---@return boolean
function gvim.dashboard.remove_section(id_or_name) end
---@return {id: integer, plugin: string, name: string, title: string, key: string, ttl_ms: integer, timeout_ms: integer, priority: integer}[] key is non-empty only for the bundled dashboard consumer
function gvim.dashboard.sections() end

gvim.plugins = {}
---@return {name: string, status: string, version: string, source: string}[]
function gvim.plugins.list() end
---@param tbl table
---@return true|nil, string?
function gvim.plugins.export(tbl) end
---@param name string
---@return table|nil
function gvim.plugins.get(name) end

-- data libraries (also reachable as the globals json, re, utf8 and bit)

gvim.json = {}
gvim.json.null = nil
---@param v any
---@param opts? {indent?: integer, sort_keys?: boolean}
---@return string
function gvim.json.encode(v, opts) end
---@param s string
---@param opts? {jsonc?: boolean}
---@return any, string?
function gvim.json.decode(s, opts) end
---@param s string
---@return any, string?
function gvim.json.decode_jsonc(s) end
---@param t table
---@return table
function gvim.json.array(t) end

gvim.re = {}
---@param pattern string
---@param flags? string "i", "m", "s"
---@return Regex|nil, string?
function gvim.re.compile(pattern, flags) end
---@param s string
---@param pattern string|Regex
---@param init? integer
---@return integer|nil, integer|string?, ...
function gvim.re.find(s, pattern, init) end
---@param s string
---@param pattern string|Regex
---@param init? integer
---@return string|false|nil, ...
function gvim.re.match(s, pattern, init) end
---@param s string
---@param pattern string|Regex
---@return boolean|nil, string?
function gvim.re.test(s, pattern) end
---@param s string
---@param pattern string|Regex
---@return fun(): string|false|nil, ...
function gvim.re.gmatch(s, pattern) end
---@param s string
---@param pattern string|Regex
---@param repl string|table|fun(...): string
---@param n? integer
---@return string|nil, integer|string
function gvim.re.gsub(s, pattern, repl, n) end
---@param s string
---@param pattern string|Regex
---@param n? integer
---@return string[]|nil, string?
function gvim.re.split(s, pattern, n) end
---@param s string
---@return string
function gvim.re.escape(s) end

gvim.text = {}
---@param s string
---@return integer
function gvim.text.width(s) end
---@param s string
---@param w integer
---@param ellipsis? string
---@return string
function gvim.text.truncate(s, w, ellipsis) end
---@param s string
---@param w integer
---@param align? "left"|"right"|"center"
---@return string
function gvim.text.pad(s, w, align) end
---@param s string
---@return string
function gvim.text.upper(s) end
---@param s string
---@return string
function gvim.text.lower(s) end
---@param s string
---@return string
function gvim.text.trim(s) end
---@param s string
---@param sep string
---@param plain? boolean
---@return string[]
function gvim.text.split(s, sep, plain) end
---@param s string
---@param prefix string
---@return boolean
function gvim.text.starts_with(s, prefix) end
---@param s string
---@param suffix string
---@return boolean
function gvim.text.ends_with(s, suffix) end
---@generic T
---@param query string
---@param items T[]
---@param opts? {key?: string, limit?: integer}
---@return {item: T, score: integer, indexes: integer[]}[]|nil, string?
function gvim.text.fuzzy(query, items, opts) end
---@param data string
---@param encoding string
---@param opts? {normalize?: boolean}
---@return string|nil, string?
function gvim.text.decode(data, encoding, opts) end

gvim.utf8 = {}
gvim.utf8.charpattern = ""
---@param s string
---@param i? integer
---@param j? integer
---@return integer|nil, integer?
function gvim.utf8.len(s, i, j) end
---@param ... integer
---@return string
function gvim.utf8.char(...) end
---@param s string
---@param i? integer
---@param j? integer
---@return integer ...
function gvim.utf8.codepoint(s, i, j) end
---@param s string
---@param n integer
---@param i? integer
---@return integer|nil
function gvim.utf8.offset(s, n, i) end
---@param s string
---@return fun(): integer?, integer?
function gvim.utf8.codes(s) end
---@param s string
---@return boolean
function gvim.utf8.valid(s) end
---@param s string
---@param ci integer
---@param cj? integer
---@return string
function gvim.utf8.sub(s, ci, cj) end
---@param s string
---@param b integer
---@return integer
function gvim.utf8.byte_to_char(s, b) end
---@param s string
---@param c integer
---@return integer
function gvim.utf8.char_to_byte(s, c) end

gvim.menu = {}
---@param item {menu: string, label: string, action: string, checkbox?: string, when?: string, after?: string, before?: string}
---@return true|nil, string?
function gvim.menu.add_item(item) end
---@param filter {menu: string, action?: string, label?: string}
---@return true|nil, string?
function gvim.menu.remove_item(filter) end
---@return true
function gvim.menu.clear() end
gvim.i18n = {}
---@param key string a key of the plugin's <name>.i18n.json ("cmd.open", "status.ready", ...)
---@param vars? table<string, string|number|boolean> values for the %{name} placeholders
---@return string current locale, then English, then the key itself
function gvim.i18n.t(key, vars) end
---@param key string
---@param vars? table<string, string|number|boolean>
---@return string
function gvim.i18n.translate(key, vars) end
---@return string the editor's locale ("en" today)
function gvim.i18n.locale() end

gvim.bit = {}
---@param ... integer
---@return integer
function gvim.bit.band(...) end
---@param ... integer
---@return integer
function gvim.bit.bor(...) end
---@param ... integer
---@return integer
function gvim.bit.bxor(...) end
---@param x integer
---@return integer
function gvim.bit.bnot(x) end
---@param x integer
---@param n integer
---@return integer
function gvim.bit.lshift(x, n) end
---@param x integer
---@param n integer
---@return integer
function gvim.bit.rshift(x, n) end

gvim.time = {}
---@return integer
function gvim.time.now_ms() end
---@return integer
function gvim.time.monotonic_ms() end
---@param fmt string strftime subset; a leading "!" means UTC
---@param t_ms? integer
---@return string
function gvim.time.format(fmt, t_ms) end

gvim.theme = {}
---@return string
function gvim.theme.name() end
---@param key string "section.key"
---@return {r: integer, g: integer, b: integer}|nil
function gvim.theme.color(key) end
---@return boolean
function gvim.theme.is_dark() end
---@return string[] the built-in theme names
function gvim.theme.builtin_themes() end
---@return string the user theme directory
function gvim.theme.user_dir() end
---@param ref string
---@return boolean
function gvim.theme.file_exists(ref) end
---@param ref string
---@return string|nil resolved theme as theme-file JSON, string?
function gvim.theme.data(ref) end
---@return {name: string, ref: string, builtin: boolean}[]
function gvim.theme.all() end
---@param name string
---@param data string a theme file's JSON (at most 1 MiB)
---@return string|nil portable ref, string?
function gvim.theme.save_file(name, data) end
---@param ref string a user theme
---@return true|nil, string?
function gvim.theme.delete_file(ref) end
---@param ref? string a theme to apply after the rescan
---@return true|nil, string?
function gvim.theme.reload(ref) end
---@return {keys: {name: string, kind: "required"|"fallback"|"optional", fallback?: string}[], modifier_only: string[], named_colors: string[]}
function gvim.theme.schema() end

---@param colors table<string, string> "section.key" -> "#RRGGBB", a named color or "Default"
---@return true|nil, string?
function gvim.theme.override(colors) end
---@return true|nil, string?
function gvim.theme.clear() end
---@return table<string, string> the session overrides in force
function gvim.theme.overrides() end

json = gvim.json
re = gvim.re
utf8 = gvim.utf8
bit = gvim.bit
