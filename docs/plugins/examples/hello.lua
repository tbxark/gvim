-- hello.lua: the smallest useful plugin.
--
-- Copy it to ~/.config/gvim/plugins/hello.lua (or load it once with
-- `gvim --plugin hello.lua`). "Hello: Say Hello" then shows up in the command palette.
local gvim = require "gvim"

gvim.command.register {
  name = "hello", -- the action id is "hello.hello" (<plugin>.<name>)
  title = "Hello: Say Hello",
  description = "Show a greeting that names the current buffer",
  run = function(ctx)
    local buf = gvim.buf.current()
    local name = buf and buf:name() or "nobody"
    gvim.notify("Hello from " .. name)
  end,
}
