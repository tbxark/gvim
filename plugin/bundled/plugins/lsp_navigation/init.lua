-- LSP navigation: a port of Fresh's plugins/lsp_navigation.ts ("Go to LSP Symbol", 424 lines)
-- on the shared gvim.finder library.
--
-- Ported from Fresh:
--   * Go to LSP Symbol: textDocument/documentSymbol, one row per symbol ("[kind] name" plus a
--     trimmed source-line snippet), fuzzy filtered over the symbol name and re-sorted by line;
--     moving the selection moves the buffer cursor over the symbol and highlights it with the
--     search-match style, Enter commits the jump, Esc restores the cursor and clears the
--     highlight. The symbol name position is refined against the line (LSP reports the start
--     of the declaration, not always the name), as Fresh does. A no-language/empty/error
--     document produces the Fresh status line instead of an empty list.
--   * The kind labels and the DocumentSymbol/SymbolInformation parsing are Fresh's; children
--     of a hierarchical DocumentSymbol are not listed (Fresh reads the top level only).
--
-- gvim additions (Fresh's plugin has only Go to LSP Symbol; the definition/implementation/
-- type-definition jumping is a core action there, and gvim keeps its native Go to Definition
-- rows): list commands that use the same Finder location list as find_references - LSP: Show
-- Definitions / Show Implementations / Show Type Definitions for the symbol under the cursor,
-- LSP: Workspace Symbols, LSP: Incoming Calls / Outgoing Calls (call sites), LSP: Document
-- Outline (the same list as Go to LSP Symbol), and LSP: Show Breadcrumbs / Breadcrumbs
-- Outline (the symbols enclosing the caret). One result jumps directly, several open the
-- list with preview tabs.
--
-- Fresh preselects the smallest symbol enclosing the cursor and previews it. gvim's picker has
-- no initial-selected-index option, so the port approximates this user-visible behavior by
-- ordering the preselected symbol first for the empty query and previewing it immediately;
-- typed queries return to the normal line order. The preview highlight is a decoration
-- (gvim has no overlay namespace API for plugins) and is cleared on cancel/select. No default
-- key binding: Fresh registers only the command, with no chord.
local gvim = require "gvim"
local finder = require "gvim.finder"

local t = gvim.i18n.t

local NS = gvim.decor.namespace("lsp_symbol_nav")
local MATCH_STYLE = { fg = "search.match_fg", bg = "search.match_bg", bold = true }
local MAX_SYMBOLS = 2000

-- Fresh's getKindLabel: the LSP symbol kind number as a short word.
local KIND_LABELS = {
  [1] = "file", [2] = "mod", [3] = "ns", [4] = "pkg", [5] = "class",
  [6] = "method", [7] = "prop", [8] = "field", [9] = "construct", [10] = "enum",
  [11] = "iface", [12] = "fn", [13] = "var", [14] = "const", [22] = "enum-mem",
  [23] = "struct", [24] = "event", [25] = "op", [26] = "type-param",
}

local function kind_label(kind)
  return KIND_LABELS[kind] or "item"
end

-- session is the state of the open symbol finder (nil while closed).
local session = nil

local function buffer_state()
  local buf = gvim.buf.current()
  if not buf or buf:is_virtual() then
    return nil
  end
  local path = buf:path()
  if not path or path == "" then
    return nil
  end
  return buf, path
end

-- ready_buffer returns the current file buffer and its path when it can ask a language
-- server; otherwise it tells the user why and returns nil.
local function ready_buffer()
  local buf, path = buffer_state()
  if not buf then
    gvim.notify(t("status.no_file"))
    return nil
  end
  if not buf:language() then
    gvim.notify(t("status.no_language"))
    return nil
  end
  return buf, path
end

local function jump_to(item)
  local ok, err = gvim.buf.open(item.path, {
    pos = gvim.pos(item.line or 0, item.col or 0),
    focus = true,
  })
  if not ok then
    gvim.log.warn("lsp_navigation: open %s: %s", item.path, tostring(err))
    return
  end
  gvim.notify(t("status.jumped_to", {
    file = finder.relative_path(item.path), line = (item.line or 0) + 1,
  }))
end

-- ---------------------------------------------------------------------------
-- Document symbols
-- ---------------------------------------------------------------------------
-- symbol_from_utf16 fills a parsed symbol's byte column and its source-line snippet.
local function attach_symbol_line(buf, symbol)
  local line = buf:line(symbol.name_line)
  if not line then
    return
  end
  local col = finder.utf16_to_byte(line, symbol.name_char_utf16 or 0)
  -- LSP reports the start of the declaration for SymbolInformation: find the name itself,
  -- searching from the reported column, exactly as Fresh does.
  local found = line:find(symbol.name, col + 1, true)
  if not found then
    found = line:find(symbol.name, 1, true)
  end
  if found then
    col = found - 1
  end
  symbol.name_col = col
  symbol.line_text = finder.cells(gvim.text.trim(line), 120)
end

-- parse_symbols reads DocumentSymbol[] or SymbolInformation[] (the two shapes the protocol
-- allows for documentSymbol), bounded to MAX_SYMBOLS rows and the top level only.
local function parse_symbols(result, buf)
  local symbols = {}
  if type(result) ~= "table" then
    return symbols
  end
  for _, raw in ipairs(result) do
    if #symbols >= MAX_SYMBOLS then
      break
    end
    if type(raw) == "table" then
      local name = tostring(raw.name or "")
      if name ~= "" then
        local kind = tonumber(raw.kind) or 0
        local start_line, end_line, name_line, name_char = 0, 0, 0, 0
        local location = raw.location
        if type(location) == "table" and type(location.range) == "table" then
          local start = location.range.start or {}
          local stop = location.range["end"] or {}
          start_line = tonumber(start.line) or 0
          end_line = tonumber(stop.line) or start_line
          name_line = start_line
          name_char = tonumber(start.character) or 0
        else
          local range = raw.range
          if type(range) == "table" then
            local start = range.start or {}
            local stop = range["end"] or {}
            start_line = tonumber(start.line) or 0
            end_line = tonumber(stop.line) or start_line
            name_line = start_line
            name_char = tonumber(start.character) or 0
          end
          local selection = raw.selectionRange
          if type(selection) == "table" and type(selection.start) == "table" then
            name_line = tonumber(selection.start.line) or name_line
            name_char = tonumber(selection.start.character) or name_char
          end
        end
        symbols[#symbols + 1] = {
          name = finder.clip(name, 120),
          kind = kind,
          start_line = start_line,
          end_line = end_line,
          name_line = name_line,
          name_char_utf16 = name_char,
          name_col = 0,
          line_text = "",
        }
      end
    end
  end
  for _, symbol in ipairs(symbols) do
    attach_symbol_line(buf, symbol)
  end
  return symbols
end

-- find_enclosing_symbol matches Fresh's findMatchingSymbolIndex: the smallest symbol range
-- containing cursor_line, then the earliest start line, then the earliest name character.
local function find_enclosing_symbol(symbols, cursor_line)
  local best, best_span, best_start, best_char = nil, math.huge, math.huge, math.huge
  for _, symbol in ipairs(symbols) do
    if symbol.start_line <= cursor_line and cursor_line <= symbol.end_line then
      local span = symbol.end_line - symbol.start_line
      if span < best_span or
        (span == best_span and symbol.start_line < best_start) or
        (span == best_span and symbol.start_line == best_start and
          symbol.name_col < best_char) then
        best, best_span, best_start, best_char = symbol, span, symbol.start_line, symbol.name_col
      end
    end
  end
  return best
end

local function symbols_filter(items, query)
  local ranked = finder.filter(items, query, function(symbol) return symbol.name end, finder.MAX_RESULTS)
  table.sort(ranked, function(a, b)
    if a.start_line ~= b.start_line then
      return a.start_line < b.start_line
    end
    return a.name < b.name
  end)
  -- gvim's picker cannot be given an initial selected index. For Fresh's enclosing-symbol
  -- preselection, put that symbol first while the query is empty (the host highlights the
  -- first row); a typed query keeps Fresh's line order.
  if query == "" and session and session.preselected then
    for i, item in ipairs(ranked) do
      if item == session.preselected then
        table.remove(ranked, i)
        table.insert(ranked, 1, item)
        break
      end
    end
  end
  return ranked
end

local function clear_highlight()
  if session and session.buf and session.buf:valid() then
    NS:clear(session.buf)
  end
end

local function show_symbol_at(symbol)
  local buf = session and session.buf
  if not buf or not buf:valid() or not symbol then
    return
  end
  buf:set_cursor(gvim.pos(symbol.name_line, symbol.name_col), { reveal = true })
  NS:set(buf, {
    {
      kind = "highlight",
      range = gvim.range(symbol.name_line, symbol.name_col, symbol.name_line, symbol.name_col + #symbol.name),
      style = MATCH_STYLE,
    },
  })
end

local function restore_cursor()
  if not session or not session.buf or not session.buf:valid() then
    return
  end
  session.buf:set_cursor(session.origin, { reveal = true })
end

local function open_symbol_finder(buf, symbols)
  local finder_obj
  session = {
    buf = buf,
    origin = buf:cursor(),
    confirmed = false,
    preselected = find_enclosing_symbol(symbols, buf:cursor().line),
  }
  finder_obj = finder.new {
    title = t("panel.symbols_title"),
    placeholder = t("panel.placeholder"),
    footer = t("panel.help"),
    load = function()
      return symbols
    end,
    format = function(symbol)
      local snippet = symbol.line_text ~= "" and symbol.line_text or ("line " .. (symbol.name_line + 1))
      return {
        id = symbol.name_line .. ":" .. symbol.name_col .. ":" .. symbol.kind .. ":" .. symbol.name,
        label = "[" .. kind_label(symbol.kind) .. "] " .. symbol.name,
        description = finder.cells(snippet, 120),
      }
    end,
    rank_text = function(symbol)
      return symbol.name
    end,
    filter = symbols_filter,
    on_move = function(symbol)
      show_symbol_at(symbol)
    end,
    on_select = function(symbol)
      session.confirmed = true
      clear_highlight()
      show_symbol_at(symbol)
    end,
    on_close = function(reason)
      clear_highlight()
      if reason ~= "selected" then
        restore_cursor()
      end
      session = nil
    end,
  }
  local pick, err = finder_obj:open()
  if pick and session.preselected then
    -- Fresh previews its initial selected symbol immediately; the first-row ordering above
    -- makes that same symbol the picker's selected row.
    show_symbol_at(session.preselected)
  end
  return pick, err
end

-- document_symbols asks the server for the symbols of the current buffer. It tells the user
-- and returns nil when the buffer cannot be asked, the request failed or there are none.
local function document_symbols()
  local buf, path = ready_buffer()
  if not buf then
    return nil
  end
  local result, err = gvim.lsp.request("textDocument/documentSymbol", {
    textDocument = { uri = gvim.path.to_uri(path) },
  }, { buf = buf })
  if result == nil and err then
    gvim.notify(t("status.symbols_failed", { error = tostring(err) }), "warn")
    return nil
  end
  local symbols = parse_symbols(result, buf)
  if #symbols == 0 then
    gvim.notify(t("status.no_symbols"))
    return nil
  end
  return buf, path, symbols
end

local function goto_lsp_symbol()
  local buf, _, symbols = document_symbols()
  if buf then
    open_symbol_finder(buf, symbols)
  end
end

-- ---------------------------------------------------------------------------
-- Location lists (definitions / implementations / type definitions)
-- ---------------------------------------------------------------------------
local LOCATION_KINDS = {
  definitions = { method = "textDocument/definition", label = "kind.definition" },
  implementations = { method = "textDocument/implementation", label = "kind.implementation" },
  type_definitions = { method = "textDocument/typeDefinition", label = "kind.type_definition" },
}

local function open_location_finder(items, label)
  finder.read_snippets(items)
  finder.open_locations {
    title = t("panel.location_title", { kind = label, count = #items }),
    footer = t("panel.help"),
    items = items,
    on_jump = jump_to,
  }
end

local function show_locations(kind)
  local spec = LOCATION_KINDS[kind]
  local buf, path = ready_buffer()
  if not buf then
    return
  end
  local pos = buf:cursor()
  local line_text = buf:line(pos.line) or ""
  local result, err = gvim.lsp.request(spec.method, {
    textDocument = { uri = gvim.path.to_uri(path) },
    position = { line = pos.line, character = finder.byte_to_utf16(line_text, pos.col) },
  }, { buf = buf })
  local label = t(spec.label)
  if result == nil and err then
    gvim.notify(t("status.request_failed", { kind = label, error = tostring(err) }), "warn")
    return
  end
  local items = finder.lsp_locations(result)
  if #items == 0 then
    gvim.notify(t("status.no_locations", { kind = label }))
    return
  end
  if #items == 1 then
    jump_to(items[1])
    return
  end
  open_location_finder(items, label)
end

gvim.command.register {
  name = "goto_lsp_symbol",
  title = "%cmd.goto_lsp_symbol",
  description = "%cmd.goto_lsp_symbol_desc",
  run = goto_lsp_symbol,
}

gvim.command.register {
  name = "show_definitions",
  title = "%cmd.show_definitions",
  description = "%cmd.show_definitions_desc",
  run = function()
    show_locations("definitions")
  end,
}

gvim.command.register {
  name = "show_implementations",
  title = "%cmd.show_implementations",
  description = "%cmd.show_implementations_desc",
  run = function()
    show_locations("implementations")
  end,
}

gvim.command.register {
  name = "show_type_definitions",
  title = "%cmd.show_type_definitions",
  description = "%cmd.show_type_definitions_desc",
  run = function()
    show_locations("type_definitions")
  end,
}
-- ---------------------------------------------------------------------------
-- Workspace symbols
-- ---------------------------------------------------------------------------
local function parse_workspace_symbols(result)
  local out = {}
  if type(result) ~= "table" then
    return out
  end
  for _, raw in ipairs(result) do
    if #out >= MAX_SYMBOLS then
      break
    end
    if type(raw) == "table" then
      local name = tostring(raw.name or "")
      local loc = raw.location
      local uri = loc and (loc.uri or loc.targetUri)
      local range = loc and (loc.range or loc.targetSelectionRange or loc.targetRange)
      if name ~= "" and type(uri) == "string" and type(range) == "table" then
        local path = gvim.path.from_uri(uri)
        local start = range.start or {}
        if path and type(start.line) == "number" then
          local line = start.line
          local col = type(start.character) == "number" and start.character or 0
          local kind = tonumber(raw.kind) or 0
          local kind_name = kind_label(kind)
          local desc = string.format("[%s] %s", kind_name, name)
          if raw.containerName and raw.containerName ~= "" then
            desc = desc .. " (" .. tostring(raw.containerName) .. ")"
          end
          out[#out + 1] = {
            path = path,
            line = line,
            col = col,
            text = desc,
            name = name,
          }
        end
      end
    end
  end
  return out
end

local function show_workspace_symbols()
  local buf = ready_buffer()
  if not buf then
    return
  end
  local result, err = gvim.lsp.request("workspace/symbol", {
    query = "",
  }, { buf = buf })
  if result == nil and err then
    gvim.notify(t("status.symbols_failed", { error = tostring(err) }), "warn")
    return
  end
  local symbols = parse_workspace_symbols(result)
  if #symbols == 0 then
    gvim.notify(t("status.no_symbols"))
    return
  end
  finder.read_snippets(symbols)
  finder.open_locations {
    title = t("panel.workspace_symbols_title", { count = #symbols }),
    footer = t("panel.help"),
    items = symbols,
    on_jump = jump_to,
  }
end

-- ---------------------------------------------------------------------------
-- Call Hierarchy (Incoming / Outgoing calls)
-- ---------------------------------------------------------------------------
local function show_call_hierarchy(direction)
  local buf, path = ready_buffer()
  if not buf then
    return
  end
  local pos = buf:cursor()
  local line_text = buf:line(pos.line) or ""
  local char_utf16 = finder.byte_to_utf16(line_text, pos.col)
  local label = direction == "incoming" and t("kind.incoming_calls") or t("kind.outgoing_calls")

  local prep_result, err = gvim.lsp.request("textDocument/prepareCallHierarchy", {
    textDocument = { uri = gvim.path.to_uri(path) },
    position = { line = pos.line, character = char_utf16 },
  }, { buf = buf })

  if prep_result == nil and err then
    gvim.notify(t("status.request_failed", { kind = label, error = tostring(err) }), "warn")
    return
  end
  if type(prep_result) ~= "table" or #prep_result == 0 then
    gvim.notify(t("status.no_call_hierarchy"))
    return
  end

  local root_item = prep_result[1]
  local method = direction == "incoming" and "callHierarchy/incomingCalls" or "callHierarchy/outgoingCalls"
  local calls_result, call_err = gvim.lsp.request(method, {
    item = root_item,
  }, { buf = buf })

  if calls_result == nil and call_err then
    gvim.notify(t("status.request_failed", { kind = label, error = tostring(call_err) }), "warn")
    return
  end

  local items = {}
  if type(calls_result) == "table" then
    for _, c in ipairs(calls_result) do
      if #items >= MAX_SYMBOLS then break end
      local target = direction == "incoming" and c.from or c.to
      -- fromRanges are call sites inside the caller: the other end of an incoming call, but
      -- the file of the prepared item for an outgoing one (target is the callee there).
      local uri = direction == "incoming" and target and target.uri or root_item.uri
      local ranges = c.fromRanges
      if type(target) == "table" and type(uri) == "string" and type(ranges) == "table" and #ranges > 0 then
        local target_path = gvim.path.from_uri(uri)
        if target_path and target_path ~= "" then
          for _, rng in ipairs(ranges) do
            local start = rng.start or {}
            local line = tonumber(start.line) or 0
            local col = tonumber(start.character) or 0
            local kind_name = kind_label(tonumber(target.kind) or 0)
            local name = tostring(target.name or "")
            items[#items + 1] = {
              path = target_path,
              line = line,
              col = col,
              text = string.format("[%s] %s", kind_name, name),
            }
          end
        end
      end
    end
  end

  if #items == 0 then
    gvim.notify(t("status.no_locations", { kind = label }))
    return
  end
  if #items == 1 then
    jump_to(items[1])
    return
  end
  open_location_finder(items, label)
end

gvim.command.register {
  name = "show_workspace_symbols",
  title = "%cmd.show_workspace_symbols",
  description = "%cmd.show_workspace_symbols_desc",
  run = show_workspace_symbols,
}

gvim.command.register {
  name = "show_incoming_calls",
  title = "%cmd.show_incoming_calls",
  description = "%cmd.show_incoming_calls_desc",
  run = function()
    show_call_hierarchy("incoming")
  end,
}

gvim.command.register {
  name = "show_outgoing_calls",
  title = "%cmd.show_outgoing_calls",
  description = "%cmd.show_outgoing_calls_desc",
  run = function()
    show_call_hierarchy("outgoing")
  end,
}

local function collect_enclosing_symbols(symbols, cursor_line)
  local matches = {}
  for _, symbol in ipairs(symbols) do
    if symbol.start_line <= cursor_line and cursor_line <= symbol.end_line then
      matches[#matches + 1] = symbol
    end
  end
  table.sort(matches, function(a, b)
    local span_a = a.end_line - a.start_line
    local span_b = b.end_line - b.start_line
    if span_a ~= span_b then
      return span_a > span_b
    end
    return a.start_line < b.start_line
  end)
  return matches
end

-- enclosing_symbols returns the current buffer, its path and the symbols that contain the
-- caret, outermost first. It tells the user and returns nil when there are none.
local function enclosing_symbols()
  local buf, path, symbols = document_symbols()
  if not buf then
    return nil
  end
  local enclosing = collect_enclosing_symbols(symbols, buf:cursor().line)
  if #enclosing == 0 then
    gvim.notify(t("status.no_enclosing_symbol"))
    return nil
  end
  return buf, path, enclosing
end

local function show_breadcrumbs()
  local buf, _, enclosing = enclosing_symbols()
  if not buf then
    return
  end
  local parts = {}
  for _, s in ipairs(enclosing) do
    parts[#parts + 1] = s.name
  end
  gvim.notify(table.concat(parts, " > "))
end

gvim.command.register {
  name = "document_outline",
  title = "%cmd.document_outline",
  description = "%cmd.document_outline_desc",
  run = goto_lsp_symbol,
}

gvim.command.register {
  name = "show_breadcrumbs",
  title = "%cmd.show_breadcrumbs",
  description = "%cmd.show_breadcrumbs_desc",
  run = show_breadcrumbs,
}

local function open_breadcrumbs_picker()
  local buf, path, enclosing = enclosing_symbols()
  if not buf then
    return
  end
  local items = {}
  for i, s in ipairs(enclosing) do
    items[#items + 1] = {
      path = path,
      line = s.start_line,
      col = s.name_col or 0,
      text = string.format("%d. [%s] %s", i, kind_label(s.kind), s.name),
    }
  end
  finder.open_locations {
    title = t("panel.breadcrumbs_title", { count = #items }),
    footer = t("panel.help"),
    items = items,
    on_jump = jump_to,
  }
end

gvim.command.register {
  name = "breadcrumbs_picker",
  title = "%cmd.breadcrumbs_picker",
  description = "%cmd.breadcrumbs_picker_desc",
  run = open_breadcrumbs_picker,
}


gvim.log.debug("lsp navigation plugin loaded (shared gvim.finder)")
