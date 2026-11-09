-- Rust language-server helper: a reduced port of Fresh's plugins/rust-lsp.ts.
--
-- Fresh's plugin owns the Rust LSP error flow (a contributed "Plugin actions" slice in
-- the native LSP-Servers popup, recovery detection, disable, and a "Configure Mode"
-- command that registers a full/reduced-memory rust-analyzer). gvim's plugin API has no
-- LSP-menu contribution, language-disable or register/restart-for-language seam, so this
-- port keeps the user-visible install/help half as explicit commands: copy the official
-- rustup command, copy the Homebrew alternative, check rust-analyzer on the workspace (its
-- --version), and a troubleshooting popup that reuses the native Start/Restart and
-- Show LSP Status actions. No automatic LSP error status, contributed menu rows,
-- disable action or reduced-memory registration are ported (see porting.md). The native
-- Rust LSP server is gvim's built-in entry and is not controlled by this plugin: the
-- enable/disable toggle here only affects these helper commands.
local gvim = require "gvim"

local t = gvim.i18n.t

local RUSTUP_COMMAND = "rustup component add rust-analyzer"
local BREW_COMMAND = "brew install rust-analyzer"
local VERSION_TIMEOUT_MS = 5000
local VERSION_MAX_OUTPUT = 64 * 1024

local function first_line(text)
  return (tostring(text or ""):match("^[^\r\n]*")) or ""
end

local function copy_command(value)
  local ok, err = gvim.clipboard.set(value)
  if not ok then
    gvim.status(t("status.copy_failed", { error = tostring(err) }))
    return
  end
  gvim.status(t("status.copied", { value = value }))
end

-- check_server probes "rust-analyzer --version" through gvim.proc, which follows the
-- workspace authority (local or remote). The plugin API's LookPath is documented as a
-- local-machine lookup, so a missing entry there must never be reported as "missing" in a
-- remote workspace: a failed probe is reported as "could not run on the workspace" with
-- the local-PATH limitation named, not as an authoritative not-found.
local function check_server()
  local result, err = gvim.proc.run(
    { "rust-analyzer", "--version" },
    { timeout_ms = VERSION_TIMEOUT_MS, max_output = VERSION_MAX_OUTPUT }
  )
  if not result then
    gvim.status(t("status.probe_failed", { error = tostring(err), rustup = RUSTUP_COMMAND }))
    return
  end
  -- result.ok covers non-zero exits plus a signal or timeout; checking only code ~= 0
  -- would report a probe that timed out or was killed as runnable.
  if not result.ok then
    local detail
    if result.timed_out then
      detail = t("status.timeout")
    elseif result.signal and result.signal ~= "" then
      detail = t("status.signal", { signal = tostring(result.signal) })
    else
      detail = first_line(result.stderr)
      if detail == "" then
        detail = t("status.exit_code", { code = tostring(result.code) })
      end
    end
    gvim.status(t("status.version_failed", { error = detail }))
    return
  end
  local version = first_line(result.stdout)
  if version == "" then
    version = t("status.version_unknown")
  end
  gvim.status(t("status.found", { version = version }))
end

-- show_help is the troubleshooting popup: the fresh install commands, the workspace
-- probe, the native LSP actions and the common rust-analyzer setup notes.
local function show_help()
  local message = t("help.message")
  local choice = gvim.ui.action_popup({
    title = t("help.title"),
    message = message,
    actions = {
      { id = "copy:rustup", label = t("actions.copy_rustup", { command = RUSTUP_COMMAND }) },
      { id = "copy:brew", label = t("actions.copy_brew", { command = BREW_COMMAND }) },
      { id = "act:check", label = t("actions.check") },
      { id = "act:lsp_restart", label = t("actions.restart") },
      { id = "act:show_lsp_status", label = t("actions.status") },
      { id = "dismiss", label = t("actions.dismiss") },
    },
  })
  if not choice or choice == "dismiss" then
    return
  end
  if choice == "copy:rustup" then
    copy_command(RUSTUP_COMMAND)
    return
  end
  if choice == "copy:brew" then
    copy_command(BREW_COMMAND)
    return
  end
  local action = choice:match("^act:(.+)$")
  if action == "check" then
    check_server()
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
  name = "copy_rustup",
  title = t("cmd.copy_rustup"),
  description = t("cmd.copy_rustup_desc"),
  run = function()
    copy_command(RUSTUP_COMMAND)
  end,
})

gvim.command.register({
  name = "copy_brew",
  title = t("cmd.copy_brew"),
  description = t("cmd.copy_brew_desc"),
  run = function()
    copy_command(BREW_COMMAND)
  end,
})

gvim.command.register({
  name = "check",
  title = t("cmd.check"),
  description = t("cmd.check_desc"),
  run = function()
    check_server()
  end,
})

gvim.command.register({
  name = "help",
  title = t("cmd.help"),
  description = t("cmd.help_desc"),
  run = function()
    show_help()
  end,
})

gvim.log.debug("rust_lsp: Plugin loaded")
