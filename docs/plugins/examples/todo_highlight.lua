-- todo_highlight.lua: highlight TODO, FIXME and NOTE in the buffers you edit.
--
-- Uses: b:find (a host-side search, bounded by `max`), gvim.decor highlights, the
-- buffer_changed event with a debounce, and gvim.settings for the words and the colours.
local gvim = require "gvim"

local settings = gvim.settings.define {
  words = {
    type = "string_list",
    default = { "TODO", "FIXME", "NOTE" },
    description = "Words to highlight (case sensitive, whole words)",
  },
  max_per_word = {
    type = "integer",
    default = 500,
    minimum = 1,
    maximum = 5000,
    description = "Most highlights drawn for one word in one buffer",
  },
}

local ns = gvim.decor.namespace "todo"
local STYLES = {
  TODO = { fg = "warning", bold = true },
  FIXME = { fg = "error", bold = true },
}
local DEFAULT_STYLE = { fg = "info", bold = true }

local function scan(buf)
  if not buf or buf:is_large() or buf:is_virtual() then
    return
  end
  -- Positions are only right for the text they were computed from: tell the host which
  -- version that was, and it refuses (stale) if the buffer changed meanwhile.
  local version = buf:version()
  local items = {}
  for _, word in ipairs(settings.words) do
    local ranges = buf:find(word, { case_sensitive = true, whole_word = true, max = settings.max_per_word })
    for _, r in ipairs(ranges or {}) do
      items[#items + 1] = { kind = "highlight", range = r, style = STYLES[word] or DEFAULT_STYLE }
    end
  end
  ns:set(buf, items, { version = version })
end

gvim.on("buffer_activated", function(ev)
  scan(ev.buf)
end)
gvim.on("after_file_open", function(ev)
  scan(ev.buf)
end)
-- One handler call per pause in typing, not one per key.
gvim.on("buffer_changed", function(ev)
  scan(ev.buf)
end, { debounce = 250 })

for _, buf in ipairs(gvim.buf.list()) do
  scan(buf)
end
