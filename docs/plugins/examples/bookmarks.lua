-- bookmarks.lua: named bookmarks per file, kept between sessions and shown in the gutter.
--
-- Uses: gvim.state (a small JSON store per plugin), gvim.ui.input and gvim.ui.select
-- (prompts that read like blocking calls inside a command), gvim.decor (gutter marks) and
-- two events. Bookmarks are stored as line numbers, so they do not move when you edit the
-- text above them; a real plugin would store the text of the line as well.
local gvim = require "gvim"

local ns = gvim.decor.namespace "bookmarks"

-- marks() returns { [path] = { {line = 0-based line, name = text}, ... } }
local function marks()
  return gvim.state.get("marks", {})
end

-- paint draws the gutter marks of one buffer; setting them again replaces the old ones.
local function paint(buf)
  local path = buf and buf:path()
  if not path then
    return
  end
  local items = {}
  for _, m in ipairs(marks()[path] or {}) do
    items[#items + 1] = { kind = "gutter", line = m.line, text = "*", style = { fg = "accent", bold = true } }
  end
  ns:set(buf, items)
end

gvim.command.register {
  name = "toggle",
  title = "Bookmarks: Toggle on This Line",
  run = function()
    local buf = gvim.buf.current()
    local path = buf and buf:path()
    if not path then
      gvim.notify("Bookmarks: this buffer has no file", "warn")
      return
    end
    local line = buf:cursor().line
    local all = marks()
    local list = all[path] or {}
    for i, m in ipairs(list) do
      if m.line == line then
        table.remove(list, i)
        all[path] = list
        gvim.state.set("marks", all)
        paint(buf)
        gvim.notify("Bookmark removed")
        return
      end
    end
    -- ui.input waits for the answer like a blocking call; nil means the user pressed Esc
    local name = gvim.ui.input { prompt = "Bookmark name:", default = (buf:line(line) or ""):match "^%s*(.-)%s*$" }
    if name == nil then
      return
    end
    list[#list + 1] = { line = line, name = name }
    all[path] = list
    gvim.state.set("marks", all)
    paint(buf)
    gvim.notify("Bookmark added: " .. name)
  end,
}

gvim.command.register {
  name = "go",
  title = "Bookmarks: Go To...",
  run = function()
    local buf = gvim.buf.current()
    local list = buf and buf:path() and marks()[buf:path()] or {}
    if #list == 0 then
      gvim.notify("No bookmarks in this file")
      return
    end
    local m = gvim.ui.select(list, {
      title = "Bookmarks",
      format = function(item)
        return item.name .. "  (line " .. (item.line + 1) .. ")"
      end,
    })
    if m then
      buf:set_cursor({ line = m.line, col = 0 }, { center = true })
    end
  end,
}

-- The first event of a file and every switch of tab redraw its marks.
gvim.on("after_file_open", function(ev)
  paint(ev.buf)
end)
gvim.on("buffer_activated", function(ev)
  paint(ev.buf)
end)
-- Files that were already open when the plugin loaded (it may also load later, from init.lua).
for _, buf in ipairs(gvim.buf.list()) do
  paint(buf)
end
