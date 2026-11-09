-- dashboard_section.lua: contribute a section to the dashboard.
--
-- The bundled dashboard plugin discovers this registration and calls the refresh
-- callback as a task with a per-section timeout. Return bounded row data; the dashboard
-- clips/validates it, keeps the last good rows on failure and never blocks the UI.
local gvim = require "gvim"

local calls = 0
gvim.dashboard.register_section("demo", function(ctx)
  calls = calls + 1
  return {
    { kind = "kv", label = "demo", value = "hello from a plugin" },
    { kind = "text", text = string.format("width %d", ctx.width or 0) },
  }
end, { title = "Demo section", ttl_ms = 5000, timeout_ms = 1000, priority = 10 })

gvim.command.register {
  name = "report",
  title = "Dashboard Example: Report",
  run = function()
    local list = gvim.dashboard.sections()
    gvim.notify(string.format("demo %d/%d", calls, #list))
  end,
}
