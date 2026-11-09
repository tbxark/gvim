-- Slang shader language-server helper: a user-facing port of Fresh's plugins/slang-lsp.ts.
--
-- Fresh's plugin is an LSP URI-scheme provider: it claims slang-synth:// and, on
-- lsp_open_external_uri, dumps builtin modules with slangd --print-builtin-module so
-- go-to-definition on builtins opens the generated source. gvim's plugin API has no
-- LSP URI-scheme claim or external-URI hook, so that half is not portable here (the
-- editor keeps its external-location fallback; documented in porting.md). This port
-- keeps the install/verification/help surface as three explicit commands.
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

-- show_help explains the two slangd-specific pieces Fresh's plugin covers plus the
-- gvim configuration/verification steps this plugin can support.
local function show_help()
  local choice = gvim.ui.action_popup({
    title = t("help.title"),
    message = t("help.message"),
    actions = {
      { id = "copy:install", label = t("actions.copy_install", { command = INSTALL_COMMAND }) },
      { id = "act:check", label = t("actions.check") },
      { id = "act:lsp_restart", label = t("actions.restart") },
      { id = "act:show_lsp_status", label = t("actions.status") },
      { id = "dismiss", label = t("actions.dismiss") },
    },
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
