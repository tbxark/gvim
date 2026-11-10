-- Dev Container (reduced, read-only port of Fresh's devcontainer.ts).
--
-- Fresh's plugin detects a dev container config, shows a status bar summary and an info panel,
-- can run lifecycle commands, attach/rebuild through the `devcontainer` CLI, stream logs,
-- forward ports and show failed-attach popups. gvim has no container runtime or attach
-- surface, so this port deliberately does only what is real and read-only here:
--
--   * find `.devcontainer/devcontainer.json`, `.devcontainer.json` or
--     `.devcontainer/<subfolder>/devcontainer.json`, in Fresh's priority order;
--   * parse JSONC with the host's bounded json.decode_jsonc;
--   * show a read-only virtual info panel with the image/build/compose, features, forwarded
--     ports, container/remote environment and lifecycle commands;
--   * open the detected config file; Refresh re-detects it.
--
-- It never runs docker/devcontainer or any lifecycle command, never attaches, rebuilds or
-- claims the editor is in a container. Reads go through gvim.fs (the workspace authority on a
-- remote workspace) and are bounded; all work is a plugin-task await, never Update. Invalid
-- JSON and missing files are reported instead of presenting a fake configuration.
local gvim = require "gvim"

local t = gvim.i18n.t

local MAX_CONFIG_BYTES = 1024 * 1024
local MAX_DIRS = 100
local MAX_FEATURES = 100
local MAX_PORTS = 100
local MAX_ENV = 200
local MAX_LIFE_ITEMS = 10
local MAX_VALUE = 300
local MAX_LINES = 600

local LIFECYCLE = {
  "initializeCommand", "onCreateCommand", "updateContentCommand",
  "postCreateCommand", "postStartCommand", "postAttachCommand",
}

local state = {
  config = nil,      -- parsed object
  path = nil,        -- detected path, even when parsing failed
  parse_error = nil,
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

-- format_command renders a lifecycle value: a string, an argv array, or a named-command
-- object. It is display only; nothing is passed to a shell.
local function format_command(v)
  local s = scalar(v)
  if s then
    return s
  end
  if type(v) == "table" then
    local parts = {}
    if #v > 0 then
      for _, item in ipairs(v) do
        local x = scalar(item)
        if x then
          parts[#parts + 1] = x
        end
      end
      return clip(table.concat(parts, " "), MAX_VALUE)
    else
      local keys = {}
      for k in pairs(v) do
        keys[#keys + 1] = tostring(k)
      end
      table.sort(keys)
      for _, k in ipairs(keys) do
        local x = scalar(v[k])
        if x then
          parts[#parts + 1] = k .. ": " .. x
        elseif type(v[k]) == "table" then
          local sub = format_command(v[k])
          if sub ~= "" then
            parts[#parts + 1] = k .. ": " .. sub
          end
        end
      end
    end
    return clip(table.concat(parts, "; "), MAX_VALUE)
  end
  return ""
end

local function sorted_keys(tbl)
  local keys = {}
  for k in pairs(tbl) do
    keys[#keys + 1] = k
  end
  table.sort(keys, function(a, b) return tostring(a) < tostring(b) end)
  return keys
end

local function add(lines, line)
  if #lines >= MAX_LINES then
    return false
  end
  lines[#lines + 1] = line
  return true
end

local function build_info(config, path)
  local lines = {}
  local name = scalar(config.name) or t("panel.unnamed")
  add(lines, t("panel.title", { name = name }))
  add(lines, t("panel.config", { path = path }))
  add(lines, "")

  local image = scalar(config.image)
  local build = type(config.build) == "table" and config.build or nil
  local compose = config.dockerComposeFile
  if image then
    add(lines, t("panel.section_image"))
    add(lines, "  " .. image)
    add(lines, "")
  elseif build then
    add(lines, t("panel.section_build"))
    for _, key in ipairs({ "dockerfile", "context", "target" }) do
      local v = scalar(build[key])
      if v then
        add(lines, "  " .. key .. ": " .. v)
      end
    end
    add(lines, "")
  elseif type(compose) == "string" or (type(compose) == "table" and #compose > 0) then
    add(lines, t("panel.section_compose"))
    if type(compose) == "table" then
      local files = {}
      for _, f in ipairs(compose) do
        local x = scalar(f)
        if x then
          files[#files + 1] = x
        end
      end
      add(lines, "  files: " .. table.concat(files, ", "))
    else
      add(lines, "  files: " .. scalar(compose))
    end
    local service = scalar(config.service)
    if service then
      add(lines, "  service: " .. service)
    end
    add(lines, "")
  end

  if type(config.features) == "table" then
    local keys = sorted_keys(config.features)
    if #keys > 0 then
      add(lines, t("panel.section_features"))
      for i = 1, math.min(#keys, MAX_FEATURES) do
        local id = tostring(keys[i])
        add(lines, "  + " .. clip(id, MAX_VALUE))
        local opts = config.features[keys[i]]
        if type(opts) == "table" and not (next(opts) == nil) then
          local parts = {}
          for _, k in ipairs(sorted_keys(opts)) do
            local v = scalar(opts[k])
            if v then
              parts[#parts + 1] = tostring(k) .. " = " .. v
            end
          end
          if #parts > 0 then
            add(lines, "      " .. clip(table.concat(parts, ", "), MAX_VALUE))
          end
        end
      end
      add(lines, "")
    end
  end

  if type(config.forwardPorts) == "table" then
    local ports = config.forwardPorts
    if #ports > 0 then
      add(lines, t("panel.section_ports"))
      local attrs = type(config.portsAttributes) == "table" and config.portsAttributes or {}
      for i = 1, math.min(#ports, MAX_PORTS) do
        local port = scalar(ports[i])
        if port then
          local a = attrs[port] or attrs[tostring(ports[i])]
          local proto = (type(a) == "table" and scalar(a.protocol)) or "tcp"
          local line = "  " .. t("panel.port", { port = port, protocol = proto })
          if type(a) == "table" then
            local label = scalar(a.label)
            if label then
              line = line .. t("panel.port_label", { label = label })
            end
            local mode = scalar(a.onAutoForward)
            if mode then
              line = line .. t("panel.port_forward", { mode = mode })
            end
          end
          add(lines, line)
        end
      end
      add(lines, "")
    end
  end

  local env = {}
  for _, field in ipairs({ "containerEnv", "remoteEnv" }) do
    if type(config[field]) == "table" then
      for k, v in pairs(config[field]) do
        local display = scalar(v)
        if display then
          env[tostring(k)] = display
        end
      end
    end
  end
  local env_keys = sorted_keys(env)
  if #env_keys > 0 then
    add(lines, t("panel.section_env"))
    for i = 1, math.min(#env_keys, MAX_ENV) do
      local k = env_keys[i]
      add(lines, "  " .. t("panel.env", { key = clip(k, 120), value = env[k] }))
    end
    add(lines, "")
  end

  local life = {}
  for _, key in ipairs(LIFECYCLE) do
    local cmd = config[key]
    if cmd ~= nil and cmd ~= json.null then
      local text = format_command(cmd)
      if text ~= "" then
        life[#life + 1] = { key = key, text = text }
      end
    end
  end
  if #life > 0 then
    add(lines, t("panel.section_lifecycle"))
    for i = 1, math.min(#life, MAX_LIFE_ITEMS) do
      add(lines, "  " .. life[i].key .. ": " .. life[i].text)
    end
    add(lines, "")
  end

  local user = scalar(config.containerUser)
  local remote_user = scalar(config.remoteUser)
  if user or remote_user then
    add(lines, t("panel.section_users"))
    if user then add(lines, "  containerUser: " .. user) end
    if remote_user then add(lines, "  remoteUser: " .. remote_user) end
    add(lines, "")
  end

  if type(config.mounts) == "table" and #config.mounts > 0 then
    add(lines, t("panel.section_mounts"))
    for i = 1, math.min(#config.mounts, 50) do
      local m = config.mounts[i]
      if type(m) == "string" then
        add(lines, "  " .. clip(m, MAX_VALUE))
      elseif type(m) == "table" then
        local source, target, typ = scalar(m.source), scalar(m.target), scalar(m.type)
        local line = "  " .. (source or "?") .. " -> " .. (target or "?")
        if typ then line = line .. " (" .. typ .. ")" end
        add(lines, line)
      end
    end
    add(lines, "")
  end

  local hr = type(config.hostRequirements) == "table" and config.hostRequirements or nil
  if hr then
    add(lines, t("panel.section_host"))
    for _, key in ipairs({ "cpus", "memory", "storage", "gpu" }) do
      local v = scalar(hr[key])
      if v then add(lines, "  " .. key .. ": " .. v) end
    end
    add(lines, "")
  end

  add(lines, t("panel.footer"))
  return table.concat(lines, "\n")
end

local function read_if_exists(path)
  local exists = gvim.fs.exists(path)
  if not exists then
    return nil
  end
  local text, err = gvim.fs.read(path, { max = MAX_CONFIG_BYTES })
  if not text then
    return nil, err
  end
  local decoded, derr = json.decode_jsonc(text)
  if decoded == nil then
    return nil, derr, path
  end
  if type(decoded) ~= "table" then
    return nil, "the top level is not an object", path
  end
  return decoded
end

-- detect reads Fresh's three config locations in priority order. A present but invalid file
-- remains the open-config target, so the user can fix it; a valid later candidate wins.
local function detect()
  state.config, state.path, state.parse_error = nil, nil, nil
  local root = gvim.workspace.root()
  if not root or root == "" then
    return
  end
  local candidates = {
    gvim.path.join(root, ".devcontainer", "devcontainer.json"),
    gvim.path.join(root, ".devcontainer.json"),
  }
  local dc = gvim.path.join(root, ".devcontainer")
  local listed = gvim.fs.list(dc, { hidden = true })
  if listed then
    local dirs = {}
    for _, entry in ipairs(listed) do
      if entry.kind == "dir" then
        dirs[#dirs + 1] = entry.name
      end
    end
    table.sort(dirs)
    for i = 1, math.min(#dirs, MAX_DIRS) do
      candidates[#candidates + 1] = gvim.path.join(dc, dirs[i], "devcontainer.json")
    end
  end
  local first_error
  for _, path in ipairs(candidates) do
    local cfg, err = read_if_exists(path)
    if cfg then
      state.config, state.path = cfg, path
      return
    elseif err then
      state.path = state.path or path
      first_error = first_error or { path = path, error = tostring(err) }
    end
  end
  if first_error then
    state.parse_error = first_error
  end
end

local function close_panel()
  if state.panel_buf and state.panel_buf:valid() then
    pcall(state.panel_buf.close, state.panel_buf)
  end
  state.panel_buf = nil
end

local function show_info()
  detect()
  if not state.config then
    if state.parse_error then
      notice(t("status.parse_failed", { path = state.parse_error.path, error = state.parse_error.error }), "warn")
    else
      notice(t("status.no_config"))
    end
    return
  end
  local text = build_info(state.config, state.path)
  close_panel()
  local buf, err = gvim.buf.create {
    name = "*Dev Container: " .. (scalar(state.config.name) or t("panel.unnamed")) .. "*",
    text = text,
    read_only = true,
    focus = true,
  }
  if not buf then
    notice(t("status.open_failed", { path = state.path, error = tostring(err) }), "warn")
    return
  end
  state.panel_buf = buf
  notice(t("status.opened", { path = state.path }))
end

local function open_config()
  detect()
  if not state.path then
    notice(t("status.no_config_open"))
    return
  end
  local buf, err = gvim.buf.open(state.path, { pos = gvim.pos(0, 0), focus = true })
  if not buf then
    notice(t("status.open_failed", { path = state.path, error = tostring(err) }), "warn")
    return
  end
  if state.parse_error then
    notice(t("status.parse_failed", { path = state.parse_error.path, error = state.parse_error.error }), "warn")
  else
    notice(t("status.opened", { path = state.path }))
  end
end

gvim.command.register {
  name = "show_info",
  title = "%cmd.show_info",
  description = "%cmd.show_info_desc",
  run = show_info,
}

gvim.command.register {
  name = "open_config",
  title = "%cmd.open_config",
  description = "%cmd.open_config_desc",
  run = open_config,
}

gvim.command.register {
  name = "refresh",
  title = "%cmd.refresh",
  description = "%cmd.refresh_desc",
  run = function()
    detect()
    if state.parse_error then
      notice(t("status.parse_failed", { path = state.parse_error.path, error = state.parse_error.error }), "warn")
    elseif state.config then
      if state.panel_buf and state.panel_buf:valid() then
        show_info()
      else
        notice(t("status.refreshed", { path = state.path }))
      end
    else
      notice(t("status.no_config"))
    end
  end,
}

gvim.plugin.on_unload(close_panel)

-- Detect at load so a broken config is reported early, without blocking plugin startup.
gvim.async.spawn(function()
  detect()
  if state.parse_error then
    notice(t("status.parse_failed", { path = state.parse_error.path, error = state.parse_error.error }), "warn")
  end
end)

gvim.log.debug("dev container plugin loaded")
