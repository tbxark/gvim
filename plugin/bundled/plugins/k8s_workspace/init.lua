-- Kubernetes Workspace Config (reduced, read-only port of Fresh's k8s-workspace.ts).
--
-- Fresh's plugin loads `.fresh/k8s.json`, resolves a target through one of four providers
-- (attach-existing, manifest, run, command) by running kubectl/host commands, checks RBAC,
-- probes the pod environment and calls attachRemoteAgent; Disconnect tears the provider down.
-- gvim has no kubectl/attachRemoteAgent/orchestrator surface, so this port only reads and
-- validates the configuration and displays it:
--
--   * K8s: Show Workspace Config reads `.fresh/k8s.json` as bounded JSONC, validates the
--     targets/defaultTarget/provider shapes, and opens a read-only virtual panel with the
--     cluster context, namespace, manifest/run/command settings, workspace, vars and any
--     configuration errors;
--   * K8s: Open Config opens the config file;
--   * K8s: Refresh Workspace Config re-reads/validates it (and refreshes an open panel).
--
-- It never executes kubectl or a provider command, never connects/attaches, never creates a
-- pod and never claims a cluster is connected. Reads are gvim.fs (workspace authority on a
-- remote workspace), bounded and asynchronous; errors are reported instead of hidden.
local gvim = require "gvim"

local t = gvim.i18n.t

local CONFIG_REL = ".fresh/k8s.json"
local MAX_CONFIG_BYTES = 1024 * 1024
local MAX_TARGETS = 100
local MAX_VARS = 100
local MAX_ERRORS = 100
local MAX_VALUE = 240
local MAX_LINES = 500

local PROVIDERS = {
  ["attach-existing"] = { required = {}, optional = { "context", "namespace" } },
  manifest = { required = { "template", "namespace" }, optional = { "context", "waitTimeoutSec" } },
  run = { required = { "name", "image", "namespace" }, optional = { "context", "waitTimeoutSec" } },
  command = { required = { "up" }, optional = { "down", "status" } },
}

local state = {
  config = nil,
  path = nil,
  errors = {},
  panel_buf = nil,
}

local function notice(msg, level)
  gvim.notify(msg, level or "info")
end

local function clip(s, limit)
  s = tostring(s or ""):gsub("[\r\n]+", " ")
  if #s <= limit then
    return s
  end
  local cut = s:sub(1, limit)
  while #cut > 0 and cut:byte(-1) >= 128 do
    cut = cut:sub(1, #cut - 1)
  end
  return cut .. "…"
end

local function scalar(v)
  if type(v) == "string" then
    return clip(v, MAX_VALUE)
  elseif type(v) == "number" or type(v) == "boolean" then
    return tostring(v)
  elseif v == nil or v == json.null then
    return nil
  end
  return nil
end

-- Both caps are inclusive: MAX_LINES rows and MAX_ERRORS errors are accepted; the next
-- one is dropped silently (these are display caps, not validation failures).
local function add(lines, line)
  if #lines < MAX_LINES then
    lines[#lines + 1] = line
  end
end

local function add_error(msg)
  if #state.errors < MAX_ERRORS then
    state.errors[#state.errors + 1] = clip(msg, MAX_VALUE)
  end
end

local function sorted_keys(tbl)
  local keys = {}
  for k in pairs(tbl) do
    keys[#keys + 1] = k
  end
  table.sort(keys, function(a, b) return tostring(a) < tostring(b) end)
  return keys
end

local function validate_string(target_name, field, value, required, where)
  if value == nil or value == json.null then
    if required then
      add_error((where or target_name) .. ": " .. field .. " is required")
    end
    return nil
  end
  if type(value) ~= "string" or value == "" then
    add_error((where or target_name) .. ": " .. field .. " must be a non-empty string")
    return nil
  end
  return value
end

local function validate_number(target_name, field, value, where)
  if value == nil or value == json.null then
    return nil
  end
  if type(value) ~= "number" or value <= 0 then
    add_error((where or target_name) .. ": " .. field .. " must be a positive number")
    return nil
  end
  return value
end

local function command_text(cmd)
  if type(cmd) ~= "table" then
    return ""
  end
  local parts = { scalar(cmd.command) or "?" }
  if type(cmd.args) == "table" then
    for _, a in ipairs(cmd.args) do
      local x = scalar(a)
      if x then
        parts[#parts + 1] = x
      end
    end
  end
  return clip(table.concat(parts, " "), MAX_VALUE)
end

local function validate_command(target_name, field, value, required, where)
  if value == nil or value == json.null then
    if required then
      add_error((where or target_name) .. ": " .. field .. " is required")
    end
    return nil
  end
  if type(value) ~= "table" then
    add_error((where or target_name) .. ": " .. field .. " must be an object")
    return nil
  end
  validate_string(target_name, field .. ".command", value.command, true, where)
  if value.args ~= nil and value.args ~= json.null then
    if type(value.args) ~= "table" then
      add_error((where or target_name) .. ": " .. field .. ".args must be an array")
    else
      for i, a in ipairs(value.args) do
        if type(a) ~= "string" then
          add_error((where or target_name) .. ": " .. field .. ".args[" .. i .. "] must be a string")
        end
      end
    end
  end
  validate_string(target_name, field .. ".cwd", value.cwd, false, where)
  return value
end

local function validate_provider(name, target)
  local provider = target.provider
  if type(provider) ~= "table" then
    add_error(name .. ": provider must be an object")
    return nil
  end
  local kind = provider.kind
  if type(kind) ~= "string" or not PROVIDERS[kind] then
    add_error(name .. ": unknown provider.kind " .. tostring(kind))
    return nil
  end
  if kind == "attach-existing" then
    validate_string(name, "provider.context", provider.context, false)
    validate_string(name, "provider.namespace", provider.namespace, false)
  elseif kind == "manifest" then
    validate_string(name, "provider.template", provider.template, true)
    validate_string(name, "provider.namespace", provider.namespace, true)
    validate_string(name, "provider.context", provider.context, false)
    validate_number(name, "provider.waitTimeoutSec", provider.waitTimeoutSec)
  elseif kind == "run" then
    validate_string(name, "provider.name", provider.name, true)
    validate_string(name, "provider.image", provider.image, true)
    validate_string(name, "provider.namespace", provider.namespace, true)
    validate_string(name, "provider.context", provider.context, false)
    validate_number(name, "provider.waitTimeoutSec", provider.waitTimeoutSec)
  elseif kind == "command" then
    validate_command(name, "provider.up", provider.up, true)
    validate_command(name, "provider.down", provider.down, false)
    validate_command(name, "provider.status", provider.status, false)
  end
  validate_string(name, "workspace", target.workspace, false)
  if target.confirmCreate ~= nil and target.confirmCreate ~= json.null and type(target.confirmCreate) ~= "boolean" then
    add_error(name .. ": confirmCreate must be a boolean")
  end
  if target.vars ~= nil and target.vars ~= json.null then
    if type(target.vars) ~= "table" then
      add_error(name .. ": vars must be an object")
    else
      local count = 0
      for k, v in pairs(target.vars) do
        count = count + 1
        if type(k) ~= "string" or type(v) ~= "string" then
          add_error(name .. ": vars must map strings to strings")
          break
        end
        -- The cap is inclusive: exactly MAX_VARS valid entries are accepted; the next
        -- one is the error. (The old `>=` rejected the 100th valid entry.)
        if count > MAX_VARS then
          add_error(name .. ": vars is too large")
          break
        end
      end
    end
  end
  return provider
end

-- validate_config records configuration errors and returns the targets table. It never
-- resolves a provider or touches a cluster.
local function validate_config(config)
  state.errors = {}
  if type(config) ~= "table" then
    add_error("the top level must be an object")
    return nil
  end
  local targets = config.targets
  if type(targets) ~= "table" or next(targets) == nil then
    add_error("targets is required and must be a non-empty object")
    return nil
  end
  local names = sorted_keys(targets)
  if #names > MAX_TARGETS then
    add_error("too many targets (max " .. MAX_TARGETS .. ")")
  end
  local default = config.defaultTarget
  if default ~= nil and default ~= json.null then
    if type(default) ~= "string" then
      add_error("defaultTarget must be a string")
    elseif targets[default] == nil then
      add_error("defaultTarget '" .. clip(default, 80) .. "' is not a target")
    end
  end
  for i = 1, math.min(#names, MAX_TARGETS) do
    local name = names[i]
    local target = targets[name]
    if type(target) ~= "table" then
      add_error(tostring(name) .. ": must be an object")
    else
      validate_provider(tostring(name), target)
    end
  end
  return targets
end

local function format_target(name, target, default_name)
  local lines = {}
  local provider = target.provider
  local kind = type(provider) == "table" and scalar(provider.kind) or "?"
  add(lines, t("panel.target", {
    name = clip(name, 80),
    default = name == default_name and t("panel.target_default") or "",
  }))
  add(lines, "  " .. t("panel.kind", { kind = kind }))
  if type(provider) == "table" then
    if kind == "attach-existing" then
      local ctx, ns = scalar(provider.context), scalar(provider.namespace)
      if ctx then add(lines, "  " .. t("panel.context", { value = ctx })) end
      if ns then add(lines, "  " .. t("panel.namespace", { value = ns })) end
    elseif kind == "manifest" then
      local ctx, ns = scalar(provider.context), scalar(provider.namespace)
      local template = scalar(provider.template)
      if ctx then add(lines, "  " .. t("panel.context", { value = ctx })) end
      if ns then add(lines, "  " .. t("panel.namespace", { value = ns })) end
      if template then add(lines, "  " .. t("panel.manifest", { value = template })) end
      local wait = scalar(provider.waitTimeoutSec)
      if wait then add(lines, "  " .. t("panel.wait", { value = wait })) end
    elseif kind == "run" then
      local ctx, ns = scalar(provider.context), scalar(provider.namespace)
      local pname, image = scalar(provider.name), scalar(provider.image)
      if ctx then add(lines, "  " .. t("panel.context", { value = ctx })) end
      if ns then add(lines, "  " .. t("panel.namespace", { value = ns })) end
      if pname and image then add(lines, "  " .. t("panel.run", { name = pname, image = image })) end
      local wait = scalar(provider.waitTimeoutSec)
      if wait then add(lines, "  " .. t("panel.wait", { value = wait })) end
    elseif kind == "command" then
      local up = command_text(provider.up)
      if up ~= "" then add(lines, "  " .. t("panel.command", { value = up })) end
      local cwd = type(provider.up) == "table" and scalar(provider.up.cwd) or nil
      if cwd then add(lines, "  " .. t("panel.cwd", { value = cwd })) end
      local down = command_text(provider.down)
      if down ~= "" then add(lines, "  " .. t("panel.down", { value = down })) end
      local status_cmd = command_text(provider.status)
      if status_cmd ~= "" then add(lines, "  " .. t("panel.status_cmd", { value = status_cmd })) end
    end
  end
  local ws = scalar(target.workspace)
  if ws then add(lines, "  " .. t("panel.workspace", { value = ws })) end
  if target.confirmCreate ~= nil and target.confirmCreate ~= json.null then
    add(lines, "  " .. t("panel.confirm", { value = tostring(target.confirmCreate) }))
  end
  if type(target.vars) == "table" then
    local parts = {}
    local keys = sorted_keys(target.vars)
    for i = 1, math.min(#keys, 20) do
      local k = keys[i]
      local v = scalar(target.vars[k])
      if v then parts[#parts + 1] = tostring(k) .. " = " .. v end
    end
    if #parts > 0 then add(lines, "  " .. t("panel.vars", { value = table.concat(parts, ", ") })) end
  end
  add(lines, "")
  return lines
end

local function build_panel(path, config, targets, default_name)
  local lines = {}
  add(lines, t("panel.title"))
  add(lines, t("panel.path", { path = path }))
  if default_name then
    add(lines, t("panel.default", { name = clip(default_name, 80) }))
  else
    add(lines, t("panel.default_none"))
  end
  add(lines, "")
  if #state.errors > 0 then
    add(lines, t("panel.errors", { count = #state.errors }))
    for _, err in ipairs(state.errors) do
      add(lines, "  - " .. err)
    end
    add(lines, "")
  end
  if targets then
    local names = sorted_keys(targets)
    add(lines, t("panel.targets", { count = math.min(#names, MAX_TARGETS) }))
    add(lines, "")
    for i = 1, math.min(#names, MAX_TARGETS) do
      local name = names[i]
      local target = targets[name]
      if type(target) == "table" then
        for _, line in ipairs(format_target(tostring(name), target, default_name)) do
          add(lines, line)
        end
      end
    end
  end
  if #state.errors == 0 then
    add(lines, t("panel.no_errors"))
    add(lines, "")
  end
  add(lines, t("panel.footer"))
  return table.concat(lines, "\n")
end

local function close_panel()
  if state.panel_buf and state.panel_buf:valid() then
    pcall(state.panel_buf.close, state.panel_buf)
  end
  state.panel_buf = nil
end

-- load reads .fresh/k8s.json. It returns the path even when parsing/validation fails so Open
-- Config can still reach the broken file.
local function load()
  state.config, state.path, state.errors = nil, nil, {}
  local root = gvim.workspace.root()
  if not root or root == "" then
    return nil
  end
  local path = gvim.path.join(root, CONFIG_REL)
  if not gvim.fs.exists(path) then
    return nil
  end
  state.path = path
  local text, err = gvim.fs.read(path, { max = MAX_CONFIG_BYTES })
  if not text then
    state.errors = { "cannot read: " .. tostring(err) }
    return nil
  end
  local config, derr = json.decode_jsonc(text)
  if config == nil then
    state.errors = { "cannot parse JSONC: " .. tostring(derr) }
    return nil
  end
  state.config = config
  local targets = validate_config(config)
  return targets
end

local function show()
  local targets = load()
  if not state.path then
    notice(t("status.no_config"))
    return
  end
  if not state.config then
    notice(t("status.parse_failed", { path = state.path, error = state.errors[1] or "invalid config" }), "warn")
  else
    local default = scalar(state.config.defaultTarget)
    if #state.errors > 0 then
      notice(t("status.invalid", { count = #state.errors, path = state.path }), "warn")
    elseif targets then
      notice(t("status.valid", { targets = #targets, path = state.path }))
    end
  end
  local default = type(state.config) == "table" and scalar(state.config.defaultTarget) or nil
  local text = build_panel(state.path, state.config, targets, default)
  close_panel()
  local buf, err = gvim.buf.create {
    name = "*K8s Workspace: " .. clip(gvim.path.basename(state.path), 40) .. "*",
    text = text,
    read_only = true,
    focus = true,
  }
  if not buf then
    notice(t("status.open_failed", { path = state.path, error = tostring(err) }), "warn")
    return
  end
  state.panel_buf = buf
end

local function open_config()
  if not state.path then
    local root = gvim.workspace.root()
    if root and root ~= "" then
      local path = gvim.path.join(root, CONFIG_REL)
      if gvim.fs.exists(path) then
        state.path = path
      end
    end
  end
  if not state.path then
    notice(t("status.no_config"))
    return
  end
  local buf, err = gvim.buf.open(state.path, { pos = gvim.pos(0, 0), focus = true })
  if not buf then
    notice(t("status.open_failed", { path = state.path, error = tostring(err) }), "warn")
    return
  end
  notice(t("status.opened", { path = state.path }))
end

gvim.command.register {
  name = "show",
  title = "%cmd.show",
  description = "%cmd.show_desc",
  run = show,
}

gvim.command.register {
  name = "open",
  title = "%cmd.open",
  description = "%cmd.open_desc",
  run = open_config,
}

gvim.command.register {
  name = "refresh",
  title = "%cmd.refresh",
  description = "%cmd.refresh_desc",
  run = show,
}

gvim.plugin.on_unload(close_panel)

gvim.log.debug("k8s workspace plugin loaded")
