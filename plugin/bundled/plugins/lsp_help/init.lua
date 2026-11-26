-- LSP install help: a port of Fresh's plugins/lsp_help.ts (660 lines, mostly a table of
-- per-language server install hints; rust/asm/slang keep their own files in Fresh).
--
-- Fresh's two active handlers map onto gvim's events:
--   lsp_server_error   -> remember the failure, put a Fresh-style status line on the row
--   lsp_status_clicked -> open an action popup with the entry's message, copyable install
--                         commands and gvim-native LSP quick actions, and await its choice
--
-- Reduction vs Fresh: gvim's lsp_server_error payload is only {error} (no language,
-- error_type or server_command), so the language is the active buffer's and the server
-- command in the message is the entry's documented default; the not-found status is
-- inferred from the error text. gvim's LSP status click also opens the native server list,
-- so this popup stacks on top of it (dismissing ours reveals it). There is no
-- workspace-symbol quick action in gvim's command table, so the popup offers definition,
-- references, restart, stop, per-buffer toggle and disable. Fresh has no i18n file; the
-- generic strings here are translated and the per-server install hints stay English.
local gvim = require "gvim"

local t = gvim.i18n.t

-- by_language maps a language id to its install hints. The table (about forty KiB of strings
-- and tables) is read from lsp_help.servers on the first language server error: while every
-- server works, the plugin costs the editor next to nothing.
local by_language

local function help_for(language)
  if by_language == nil then
    by_language = {}
    for _, help in ipairs(require "lsp_help.servers") do
      for _, id in ipairs(help.languages) do
        if by_language[id] == nil then
          by_language[id] = help
        end
      end
    end
  end
  return by_language[language]
end

-- One popup takes at most 16 actions: keep room for the seven native rows below.
local MAX_COPIES = 8
local MAX_MESSAGE = 4000

-- failures: language -> true once a server of that language failed; popup_open guards
-- against stacking duplicate popups while one request is open.
local failures = {}
local popup_open = false

local function first_line(text)
  text = tostring(text or "")
  local cut = text:find("\n", 1, true)
  if cut then
    text = text:sub(1, cut - 1)
  end
  return gvim.text.trim(text)
end

local function clip(text, limit)
  text = tostring(text or "")
  if #text <= limit then
    return text
  end
  if limit <= 3 then
    return gvim.text.truncate(text, limit, "")
  end
  local cut = text:sub(1, limit - 3)
  local i = #cut
  while i > 0 do
    local b = cut:byte(i)
    if b < 0x80 then
      break
    end
    if b >= 0xC0 then
      cut = cut:sub(1, i - 1)
      break
    end
    i = i - 1
  end
  return cut .. "..."
end

local function active_language()
  local buf = gvim.buf.current()
  if not buf or buf:is_virtual() then
    return nil
  end
  return buf:language()
end

local function expand(template, help)
  return clip((template:gsub("{server}", help.server)), MAX_MESSAGE)
end

local function looks_not_found(error_text)
  local lower = (error_text or ""):lower()
  return lower:find("not found", 1, true) ~= nil
    or lower:find("no such file", 1, true) ~= nil
    or lower:find("executable file not found", 1, true) ~= nil
end

gvim.on("lsp_server_error", function(ev)
  local language = active_language()
  local help = language and help_for(language)
  if not help then
    return
  end
  local message = first_line(ev and ev.error)
  failures[language] = true
  gvim.log.debug("lsp_help: %s: %s", help.id, clip(message, 200))
  if looks_not_found(message) then
    gvim.notify(t("status.server_not_found", { name = help.name }))
  else
    gvim.notify(t("status.server_error", { name = help.name, error = clip(message, 160) }))
  end
end)

local function popup_actions(help)
  local actions = {}
  local copies = help.copies or {}
  for i = 1, math.min(#copies, MAX_COPIES) do
    local copy = copies[i]
    actions[#actions + 1] = {
      id = "copy:" .. copy.id,
      label = clip(t("popup.copy", { value = copy.value }), 200),
    }
  end
  local quick = {
    { id = "act:lsp_restart", label = t("actions.restart") },
    { id = "act:lsp_stop", label = t("actions.stop") },
    { id = "act:lsp_toggle_for_buffer", label = t("actions.toggle_buffer") },
    { id = "act:lsp_disable_language_target", label = t("actions.disable", { name = help.name }) },
    { id = "act:lsp_goto_definition", label = t("actions.definition") },
    { id = "act:lsp_references", label = t("actions.references") },
    { id = "dismiss", label = t("actions.dismiss") },
  }
  for _, action in ipairs(quick) do
    actions[#actions + 1] = action
  end
  return actions
end

local function handle_action(language, help, action)
  if action == nil or action == "dismiss" then
    return
  end
  if action:sub(1, 5) == "copy:" then
    local wanted = action:sub(6)
    for _, copy in ipairs(help.copies or {}) do
      if copy.id == wanted then
        gvim.clipboard.set(copy.value)
        gvim.notify(t("status.copied", { value = copy.value }))
        return
      end
    end
    return
  end
  if action:sub(1, 4) == "act:" then
    local name = action:sub(5)
    if name == "lsp_disable_language_target" then
      local buf = gvim.buf.current()
      local path = buf and buf:path() or ""
      local ok = gvim.command.run(name, { path = path, language = language })
      if ok ~= false then
        gvim.notify(t("status.disabled", { name = help.name }))
        failures[language] = nil
      end
    else
      gvim.command.run(name)
    end
  end
end

local function show_help(language, help)
  if not failures[language] then
    return
  end
  if popup_open then
    -- One popup at a time: a second click while ours is up is the same request.
    return
  end
  local actions = popup_actions(help)
  popup_open = true
  local title = clip(t("popup.title", { name = help.name }), 120)
  local message = expand(help.message, help)
  gvim.async.spawn(function()
    local action, err = gvim.ui.action_popup { title = title, message = message, actions = actions }
    popup_open = false
    if err then
      return
    end
    handle_action(language, help, action)
  end)
end

gvim.on("lsp_status_clicked", function()
  local language = active_language()
  if not language then
    return
  end
  local help = help_for(language)
  if not help or not failures[language] then
    return
  end
  show_help(language, help)
end)

gvim.log.debug("lsp help plugin loaded")
