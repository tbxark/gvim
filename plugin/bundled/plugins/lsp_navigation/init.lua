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
-- rows): three list commands for the symbol under the cursor that use the same Finder
-- location list as find_references - LSP: Show Definitions / Show Implementations / Show Type
-- Definitions. One result jumps directly, several open the list with preview tabs.
--
-- Reductions vs Fresh: the picker has no initial selected index, so the enclosing symbol is
-- not preselected (Fresh preselects it and previews it); the preview highlight is a decoration
-- (gvim has no overlay namespace API for plugins) and is cleared on cancel/select. No default
-- key binding: Fresh registers only the command, with no chord.
local gvim = require "gvim"
local finder = require "gvim.finder"

local t = gvim.i18n.t

local NS = gvim.decor.namespace("lsp_symbol_nav")
local MATCH_STYLE = { fg = "search.match_fg", bg = "search.match_bg", bold = true }
local MAX_SYMBOLS = 2000
local MAX_SCORE_ITEMS = 2000

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
  symbols = symbols
  return symbols
end

local function symbols_filter(items, query)
  local ranked = finder.filter(items, query, function(symbol) return symbol.name end, finder.MAX_RESULTS)
  table.sort(ranked, function(a, b)
    if a.start_line ~= b.start_line then
      return a.start_line < b.start_line
    end
    return a.name < b.name
  end)
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
  return finder_obj:open()
end

local function goto_lsp_symbol()
  local buf, path = buffer_state()
  if not buf then
    gvim.notify(t("status.no_file"))
    return
  end
  local language = buf:language()
  if not language then
    gvim.notify(t("status.no_language"))
    return
  end
  local result, err = gvim.lsp.request("textDocument/documentSymbol", {
    textDocument = { uri = gvim.path.to_uri(path) },
  }, { buf = buf })
  if result == nil and err then
    gvim.notify(t("status.symbols_failed", { error = tostring(err) }), "warn")
    return
  end
  local symbols = parse_symbols(result, buf)
  if #symbols == 0 then
    gvim.notify(t("status.no_symbols"))
    return
  end
  open_symbol_finder(buf, symbols)
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
  local buf, path = buffer_state()
  if not buf then
    gvim.notify(t("status.no_file"))
    return
  end
  if not buf:language() then
    gvim.notify(t("status.no_language"))
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

gvim.log.debug("lsp navigation plugin loaded (shared gvim.finder)")
