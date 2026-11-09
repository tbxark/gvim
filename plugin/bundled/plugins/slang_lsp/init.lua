-- Slang shader language-server helper: a user-facing port of Fresh's plugins/slang-lsp.ts.
--
-- Fresh's plugin is an LSP URI-scheme provider: it claims slang-synth:// and, on
-- lsp_open_external_uri, dumps builtin modules with slangd --print-builtin-module so
-- go-to-definition on builtins opens the generated source. gvim now implements that
-- external-location flow in native core (internal/app/slang_synth.go); this opt-in
-- helper keeps only the install/verification/help surface as three explicit commands
-- and does not handle synth URIs itself. The native flow is local-authority only and
-- explicitly refuses a remote workspace (no local fallback).
local gvim = require "gvim"

local t = gvim.i18n.t

-- Slang has no single official package-manager command: the upstream install page
-- points at binary releases. The copied text is the official release instruction
-- (not a shell command), and the help popup explains putting the binaries on PATH.
local INSTALL_COMMAND = "Download the Slang binary release from https://github.com/shader-slang/slang/releases and put slangc and slangd on PATH"
local TIMEOUT_MS = 5000
local MAX_OUTPUT = 64 * 1024

local function first_line(text)
  return (tostring(text or ""):match("^[^\r\n]*")) or ""
end

local SLANG_EXTENSIONS = { slang = true, slangh = true }

local function is_slang_path(path)
  local name = tostring(path or ""):lower()
  local ext = name:match("%.([^.]+)$")
  return ext ~= nil and SLANG_EXTENSIONS[ext] == true
end

local function active_slang_buffer()
  local buf = gvim.buf.current()
  if not buf or not is_slang_path(buf:path()) then
    return nil
  end
  -- gvim's language id must be exactly slang. A .slang/.slangh buffer detected as text
  -- (for example .slangh without a configured languages.slang entry) is not a Slang
  -- buffer and must never pass "text" to the native language mute.
  if tostring(buf:language() or ""):lower() ~= "slang" then
    return nil
  end
  return buf
end

-- disable_slang_lsp is the controlled native language disable: re-check the active
-- Slang buffer and the LSP feature, carry that buffer's path/language, and let the TUI
-- re-validate the exact target before the root-level LSP-port mute.
local function disable_slang_lsp()
  local buf = active_slang_buffer()
  if not buf then
    gvim.status(t("status.not_slang"))
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
    gvim.status(t("status.action_failed", { action = "lsp_disable_language_target", error = tostring(err) }))
  end
end

local function proc_output(result)
  local out = first_line(result.stdout)
  if out == "" then
    out = first_line(result.stderr)
  end
  if out == "" then
    out = t("status.no_output")
  end
  return out
end

-- check_tool runs one tool through gvim.proc (which follows the workspace authority)
-- and returns a status fragment. A start failure is reported from the actual error:
-- it is never turned into a missing/not-found claim, because the same error can come
-- from a remote authority, a sandbox or a broken executable. A non-ok result is
-- reported with its exit code and first output line.
local function check_tool(argv, label)
  local result, err = gvim.proc.run(argv, { timeout_ms = TIMEOUT_MS, max_output = MAX_OUTPUT })
  if not result then
    return t("status.start_failed", { tool = label, error = tostring(err) })
  end
  if not result.ok then
    local detail = first_line(result.stderr)
    if detail == "" then
      detail = first_line(result.stdout)
    end
    if detail == "" then
      detail = t("status.no_output")
    end
    return t("status.exit_failed", { tool = label, code = tostring(result.code), detail = detail })
  end
  return t("status.found", { tool = label, version = proc_output(result) })
end

-- check_slang runs both shader tools. One failing does not hide the other's answer;
-- the combined status keeps each honest fragment.
local function check_slang()
  local parts = {
    check_tool({ "slangd", "--version" }, "slangd"),
    check_tool({ "slangc", "-v" }, "slangc"),
  }
  gvim.status(table.concat(parts, "; "))
end

local function copy_install()
  local ok, err = gvim.clipboard.set(INSTALL_COMMAND)
  if not ok then
    gvim.status(t("status.copy_failed", { error = tostring(err) }))
    return
  end
  gvim.status(t("status.copied", { value = INSTALL_COMMAND }))
end

-- show_help explains the configuration/verification steps this helper supports, the native
-- local-only slang-synth flow, and the plugin API gap it does not expose.
local function show_help()
  local actions = {
    { id = "copy:install", label = t("actions.copy_install", { command = INSTALL_COMMAND }) },
    { id = "act:check", label = t("actions.check") },
    { id = "act:lsp_restart", label = t("actions.restart") },
    { id = "act:show_lsp_status", label = t("actions.status") },
  }
  if active_slang_buffer() and gvim.has("lsp") then
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
  if choice == "copy:install" then
    copy_install()
    return
  end
  local action = choice:match("^act:(.+)$")
  if action == "check" then
    check_slang()
    return
  end
  if action == "lsp_disable_language" then
    disable_slang_lsp()
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
  name = "check",
  title = t("cmd.check"),
  description = t("cmd.check_desc"),
  run = function()
    check_slang()
  end,
})

gvim.command.register({
  name = "copy_install",
  title = t("cmd.copy_install"),
  description = t("cmd.copy_install_desc"),
  run = function()
    copy_install()
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

gvim.log.debug("slang_lsp: Plugin loaded")
