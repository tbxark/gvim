-- Deno language-server helper: a reduced port of Fresh's plugins/deno_lsp.ts.
--
-- Fresh's plugin selects "deno lsp" for TypeScript/JavaScript when the active window's
-- root has deno.json/deno.jsonc and the Deno runtime is on PATH, restoring the configured
-- server otherwise. gvim's plugin API has no register/select-language-server call and no
-- active-window list, so this port keeps the user-visible check/install/help surface as
-- palette commands; the user configures "deno lsp" in the LSP settings (the help popup
-- shows the entry), and Check Project reports both prerequisites.
local gvim = require "gvim"

local t = gvim.i18n.t

local CURL_INSTALL = "curl -fsSL https://deno.land/install.sh | sh"
local BREW_INSTALL = "brew install deno"

-- project_root is the workspace root; a session without one falls back to the process cwd,
-- matching the other language helpers.
local function project_root()
  local root = gvim.workspace.root()
  if root and root ~= "" then
    return root
  end
  return gvim.dirs.cwd()
end

-- project_configs lists the Deno project files at the root, in the order the help text
-- mentions them. The file system calls follow the workspace authority (remote included).
local function project_configs(root)
  local found = {}
  for _, name in ipairs({ "deno.json", "deno.jsonc" }) do
    local path = gvim.path.join(root, name)
    if gvim.fs.exists(path) then
      found[#found + 1] = path
    end
  end
  return found
end

-- install_command picks Homebrew when it is on PATH, otherwise the official curl script.
-- The command that will be copied is also the one reported by Check Project, so the user sees
-- exactly which alternative applies to this machine.
local function install_command()
  if gvim.proc.which("brew") then
    return BREW_INSTALL
  end
  return CURL_INSTALL
end

local function copy_command(value)
  local ok, err = gvim.clipboard.set(value)
  if not ok then
    gvim.status(t("status.copy_failed", { error = tostring(err) }))
    return
  end
  gvim.status(t("status.copied", { value = value }))
end

local function check_project()
  local root = project_root()
  local configs = project_configs(root)
  local deno = gvim.proc.which("deno")
  local has_deno = deno ~= nil and deno ~= ""
  local config_text = table.concat(configs, ", ")
  local install = install_command()

  if #configs > 0 and has_deno then
    gvim.status(t("status.ready", { root = root, config = config_text, path = deno }))
    return
  end
  if #configs > 0 then
    gvim.status(t("status.config_no_deno", { root = root, config = config_text, install = install }))
    return
  end
  if has_deno then
    gvim.status(t("status.deno_no_config", { root = root, path = deno }))
    return
  end
  gvim.status(t("status.no_project_no_deno", { root = root, install = install }))
end

-- show_help explains the reduced port: install commands, the config entry gvim needs, the
-- local-PATH caveat and the native LSP actions. Its copy/check/restart actions reuse the
-- command implementations above, so there is one source of behaviour.
local function show_help()
  local choice = gvim.ui.action_popup({
    title = t("help.title"),
    message = t("help.message"),
    actions = {
      { id = "copy:curl", label = t("actions.copy_curl", { command = CURL_INSTALL }) },
      { id = "copy:brew", label = t("actions.copy_brew", { command = BREW_INSTALL }) },
      { id = "act:check", label = t("actions.check") },
      { id = "act:lsp_restart", label = t("actions.restart") },
      { id = "act:show_lsp_status", label = t("actions.status") },
      { id = "dismiss", label = t("actions.dismiss") },
    },
  })
  if not choice or choice == "dismiss" then
    return
  end
  if choice == "copy:curl" then
    copy_command(CURL_INSTALL)
    return
  end
  if choice == "copy:brew" then
    copy_command(BREW_INSTALL)
    return
  end
  local action = choice:match("^act:(.+)$")
  if action == "check" then
    check_project()
    return
  end
  if action then
    local ok, err = gvim.command.run(action)
    if not ok then
      gvim.status(t("status.action_failed", { action = action, error = tostring(err) }))
    end
  end
end

gvim.command.register({
  name = "check_project",
  title = t("cmd.check_project"),
  description = t("cmd.check_project_desc"),
  run = check_project,
})

gvim.command.register({
  name = "copy_install",
  title = t("cmd.copy_install"),
  description = t("cmd.copy_install_desc"),
  run = function()
    copy_command(install_command())
  end,
})

gvim.command.register({
  name = "help",
  title = t("cmd.help"),
  description = t("cmd.help_desc"),
  run = show_help,
})

gvim.log.debug("deno_lsp: Plugin loaded")
