-- Dashboard. A port of Fresh's dashboard plugin.
--
-- The empty editor gets a read-only "dashboard" tab: a centered frame with the
-- repository state, recently visited files, quick actions and disk usage. It is
-- drawn as one styled content write per paint (b:set_content) so a frame never
-- comes out half-written, and every colour is a theme key so a theme switch is
-- picked up by the next paint.
--
-- Reduction vs Fresh (docs/plugins/bundled.md):
--   * weather, GitHub PRs and the network sections are not ported (no network
--     fetches from a bundled plugin); the public third-party section API is not
--     ported either (gvim cannot pass Lua closures between plugin runtimes).
--   * the disk section shells out to df for the workspace mount and $HOME only.
--   * the recent-file rows are line targets (gvim.buf.set_line_targets): a
--     single click opens the file. The command rows cannot be targets because a
--     target opens a file, not an action, so those stay keyboard-only (the
--     buffer's mode binds Tab/Down/j, Shift-Tab/Up/k and Enter, like Fresh's
--     dashboard mode).
--   * sessions are not shown (gvim has no plugin-facing session list); the
--     recent-files section covers the same "get back to work" role.
local gvim = require "gvim"

local settings = gvim.settings.define {
  show_on_empty = {
    type = "boolean", default = true,
    description = "Open the dashboard when the editor has no file buffer (startup and after the last file closes)",
  },
  refresh_seconds = {
    type = "integer", default = 5, minimum = 1, maximum = 60,
    description = "How often the dashboard refreshes its git/disk/clock content while visible",
  },
}

local C = {
  frame = "ui.popup_border_fg",
  title = "syntax.keyword",
  accent = "syntax.function",
  value = "syntax.string",
  number = "syntax.constant",
  muted = "syntax.comment",
  ok = "ui.file_status_added_fg",
  warn = "syntax.constant",
  err = "diagnostic.error_fg",
  focus = "editor.selection_bg",
}
local STYLE = {
  frame = { fg = C.frame },
  title = { fg = C.title, bold = true },
  muted = { fg = C.muted },
  accent = { fg = C.accent },
  value = { fg = C.value },
  number = { fg = C.number },
  ok = { fg = C.ok },
  warn = { fg = C.warn },
  err = { fg = C.err },
  heading = { fg = C.accent, bold = true },
}
local STYLE_FOCUS = { fg = C.accent, bg = C.focus, bold = true }

local INNER_MIN = 24
local INNER_MAX = 110
local SIZE_FRACTION = 0.9
local GIT_TTL_MS = 15000
local DISK_TTL_MS = 60000
local RECENT_MAX = 10
local MAX_ACTIONS = 8
local MAX_RECENT_ROWS = 10

local ACTIONS = {
  { label = "Open Settings", action = "open_settings" },
  { label = "Toggle File Explorer", action = "toggle_file_explorer" },
  { label = "Live Grep", action = "resume_live_grep" },
  { label = "Select Theme", action = "select_theme" },
  { label = "Git Log", action = "git_log.show_git_log" },
  { label = "Diagnostics Panel", action = "diagnostics_panel.toggle_diagnostics_panel" },
}

local state = {
  buf = nil,             -- the dashboard Buffer handle
  auto = false,          -- the current dashboard was opened automatically
  focus = 0,             -- index into rows
  rows = {},             -- { kind = "recent"|"action", label, path?, action? }
  frame = "",            -- last painted frame text (skip identical writes)
  git = nil,             -- { at, branch, counts, url, ahead, behind, base }
  disk = nil,            -- { at, rows = { {mount, pct, free} } }
  timer = nil,
}

local function now_ms()
  return gvim.time.monotonic_ms()
end

local function notify(msg)
  gvim.notify(msg)
end

local function valid_buf()
  local buf = state.buf
  if buf and buf:valid() then
    return buf
  end
  state.buf = nil
  return nil
end

local function file_buffers()
  local out = {}
  for _, buf in ipairs(gvim.buf.list()) do
    local info = buf:info()
    if info and not info.virtual and info.path and info.path ~= "" then
      out[#out + 1] = buf
    end
  end
  return out
end

local function recent_files()
  local stored = gvim.state.get("dashboard.recent", nil)
  if type(stored) ~= "table" then
    return {}
  end
  local out = {}
  for i = 1, math.min(#stored, RECENT_MAX) do
    if type(stored[i]) == "string" and stored[i] ~= "" then
      out[#out + 1] = stored[i]
    end
  end
  return out
end

local function remember_recent(path)
  if not path or path == "" then
    return
  end
  local list = recent_files()
  local out = { path }
  for _, item in ipairs(list) do
    if item ~= path then
      out[#out + 1] = item
    end
    if #out >= RECENT_MAX then
      break
    end
  end
  gvim.state.set("dashboard.recent", out)
end

-- git data ---------------------------------------------------------------

local function git_out(root, args, max)
  if not gvim.git.available() or root == "" then
    return nil
  end
  local r, err = gvim.git.run(args, { cwd = root, timeout_ms = 5000, max_output = max or 65536 })
  if not r or not r.ok then
    return nil
  end
  return r.stdout
end

local function trimmed(s)
  if not s then
    return ""
  end
  return (gvim.text.trim(s))
end

local function default_branch(root)
  local head = git_out(root, { "symbolic-ref", "--short", "refs/remotes/origin/HEAD" }, 4096)
  if head then
    local name = trimmed(head)
    if name:sub(1, 7) == "origin/" then
      return name:sub(8)
    end
    if name ~= "" then
      return name
    end
  end
  if git_out(root, { "rev-parse", "--verify", "main" }, 4096) then
    return "main"
  end
  if git_out(root, { "rev-parse", "--verify", "master" }, 4096) then
    return "master"
  end
  return nil
end

local function fetch_git()
  local root = gvim.workspace.root() or ""
  if root == "" then
    state.git = { at = now_ms(), missing = true }
    return
  end
  if not gvim.git.available() then
    state.git = { at = now_ms(), missing = true }
    return
  end
  local branch = trimmed(git_out(root, { "rev-parse", "--abbrev-ref", "HEAD" }, 4096))
  if branch == "" then
    branch = "(no branch)"
  end
  local url = trimmed(git_out(root, { "remote", "get-url", "origin" }, 8192))
  local staged, changed, untracked = 0, 0, 0
  local porcelain = git_out(root, { "status", "--porcelain=v1" }, 131072)
  if porcelain then
    for line in porcelain:gmatch("[^\n]+") do
      local x, y = line:sub(1, 1), line:sub(2, 2)
      if x == "?" and y == "?" then
        untracked = untracked + 1
      elseif x ~= " " then
        staged = staged + 1
      end
      if y ~= " " and y ~= "?" then
        changed = changed + 1
      end
    end
  end
  local base = default_branch(root)
  local ahead, behind = 0, 0
  if base then
    local counts = trimmed(git_out(root, { "rev-list", "--left-right", "--count", base .. "...HEAD" }, 4096))
    local a, b = counts:match("^(%d+)%s+(%d+)$")
    if a then
      behind, ahead = tonumber(a), tonumber(b)
    end
  end
  -- Upstream tracking, as Fresh's dashboard shows it: commits ahead/behind the
  -- branch's @{upstream}. A branch without an upstream (or a detached HEAD) has none,
  -- and the row says so instead of failing the section.
  local track_ahead, track_behind, upstream = 0, 0, false
  local tracking = trimmed(git_out(root, { "rev-list", "--left-right", "--count", "HEAD...@{upstream}" }, 4096))
  local ta, tb = tracking:match("^(%d+)%s+(%d+)$")
  if ta then
    upstream = true
    track_ahead, track_behind = tonumber(ta), tonumber(tb)
  end
  state.git = {
    at = now_ms(), branch = branch, url = url, base = base,
    staged = staged, changed = changed, untracked = untracked,
    ahead = ahead, behind = behind,
    upstream = upstream, track_ahead = track_ahead, track_behind = track_behind,
    missing = false,
  }
end

-- disk data --------------------------------------------------------------

local function fetch_disk()
  local rows = {}
  local seen = {}
  local candidates = {}
  local root = gvim.workspace.root() or ""
  if root ~= "" then
    table.insert(candidates, root)
  end
  local home = gvim.env.get("HOME")
  if home and home ~= "" then
    table.insert(candidates, home)
  end
  for _, path in ipairs(candidates) do
    local r = gvim.proc.run({ "df", "-k", path }, { timeout_ms = 3000, max_output = 8192 })
    if r and r.ok then
      local last = nil
      for line in r.stdout:gmatch("[^\n]+") do
        last = line
      end
      if last then
        local parts = gvim.text.split(last, " ", true)
        local fields = {}
        for _, part in ipairs(parts) do
          if part ~= "" then
            fields[#fields + 1] = part
          end
        end
        if #fields >= 6 then
          local mount = fields[#fields]
          if not seen[mount] then
            seen[mount] = true
            rows[#rows + 1] = {
              mount = gvim.path.basename(mount) ~= "" and gvim.path.basename(mount) or mount,
              pct = fields[#fields - 1],
              free = fields[#fields - 2],
              capacity = fields[#fields - 4],
            }
          end
        end
      end
    end
  end
  state.disk = { at = now_ms(), rows = rows }
end

-- painting ---------------------------------------------------------------

local function visual_width(s)
  return gvim.text.width(s)
end

local function padded(s, width)
  local w = visual_width(s)
  if w >= width then
    return gvim.text.truncate(s, width, "…")
  end
  return s .. string.rep(" ", width - w)
end

local function clip(s, width)
  return gvim.text.truncate(s, width, "…")
end

-- build_rows flattens the sections into display lines and the focusable rows.
local function build_rows(inner)
  local lines = {}
  local rows = {}
  local function plain(text, style)
    lines[#lines + 1] = { segments = { { text = padded(text, inner), style = style } } }
  end
  local function heading(text)
    plain(text, STYLE.heading)
    lines[#lines + 1] = { segments = { { text = string.rep("─", inner), style = STYLE.frame } } }
  end
  local function kv(label, value, style)
    local label_w = 12
    local value_room = math.max(inner - label_w - 2, 4)
    lines[#lines + 1] = { segments = {
      { text = padded(" " .. label, label_w + 1), style = STYLE.muted },
      { text = " " .. clip(value, value_room), style = style or STYLE.value },
    } }
  end
  local function action_row(label, detail, row)
    local index = #rows
    rows[#rows + 1] = row
    local focus = index == state.focus
    local marker = focus and "› " or "  "
    local style = focus and STYLE_FOCUS or STYLE.accent
    local detail_room = math.max(inner - visual_width(marker) - visual_width(label) - 2, 0)
    local segments = {
      { text = marker .. clip(label, math.max(inner - 2, 4)), style = style },
    }
    if detail ~= "" and detail_room > 0 then
      segments[#segments + 1] = { text = "  " .. clip(detail, detail_room), style = STYLE.muted }
    end
    lines[#lines + 1] = { segments = segments }
    row.line_index = #lines
  end
  local recent = recent_files()
  if state.git and not state.git.missing then
    heading("Git")
    kv("branch", state.git.branch, STYLE.title)
    if state.git.base then
      kv("vs " .. state.git.base, string.format("↑%d ↓%d", state.git.ahead, state.git.behind), STYLE.number)
    end
    if state.git.upstream then
      local color = (state.git.track_ahead + state.git.track_behind) > 0 and STYLE.accent or STYLE.ok
      kv("tracking", string.format("↑%d ↓%d", state.git.track_ahead, state.git.track_behind), color)
    else
      kv("tracking", "no upstream", STYLE.muted)
    end
    kv("working tree", string.format("%d changed · %d staged · %d untracked",
      state.git.changed, state.git.staged, state.git.untracked),
      (state.git.changed + state.git.staged + state.git.untracked) > 0 and STYLE.warn or STYLE.ok)
    if state.git.url ~= "" then
      kv("remote", state.git.url, STYLE.muted)
    end
  elseif state.git and state.git.missing then
    heading("Git")
    kv("repository", "none in this workspace", STYLE.muted)
  end
  if #recent > 0 then
    if #lines > 0 then
      lines[#lines + 1] = { text = "" }
    end
    heading("Recent")
    for i = 1, math.min(#recent, MAX_RECENT_ROWS) do
      local path = recent[i]
      action_row(gvim.path.basename(path), path, { kind = "recent", path = path })
    end
  end
  if #lines > 0 then
    lines[#lines + 1] = { text = "" }
  end
  heading("Actions")
  for i = 1, math.min(#ACTIONS, MAX_ACTIONS) do
    action_row(ACTIONS[i].label, "", { kind = "action", action = ACTIONS[i].action })
  end
  if state.disk and #state.disk.rows > 0 then
    if #lines > 0 then
      lines[#lines + 1] = { text = "" }
    end
    heading("Disk")
    for _, row in ipairs(state.disk.rows) do
      kv(row.mount, string.format("%s used · %s free", row.pct, row.free), STYLE.muted)
    end
  end
  return lines, rows
end

local function frame_width(width)
  local target = math.floor(width * SIZE_FRACTION) - 2
  local inner = math.max(INNER_MIN, math.min(target, INNER_MAX))
  local leftPad = math.max(0, math.floor((width - inner - 2) / 2))
  return inner, leftPad
end

local function clock()
  return os.date("%H:%M")
end

-- publish_targets makes the recent-file rows clickable. A line target opens its path at
-- the row's line, which is refreshing the dashboard already does on Enter; the command
-- rows cannot be targets because a target points at a file, not an action. Targets are
-- replaced on every paint (a row moving with the frame keeps its own line) and cleared
-- when the dashboard closes or the plugin unloads.
local function publish_targets(buf, top_pad)
  local set = gvim.buf.set_line_targets
  if type(set) ~= "function" then
    return
  end
  local targets = {}
  for _, row in ipairs(state.rows) do
    if row.kind == "recent" and row.path and row.line_index then
      targets[#targets + 1] = { line = top_pad + row.line_index + 1, path = row.path }
    end
  end
  local ok, err = pcall(set, buf, targets)
  if not ok then
    gvim.log.debug("dashboard: line targets: " .. tostring(err))
  end
end

local function paint()
  local buf = valid_buf()
  if not buf then
    return false
  end
  local size = gvim.ui.size()
  local width, height = size.width or 100, size.height or 30
  local inner, leftPad = frame_width(width)
  local wanted = state.focus
  local lines, rows = build_rows(inner)
  if #rows == 0 then
    state.focus = 0
  elseif state.focus >= #rows then
    state.focus = #rows - 1
  end
  -- Rebuild once if the clamped focus changed, so the highlight matches.
  if state.focus ~= wanted then
    lines, rows = build_rows(inner)
  end
  state.rows = rows
  local frame = {}
  local function push(segments)
    frame[#frame + 1] = { segments = segments }
  end
  local pad = string.rep(" ", leftPad)
  local function row(segments, border_left, border_right)
    local line = { { text = pad }, { text = border_left, style = STYLE.frame } }
    for _, part in ipairs(segments) do
      line[#line + 1] = part
    end
    line[#line + 1] = { text = border_right, style = STYLE.frame }
    frame[#frame + 1] = { segments = line }
  end
  local title = " FRESH "
  local stamp = " " .. clock() .. " "
  local fill = math.max(inner - 2 - visual_width(title) - visual_width(stamp), 2)
  local top = {
    { text = pad },
    { text = "╭", style = STYLE.frame },
    { text = "─", style = STYLE.frame },
    { text = title, style = STYLE.title },
    { text = string.rep("─", fill), style = STYLE.frame },
    { text = stamp, style = STYLE.muted },
    { text = "─", style = STYLE.frame },
    { text = "╮", style = STYLE.frame },
  }
  frame[#frame + 1] = { segments = top }
  frame[#frame + 1] = { segments = { { text = pad }, { text = "│", style = STYLE.frame }, { text = string.rep(" ", inner) }, { text = "│", style = STYLE.frame } } }
  for _, line in ipairs(lines) do
    local segments = line.segments
    if not segments then
      segments = { { text = padded(line.text or "", inner) } }
    end
    -- Clip the row to the inner width (a section may overrun it), then pad.
    local fitted, used = {}, 0
    for _, part in ipairs(segments) do
      local text = part.text or ""
      local room = inner - used
      if room <= 0 then
        break
      end
      if visual_width(text) > room then
        text = gvim.text.truncate(text, room, "…")
      end
      fitted[#fitted + 1] = { text = text, style = part.style }
      used = used + visual_width(text)
    end
    if used < inner then
      fitted[#fitted + 1] = { text = string.rep(" ", inner - used) }
    end
    row(fitted, "│", "│")
  end
  local hint = " Tab/Shift+Tab: move   Enter: open   q: close "
  local hint_used = visual_width(hint)
  frame[#frame + 1] = { segments = {
    { text = pad }, { text = "│", style = STYLE.frame },
    { text = hint, style = STYLE.muted },
    { text = string.rep(" ", math.max(inner - hint_used, 0)) },
    { text = "│", style = STYLE.frame },
  } }
  frame[#frame + 1] = { segments = {
    { text = pad }, { text = "╰", style = STYLE.frame },
    { text = string.rep("─", inner), style = STYLE.frame },
    { text = "╯", style = STYLE.frame },
  } }
  -- Vertical centering, then pad below so the last rows use the frame background.
  local topPad = math.max(0, math.floor((height - #frame) / 2))
  local entries = {}
  for _ = 1, topPad do
    entries[#entries + 1] = { text = "\n" }
  end
  for _, line in ipairs(frame) do
    entries[#entries + 1] = { segments = line.segments }
    entries[#entries + 1] = { text = "\n" }
  end
  local bottomPad = math.max(0, height - #entries)
  for _ = 1, bottomPad do
    entries[#entries + 1] = { text = "\n" }
  end
  local text_key = ""
  for _, entry in ipairs(entries) do
    if entry.segments then
      for _, part in ipairs(entry.segments) do
        text_key = text_key .. (part.text or "")
      end
    else
      text_key = text_key .. (entry.text or "")
    end
  end
  if text_key ~= state.frame then
    local ok, err = buf:set_content(entries)
    if not ok then
      if gvim.err_code(err) == "closed" then
        state.buf = nil
      end
      return false
    end
    state.frame = text_key
  end
  publish_targets(buf, topPad)
  return true
end

local function refresh(force)
  local buf = valid_buf()
  if not buf then
    return
  end
  local at = now_ms()
  if force or not state.git or at - (state.git.at or 0) > GIT_TTL_MS then
    local ok, err = pcall(fetch_git)
    if not ok then
      gvim.log.debug("dashboard: git: " .. tostring(err))
      state.git = { at = at, missing = true }
    end
  end
  if force or not state.disk or at - (state.disk.at or 0) > DISK_TTL_MS then
    local ok, err = pcall(fetch_disk)
    if not ok then
      gvim.log.debug("dashboard: disk: " .. tostring(err))
      state.disk = { at = at, rows = {} }
    end
  end
  paint()
end

-- lifecycle ----------------------------------------------------------------

local function stop_timer()
  if state.timer then
    pcall(state.timer.cancel, state.timer)
    state.timer = nil
  end
end

local function close_dashboard(quiet)
  local buf = valid_buf()
  state.closed_by_user = true
  stop_timer()
  state.buf, state.auto, state.rows, state.frame = nil, false, {}, ""
  state.focus = 0
  if buf then
    local clear = gvim.buf.clear_line_targets
    if type(clear) == "function" then
      pcall(clear, buf)
    end
    pcall(buf.close, buf, { force = true })
  end
  if not quiet then
    notify("Dashboard closed")
  end
end

local function open_dashboard(auto)
  if valid_buf() then
    local buf = state.buf
    if auto then
      state.auto = true
    end
    pcall(buf.show, buf)
    refresh(true)
    return
  end
  local buf, err = gvim.buf.create {
    name = "Dashboard",
    read_only = true,
    focus = true,
    mode = "dashboard",
  }
  if not buf then
    notify("Dashboard: " .. tostring(err))
    return
  end
  state.buf, state.auto, state.focus, state.frame = buf, auto, 0, ""
  pcall(buf.set_option, buf, "line_numbers", false)
  pcall(buf.set_option, buf, "wrap", false)
  pcall(buf.set_mode, buf, "dashboard")
  state.timer = gvim.timer.every(math.max(settings.refresh_seconds, 1) * 1000, function()
    if valid_buf() then
      refresh(false)
    else
      stop_timer()
    end
  end)
  refresh(true)
end

local function open_if_empty()
  if not settings.show_on_empty then
    return
  end
  if #file_buffers() > 0 then
    return
  end
  if valid_buf() then
    return
  end
  open_dashboard(true)
end

local function activate_row()
  local row = state.rows[state.focus + 1]
  if not row then
    return
  end
  if row.kind == "recent" then
    local ok, buf, err = pcall(function() return gvim.buf.open(row.path, { focus = true }) end)
    if not ok or not buf then
      notify("Dashboard: " .. tostring(err or "could not open " .. row.path))
    end
    return
  end
  if row.kind == "action" and row.action then
    local ok, err = pcall(gvim.command.run, row.action, nil)
    if not ok then
      notify("Dashboard: " .. tostring(err))
    end
  end
end

local function focus_next(delta)
  if #state.rows == 0 then
    return
  end
  state.focus = (state.focus + delta) % #state.rows
  if state.focus < 0 then
    state.focus = #state.rows + state.focus
  end
  state.frame = ""
  paint()
end

-- commands, mode and events -------------------------------------------------

local function register_hidden(name, run)
  gvim.command.register { name = name, title = name, hidden = true, run = run }
end

register_hidden("focus_next", function() focus_next(1) end)
register_hidden("focus_prev", function() focus_next(-1) end)
register_hidden("activate", activate_row)
register_hidden("close", function() close_dashboard(false) end)
register_hidden("refresh", function() refresh(true) end)

gvim.mode.define {
  name = "dashboard",
  status = "DASHBOARD",
  text = "swallow",
  paste = "swallow",
  fallthrough = true,
  bindings = {
    tab = "dashboard.focus_next",
    down = "dashboard.focus_next",
    j = "dashboard.focus_next",
    up = "dashboard.focus_prev",
    k = "dashboard.focus_prev",
    enter = "dashboard.activate",
    q = "dashboard.close",
    escape = "dashboard.close",
  },
}

gvim.command.register {
  name = "show",
  title = "Show Dashboard",
  description = "Open the dashboard, or bring it to the front if it is already open",
  run = function() open_dashboard(false) end,
}

gvim.on("plugins_loaded", function()
  open_if_empty()
end)

gvim.on("ready", function()
  open_if_empty()
end)

gvim.on("buffer_closed", function(ev)
  gvim.timer.after(10, function()
    if state.closed_by_user then
      state.closed_by_user = false
      return
    end
    if valid_buf() then
      local current = gvim.buf.current()
      if current == nil or current.id ~= state.buf.id then
        open_if_empty()
      end
    else
      local has_file = #file_buffers() > 0
      if not has_file and settings.show_on_empty then
        open_dashboard(true)
      end
    end
  end)
end)

gvim.on("after_file_open", function(ev)
  local buf = ev.buf
  if not buf then
    return
  end
  local info = buf:info()
  if info and not info.virtual and info.path and info.path ~= "" then
    remember_recent(info.path)
  end
  if state.auto and valid_buf() then
    gvim.timer.after(10, function()
      local dashboard = valid_buf()
      if dashboard and state.auto and gvim.buf.current() ~= dashboard then
        close_dashboard(true)
      end
    end)
  end
end)

gvim.on("buffer_activated", function(ev)
  local dashboard = valid_buf()
  if dashboard and ev.buf and ev.buf.id == dashboard.id then
    state.frame = ""
    refresh(false)
  end
end)

gvim.on("resize", function()
  if valid_buf() then
    state.frame = ""
    paint()
  end
end)

gvim.on("theme_changed", function()
  if valid_buf() then
    state.frame = ""
    paint()
  end
end)

gvim.plugin.on_unload(function()
  stop_timer()
  local buf = valid_buf()
  local clear = gvim.buf.clear_line_targets
  if buf and type(clear) == "function" then
    pcall(clear, buf)
  end
end)

gvim.log.debug("dashboard plugin loaded")
