-- Environment Manager (a reduced port of Fresh's env-manager).
--
-- Detection, the trust gate, capture and spawn application all live in the host
-- (config env.detectors + the env package + gvim.env). This plugin decides only
-- when to apply core's detected environment:
--   * path-only environments auto-activate silently once the workspace is trusted;
--   * shell environments activate with the explicit Env: Activate/Reload commands
--     (no automatic repo-shell execution on load);
--   * an untrusted workspace requests the core trust dialog (the single trust
--     prompt) and re-runs activation on the trust_changed event.
--
-- Reduced from Fresh: no widget-panel UI yet, no file watcher (use Env: Reload),
-- no devcontainer deferral/authority handling, no automatic shell-env activation,
-- one local environment, no persisted recipe across sessions.

local gvim = require "gvim"

local function t(key, vars) return gvim.i18n.t(key, vars) end

local state = { item = nil, sets = 0, last = "" }

local function trust_level()
  local ok, level = pcall(function() return gvim.env.trust_level() end)
  if not ok or type(level) ~= "string" or level == "" then
    return "restricted"
  end
  return level
end

local function trusted() return trust_level() == "trusted" end

local function detected()
  local d = gvim.env.detected()
  if d and type(d.name) == "string" and d.name ~= "" then
    return d
  end
  return nil
end

local function active()
  local a = gvim.env.active()
  if a and type(a.name) == "string" and a.name ~= "" then
    return a
  end
  return nil
end

local function refresh_status()
  if not state.item then
    return
  end
  local d, a = detected(), active()
  local text
  if a then
    text = t("statusbar.active", { name = tostring(a.name) })
  elseif d then
    if trusted() then
      text = tostring(d.name)
    else
      text = t("statusbar.locked", { name = tostring(d.name) })
    end
  else
    text = t("statusbar.system")
  end
  state.item:set(text)
end

local function notify_status()
  local d, a = detected(), active()
  if a then
    gvim.notify(t("status.env_active_named", { name = tostring(a.name) }))
    return
  end
  if d then
    gvim.notify(t("status.env_detected", { name = tostring(d.name), trust = trust_level() }))
    return
  end
  gvim.notify(t("status.no_env", { trust = trust_level() }))
end

-- Apply the detected environment. Returns true when set, false when refused by
-- the trust gate (the host owns the gate).
local function apply(d, reload)
  local ok, err = gvim.env.set(d.snippet, { dir = "", kind = d.kind })
  if not ok then
    if err and string.find(tostring(err), "trust", 1, true) then
      gvim.notify(t("status.untrusted"))
    else
      gvim.notify(tostring(err))
    end
    return false
  end
  state.sets = state.sets + 1
  if reload then
    gvim.notify(t("status.reloading", { name = tostring(d.name) }))
  else
    gvim.notify(t("status.activating", { name = tostring(d.name) }))
  end
  refresh_status()
  return true
end

-- Ask the core trust dialog to open; the trust_changed event re-runs activation.
local function request_trust_prompt()
  gvim.notify(t("status.untrusted"))
  gvim.command.run("workspace_trust_prompt")
end

local function activate()
  local d = detected()
  if not d then
    gvim.notify(t("status.no_env_detected"))
    return
  end
  if not trusted() then
    request_trust_prompt()
    return
  end
  apply(d, active() ~= nil)
end

local function deactivate()
  gvim.env.clear()
  refresh_status()
  gvim.notify(t("status.deactivated"))
end

local function maybe_auto_activate()
  local d = detected()
  if not d or not trusted() or active() then
    refresh_status()
    return
  end
  -- Only the low-risk virtualenv kind auto-activates on load; shell environments
  -- need the explicit command (a reduction from Fresh, which prompts).
  if d.kind == "path-only" then
    apply(d, false)
  end
  refresh_status()
end

local function reload()
  local a, d = active(), detected()
  if not d then
    gvim.notify(t("status.no_env_detected"))
    return
  end
  if not trusted() then
    request_trust_prompt()
    return
  end
  if not a then
    activate()
    return
  end
  if apply(d, true) then
    gvim.notify(t("status.reload_done", { name = tostring(d.name) }))
  end
end

local function register_commands()
  local commands = {
    { name = "activate", title = "%cmd.activate", desc = "%cmd.activate_desc", run = activate },
    { name = "reload", title = "%cmd.reload", desc = "%cmd.reload_desc", run = reload },
    { name = "use_system", title = "%cmd.use_system", desc = "%cmd.use_system_desc", run = deactivate },
    { name = "status", title = "%cmd.status", desc = "%cmd.status_desc", run = notify_status },
  }
  for _, c in ipairs(commands) do
    gvim.command.register { name = c.name, title = c.title, description = c.desc, run = c.run }
  end
end

local function install_statusbar()
  local ok, item = pcall(gvim.ui.statusbar.item, {
    id = "env",
    text = t("statusbar.system"),
    align = "right",
    priority = 30,
    on_click = function()
      if not detected() then
        gvim.notify(t("status.no_env_detected"))
      elseif trusted() then
        activate()
      else
        request_trust_prompt()
      end
    end,
  })
  if ok and item then
    state.item = item
  end
end

local function refresh()
  refresh_status()
end

gvim.on("ready", function()
  maybe_auto_activate()
  refresh()
end)
gvim.on("plugins_loaded", function()
  maybe_auto_activate()
  refresh()
end)
gvim.on("env_changed", function()
  refresh()
end)
gvim.on("trust_changed", function(ev)
  if ev and ev.level == "trusted" then
    maybe_auto_activate()
  end
  refresh()
end)
gvim.on("active_window_changed", function() refresh() end)

register_commands()
install_statusbar()
refresh()

gvim.plugin.on_unload(function()
  if state.item then
    state.item:remove()
    state.item = nil
  end
end)
