-- The highlight of the matches of the last search (vi's 'hlsearch'). sync() brings it in line
-- with the state: a new search, :noh, :set hls or nohls, the buffer that is shown.
local gvim = require "gvim"
local S = require "vi.state"

local M = {}

local MAX = 1000 -- the most matches that are marked

local ns
local shown -- the search the marks show: {search = S.last_search, pattern = , buf = }
local sub -- the buffer_changed subscription, while marks are shown

local function clear()
  if ns and shown then
    pcall(function() ns:clear() end)
  end
  shown = nil
  if sub then
    sub:off()
    sub = nil
  end
end

-- show marks the matches in the active buffer.
local function show(buf, ls)
  local Search = require "vi.search"
  local re2, cs = Search.compile(ls.pattern, ls.nosmart)
  local ranges = buf:find(re2, { regex = true, case_sensitive = cs, max = MAX })
  if not ranges then
    clear() -- a pattern that does not compile, or a file the search does not read
    return
  end
  ns = ns or gvim.decor.namespace "search"
  local items = {}
  for i, r in ipairs(ranges) do
    items[i] = { kind = "highlight", range = r, style = { bg = "search.match_bg", fg = "search.match_fg" } }
  end
  pcall(function() ns:clear() end)
  ns:set(buf, items)
  shown = { search = ls, pattern = ls.pattern, buf = buf.id }
  if not sub then
    -- an edit moves the marks with the text, but the text it adds is not marked
    sub = gvim.on("buffer_changed", function() M.refresh() end, { debounce = 250 })
  end
end

-- refresh marks the matches again.
function M.refresh()
  shown = nil
  M.sync()
end

-- sync makes the marks match S.last_search, the setting and the active buffer.
function M.sync()
  local ls = S.last_search
  local buf = gvim.buf.current()
  if not S.enabled or S.suspended or not buf or not ls or S.hl_off or S.settings.hlsearch == false then
    if shown then
      clear()
    end
    return
  end
  if shown and shown.search == ls and shown.pattern == ls.pattern and shown.buf == buf.id then
    return
  end
  local ok, err = pcall(show, buf, ls)
  if not ok then
    clear()
    gvim.log.warn("vi_mode: search highlight: %s", tostring(err))
  end
end

M.clear = clear

return M
