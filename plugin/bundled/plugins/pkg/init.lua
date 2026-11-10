-- Package: Packages (reduced, read-only port of Fresh's pkg.ts).
--
-- Fresh's pkg.ts is a git-based package manager: it installs/updates/removes plugins and
-- themes, syncs git registries, resolves versions and writes a lockfile. gvim has no such
-- package store or network/process surface here, so this port only browses what the editor
-- already knows: gvim.plugins.list() (installed, bundled, disabled and failed plugins) and
-- gvim.theme.all() (native built-in/user themes).
--
-- Package: Packages opens a picker of plugin rows (INSTALLED/BUNDLED) and native theme rows;
-- Enter opens a read-only virtual detail panel with name, type, version, status, source,
-- path, description and dependencies (requires). No install/update/remove/clone/pull/registry
-- sync/lockfile operation is registered or claimed, and no process is started. Everything is
-- in-memory plugin/theme metadata, bounded by MAX_ROWS and clipped display strings; the
-- command and callbacks run as plugin tasks, never Update.
local gvim = require "gvim"

local t = gvim.i18n.t

local MAX_ROWS = 1000
local MAX_TEXT = 300

local state = { picker = nil, detail_buf = nil }

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

local function status_label(row)
  if row.enabled == false or row.status == "disabled" then
    return t("status.disabled")
  end
  if row.status == "loaded" or row.status == "" or row.status == nil then
    return t("status.enabled")
  end
  return row.status
end

local function theme_status(theme, current)
  if theme.name == current then
    return t("status.current")
  end
  if theme.builtin then
    return t("status.builtin")
  end
  return t("status.user")
end

local function package_text(pkg)
  if pkg.type == "theme" then
    return t("row.theme", { name = clip(pkg.name, 120), status = pkg.status })
  end
  return t("row.plugin", {
    name = clip(pkg.name, 120),
    version = clip(pkg.version ~= "" and pkg.version or "unknown", 40),
    status = clip(pkg.status, 40),
  })
end

local function package_detail(pkg)
  local lines = {
    t("panel.title", { name = pkg.name }),
    t("panel.name", { name = clip(pkg.name, 120) }),
    t("panel.type", { type = pkg.type == "theme" and t("type.theme") or t("type.plugin") }),
    t("panel.version", { version = clip(pkg.version ~= "" and pkg.version or "unknown", 120) }),
    t("panel.status", { status = pkg.status }),
    t("panel.source", { source = pkg.source }),
  }
  local path = pkg.path ~= "" and clip(pkg.path, MAX_TEXT) or t("panel.embedded")
  lines[#lines + 1] = t("panel.path", { path = path })
  lines[#lines + 1] = ""
  lines[#lines + 1] = t("panel.description")
  lines[#lines + 1] = "  " .. (pkg.description ~= "" and clip(pkg.description, MAX_TEXT) or t("row.no_description"))
  lines[#lines + 1] = ""
  lines[#lines + 1] = t("panel.dependencies")
  if type(pkg.requires) == "table" and #pkg.requires > 0 then
    for i = 1, #pkg.requires do
      lines[#lines + 1] = "  - " .. clip(pkg.requires[i], MAX_TEXT)
    end
  else
    lines[#lines + 1] = "  " .. t("panel.none")
  end
  if pkg.type == "theme" then
    lines[#lines + 1] = ""
    lines[#lines + 1] = t("panel.native_theme")
  end
  lines[#lines + 1] = ""
  lines[#lines + 1] = t("panel.footer")
  return table.concat(lines, "\n")
end

local function close_detail()
  if state.detail_buf and state.detail_buf:valid() then
    pcall(state.detail_buf.close, state.detail_buf)
  end
  state.detail_buf = nil
end

local function close_picker()
  if state.picker then
    local p = state.picker
    state.picker = nil
    pcall(p.close, p)
  end
end

local function show_detail(pkg)
  close_detail()
  local buf, err = gvim.buf.create {
    name = "*Package: " .. clip(pkg.name, 60) .. "*",
    text = package_detail(pkg),
    read_only = true,
    focus = true,
  }
  if not buf then
    notice("Package: cannot open details: " .. tostring(err), "warn")
    return
  end
  state.detail_buf = buf
end

local function on_select(item)
  local pkg = item and item.package
  close_picker()
  if type(pkg) ~= "table" then
    return
  end
  show_detail(pkg)
end

local function build_items()
  local plugins = gvim.plugins.list() or {}
  local themes = gvim.theme.all() or {}
  local current_theme = gvim.theme.name()
  local installed, bundled = {}, {}
  for _, row in ipairs(plugins) do
    local pkg = {
      type = "plugin",
      name = tostring(row.name or ""),
      version = tostring(row.version or ""),
      status = status_label(row),
      source = tostring(row.source or ""),
      path = tostring(row.file ~= "" and row.file or (row.dir or "")),
      description = tostring(row.description or ""),
      requires = type(row.requires) == "table" and row.requires or {},
    }
    if row.source == "bundled" then
      bundled[#bundled + 1] = pkg
    else
      installed[#installed + 1] = pkg
    end
  end
  local items, rows = {}, 0
  local function header(key, count)
    if count > 0 and rows < MAX_ROWS then
      items[#items + 1] = { text = t(key, { count = count }), disabled = true }
    end
  end
  header("section.installed", #installed)
  for _, pkg in ipairs(installed) do
    if rows >= MAX_ROWS then break end
    items[#items + 1] = { text = package_text(pkg), detail = clip(pkg.description, MAX_TEXT), package = pkg }
    rows = rows + 1
  end
  header("section.bundled", #bundled)
  for _, pkg in ipairs(bundled) do
    if rows >= MAX_ROWS then break end
    items[#items + 1] = { text = package_text(pkg), detail = clip(pkg.description, MAX_TEXT), package = pkg }
    rows = rows + 1
  end
  local theme_pkgs = {}
  for _, th in ipairs(themes) do
    if type(th) == "table" then
      theme_pkgs[#theme_pkgs + 1] = {
        type = "theme",
        name = tostring(th.name or ""),
        version = "native",
        status = theme_status(th, current_theme),
        source = th.builtin and t("status.builtin") or t("status.user"),
        path = tostring(th.ref or ""),
        description = t("panel.native_theme"),
        requires = {},
      }
    end
  end
  header("section.themes", #theme_pkgs)
  for _, pkg in ipairs(theme_pkgs) do
    if rows >= MAX_ROWS then break end
    items[#items + 1] = { text = package_text(pkg), detail = clip(pkg.description, MAX_TEXT), package = pkg }
    rows = rows + 1
  end
  if #items == 0 then
    items[1] = { text = t("status.empty"), disabled = true }
  end
  return items, #plugins, #themes
end

local function open_list()
  if state.picker then
    return
  end
  local items, plugins, themes = build_items()
  local picker = gvim.ui.picker {
    title = t("cmd.list"),
    placeholder = t("cmd.list"),
    filter = "fuzzy",
    items = items,
    on_select = on_select,
    on_close = function() state.picker = nil end,
  }
  state.picker = picker
  picker:open()
  picker:set_status(t("status.count", { plugins = plugins, themes = themes }))
end

gvim.command.register {
  name = "list",
  title = "%cmd.list",
  description = "%cmd.list_desc",
  run = function()
    -- The metadata reads are synchronous and cheap; scheduling the list build as a plugin
    -- task still keeps the command handler off the Update path.
    gvim.async.spawn(open_list)
  end,
}

gvim.plugin.on_unload(function()
  close_picker()
  close_detail()
end)

gvim.log.debug("package browser plugin loaded")
