-- gvim.finder: the shared Finder model the bundled plugins use (a Lua port of the parts of
-- Fresh's plugins/lib/finder.ts that the prompt/list plugins need).
--
-- Layers:
--   * ranking/filtering -- Fresh's defaultFuzzyFilter algorithm (consecutive-match,
--     segment-start and filename bonuses, a length penalty) with hard caps, plus a stable
--     order and an optional custom filter for sources that re-sort (symbol lists by line).
--   * prompt frontend -- Finder.new{...} over gvim.ui.picker with filter = "none": the host
--     shows the rows, the library ranks them. Esc cancels, Enter selects, and on_close runs
--     exactly once with the reason ("selected", "cancelled", "programmatic").
--   * panel backend -- M.tree(items, opts): Fresh's grouped, deduped (one entry per id) tree
--     rows for a widget panel (diagnostics_panel's livePanel subset).
--   * location lists -- M.open_locations{items, ...}: the "list of locations" finder shared
--     by find_references and lsp_navigation (relative path:line labels, snippet descriptions,
--     preview tabs on move, commit on Enter, discard on cancel).
--
-- Reduced vs Fresh's finder.ts: no asynchronous SearchSource with cancellation handles or
-- per-query process spawns (a source is a Lua loader plus the library's ranking, and the
-- picker generations protect against out-of-order set_items), no embedded preview pane /
-- utility-dock routing / syncWithEditor / navigateOnCursorMove / panelKeys / prompt-type
-- events, and the panel backend covers the grouped-tree use only. Everything is bounded:
-- at most MAX_ITEMS source items, MAX_RESULTS rows, MAX_QUERY bytes, MAX_LABEL/MAX_DETAIL
-- cells and MAX_LOCATION_FILES file reads of MAX_LOCATION_BYTES each.
local gvim = require "gvim"

local M = {}

M.MAX_ITEMS = 2000
M.MAX_RESULTS = 100
M.MAX_QUERY = 64
M.MAX_LABEL = 256
M.MAX_DETAIL = 256
M.MAX_SCORE_LEN = 512
M.MAX_LOCATION_FILES = 100
M.MAX_LOCATION_BYTES = 1024 * 1024

-- clip cuts a string to a byte budget without splitting a UTF-8 sequence.
function M.clip(s, limit)
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
function M.cells(s, limit)
  return gvim.text.truncate(tostring(s or ""), limit)
end

-- relative_path names a file the way Fresh's getRelativePath does: relative to the
-- workspace root when it is inside it, the path itself otherwise.
function M.relative_path(path)
  path = tostring(path or "")
  local root = gvim.workspace.root()
  if root ~= "" then
    local rel = gvim.path.relative(root, path)
    if rel and rel ~= "" then
      return rel
    end
  end
  return path
end

local function lower(s)
  return gvim.text.lower(tostring(s or ""))
end

-- M.score is Fresh's fuzzyScore: higher is better, nil means no match. The scan is bounded
-- (pattern and text are cut to MAX_SCORE_LEN/MAX_QUERY bytes).
function M.score(text, pattern)
  pattern = lower(M.clip(pattern, M.MAX_QUERY))
  if pattern == "" then
    return 0
  end
  text = lower(M.clip(text, M.MAX_SCORE_LEN))
  local score = 0
  local str_idx, pat_idx = 1, 1
  local consecutive = 0
  local last_match = 0
  local last_slash = text:match("^.*()/") or 0
  while str_idx <= #text and pat_idx <= #pattern do
    if text:sub(str_idx, str_idx) == pattern:sub(pat_idx, pat_idx) then
      if last_match == str_idx - 1 then
        consecutive = consecutive + 1
        score = score + consecutive * 10
      else
        consecutive = 1
        score = score + 1
      end
      local prev = str_idx > 1 and text:sub(str_idx - 1, str_idx - 1) or ""
      if str_idx == 1 or prev == "/" or prev == "_" or prev == "-" then
        score = score + 15
      end
      if str_idx > last_slash then
        score = score + 5
      end
      last_match = str_idx
      pat_idx = pat_idx + 1
    end
    str_idx = str_idx + 1
  end
  score = score - #text * 0.1
  if pat_idx > #pattern then
    return score
  end
  return nil
end

-- M.filter ranks source items with M.score and cuts the result to max_results. get_text is
-- the text ranked (a string or a function(item) -> string). Ties keep the source order.
function M.filter(items, query, get_text, max_results)
  max_results = max_results or M.MAX_RESULTS
  local scored = {}
  local n = math.min(#items, M.MAX_ITEMS)
  for i = 1, n do
    local item = items[i]
    local text = type(get_text) == "function" and get_text(item) or tostring(item)
    local score = M.score(text, query)
    if score ~= nil then
      scored[#scored + 1] = { item = item, score = score, index = i }
    end
  end
  table.sort(scored, function(a, b)
    if a.score ~= b.score then
      return a.score > b.score
    end
    return a.index < b.index
  end)
  local out = {}
  for i = 1, math.min(#scored, max_results) do
    out[i] = scored[i].item
  end
  return out
end

-- ---------------------------------------------------------------------------
-- Prompt frontend
-- ---------------------------------------------------------------------------
local Finder = {}
Finder.__index = Finder

-- finder:open() opens the picker. The source is loaded once when the prompt opens (a lazy
-- loader keeps large data out of the plugin's load), then every query is ranked in Lua.
function Finder:open(opts)
  opts = opts or {}
  if self.open_ then
    return nil, "busy: the finder is already open"
  end
  local f = self
  self.open_ = true
  self.selected_item = nil
  local pick = gvim.ui.picker {
    title = opts.title or self.title or "",
    filter = "none",
    placeholder = opts.placeholder or self.placeholder or "",
    footer = opts.footer or self.footer or "",
    initial_query = opts.initial_query or self.initial_query or "",
    on_query = function(query, picker)
      local items = f:source_items()
      f:fill(picker, query, items)
    end,
    on_move = function(item)
      if not f.open_ then
        return
      end
      f.selected_item = item and item.item or nil
      if f.on_move then
        f.on_move(f.selected_item)
      end
    end,
    on_select = function(item, index)
      f.open_ = false
      f.selected_item = item and item.item or nil
      if f.on_select then
        f.on_select(f.selected_item, index)
      end
      f:finish("selected")
    end,
    on_close = function()
      f:finish("cancelled")
    end,
  }
  self.picker = pick
  local ok, err = pick:open()
  if not ok then
    self.open_ = false
    self.picker = nil
    return nil, err
  end
  return pick
end

-- source_items loads the source once (bounded) and returns it.
function Finder:source_items()
  if self.items then
    return self.items
  end
  local loaded = {}
  if self.load then
    loaded = self.load() or {}
  end
  if #loaded > M.MAX_ITEMS then
    local cut = {}
    for i = 1, M.MAX_ITEMS do
      cut[i] = loaded[i]
    end
    loaded = cut
  end
  self.items = loaded
  return loaded
end

-- fill ranks the source for the query and replaces the picker's rows.
function Finder:fill(picker, query, items)
  query = M.clip(query or "", M.MAX_QUERY)
  local ranked
  if self.custom_filter then
    ranked = self.custom_filter(items, query) or {}
  else
    local rank_text = self.rank_text
    if type(rank_text) ~= "function" then
      rank_text = function(item)
        local d = self.format(item) or {}
        return d.label or d.text or ""
      end
    end
    ranked = M.filter(items, query, rank_text, self.max_results)
  end
  local rows = {}
  for i = 1, math.min(#ranked, self.max_results) do
    local item = ranked[i]
    local d = self.format(item, i) or {}
    rows[#rows + 1] = {
      text = M.cells(M.clip(d.label or "", M.MAX_LABEL), 160),
      detail = M.cells(M.clip(d.description or d.detail or "", M.MAX_DETAIL), 120),
      item = item,
    }
  end
  local ok, err = picker:set_items(rows)
  if not ok and err and not tostring(err):find("stale", 1, true) then
    gvim.log.debug("finder: set_items: %s", tostring(err))
  end
  if self.on_rows then
    self.on_rows(#rows)
  end
end

-- selected returns the current row's source item (nil when none).
function Finder:selected()
  if self.picker then
    local ok, item = pcall(function() return self.picker:selected() end)
    if ok and type(item) == "table" then
      return item.item
    end
  end
  return self.selected_item
end

-- close closes the picker programmatically and reports the close reason once.
function Finder:close()
  if not self.open_ then
    return false
  end
  self.open_ = false
  if self.picker then
    pcall(self.picker.close, self.picker)
  end
  self:finish("programmatic")
  return true
end

-- finish runs the close callback exactly once with the reason.
function Finder:finish(reason)
  if self.finished then
    return
  end
  self.finished = true
  self.open_ = false
  if self.on_close then
    self.on_close(reason)
  end
end

function Finder:is_open()
  return self.open_ == true
end

-- new creates a finder. opts:
--   title, placeholder, footer, initial_query
--   load            () -> items (called once, lazily)
--   format          (item, index) -> {label, description, detail, id}
--   rank_text       (item) -> string ranked by the default filter (default: label)
--   filter          (items, query) -> items   custom filter/ordering (overrides ranking)
--   max_results     default MAX_RESULTS
--   on_select       (item, index)
--   on_move         (item)
--   on_close        (reason)  reason: "selected" | "cancelled" | "programmatic"
function M.new(opts)
  opts = opts or {}
  if type(opts.load) ~= "function" then
    error("gvim.finder.new: opts.load is required", 2)
  end
  if type(opts.format) ~= "function" then
    error("gvim.finder.new: opts.format is required", 2)
  end
  local f = setmetatable({}, Finder)
  f.title, f.placeholder, f.footer = opts.title, opts.placeholder, opts.footer
  f.initial_query = opts.initial_query
  f.load = opts.load
  f.format = opts.format
  f.rank_text = opts.rank_text
  f.custom_filter = opts.filter
  f.max_results = math.min(opts.max_results or M.MAX_RESULTS, M.MAX_RESULTS)
  f.on_select, f.on_move, f.on_close = opts.on_select, opts.on_move, opts.on_close
  f.on_rows = opts.on_rows
  return f
end

-- ---------------------------------------------------------------------------
-- Panel backend: grouped, deduped tree rows for a widget panel
-- ---------------------------------------------------------------------------
-- tree builds widget-panel tree nodes from ordered items. opts:
--   key       (item) -> stable id (the dedupe key; nil skips dedupe)
--   dedupe    default true
--   group     (item) -> {key=, text=, detail=} | nil   (nil: a flat list)
--   row       (item) -> {text=, detail=, key=}
--   max_rows  cap (default MAX_ITEMS)
-- Returns {nodes=, node_map=, count=}; node_map[i] is the item of node i (nil for a group
-- header), so an activate event's node index resolves to its item.
function M.tree(items, opts)
  opts = opts or {}
  local nodes, node_map, seen = {}, {}, {}
  local last_group = nil
  local kept = 0
  local max_rows = opts.max_rows or M.MAX_ITEMS
  for _, item in ipairs(items) do
    local key = opts.key and opts.key(item) or nil
    local duplicate = false
    if key ~= nil and opts.dedupe ~= false then
      if seen[key] then
        duplicate = true
      else
        seen[key] = true
      end
    end
    if not duplicate then
      local group = opts.group and opts.group(item) or nil
      if group and group.key ~= last_group then
        nodes[#nodes + 1] = {
          key = group.key,
          text = group.text or "",
          detail = group.detail or "",
          depth = 0,
          expanded = true,
        }
        last_group = group.key
      end
      local row = opts.row(item)
      nodes[#nodes + 1] = {
        key = row.key or key,
        text = row.text,
        detail = row.detail or "",
        depth = group and 1 or 0,
        leaf = true,
      }
      node_map[#nodes] = item
      kept = kept + 1
      if kept >= max_rows then
        break
      end
    end
  end
  return { nodes = nodes, node_map = node_map, count = kept }
end

-- first_leaf returns the 1-based node index of the first row (not a group header).
function M.first_leaf(tree)
  for i = 1, #tree.nodes do
    if tree.node_map[i] ~= nil then
      return i
    end
  end
  return 1
end

-- ---------------------------------------------------------------------------
-- Location lists
-- ---------------------------------------------------------------------------
-- utf16_to_byte converts an LSP UTF-16 column of line to a byte column.
function M.utf16_to_byte(line, units)
  if units <= 0 then
    return 0
  end
  local seen = 0
  for i = 1, #line do
    local b = line:byte(i)
    if b < 0x80 or b >= 0xC0 then
      if seen == units then
        return i - 1
      end
      -- A code point above the BMP counts as two UTF-16 units.
      local size = 1
      if b >= 0xF0 then
        size = 2
      end
      seen = seen + size
    end
  end
  return #line
end

-- byte_to_utf16 converts a byte column of line to an LSP UTF-16 column.
function M.byte_to_utf16(line, byte_col)
  byte_col = math.max(0, math.min(byte_col or 0, #line))
  local units = 0
  local i = 1
  while i <= byte_col do
    local b = line:byte(i)
    if b >= 0xF0 then
      units = units + 2
      i = i + 4
    elseif b >= 0xE0 then
      units = units + 1
      i = i + 3
    elseif b >= 0xC0 then
      units = units + 1
      i = i + 2
    else
      units = units + 1
      i = i + 1
    end
  end
  return units
end

-- open_locations shows a list of {path, line (0-based), col (0-based byte), text}. Moving the
-- selection opens a preview tab at the location; Enter commits (the preview tab is closed and
-- the file opened for real); Esc closes the preview. Bounded: MAX_RESULTS rows, each file read
-- once at most MAX_LOCATION_BYTES, at most MAX_LOCATION_FILES reads.
function M.open_locations(opts)
  opts = opts or {}
  local items = opts.items or {}
  local preview_buf = nil
  local committed = false
  local finder

  local function discard_preview()
    if preview_buf and not committed then
      local ok, info = pcall(function() return preview_buf:info() end)
      if ok and info and info.preview then
        pcall(preview_buf.close, preview_buf)
      end
    end
    preview_buf = nil
  end

  local function preview(item)
    if not item or not item.path then
      return
    end
    discard_preview()
    local buf = gvim.buf.open(item.path, {
      preview = true,
      pos = gvim.pos(item.line or 0, item.col or 0),
      focus = true,
    })
    if buf then
      preview_buf = buf
    end
  end

  finder = M.new {
    title = opts.title or "",
    placeholder = opts.placeholder or "",
    footer = opts.footer or "",
    max_results = math.min(opts.max_results or M.MAX_RESULTS, M.MAX_RESULTS),
    load = function()
      return items
    end,
    format = function(item)
      local label = M.relative_path(item.path) .. ":" .. ((item.line or 0) + 1)
      return {
        id = label,
        label = label,
        description = M.cells(item.text or "", 120),
      }
    end,
    rank_text = function(item)
      return M.relative_path(item.path) .. " " .. (item.text or "")
    end,
    on_move = function(item)
      preview(item)
      if opts.on_move then
        opts.on_move(item)
      end
    end,
    on_select = function(item)
      committed = true
      discard_preview()
      if opts.on_jump then
        opts.on_jump(item)
      else
        gvim.buf.open(item.path, {
          pos = gvim.pos(item.line or 0, item.col or 0),
          focus = true,
        })
      end
    end,
    on_close = function(reason)
      if reason ~= "selected" then
        discard_preview()
      end
      if opts.on_close then
        opts.on_close(reason)
      end
    end,
  }
  return finder:open { title = opts.title, footer = opts.footer, placeholder = opts.placeholder }
end

-- read_snippets fills item.text from the source files for location rows that do not have
-- one yet: open buffers are read line by line, other files once (bounded). Returns items.
function M.read_snippets(items)
  local cache = {}
  local files = 0
  for _, item in ipairs(items) do
    if item.path and (item.text == nil or item.text == "") then
      local text = nil
      local buf = nil
      for _, open in ipairs(gvim.buf.list()) do
        if open:path() == item.path then
          buf = open
          break
        end
      end
      if buf then
        local ok, line = pcall(function() return buf:line(item.line or 0) end)
        if ok and line then
          text = line
        end
      else
        local lines = cache[item.path]
        if lines == nil and files < M.MAX_LOCATION_FILES then
          files = files + 1
          local raw = gvim.fs.read(item.path, { max = M.MAX_LOCATION_BYTES })
          lines = raw and gvim.text.split(raw, "\n", true) or {}
          cache[item.path] = lines
        end
        if lines then
          text = lines[(item.line or 0) + 1]
        end
      end
      item.text = gvim.text.trim(tostring(text or ""))
    end
  end
  return items
end

-- lsp_locations normalizes an LSP 'location response' (Location, Location[], LocationLink[] or
-- null) into {path, line (0-based), col (0-based byte), text=""} rows. File URIs are decoded
-- with gvim.path.from_uri and skipped when they do not name a file; positions are UTF-16 in the
-- protocol, so the byte column is converted only when the target line is available (an open
-- buffer); otherwise the row jumps to the line start (0). The result is capped to MAX_ITEMS.
function M.lsp_locations(result)
  local out = {}
  if type(result) ~= "table" then
    return out
  end
  local rows = result
  if result.uri ~= nil or result.targetUri ~= nil then
    rows = { result }
  end
  for _, raw in ipairs(rows) do
    if #out >= M.MAX_ITEMS then
      break
    end
    local uri = raw.uri or raw.targetUri
    local range = raw.range or raw.targetSelectionRange or raw.targetRange
    if type(uri) == "string" and type(range) == "table" then
      local path = gvim.path.from_uri(uri)
      local start = range.start or {}
      if path and type(start.line) == "number" then
        local line = start.line
        local character = type(start.character) == "number" and start.character or 0
        local col = 0
        local buf = gvim.buf.find(path)
        if buf then
          local text = buf:line(line)
          if text then
            col = M.utf16_to_byte(text, character)
          end
        end
        out[#out + 1] = { path = path, line = line, col = col, text = "" }
      end
    end
  end
  return out
end

return M