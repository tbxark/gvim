-- Find References: a port of Fresh's plugins/find_references.ts (145 lines) on the shared
-- gvim.finder library.
--
-- Fresh's plugin is host-event driven: the core Find References action sends
-- lsp_references / lsp_implementation results and the plugin answers with a Finder prompt
-- list. gvim has no lsp_references/lsp_implementation plugin events (its native Find
-- References action opens its own UI), so this port is command driven with Fresh's own i18n
-- keys: "Show References Panel" runs textDocument/references for the symbol under the cursor
-- and "Hide References Panel" closes the list (registered hidden, so the palette shows the
-- one row the core action exposed in Fresh).
--
-- Ported from Fresh: the reference rows are "<relative path>:<line>" with the source line as
-- the description, filtered in the Finder and previewed as the selection moves (gvim preview
-- tabs replace each other and become permanent on Enter); the status line reports the count
-- and the "limited to N" note; empty results say so. The position sent to the server is the
-- cursor converted from bytes to the protocol's UTF-16 column; the includeDeclaration
-- context is true as in Fresh. Results are capped at 100 rows.
--
-- Reductions: no lsp_references/lsp_implementation events (commands instead), no
-- implementation listener (the definitions/implementations lists live in lsp_navigation), no
-- source-split "opened in the split the browse came from" (preview tabs do), the snippet of a
-- file that is not open comes from a bounded read (cf. Fresh's whole-file read), and there is
-- no default key binding (Fresh's core action has its own chord; the plugin registers none).
local gvim = require "gvim"
local finder = require "gvim.finder"

local t = gvim.i18n.t

local MAX_ROWS = 100
local WORD_MAX = 256

-- current is the open references finder (nil while closed).
local current = nil

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

-- word_at returns the identifier around byte_col (bounded), for the title/status symbol.
local function word_at(line, byte_col)
  if not line or line == "" then
    return ""
  end
  line = finder.clip(line, WORD_MAX)
  byte_col = math.max(0, math.min(byte_col or 0, #line))
  local from = byte_col + 1
  local start = from
  while start > 1 do
    local c = line:sub(start - 1, start - 1)
    if c:match("[%w_]") then
      start = start - 1
    else
      break
    end
  end
  local stop = from
  while stop <= #line do
    local c = line:sub(stop, stop)
    if c:match("[%w_]") then
      stop = stop + 1
    else
      break
    end
  end
  return line:sub(start, stop - 1)
end

local function jump_to(item)
  local ok, err = gvim.buf.open(item.path, {
    pos = gvim.pos(item.line or 0, item.col or 0),
    focus = true,
  })
  if not ok then
    gvim.log.warn("find_references: open %s: %s", item.path, tostring(err))
    return
  end
  gvim.notify(t("status.jumped_to", {
    file = finder.relative_path(item.path), line = (item.line or 0) + 1,
  }))
end

local function close_references(quiet)
  if current then
    current:close()
  end
  current = nil
  if not quiet then
    gvim.notify(t("status.closed"))
  end
end

local function show_references()
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
  close_references(true)
  local pos = buf:cursor()
  local line_text = buf:line(pos.line) or ""
  local symbol = word_at(line_text, pos.col)
  if symbol == "" then
    gvim.notify(t("status.no_word"))
    return
  end
  local result, err = gvim.lsp.request("textDocument/references", {
    textDocument = { uri = gvim.path.to_uri(path) },
    position = { line = pos.line, character = finder.byte_to_utf16(line_text, pos.col) },
    context = { includeDeclaration = true },
  }, { buf = buf })
  if result == nil and err then
    gvim.notify(t("status.request_failed", { error = tostring(err) }), "warn")
    return
  end
  local items = finder.lsp_locations(result)
  if #items == 0 then
    gvim.notify(t("status.no_references", { symbol = symbol }))
    return
  end
  finder.read_snippets(items)
  local count = #items
  local limited = ""
  local shown = count
  if count > MAX_ROWS then
    shown = MAX_ROWS
    limited = t("status.showing_first", { max = MAX_ROWS })
  end
  gvim.notify(t("status.found_references", { count = count, limit = limited }))
  current = finder.open_locations {
    title = t("panel.header", {
      symbol = symbol, count = count,
      limit = count > MAX_ROWS and t("panel.limited", { max = MAX_ROWS }) or "",
    }),
    footer = t("panel.help"),
    items = items,
    max_results = shown,
    on_jump = jump_to,
    on_close = function(reason)
      current = nil
      if reason == "cancelled" then
        gvim.notify(t("status.closed"))
      end
    end,
  }
end

gvim.command.register {
  name = "show_references",
  title = "%cmd.show_references",
  description = "%cmd.show_references_desc",
  run = show_references,
}

gvim.command.register {
  name = "hide_references",
  title = "%cmd.hide_references",
  description = "%cmd.hide_references_desc",
  hidden = true,
  run = function()
    close_references(false)
  end,
}

gvim.log.debug("find references plugin loaded (shared gvim.finder)")
