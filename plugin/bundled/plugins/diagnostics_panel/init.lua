-- Diagnostics Panel: a port of Fresh's plugins/diagnostics_panel.ts
-- (refs/fresh/crates/fresh-editor/plugins/diagnostics_panel.ts, 275 lines) with the
-- live-panel part of its plugins/lib/finder.ts (1862 lines).
--
-- Ported from Fresh:
--   * a provider that rebuilds the result list on demand, and the panel refreshes itself on
--     the editor's diagnostics_updated event and when the active buffer changes
--   * groupBy = "file": one header row per file, the file's diagnostics below it
--   * rows formatted like Fresh's builder, "[E] line:col message", with the diagnostic's
--     source in the detail column and 1-based line:col (gvim reports 0-based positions).
--     gvim's gvim.lsp rows are {range, severity, message, source}: severity is already the
--     string "error"/"warning"/"info"/"hint", and there is no diagnostic "code" field, so
--     the source/code cell is the source only.
--   * the current-file / all-files filter (Fresh's 'a' key); the panel opens on the current
--     file and falls back to all files when the active buffer has no file, as Fresh does
--   * Enter jumps to the location and the panel closes so the keyboard returns to the editor
--     (a v1 widget panel holds the keyboard while it is focused; Fresh's dock panel stays up)
--   * the panel's title names the filter, like Fresh's getTitle()
--
-- Reduced or omitted from Finder (the reasons are the API, not the feature):
--   * prompt mode and search sources (ripgrep, filters, debounce, cancellation): the
--     diagnostics panel is a panel, not a prompt
--   * the preview pane, syncWithEditor and navigateOnCursorMove: one selectable list, no
--     preview tabs and no editor-cursor tracking
--   * useUtilityDock: gvim's widget panel v1 has float, buffer and sidebar placements, not a
--     utility dock; the port mounts a float card
--   * panelKeys q/a/r: a v1 panel reports widget_events only, so 'a' is the in-panel
--     "All files" toggle, 'r' is not needed (diagnostics_updated refreshes by itself) and
--     Esc (the host's closable path) is how the panel closes
--   * severity colours: a tree draws one style; the [E]/[W]/[I]/[H] tag carries severity
--
-- The gvim side is plugin API level 2 tier 1a: gvim.ui.mount_widget_panel /
-- update_widget_panel / unmount_widget_panel, widget_event for the input, and the language
-- servers read through gvim.lsp. Everything is bounded: at most MAX_ROWS rows, every field
-- cut to a cell budget, and the current-file filter asks only the active document (the
-- all-files filter is the only one that builds every open document's list, and it stops
-- reading at MAX_ROWS). Nothing runs while the panel is closed.
local gvim = require "gvim"

local t = gvim.i18n.t

-- Caps. all_diagnostics() may return up to 50 000 diagnostics; the panel keeps the first
-- MAX_ROWS (grouped by file, so a file's whole group is either listed or is where the list
-- stops). One panel node is at most 4 KiB and there are 5 000 tree nodes, both far away.
local MAX_ROWS = 500
local MAX_MESSAGE_CELLS = 120
local MAX_NAME_CELLS = 40
local MAX_DIR_CELLS = 60
local MAX_SOURCE_CELLS = 40
local MAX_KEY_BYTES = 120

-- Fresh's severity mapping is numeric 1..4; gvim already names the levels.
local SEVERITY_ICON = { error = "[E]", warning = "[W]", info = "[I]", hint = "[H]" }
local SEVERITY_RANK = { error = 1, warning = 2, info = 3, hint = 4 }

-- The live state of the plugin. panel is nil while the panel is closed.
local state = {
  panel = nil, -- the mounted widget panel id
  all_files = false, -- Fresh's showAllFiles
  node_row = {}, -- tree node index -> diagnostic row; nil for a file header
}

-- clip cuts a string to a byte budget without splitting a UTF-8 sequence. Widget keys have a
-- byte cap (128); the cell helpers below are for what is drawn.
local function clip(s, limit)
  s = tostring(s or "")
  if #s <= limit then
    return s
  end
  local cut = s:sub(1, limit)
  local i = #cut
  while i > 0 do
    local b = cut:byte(i)
    if b < 0x80 then
      break
    end
    if b >= 0xC0 then
      cut = cut:sub(1, i - 1)
      break
    end
    i = i - 1
  end
  return cut
end

-- cells cuts a drawn string to a number of display cells.
local function cells(s, limit)
  return gvim.text.truncate(tostring(s or ""), limit)
end

-- first_line keeps the first line of a message: Fresh shows one line per diagnostic.
local function first_line(message)
  local text = tostring(message or "")
  local nl = text:find("\n", 1, true)
  if nl then
    text = text:sub(1, nl - 1)
  end
  return cells(gvim.text.trim(text), MAX_MESSAGE_CELLS)
end

-- severity_of maps a gvim.lsp severity (already a name) to the two-part table above.
local function severity_of(diagnostic)
  local severity = diagnostic.severity
  if SEVERITY_ICON[severity] == nil then
    severity = "info"
  end
  return severity
end

-- active_file returns the active buffer and its path, or nil for a virtual or untitled one,
-- which Fresh treats as "no current file" (the filter then shows every file).
local function active_file()
  local buf = gvim.buf.current()
  if not buf or buf:is_virtual() then
    return nil, nil
  end
  local path = buf:path()
  if not path or path == "" then
    return nil, nil
  end
  return buf, path
end

-- add_rows appends the diagnostics of one file to rows, up to MAX_ROWS.
local function add_rows(rows, path, diagnostics)
  for _, diagnostic in ipairs(diagnostics or {}) do
    if #rows >= MAX_ROWS then
      return
    end
    local range = diagnostic.range or {}
    local from = range.from or {}
    local severity = severity_of(diagnostic)
    rows[#rows + 1] = {
      path = path,
      line = from.line or 0,
      col = from.col or 0,
      severity = severity,
      rank = SEVERITY_RANK[severity] or 4,
      message = first_line(diagnostic.message),
      source = cells(diagnostic.source or "", MAX_SOURCE_CELLS),
    }
  end
end

-- collect asks the language servers for the diagnostics of the current filter, sorted the
-- way Fresh sorts them: by file with the active file first, then line, then severity.
--
-- The current-file filter calls gvim.lsp.diagnostics(active buffer): that is exactly the
-- part of all_diagnostics() it would keep, and it does not build every open document's
-- table on the editor loop. All files (and the no-active-file fallback) is the one place
-- that calls gvim.lsp.all_diagnostics(), and it stops after MAX_ROWS rows.
local function collect()
  local buf, active = active_file()
  local rows = {}
  if not state.all_files and active then
    add_rows(rows, active, gvim.lsp.diagnostics(buf) or {})
  else
    for _, file in ipairs(gvim.lsp.all_diagnostics() or {}) do
      add_rows(rows, file.path or "", file.diagnostics)
      if #rows >= MAX_ROWS then
        break
      end
    end
  end
  table.sort(rows, function(a, b)
    if a.path ~= b.path then
      if a.path == active then
        return true
      end
      if b.path == active then
        return false
      end
      return a.path < b.path
    end
    if a.line ~= b.line then
      return a.line < b.line
    end
    if a.rank ~= b.rank then
      return a.rank < b.rank
    end
    return a.col < b.col
  end)
  return rows
end

-- relative_dir is the directory shown next to a file's name: relative to the workspace root
-- (read once per build, passed in) when the file is inside it, as it is, otherwise. "" for a
-- file at the root.
local function relative_dir(path, root)
  local shown = path
  if root ~= "" then
    local rel = gvim.path.relative(root, path)
    if rel then
      shown = rel
    end
  end
  local dir = gvim.path.dirname(shown)
  if dir == "." then
    dir = ""
  end
  return cells(dir, MAX_DIR_CELLS)
end

-- file_header is the depth-0 node that groups one file's diagnostics (Fresh's "fileName:").
local function file_header(path, root)
  local name = gvim.path.basename(path)
  if name == "" then
    name = path
  end
  return {
    key = clip("file:" .. path, MAX_KEY_BYTES),
    text = cells(name, MAX_NAME_CELLS) .. ":",
    detail = relative_dir(path, root),
    depth = 0,
    expanded = true,
  }
end

-- row_text is one diagnostic line, Fresh's "[E] line:col message" (1-based line and column).
local function row_text(row)
  return ("%s %d:%d %s"):format(SEVERITY_ICON[row.severity], row.line + 1, row.col + 1, row.message)
end

-- row_key is Fresh's entry id for a diagnostic: it keys the tree node (so a refresh keeps
-- the selection on the same diagnostic) and the id dedupe below.
local function row_key(row)
  return clip(("%s:%d:%d:%s:%s"):format(row.path, row.line, row.col, row.severity, row.message), MAX_KEY_BYTES)
end

-- row_node maps a diagnostic row to a tree node, styled like Fresh's buildItemEntry.
local function row_node(row)
  return {
    key = row_key(row),
    text = row_text(row),
    detail = row.source,
    depth = 1,
    leaf = true,
  }
end

-- filter_label names the current scope (Fresh's t("panel.current_file"/"panel.all_files")).
local function filter_label()
  if state.all_files then
    return t("panel.all_files")
  end
  return t("panel.current_file")
end

-- build returns the widget spec of the panel, the rows it shows and the node -> row map.
-- Rows with one id are one entry (Fresh's formatUnique), so a server that publishes a
-- diagnostic twice shows it once and the count agrees with the list.
local function build()
  local collected = collect()
  local rows = {}
  local seen = {}
  local root = gvim.workspace.root()
  local nodes, node_row = {}, {}
  local last_path
  for _, row in ipairs(collected) do
    local key = row_key(row)
    if not seen[key] then
      seen[key] = true
      rows[#rows + 1] = row
      if row.path ~= last_path then
        nodes[#nodes + 1] = file_header(row.path, root)
        last_path = row.path
      end
      nodes[#nodes + 1] = row_node(row)
      node_row[#nodes] = row
    end
  end

  local children = {
    {
      kind = "row",
      key = "header",
      children = {
        { kind = "toggle", key = "scope", label = t("panel.all_files_toggle"), checked = state.all_files },
        { kind = "spacer", flex = true },
        { kind = "label", key = "count", text = t("panel.count", { count = #rows }) },
      },
    },
    { kind = "divider", key = "divider" },
  }
  if #rows == 0 then
    children[#children + 1] = { kind = "label", key = "empty", text = t("panel.no_diagnostics") }
    children[#children + 1] = { kind = "hintBar", key = "hints", runs = { { text = t("panel.hint_close") } } }
    return { root = { kind = "col", key = "root", children = children }, focus_key = "scope" }, rows, node_row
  end

  -- Start the selection on the first diagnostic, not on a file header (Fresh's panel opens
  -- on its title line and needs a Down to reach the first entry; an activated header is a
  -- no-op here).
  local selected = 1
  while selected <= #nodes and node_row[selected] == nil do
    selected = selected + 1
  end
  if selected > #nodes then
    selected = 1
  end
  children[#children + 1] = {
    kind = "tree",
    key = "rows",
    nodes = nodes,
    selected_index = selected,
  }
  children[#children + 1] = {
    kind = "hintBar",
    key = "hints",
    runs = { { text = t("panel.hint_goto") }, { text = t("panel.hint_close") } },
  }
  return { root = { kind = "col", key = "root", children = children }, focus_key = "rows" }, rows, node_row
end

-- mount opens the panel with the current filter and keeps its id and rows. Fresh's finder
-- livePanel: focus, title, ratio 0.3 (the widget card is a percentage of the frame instead).
local function mount()
  local spec, rows, node_row = build()
  local id, err = gvim.ui.mount_widget_panel(spec, {
    title = t("panel.header", { filter = filter_label() }),
    focus = true,
    width = 70,
    height = 60,
  })
  if not id then
    gvim.log.warn("mount: %s", tostring(err))
    gvim.notify(t("status.failed_to_open"), "warn")
    return nil
  end
  state.panel, state.node_row = id, node_row
  return id, #rows
end

-- refresh rebuilds the rows of the open panel. A panel the host dropped (an owner change, a
-- reload) is forgotten instead of being fought over.
local function refresh()
  if not state.panel then
    return 0
  end
  local spec, rows, node_row = build()
  local ok, err = gvim.ui.update_widget_panel(state.panel, spec)
  if not ok then
    gvim.log.debug("update: %s", tostring(err))
    state.panel, state.node_row = nil, {}
    return 0
  end
  state.node_row = node_row
  return #rows
end

-- close unmounts the panel (if it is mounted) and forgets its rows.
local function close(notice)
  if state.panel then
    gvim.ui.unmount_widget_panel(state.panel)
  end
  state.panel, state.node_row = nil, {}
  if notice then
    gvim.notify(notice)
  end
end

-- show is Fresh's show_diagnostics_panel: open, or refresh when it is already open.
local function show()
  if state.panel then
    local count = refresh()
    gvim.notify(t("status.diagnostics_count", { count = count }))
    return
  end
  local id, count = mount()
  if id then
    gvim.notify(t("status.diagnostics_count", { count = count }))
  end
end

-- jump opens a diagnostic's file at its 0-based position and closes the panel so the editor
-- gets the keyboard back (see the header comment).
local function jump(row)
  if not row then
    return
  end
  local file = row.path
  local line, col = row.line, row.col
  close()
  local buf, err = gvim.buf.open(file, { pos = gvim.pos(line, col), focus = true })
  if not buf then
    gvim.log.warn("open %s: %s", tostring(file), tostring(err))
    gvim.notify(t("status.failed_to_jump", { file = file }), "warn")
    return
  end
  gvim.notify(t("status.jumped_to", { file = file, line = line + 1 }))
end

-- set_scope switches the current-file / all-files filter (Fresh's diagnostics_toggle_all).
-- The panel is mounted again rather than updated because the card title is fixed at mount
-- and Fresh's title names the filter; the new panel keeps the keyboard.
local function set_scope(all_files)
  state.all_files = all_files
  if not state.panel then
    return
  end
  close()
  mount()
end

-- The input of a v1 widget panel arrives as widget_event (there is no panel key handler):
-- Enter on a diagnostic row activates it, Space/Enter on the header toggle flips the filter,
-- and the host's Escape path arrives as "cancel" after the panel was unmounted.
gvim.on("widget_event", function(ev)
  if not state.panel or ev.panel ~= state.panel then
    return
  end
  if ev.event == "toggle" and ev.widget == "scope" then
    set_scope(ev.checked == true)
    if state.panel then
      gvim.notify(t("status.showing", { label = filter_label() }))
    end
  elseif ev.event == "activate" then
    -- A tree reports the key of the activated row, so the event is matched by its index:
    -- node_row has nil for a file header, and jump(nil) does nothing.
    jump(state.node_row[ev.index])
  elseif ev.event == "cancel" then
    -- The host unmounted the panel already; only the plugin state is left to clear.
    state.panel, state.node_row = nil, {}
    gvim.notify(t("status.closed"))
  end
end)

-- Fresh's provider.notify() on diagnostics_updated: the servers published a different set.
gvim.on("diagnostics_updated", function()
  if state.panel then
    refresh()
  end
end)

-- Fresh's buffer_activated handler: the current-file filter follows the active buffer.
-- Virtual buffers have no path and leave the filter alone, as Fresh's does.
gvim.on("buffer_activated", function(ev)
  if not state.panel or state.all_files then
    return
  end
  local buf = ev.buf
  if not buf or buf:is_virtual() or not buf:path() then
    return
  end
  refresh()
end)

-- The two commands Fresh registers, with its titles and descriptions (the % prefix is a
-- translation key). Fresh binds no key for them, and neither does this port; bind one in the
-- configuration if you want it ("diagnostics_panel.toggle_diagnostics_panel").
gvim.command.register {
  name = "show_diagnostics_panel",
  title = "%cmd.show_diagnostics_panel",
  description = "%cmd.show_diagnostics_panel_desc",
  run = function()
    show()
  end,
}

gvim.command.register {
  name = "toggle_diagnostics_panel",
  title = "%cmd.toggle_diagnostics_panel",
  description = "%cmd.toggle_diagnostics_panel_desc",
  run = function()
    if state.panel then
      close(t("status.closed"))
    else
      show()
    end
  end,
}

gvim.log.debug("diagnostics panel plugin loaded (live widget panel)")
