-- trim_on_save.lua: remove trailing whitespace before a file is saved.
--
-- before_file_save handlers run before the save is planned, may edit the buffer, and must
-- not yield. Returning false (or false, "reason") would cancel the save; this handler
-- never does.
local gvim = require "gvim"

gvim.on("before_file_save", function(ev)
  local buf = ev.buf
  if not buf or buf:is_large() then
    return
  end
  -- find_lines scans on the host side and returns line numbers; it is time-boxed, so ask
  -- again from `next` until it says the whole buffer was scanned.
  local edits = {}
  local from = 0
  while from do
    local lines, next_line = buf:find_lines("[ \t]+$", { regex = true, from = from })
    for _, n in ipairs(lines or {}) do
      local text = buf:line(n)
      local trimmed = text:gsub("[ \t]+$", "")
      edits[#edits + 1] = {
        range = { from = { line = n, col = #trimmed }, to = { line = n, col = #text } },
        text = "",
      }
    end
    from = next_line
  end
  if #edits == 0 then
    return
  end
  -- A save the user did not ask for (autosave) must not move the cursor they are typing at.
  buf:edit(edits, { cursor = ev.source == "autosave" and "keep" or "map" })
end)
