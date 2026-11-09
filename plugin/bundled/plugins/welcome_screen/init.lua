-- Welcome screen: a reduced port of Fresh's plugins/welcome_screen.ts.
--
-- Fresh's page is a virtual buffer plus a page widget panel: wordmark, doors, verbs,
-- a Contents section, and a long scrolled document. This port keeps a bounded, real
-- core in one read-only virtual buffer using existing plugin APIs:
--
--   * an ASCII wordmark and a one-line description;
--   * doors (open a file, new buffer, recent files);
--   * verbs (find file, command palette, settings, explorer, help), all real actions;
--   * dashboard-section rows from gvim.dashboard.sections(), with an explicit empty state;
--   * session recent-file rows (bounded), usable with j/k, arrows, Enter and mouse targets.
--
-- It intentionally does NOT claim Fresh's page-widget-panel behavior, Contents/focus/scroll
-- widget machinery, onboarding levels, orchestrator/workspace/agent cards, auto-attach, or
-- hover styling. The reduced scope is documented in docs/plugins/bundled.md and porting.md.
local gvim = require "gvim"

local t = gvim.i18n.t

local settings = gvim.settings.define {
  auto_open = {
    type = "boolean", default = true,
    description = "Open the welcome page as a background tab when the editor starts",
  },
}

local MAX_RECENT = 8
local MAX_SECTIONS = 8
local MAX_BUFFER_SCAN = 64
local ACTION_IDS = { "open", "new", "quick_open_buffers", "quick_open_files", "command_palette", "open_settings", "toggle_file_explorer", "show_help" }
local WORDMARK = {
  "  ██████╗ ██╗   ██╗██╗███╗   ███╗",
  " ██╔════╝ ██║   ██║██║████╗ ████║",
  " ██║  ███╗██║   ██║██║██╔████╔██║",
  " ╚██████╔╝╚██████╔╝██║██║ ╚═╝ ██║",
  "  ╚═════╝  ╚═════╝ ╚═╝╚═╝     ╚═╝",
}

local STYLE = {
  wordmark = { fg = "syntax.function", bold = true },
  tagline = { fg = "syntax.keyword", bold = true },
  heading = { fg = "syntax.keyword", bold = true },
  muted = { fg = "editor.line_number_fg" },
  accent = { fg = "syntax.function" },
  value = { fg = "syntax.string" },
}

local state = {
  buf = nil,
  focus = 1,
  rows = {},
  recent = {},
  opened_auto = false,
}

local function valid_buf()
  local buf = state.buf
  if buf and buf:valid() then
    return buf
  end
  state.buf = nil
  return nil
end

-- Unicode Cc/Cf handling for display text. Cc controls become spaces; Cf format marks
-- (bidi overrides/isolates, zero-width/word-joiner characters, BOM, tags, ...) are removed,
-- and invalid UTF-8 bytes become spaces. Newlines therefore cannot inject a new page line or
-- move a line target: row() computes the target from the cleaned single-line text.
-- The range table is the assigned Cf set as of Unicode 15.1, matching the dashboard's
-- existing strip_pr_format sanitizer.
local CF_RANGES = {
  { 0x00AD, 0x00AD }, { 0x0600, 0x0605 }, { 0x061C, 0x061C }, { 0x06DD, 0x06DD },
  { 0x070F, 0x070F }, { 0x0890, 0x0891 }, { 0x08E2, 0x08E2 }, { 0x180E, 0x180E },
  { 0x200B, 0x200F }, { 0x202A, 0x202E }, { 0x2060, 0x2064 }, { 0x2066, 0x206F },
  { 0xFEFF, 0xFEFF }, { 0xFFF9, 0xFFFB }, { 0x110BD, 0x110BD }, { 0x110CD, 0x110CD },
  { 0x13430, 0x1343F }, { 0x1BCA0, 0x1BCA3 }, { 0x1D173, 0x1D17A },
  { 0xE0001, 0xE0001 }, { 0xE0020, 0xE007F },
}

local function is_cf(cp)
  for _, range in ipairs(CF_RANGES) do
    if cp >= range[1] and cp <= range[2] then
      return true
    end
  end
  return false
end

local function utf8_codepoint(text, i)
  local b1 = text:byte(i)
  if not b1 then
    return nil, nil
  end
  if b1 < 0x80 then
    return b1, 1
  end
  if b1 >= 0xC2 and b1 <= 0xDF then
    local b2 = text:byte(i + 1)
    if b2 and b2 >= 0x80 and b2 <= 0xBF then
      return (b1 - 0xC0) * 0x40 + (b2 - 0x80), 2
    end
  elseif b1 >= 0xE0 and b1 <= 0xEF then
    local b2, b3 = text:byte(i + 1), text:byte(i + 2)
    if b2 and b3 and b2 >= 0x80 and b2 <= 0xBF and b3 >= 0x80 and b3 <= 0xBF then
      local cp = (b1 - 0xE0) * 0x1000 + (b2 - 0x80) * 0x40 + (b3 - 0x80)
      if cp >= 0x800 and not (cp >= 0xD800 and cp <= 0xDFFF) then
        return cp, 3
      end
    end
  elseif b1 >= 0xF0 and b1 <= 0xF4 then
    local b2, b3, b4 = text:byte(i + 1), text:byte(i + 2), text:byte(i + 3)
    if b2 and b3 and b4 and b2 >= 0x80 and b2 <= 0xBF and b3 >= 0x80 and b3 <= 0xBF and b4 >= 0x80 and b4 <= 0xBF then
      local cp = (b1 - 0xF0) * 0x40000 + (b2 - 0x80) * 0x1000 + (b3 - 0x80) * 0x40 + (b4 - 0x80)
      if cp >= 0x10000 and cp <= 0x10FFFF then
        return cp, 4
      end
    end
  end
  return nil, 1
end

local function strip_format(text, limit)
  local out = {}
  local raw = text:sub(1, limit * 4)
  local i = 1
  while i <= #raw do
    local cp, width = utf8_codepoint(raw, i)
    if not cp or width == nil then
      out[#out + 1] = " "
      i = i + 1
    elseif cp <= 0x1F or cp == 0x7F or (cp >= 0x80 and cp <= 0x9F) then
      out[#out + 1] = " "
      i = i + width
    elseif is_cf(cp) then
      i = i + width
    else
      out[#out + 1] = raw:sub(i, i + width - 1)
      i = i + width
    end
  end
  return table.concat(out)
end

local function sanitize(value, max)
  local text = type(value) == "string" and value or ""
  text = strip_format(text, max):gsub("%s+", " ")
  text = text:match("^%s*(.-)%s*$") or ""
  if #text > max then
    text = gvim.text.truncate(text, max, "…")
  end
  return text
end

local function remember_recent(path)
  if type(path) ~= "string" or path == "" then
    return
  end
  for i, old in ipairs(state.recent) do
    if old == path then
      table.remove(state.recent, i)
      break
    end
  end
  table.insert(state.recent, 1, path)
  while #state.recent > MAX_RECENT do
    table.remove(state.recent)
  end
end

local function seed_recent_from_buffers()
  local ok, list = pcall(gvim.buf.list)
  if not ok or type(list) ~= "table" then
    return
  end
  local scanned = 0
  for _, buf in ipairs(list) do
    if scanned >= MAX_BUFFER_SCAN then
      break
    end
    scanned = scanned + 1
    local info = buf:info()
    if info and not info.virtual and info.path and info.path ~= "" then
      remember_recent(info.path)
    end
  end
end

-- The dashboard exposes registered external-section metadata, not a rendered snapshot.
-- These rows therefore name the available sections; with none, the page says so.
local function dashboard_sections()
  local ok, list = pcall(gvim.dashboard.sections)
  if not ok or type(list) ~= "table" then
    return {}
  end
  local out = {}
  for _, sec in ipairs(list) do
    if #out >= MAX_SECTIONS then
      break
    end
    if type(sec) == "table" then
      local title = sanitize(sec.title, 72)
      if title == "" then
        title = sanitize(sec.name, 72)
      end
      if title ~= "" then
        out[#out + 1] = title
      end
    end
  end
  return out
end

local function push_line(lines, text, style)
  if style then
    lines[#lines + 1] = { segments = { { text = text, style = style } } }
  else
    lines[#lines + 1] = { text = text }
  end
end

local function build_page()
  local lines, rows = {}, {}
  local function blank()
    lines[#lines + 1] = { text = "" }
  end
  local function heading(text)
    push_line(lines, text, STYLE.heading)
    push_line(lines, string.rep("─", 58), STYLE.muted)
  end
  local function row(label, detail, kind, value, target)
    label = sanitize(label, 72)
    detail = sanitize(detail, 512)
    local index = #rows + 1
    local marker = index == state.focus and "› " or "  "
    local style = index == state.focus and STYLE.accent or STYLE.value
    local segments = { { text = marker .. label, style = style } }
    if detail ~= "" then
      segments[#segments + 1] = { text = "   " .. detail, style = STYLE.muted }
    end
    lines[#lines + 1] = { segments = segments }
    rows[#rows + 1] = {
      kind = kind,
      value = value,
      target = target,
      line = #lines - 1,
    }
  end

  for _, line in ipairs(WORDMARK) do
    push_line(lines, line, STYLE.wordmark)
  end
  push_line(lines, t("wordmark.tagline"), STYLE.tagline)
  blank()

  heading(t("section.dashboard"))
  local sections = dashboard_sections()
  if #sections == 0 then
    push_line(lines, "  " .. t("section.empty_dashboard"), STYLE.muted)
  else
    for _, title in ipairs(sections) do
      row(title, t("section.dashboard_detail"), "action", "dashboard.show", "welcome_screen.click_dashboard")
    end
  end
  blank()

  heading("DOORS")
  row(t("door.open.label"), t("door.open.detail"), "action", "open", "welcome_screen.click_action_1")
  row(t("door.new.label"), t("door.new.detail"), "action", "new", "welcome_screen.click_action_2")
  row(t("door.recent.label"), t("door.recent.detail"), "action", "quick_open_buffers", "welcome_screen.click_action_3")

  heading(t("section.recent"))
  if #state.recent == 0 then
    push_line(lines, "  " .. t("section.empty_recent"), STYLE.muted)
  else
    for i, path in ipairs(state.recent) do
      row(sanitize(gvim.path.basename(path), 72), path, "recent", path, "welcome_screen.click_recent_" .. i)
    end
  end
  blank()

  heading("VERBS")
  row(t("verb.find.label"), t("verb.find.detail"), "action", "quick_open_files", "welcome_screen.click_action_4")
  row(t("verb.palette.label"), t("verb.palette.detail"), "action", "command_palette", "welcome_screen.click_action_5")
  row(t("verb.settings.label"), t("verb.settings.detail"), "action", "open_settings", "welcome_screen.click_action_6")
  row(t("verb.explorer.label"), t("verb.explorer.detail"), "action", "toggle_file_explorer", "welcome_screen.click_action_7")
  row(t("verb.help.label"), t("verb.help.detail"), "action", "show_help", "welcome_screen.click_action_8")
  blank()
  push_line(lines, "  " .. t("hint"), STYLE.muted)
  return lines, rows
end

local function publish_targets(buf)
  local set = gvim.buf.set_line_targets
  if type(set) ~= "function" then
    return
  end
  local targets = {}
  for _, item in ipairs(state.rows) do
    if item.kind == "recent" and item.value then
      targets[#targets + 1] = { line = item.line, path = item.value }
    elseif item.kind == "action" and item.target then
      targets[#targets + 1] = { line = item.line, action = item.target }
    end
  end
  pcall(set, buf, targets)
end

local function paint()
  local buf = valid_buf()
  if not buf then
    return
  end
  local lines, rows = build_page()
  if #rows == 0 then
    state.focus = 1
  elseif state.focus > #rows then
    state.focus = #rows
  end
  state.rows = rows
  local ok, err = buf:set_content(lines)
  if not ok then
    if gvim.err_code(err) == "closed" then
      state.buf = nil
    end
    return
  end
  publish_targets(buf)
end

local function refresh()
  seed_recent_from_buffers()
  paint()
end

local function run_action(action)
  local ok, ran, err = pcall(gvim.command.run, action, nil)
  if not ok then
    gvim.notify(t("status.action_failed", { error = tostring(ran) }))
  elseif not ran then
    gvim.notify(t("status.action_failed", { error = tostring(err) }))
  end
end

local function open_path(path)
  if type(path) ~= "string" or path == "" then
    return
  end
  local ok, err = pcall(gvim.buf.open, path, { focus = true })
  if not ok then
    gvim.notify(t("status.action_failed", { error = tostring(err) }))
  end
end

local function open_recent(i)
  open_path(state.recent[i])
end

local function focus_by(delta)
  local n = #state.rows
  if n == 0 then
    return
  end
  state.focus = ((state.focus - 1 + delta) % n) + 1
  paint()
end

local function activate()
  local row = state.rows[state.focus]
  if not row then
    return
  end
  if row.kind == "recent" then
    open_path(row.value)
  elseif row.kind == "action" and row.value then
    run_action(row.value)
  end
end

local function close_page()
  local buf = valid_buf()
  if buf then
    pcall(gvim.buf.clear_line_targets, buf)
    pcall(buf.close, buf, { force = true })
  end
  state.buf = nil
  state.rows = {}
end

local function ensure_buffer(focus)
  local buf = valid_buf()
  if buf then
    if focus then
      pcall(buf.show, buf)
    end
    refresh()
    return buf
  end
  local created, err = gvim.buf.create {
    name = "Welcome",
    read_only = true,
    focus = focus,
    mode = "welcome_screen",
  }
  if not created then
    gvim.notify("Welcome screen: " .. tostring(err))
    return nil
  end
  state.buf = created
  state.focus = 1
  pcall(created.set_option, created, "line_numbers", false)
  pcall(created.set_option, created, "wrap", false)
  pcall(created.set_mode, created, "welcome_screen")
  refresh()
  return created
end

local function auto_open()
  if not settings.auto_open or state.opened_auto then
    return
  end
  state.opened_auto = true
  -- Fresh opens the page as a background tab and never steals the foreground.
  ensure_buffer(false)
end

local function register_hidden(name, run)
  gvim.command.register { name = name, title = name, hidden = true, run = run }
end

for i = 1, #ACTION_IDS do
  local action = ACTION_IDS[i]
  register_hidden("click_action_" .. i, function()
    run_action(action)
  end)
end
for i = 1, MAX_RECENT do
  register_hidden("click_recent_" .. i, function()
    open_recent(i)
  end)
end
register_hidden("click_dashboard", function()
  run_action("dashboard.show")
end)
register_hidden("focus_next", function() focus_by(1) end)
register_hidden("focus_prev", function() focus_by(-1) end)
register_hidden("activate", activate)
register_hidden("close", close_page)
register_hidden("refresh", refresh)

gvim.mode.define {
  name = "welcome_screen",
  status = "WELCOME",
  text = "swallow",
  paste = "swallow",
  fallthrough = true,
  bindings = {
    tab = "welcome_screen.focus_next",
    down = "welcome_screen.focus_next",
    j = "welcome_screen.focus_next",
    up = "welcome_screen.focus_prev",
    k = "welcome_screen.focus_prev",
    enter = "welcome_screen.activate",
    q = "welcome_screen.close",
    escape = "welcome_screen.close",
    r = "welcome_screen.refresh",
  },
}

gvim.command.register {
  name = "show",
  title = "Welcome Screen: Show",
  description = "Open the welcome page and focus it",
  run = function()
    ensure_buffer(true)
  end,
}

gvim.on("plugins_loaded", auto_open)
gvim.on("ready", auto_open)

gvim.on("after_file_open", function(ev)
  local buf = ev and ev.buf
  local info = buf and buf:info()
  if info and not info.virtual and info.path and info.path ~= "" then
    remember_recent(info.path)
    if valid_buf() then
      paint()
    end
  end
end)

gvim.on("buffer_activated", function(ev)
  local buf = valid_buf()
  if buf and ev and ev.buf and ev.buf.id == buf.id then
    refresh()
  end
end)

gvim.on("theme_changed", function()
  if valid_buf() then
    paint()
  end
end)

gvim.plugin.on_unload(function()
  local buf = valid_buf()
  if buf then
    pcall(gvim.buf.clear_line_targets, buf)
  end
end)

gvim.log.debug("welcome_screen: Plugin loaded")
