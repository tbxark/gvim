-- Go to Line with Selection. A port of Fresh's goto_with_selection plugin: it starts a
-- selection at the caret and then opens the Go to Line prompt, so the selection runs from
-- the old position to the line you type.
local gvim = require "gvim"

gvim.command.register {
  name = "goto_line_with_selection",
  title = "Go to Line with Selection",
  description = "Select from current position to target line",
  run = function()
    local ok, err = gvim.command.run_all { "set_mark", "goto_line" }
    if not ok then
      gvim.notify("Go to Line with Selection: " .. tostring(err), "warn")
    end
  end,
}
