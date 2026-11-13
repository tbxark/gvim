-- C# language-server support: a reduced port of Fresh's plugins/csharp_support.ts
-- (400 lines).
--
-- Fresh's plugin does four jobs: find the nearest .csproj/.sln from a C# file, set the
-- C# language server's root URI, run dotnet restore once per project (deferred until the
-- workspace is trusted, because MSBuild targets in a repository can execute code), and
-- answer Roslyn's workspace/_roslyn_projectNeedsRestore request plus the install-help
-- popup. gvim's plugin API has no call to set an LSP root URI and no server-request event,
-- so this port keeps the project-root detection, the trust-gated restore and the install
-- help, and adds commands for the same restore/help flows (Fresh has no commands here).
local gvim = require "gvim"

local t = gvim.i18n.t

local INSTALL_COMMAND = "dotnet tool install --global csharp-ls"
local RESTORE_TIMEOUT_MS = 300000
local MAX_OUTPUT = 1048576

-- Project roots restored this session and roots whose restore was deferred because the
-- workspace was not trusted yet (drained by trust_changed, like env_manager).
local restored = {}
local pending = {}
local last_error = nil

local function lower(s)
  return string.lower(s or "")
end

local function dir_name(path)
  return gvim.path.dirname(path)
end

local function is_csharp_path(path)
  return path ~= nil and path ~= "" and lower(path):sub(-3) == ".cs"
end

local function active_csharp_buffer()
  local buf = gvim.buf.current()
  if buf and is_csharp_path(buf:path()) then
    return buf
  end
  return nil
end

-- find_project_root walks up from the file (at most 20 directories, like Fresh) and
-- returns the first directory holding a .csproj or .sln file. A directory that cannot be
-- listed stops the walk; nil means "no project file found".
local function find_project_root(path)
  local current = dir_name(path)
  if not current or current == "" then
    return nil
  end
  for _ = 1, 20 do
    local entries = gvim.fs.list(current, { hidden = true })
    if not entries then
      return nil
    end
    for _, entry in ipairs(entries) do
      if entry.kind == "file" then
        local name = lower(entry.name)
        if name:sub(-7) == ".csproj" or name:sub(-4) == ".sln" then
          return current
        end
      end
    end
    local parent = dir_name(current)
    if not parent or parent == "" or parent == current then
      break
    end
    current = parent
  end
  return nil
end

local function first_line(s)
  return (tostring(s or ""):match("^[^\r\n]*")) or ""
end

-- run_restore runs dotnet restore for one project root, once per session unless force is
-- set. While the workspace is untrusted it is recorded and retried by trust_changed:
-- dotnet evaluates repository-controlled MSBuild targets, so opening a .cs file must not
-- execute them before the user trusts the folder. The process follows the workspace
-- authority (remote restores run on the remote host).
local function run_restore(root, force)
  if not root or root == "" then
    return
  end
  if gvim.env.trust_level() ~= "trusted" then
    pending[root] = true
    return
  end
  if restored[root] and not force then
    return
  end
  restored[root] = true
  gvim.status(t("status.restoring_packages", { project = root }))
  local result, err = gvim.proc.run({ "dotnet", "restore", root }, { timeout_ms = RESTORE_TIMEOUT_MS, max_output = MAX_OUTPUT })
  if not result then
    gvim.status(t("status.restore_error", { error = tostring(err) }))
    return
  end
  if result.code == 0 then
    gvim.status(t("status.restore_completed", { project = root }))
  else
    local detail = first_line(result.stderr)
    if detail == "" then
      detail = t("status.exit_code", { code = tostring(result.code) })
    end
    gvim.status(t("status.restore_failed", { error = detail }))
  end
end

local function restore_for_path(path, force)
  local root = find_project_root(path)
  if not root then
    root = dir_name(path)
  end
  run_restore(root, force)
end

-- disable_active_csharp_language is the controlled native disable action: re-check the
-- active buffer, carry its path/language to the TUI, and let the native action validate the
-- target atomically before its root-level language mute. The async mute result is native-owned.
local function disable_active_csharp_language()
  local buf = active_csharp_buffer()
  if not buf then
    gvim.status(t("status.not_csharp"))
    return
  end
  if not gvim.has("lsp") then
    gvim.status(t("status.no_lsp"))
    return
  end
  local ran, err = gvim.command.run("lsp_disable_language_target", {
    path = buf:path(),
    language = tostring(buf:language() or ""),
  })
  if not ran then
    gvim.status(t("status.action_failed", { error = tostring(err) }))
  end
end

-- show_help is the install-help popup for a missing/broken csharp-ls server.
local function show_help()
  local buf = active_csharp_buffer()
  if not buf then
    gvim.status(t("status.not_csharp"))
    return
  end
  local message = t("help.message")
  if last_error then
    message = message .. "\n\n" .. t("help.last_error", { error = last_error })
  end
  local choice = gvim.ui.action_popup({
    title = t("help.title"),
    message = message,
    actions = {
      { id = "copy:install", label = t("actions.copy", { value = INSTALL_COMMAND }) },
      { id = "act:lsp_restart", label = t("actions.restart") },
      { id = "act:show_lsp_status", label = t("actions.status") },
      { id = "act:lsp_disable_language", label = t("actions.disable") },
      { id = "dismiss", label = t("actions.dismiss") },
    },
  })
  if not choice or choice == "dismiss" then
    return
  end
  if choice == "copy:install" then
    gvim.clipboard.set(INSTALL_COMMAND)
    gvim.status(t("status.copied", { value = INSTALL_COMMAND }))
    return
  end
  local action = choice:match("^act:(.+)$")
  if action == "lsp_disable_language" then
    disable_active_csharp_language()
    return
  end
  if action then
    local ran, err = gvim.command.run(action)
    if not ran then
      gvim.status(t("status.action_failed", { error = tostring(err) }))
    end
  end
end

gvim.command.register({
  name = "restore_project",
  title = t("cmd.restore"),
  description = t("cmd.restore_desc"),
  run = function()
    local buf = active_csharp_buffer()
    if not buf then
      gvim.status(t("status.not_csharp"))
      return
    end
    if gvim.env.trust_level() ~= "trusted" then
      gvim.status(t("status.not_trusted"))
      return
    end
    restore_for_path(buf:path(), true)
  end,
})

gvim.command.register({
  name = "show_help",
  title = t("cmd.show_help"),
  description = t("cmd.show_help_desc"),
  run = function()
    show_help()
  end,
})

gvim.on("after_file_open", function(ev)
  local path = ev.path
  if is_csharp_path(path) then
    restore_for_path(path, false)
  end
end)

gvim.on("trust_changed", function(ev)
  if not ev or ev.level ~= "trusted" then
    return
  end
  for root in pairs(pending) do
    pending[root] = nil
    run_restore(root, false)
  end
end)

gvim.on("lsp_server_error", function(ev)
  local buf = active_csharp_buffer()
  if not buf then
    return
  end
  local message = tostring(ev.error or "")
  local text = lower(message)
  -- gvim's payload carries only the error text: only claim a failure that names the C#
  -- server, so another language's error on an open .cs tab is not misreported.
  if not (text:find("csharp", 1, true) or text:find("c#", 1, true) or
      text:find("roslyn", 1, true) or text:find("omnisharp", 1, true)) then
    return
  end
  last_error = message
  if text:find("not found", 1, true) or text:find("command not found", 1, true) then
    gvim.status(t("status.server_not_found"))
  else
    gvim.status(t("status.server_error", { error = message }))
  end
end)

gvim.on("lsp_status_clicked", function()
  if not last_error then
    return
  end
  if not active_csharp_buffer() then
    return
  end
  show_help()
end)

gvim.log.debug("csharp_support: Plugin loaded")
