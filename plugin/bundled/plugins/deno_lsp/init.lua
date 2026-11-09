-- Deno language-server helper: a reduced port of Fresh's plugins/deno_lsp.ts.
--
-- Fresh's plugin selects "deno lsp" for TypeScript/JavaScript when the active window's
-- root has deno.json/deno.jsonc and the Deno runtime is available. gvim now ports that
-- selection natively in core (internal/app/deno_lsp.go, docs/lsp.md): the probed workspace
-- root, authority/trust-gated runtime confirmation, lsp_enabled and the explicit/disabled
-- user rules decide it, and this helper plugin's opt-in toggle does not control it. gvim's
-- plugin API has no register/select-language-server call and no active-window list, so this
-- port keeps only the user-visible check/install/help surface as palette commands; Check
-- Project reports both prerequisites through the workspace authority.
local gvim = require "gvim"

local t = gvim.i18n.t

local CURL_INSTALL = "curl -fsSL https://deno.land/install.sh | sh"
local BREW_INSTALL = "brew install deno"
local VERSION_TIMEOUT_MS = 5000
local VERSION_MAX_OUTPUT = 64 * 1024

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

-- install_command copies the official curl installer. The plugin must not choose Homebrew
-- from a local gvim.proc.which: in a remote workspace the clipboard/install command belongs
-- to the remote host, where the local machine's brew tells us nothing. The help popup still
-- lists both alternatives for the user to pick.
local function install_command()
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

-- probe_deno runs "deno --version" through gvim.proc, which follows the workspace
-- authority (local or remote). The plugin API's LookPath is a documented local-machine
-- lookup, so it is never used to declare deno missing: a failed probe is reported as
-- "could not run on the workspace" with that limitation named.
local function probe_deno()
  return gvim.proc.run(
    { "deno", "--version" },
    { timeout_ms = VERSION_TIMEOUT_MS, max_output = VERSION_MAX_OUTPUT }
  )
end

local function first_line(text)
  return (tostring(text or ""):match("^[^\r\n]*")) or ""
end

local DENO_EXTENSIONS = { ts = true, tsx = true, js = true, jsx = true, mjs = true, cjs = true }
local DENO_LANGUAGES = { typescript = true, javascript = true }

-- deno_target is the language the native LSP port must mute, or nil when this is not a
-- recognized Deno TypeScript/JavaScript buffer. Fresh's DENO_LANGUAGES has only
-- typescript/javascript; a text/unrecognized language must never be sent as the target.
-- For .tsx, gvim's native openLanguage maps TypeScript detection to the tsx server; a
-- buffer already identified as tsx is accepted as tsx too. A .tsx detected as JavaScript
-- stays javascript (openLanguage has no tsx rewrite for it).
local function deno_target(buf)
  local path = buf:path()
  if type(path) ~= "string" or path == "" then
    return nil
  end
  local ext = tostring(path):lower():match("%.([^.]+)$")
  if ext == nil or not DENO_EXTENSIONS[ext] then
    return nil
  end
  local language = tostring(buf:language() or ""):lower()
  if ext == "tsx" then
    if language == "typescript" or language == "tsx" then
      return "tsx"
    end
    if language == "javascript" then
      return "javascript"
    end
    return nil
  end
  if DENO_LANGUAGES[language] then
    return language
  end
  return nil
end

local function active_deno_buffer()
  local buf = gvim.buf.current()
  if not buf then
    return nil, nil
  end
  return buf, deno_target(buf)
end

-- disable_deno_lsp is the controlled native language disable: re-check the active
-- TypeScript/JavaScript buffer and the LSP feature, carry that buffer's path/language,
-- and let the TUI re-validate the exact target before the root-level LSP-port mute.
local function disable_deno_lsp()
  local buf, target = active_deno_buffer()
  if not buf or not target then
    gvim.status(t("status.not_deno"))
    return
  end
  if not gvim.has("lsp") then
    gvim.status(t("status.no_lsp"))
    return
  end
  local ran, err = gvim.command.run("lsp_disable_language_target", {
    path = buf:path(),
    language = target,
  })
  if not ran then
    gvim.status(t("status.action_failed", { action = "lsp_disable_language_target", error = tostring(err) }))
  end
end

-- probe_error describes why a completed process is not a successful version check.
-- proc.run's result.ok is false on a non-zero exit, a signal or a timeout, so checking
-- only "result exists" would report a probe that never succeeded as a runnable runtime.
local function probe_error(result)
  if result.timed_out then
    return t("status.timeout")
  end
  if result.signal and result.signal ~= "" then
    return t("status.signal", { signal = tostring(result.signal) })
  end
  local detail = first_line(result.stderr)
  if detail ~= "" then
    return detail
  end
  return t("status.exit_code", { code = tostring(result.code) })
end

local function check_project()
  local root = project_root()
  local configs = project_configs(root)
  local config_text = table.concat(configs, ", ")
  local install = install_command()
  local result, err = probe_deno()
  if not result then
    if #configs > 0 then
      gvim.status(t("status.config_probe_failed", {
        root = root,
        config = config_text,
        install = install,
        error = tostring(err),
      }))
      return
    end
    gvim.status(t("status.no_project_probe_failed", {
      root = root,
      install = install,
      error = tostring(err),
    }))
    return
  end
  -- result.ok is true only for exit code 0 with no signal and no timeout. A non-zero
  -- deno --version is not evidence that the runtime can serve Deno LSP, so it must never
  -- reach the ready/no-config-success branches.
  if not result.ok then
    local detail = probe_error(result)
    if #configs > 0 then
      gvim.status(t("status.version_failed", {
        root = root,
        config = config_text,
        install = install,
        error = detail,
      }))
      return
    end
    gvim.status(t("status.version_failed_no_config", {
      root = root,
      install = install,
      error = detail,
    }))
    return
  end
  if #configs > 0 then
    gvim.status(t("status.ready", { root = root, config = config_text }))
    return
  end
  gvim.status(t("status.deno_no_config", { root = root }))
end

-- show_help explains the reduced port: install commands, the config entry gvim needs, the
-- workspace-authority probe and the native LSP actions. Its copy/check/restart actions reuse
-- the command implementations above, so there is one source of behaviour.
local function show_help()
  local actions = {
    { id = "copy:curl", label = t("actions.copy_curl", { command = CURL_INSTALL }) },
    { id = "copy:brew", label = t("actions.copy_brew", { command = BREW_INSTALL }) },
    { id = "act:check", label = t("actions.check") },
    { id = "act:lsp_restart", label = t("actions.restart") },
    { id = "act:show_lsp_status", label = t("actions.status") },
  }
  local buf, target = active_deno_buffer()
  if buf and target and gvim.has("lsp") then
    actions[#actions + 1] = { id = "act:lsp_disable_language", label = t("actions.disable") }
  end
  actions[#actions + 1] = { id = "dismiss", label = t("actions.dismiss") }
  local choice = gvim.ui.action_popup({
    title = t("help.title"),
    message = t("help.message"),
    actions = actions,
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
  if action == "lsp_disable_language" then
    disable_deno_lsp()
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
