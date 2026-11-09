-- Welcome plugin: a port of Fresh's plugins/welcome.ts (78 lines).
--
-- Fresh's welcome plugin is deliberately a plugin-API demo, not a feature: it registers five
-- palette commands, two dispatching built-in editor actions (show_help, save) and three
-- inserting text through the plugin API. This port keeps the same five commands and the same
-- user-visible outcome; gvim has no plugin status message on load (Fresh's comments mention a
-- welcome message but the only setStatus calls are the per-command ones), and the hard-coded
-- "//" comment prefix is adapted to the active buffer's language instead of assuming a C-like
-- language.
local gvim = require "gvim"

local t = gvim.i18n.t

local function current_buffer()
  local buf = gvim.buf.current()
  if not buf then
    gvim.status(t("status.no_buffer"))
    return nil
  end
  return buf
end

-- insert_at_cursor inserts text at the primary caret, like Fresh's insertAtCursor: it is a
-- plain buffer edit (no auto-indent or bracket completion) and one undo step.
local function insert_at_cursor(text)
  local buf = current_buffer()
  if not buf then
    return false
  end
  local pos = buf:cursor()
  local ok, err = buf:insert(pos, text)
  if not ok then
    gvim.status(t("status.insert_failed", { error = tostring(err) }))
    return false
  end
  return true
end

local function run_action(action, label)
  local ok, err = gvim.command.run(action)
  if not ok then
    gvim.status(t("status.action_failed", { action = label, error = tostring(err) }))
  end
end

-- comment_prefix picks a line-comment leader from the active buffer's language, so the demo
-- comment is valid in more files than Fresh's fixed "//". Unknown languages keep "//".
local function comment_prefix(language)
  if language == "lua" then
    return "--"
  end
  if language == "python" or language == "ruby" or language == "bash" or
      language == "yaml" or language == "toml" or language == "makefile" or
      language == "dockerfile" then
    return "#"
  end
  if language == "sql" then
    return "--"
  end
  return "//"
end

gvim.command.register({
  name = "open_help",
  title = t("cmd.open_help"),
  description = t("cmd.open_help_desc"),
  run = function()
    run_action("show_help", "Open Help")
  end,
})

gvim.command.register({
  name = "save_file",
  title = t("cmd.save_file"),
  description = t("cmd.save_file_desc"),
  run = function()
    run_action("save", "Save File")
  end,
})

gvim.command.register({
  name = "say_hello",
  title = t("cmd.say_hello"),
  description = t("cmd.say_hello_desc"),
  run = function()
    if insert_at_cursor(t("text.greeting") .. "\n") then
      gvim.status(t("status.greeting_inserted"))
    end
  end,
})

gvim.command.register({
  name = "insert_time",
  title = t("cmd.insert_time"),
  description = t("cmd.insert_time_desc"),
  run = function()
    local time = os.date("%H:%M:%S")
    if insert_at_cursor(t("text.time", { time = time }) .. "\n") then
      gvim.status(t("status.time_inserted"))
    end
  end,
})

gvim.command.register({
  name = "insert_comment",
  title = t("cmd.insert_comment"),
  description = t("cmd.insert_comment_desc"),
  run = function()
    local buf = current_buffer()
    if not buf then
      return
    end
    local prefix = comment_prefix(buf:language())
    if insert_at_cursor(prefix .. " " .. t("text.comment") .. "\n") then
      gvim.status(t("status.comment_inserted"))
    end
  end,
})

gvim.log.debug("welcome: Plugin loaded")
