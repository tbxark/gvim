-- Dashboard. A port of Fresh's dashboard plugin.
--
-- The empty editor gets a read-only "dashboard" tab: a centered frame with the
-- repository state, recently visited files, quick actions and disk usage. It is
-- drawn as one styled content write per paint (b:set_content) so a frame never
-- comes out half-written, and every colour is a theme key so a theme switch is
-- picked up by the next paint.
--
-- Reduction vs Fresh (docs/plugins/bundled.md):
--   * weather and GitHub PRs are user-opt-in sections (weather / github_prs, plus
--     network) over the bounded gvim.http.get capability. Weather uses wttr.in j1
--     JSON (public conditions only), GitHub reads public repositories; optional
--     GitHub commit-status tags use a rate-bounded public endpoint with no token.
--     PR titles strip Cc/Cf formatting characters before display. Failures are a
--     muted unavailable line and responses never touch disk.
--   * third-party plugins register sections through gvim.dashboard; this plugin
--     discovers them and invokes their proxy callback as a bounded task with a
--     per-section timeout, keeping failures/stale data isolated to that section.
--     A developer can also override the ambient auto-open decision for the current
--     process with gvim.dashboard.set_auto_open; unset falls back to show_on_empty.
--   * the disk section shells out to df for the workspace mount and $HOME only, and paints
--     Fresh's bounded ASCII fill meter + percentage + used/capacity cell per mount; cells
--     that are not finite non-negative integers (or are inconsistent) render unavailable
--     instead of being treated as real capacity.
--   * every row is clickable: recent-file rows are path line targets and the
--     action rows (including the Git section's branch-log row) are action line
--     targets running a hidden command this plugin owns, so a click goes through
--     the same dispatcher as the keyboard mode (Tab/Down/j, Shift-Tab/Up/k and
--     Enter, like Fresh's dashboard mode). A target can never dispatch another
--     plugin's commands; URL rows stay plain text (no safe URL opener).
--   * sessions are not shown (gvim has no plugin-facing session list); the
--     recent-files section covers the same "get back to work" role.
local gvim = require "gvim"

-- Weather strings are plugin-owned: dashboard.i18n.json holds the en table, and
-- gvim.i18n.t falls back to en then the key. The editor Go catalogue is not injected.
local t = gvim.i18n.t

local settings = gvim.settings.define {
  show_on_empty = {
    type = "boolean", default = true,
    description = "Open the dashboard when the editor has no file buffer (startup and after the last file closes)",
  },
  refresh_seconds = {
    type = "integer", default = 5, minimum = 1, maximum = 60,
    description = "How often the dashboard refreshes its git/disk/clock content while visible",
  },
  github_prs = {
    type = "boolean", default = false,
    description = "Show open GitHub pull requests for the workspace repository (needs network = true)",
  },
  github_checks = {
    type = "boolean", default = false,
    description = "Fetch each shown PR's public commit status (up to 6 requests per 15 min; needs github_prs and network = true)",
  },
  weather = {
    type = "boolean", default = false,
    description = "Show current weather and today/tomorrow from wttr.in (needs network = true)",
  },
  weather_location = {
    type = "string", default = "",
    description = "Place name or coordinates passed to wttr.in (e.g. Berlin or 52.52,13.40); empty uses wttr.in's IP geolocation",
  },
  weather_units = {
    type = "enum", values = { "metric", "imperial" }, default = "metric",
    description = "Units of the weather section: metric (C, km/h) or imperial (F, mph)",
  },
  network = {
    type = "boolean", default = false,
    description = "Allow this plugin to make bounded HTTPS GET requests (needed by the weather and GitHub PR sections)",
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
-- Unauthenticated GitHub REST allows 60 requests/hour per IP. The PR list refreshes at
-- most every 2 min (30/h) and the optional commit-status batch at most every 15 min with
-- six requests (24/h): the plugin's steady-state ceiling is 54/h, leaving headroom.
local GITHUB_TTL_MS = 120000
local GITHUB_CHECKS_TTL_MS = 900000
local GITHUB_CHECKS_MAX = 6
local GITHUB_CHECK_TIMEOUT_MS = 2000
local GITHUB_API = "https://api.github.com"
local GITHUB_MAX_ROWS = 6
local WEATHER_TTL_MS = 600000
local WEATHER_URL = "https://wttr.in"
local WEATHER_MAX_LOCATION = 100
local RECENT_MAX = 10
local MAX_ACTIONS = 8
local MAX_RECENT_ROWS = 10
local EXTERNAL_MAX_SECTIONS = 8
local EXTERNAL_MAX_ROWS = 64
local EXTERNAL_MAX_TEXT = 500

local ACTIONS = {
  { label = "Open Settings", action = "open_settings" },
  { label = "Toggle File Explorer", action = "toggle_file_explorer" },
  { label = "Live Grep", action = "resume_live_grep" },
  { label = "Select Theme", action = "select_theme" },
  { label = "Git Log", action = "git_log.show_git_log" },
  { label = "Diagnostics Panel", action = "diagnostics_panel.toggle_diagnostics_panel" },
}
-- Each action row clicks its own hidden command (registered at the bottom), so the
-- line-target API only ever dispatches commands this plugin owns.
for i, a in ipairs(ACTIONS) do
  a.target = "dashboard.click_" .. i
end
local BRANCH_LOG = "dashboard.click_branch_log"
local REPO_URL = "dashboard.click_repo_url"
local BRANCH_URL = "dashboard.click_branch_url"

-- gitURL normalizes a git remote for opening in a browser. https/http URLs are kept
-- (an http(s) URL on any host is openable); the common scp-like/ssh/HTTPS GitHub
-- forms become https://github.com/owner/repo. Anything else (local paths, unknown
-- schemes) yields "" so the dashboard never advertises an unsafe URL.
local function url_encode(s)
  return (tostring(s):gsub("([^%w%-%._~])", function(c)
    return string.format("%%%02X", string.byte(c))
  end))
end

local function normalize_repo_url(raw)
  local text = raw and raw:match("^%s*(.-)%s*$") or ""
  if text == "" then
    return ""
  end
  local scheme, rest = text:match("^([%a][%w+.-]*)://(.+)$")
  local host, path
  if scheme then
    scheme = string.lower(scheme)
    rest = rest:gsub("^[^@/]*@", "") -- strip userinfo
    host, path = rest:match("^([^/:]+)(.*)$")
  else
    local user_host, p = text:match("^([%w%._%-]+@[^:]+):(.+)$")
    if not user_host then
      return ""
    end
    host, path = user_host:gsub("^.*@", ""), p
  end
  if not host or not path then
    return ""
  end
  host = string.lower(host:gsub(":.*$", ""))
  path = path:gsub("^/", ""):gsub("%.git$", "")
  if host == "github.com" then
    local owner, repo = path:match("^([^/]+)/([^/]+)$")
    if not owner or not repo then
      return ""
    end
    return "https://github.com/" .. owner .. "/" .. repo
  end
  if scheme == "http" or scheme == "https" then
    return text
  end
  return ""
end

local function branch_url(repo, branch)
  if repo == "" or not branch or branch == "" or branch == "HEAD" then
    return ""
  end
  return repo .. "/tree/" .. url_encode(branch)
end

-- command_available says whether a command the dashboard points at is currently
-- registered (visible commands only; hidden ones are not another plugin's API). It
-- keeps the branch-log row from advertising a click when the git_log plugin is off.
local function command_available(id)
  local ok, list = pcall(gvim.command.list, { query = id })
  if not ok or type(list) ~= "table" then
    return false
  end
  for _, c in ipairs(list) do
    if c.id == id then
      return true
    end
  end
  return false
end

local state = {
  buf = nil,             -- the dashboard Buffer handle
  auto = false,          -- the current dashboard was opened automatically
  focus = 0,             -- index into rows
  rows = {},             -- { kind = "recent"|"action", label, path?, action? }
  frame = "",            -- last painted frame text (skip identical writes)
  git = nil,             -- { at, branch, url, repo_url, branch_url, ... }
  github = nil,          -- { at, rows, unavailable, loading } from the GitHub REST API
  weather = nil,         -- { at, now_text, evening, tomorrow, unavailable, loading }
  click_rows = {},       -- non-focusable clickable lines: { index, action }
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
  local repo_url = normalize_repo_url(url)
  state.git = {
    at = now_ms(), branch = branch, url = url, base = base,
    repo_url = repo_url,
    -- Only GitHub URLs have a branch page path; other hosts keep the repo line clickable
    -- but advertise no branch URL.
    branch_url = (repo_url:sub(1, 19) == "https://github.com/") and branch_url(repo_url, branch) or "",
    staged = staged, changed = changed, untracked = untracked,
    ahead = ahead, behind = behind,
    upstream = upstream, track_ahead = track_ahead, track_behind = track_behind,
    missing = false,
  }
end

-- GitHub PRs (opt-in) -----------------------------------------------------
-- The section is off unless github_prs is true. The host also requires the user
-- layer setting network = true; without it gvim.http.get refuses before any DNS.
-- Only public repositories can be read without a token: a 404 (private/missing) or
-- any other failure renders one muted unavailable line, never a notice, and never
-- blocks the rest of the dashboard.

local function github_nwo(repo_url)
  local owner, repo = (repo_url or ""):match("^https://github%.com/([%w%-%._]+)/([%w%-%._]+)$")
  if not owner then
    return nil
  end
  return owner .. "/" .. repo
end

-- PR rows come from an untrusted REST response: never use an API-provided link. The
-- URL is rebuilt from the repository slug the dashboard itself normalized and a strictly
-- validated positive integer PR number, so a hostile body cannot introduce another host
-- or scheme; gvim.url.open still applies its own scheme policy and Yes/No confirmation.
local function pull_url(nwo, number)
  return "https://github.com/" .. nwo .. "/pull/" .. number
end

local function pull_number(value)
  if type(value) ~= "number" or value ~= math.floor(value) or value < 1 or value > 999999999 then
    return nil
  end
  return string.format("%d", value)
end

-- Unicode Cc/Cf handling for PR titles. Cc controls become spaces; Cf format marks
-- (bidi overrides/isolates, ZWSP/ZWJ/ZWNJ, soft hyphen, BOM, tags, interlinear
-- annotations, ...) are removed so a hostile title cannot visually reorder or hide the
-- dashboard row. The table is the assigned Cf set as of Unicode 15.1; invalid UTF-8 bytes
-- become spaces rather than reaching the terminal.
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

local function strip_pr_format(text)
  local out = {}
  local i = 1
  while i <= #text do
    local cp, width = utf8_codepoint(text, i)
    if not cp or width == nil then
      out[#out + 1] = " "
      i = i + 1
    elseif cp <= 0x1F or cp == 0x7F or (cp >= 0x80 and cp <= 0x9F) then
      out[#out + 1] = " "
      i = i + width
    elseif is_cf(cp) then
      -- Cf marks are invisible/format-only: remove them, keeping surrounding text.
      i = i + width
    else
      out[#out + 1] = text:sub(i, i + width - 1)
      i = i + width
    end
  end
  return table.concat(out)
end

-- pr_title keeps a display string from untrusted JSON: non-strings, Cc controls and Cf
-- format characters and runaway lengths are replaced/removed/clipped instead of reaching
-- the frame verbatim. The input slice is bounded before decoding.
local function pr_title(value)
  if type(value) ~= "string" then
    return "(untitled)"
  end
  local text = value:sub(1, 1024)
  text = strip_pr_format(text):gsub("%s+", " ")
  text = text:match("^%s*(.-)%s*$") or ""
  if text == "" then
    return "(untitled)"
  end
  if #text > 200 then
    text = gvim.text.truncate(text, 200, "…")
  end
  return text
end

-- pull_sha reads the PR head SHA from the untrusted list response; only a 40/64-char
-- hexadecimal value is accepted, and it is percent-safe input to the fixed status URL.
local function pull_sha(pr)
  local head = pr.head
  if type(head) ~= "table" then
    return nil
  end
  local sha = head.sha
  if type(sha) ~= "string" or (#sha ~= 40 and #sha ~= 64) or not sha:match("^[%x]+$") then
    return nil
  end
  return sha
end

-- github_check_tag maps the public commit-status state onto Fresh's compact check labels.
-- Unknown states are omitted rather than guessed.
local function github_check_tag(state)
  if state == "success" then
    return t("github.check_pass")
  end
  if state == "failure" or state == "error" then
    return t("github.check_fail")
  end
  if state == "pending" then
    return t("github.check_run")
  end
  return nil
end

local function fetch_github()
  if not settings.github_prs then
    state.github = nil
    return
  end
  local now = now_ms()
  if state.github and state.github.at and now - state.github.at < GITHUB_TTL_MS then
    return
  end
  -- Keep the last-good check tags/cooldown across a failed list refresh.
  local previous = state.github
  local checks = (previous and previous.checks) or {}
  local last_checks = (previous and previous.checked_at) or -1
  local nwo = state.git and github_nwo(state.git.repo_url) or nil
  if not nwo then
    state.github = { at = now, unavailable = "not a github repository", checks = checks, checked_at = last_checks }
    return
  end
  local url = GITHUB_API .. "/repos/" .. nwo .. "/pulls?state=open&per_page=" .. tostring(GITHUB_MAX_ROWS)
  state.github = { at = now, loading = true, checks = checks, checked_at = last_checks }
  local res, err = gvim.http.get(url, { timeout_ms = 5000 })
  if not res then
    local text = tostring(err)
    local reason = "unavailable"
    if text:find("network is disabled", 1, true) then
      reason = "network disabled (set network = true)"
    end
    state.github = { at = now, unavailable = reason, checks = checks, checked_at = last_checks }
    return
  end
  if res.status ~= 200 then
    state.github = { at = now, unavailable = "unavailable", checks = checks, checked_at = last_checks }
    return
  end
  local ok, list = pcall(json.decode, res.body)
  if not ok or type(list) ~= "table" then
    state.github = { at = now, unavailable = "invalid response", checks = checks, checked_at = last_checks }
    return
  end
  local rows = {}
  local examined = 0
  for _, pr in ipairs(list) do
    examined = examined + 1
    if examined > 200 or #rows >= GITHUB_MAX_ROWS then
      break
    end
    if type(pr) == "table" then
      local number = pull_number(pr.number)
      if number then
        rows[#rows + 1] = {
          number = number,
          title = pr_title(pr.title),
          draft = pr.draft == true,
          url = pull_url(nwo, number),
          sha = pull_sha(pr),
        }
      end
    end
  end
  if settings.github_checks then
    for _, row in ipairs(rows) do
      local cached = checks[row.number]
      if cached and row.sha and cached.sha == row.sha then
        row.check_tag = cached.tag
      end
    end
  end
  local checked_at = last_checks
  if settings.github_checks and #rows > 0 and (last_checks < 0 or now - last_checks >= GITHUB_CHECKS_TTL_MS) then
    local updated = {}
    local fetched = 0
    for _, row in ipairs(rows) do
      local cached = checks[row.number]
      if row.sha and fetched < GITHUB_CHECKS_MAX then
        fetched = fetched + 1
        -- Public commit-status endpoint only: no token/auth header, no provided URL.
        -- The SHA and repo slug are validated/derived above; response state is mapped
        -- to a fixed tag and every failure leaves the tag absent (cached last-good only).
        local res2 = gvim.http.get(
          GITHUB_API .. "/repos/" .. nwo .. "/commits/" .. row.sha .. "/status",
          { timeout_ms = GITHUB_CHECK_TIMEOUT_MS }
        )
        local tag = nil
        if res2 and res2.status == 200 then
          local ok2, data = pcall(json.decode, res2.body)
          if ok2 and type(data) == "table" then
            tag = github_check_tag(data.state)
          end
        end
        if tag then
          updated[row.number] = { sha = row.sha, tag = tag }
          row.check_tag = tag
        elseif cached and cached.sha == row.sha then
          updated[row.number] = cached
          row.check_tag = cached.tag
        end
      elseif cached and row.sha and cached.sha == row.sha then
        updated[row.number] = cached
        row.check_tag = cached.tag
      end
    end
    checks = updated
    checked_at = now
  end
  state.github = {
    at = now,
    rows = rows,
    checks = settings.github_checks and checks or nil,
    checked_at = checked_at,
  }
end

-- Weather (opt-in) --------------------------------------------------------
-- Fresh registers its weather section only from explicit user init and shells out to
-- curl for wttr.in's j1 JSON. gvim uses the bounded gvim.http.get instead: the URL host
-- is fixed (https://wttr.in), a configured location is percent-encoded into one path
-- segment, there are no credentials or request headers, the 5 s timeout keeps the
-- awaitable Work off the update loop, and a failure renders one muted line.
-- Display follows Fresh: current conditions, the 5pm nearest 3-hour sample and a
-- tomorrow summary, with the user's metric/imperial setting.

local function weather_units()
  return (settings.weather_units == "imperial") and "imperial" or "metric"
end

local function weather_location()
  local loc = settings.weather_location or ""
  loc = loc:match("^%s*(.-)%s*$") or ""
  if #loc > WEATHER_MAX_LOCATION then
    loc = loc:sub(1, WEATHER_MAX_LOCATION)
  end
  return loc
end

-- encode the location as ONE path segment: everything but unreserved URL bytes becomes
-- %XX (including spaces and UTF-8 bytes), so it cannot escape wttr.in's path or add a
-- query fragment.
local function weather_encode(s)
  return (s:gsub("[^%w%-%._~]", function(c)
    return string.format("%%%02X", string.byte(c))
  end))
end

local function weather_text(v)
  if type(v) ~= "string" then
    return ""
  end
  -- wttr.in text is network-provided display data; use the same Cc/Cf sanitizer as PR
  -- titles so bidi/zero-width marks cannot spoof the weather rows.
  return strip_pr_format(v:sub(1, 1024)):gsub("%s+", " ")
end

local function weather_cond(entry)
  if type(entry) ~= "table" then
    return ""
  end
  local desc = entry.weatherDesc
  if type(desc) == "table" and type(desc[1]) == "table" then
    return weather_text(desc[1].value)
  end
  return ""
end

local function weather_join(parts)
  local out = {}
  for _, part in ipairs(parts) do
    if part ~= "" then
      out[#out + 1] = part
    end
  end
  return table.concat(out, " · ")
end

local function weather_clip(s)
  if #s > 200 then
    return gvim.text.truncate(s, 200, "…")
  end
  return s
end

-- hourly_at finds the 3-hour sample nearest target; wttr.in encodes time as "0", "300",
-- ..., "2100", so 17:00 snaps to the 1800 sample (Fresh rounds rather than floors).
local function weather_hourly_at(hours, target)
  if type(hours) ~= "table" then
    return nil
  end
  local best, best_delta = nil, math.huge
  for _, hour in ipairs(hours) do
    if type(hour) == "table" then
      local raw = tonumber(hour.time) or 0
      local hh = math.floor(raw / 100 + 0.5)
      local delta = math.abs(hh - target)
      if delta < best_delta then
        best, best_delta = hour, delta
      end
    end
  end
  return best
end

local function weather_current(entry)
  if type(entry) ~= "table" then
    return ""
  end
  local cond = weather_cond(entry)
  local temp, feels, wind
  if weather_units() == "imperial" then
    temp = weather_text(entry.temp_F)
    if temp ~= "" then
      temp = temp .. "°F"
    end
    feels = weather_text(entry.FeelsLikeF)
    if feels ~= "" and feels ~= weather_text(entry.temp_F) then
      feels = "feels " .. feels .. "°F"
    else
      feels = ""
    end
    wind = weather_text(entry.windspeedMiles)
    if wind ~= "" then
      wind = wind .. " mph"
    end
  else
    temp = weather_text(entry.temp_C)
    if temp ~= "" then
      temp = temp .. "°C"
    end
    feels = weather_text(entry.FeelsLikeC)
    if feels ~= "" and feels ~= weather_text(entry.temp_C) then
      feels = "feels " .. feels .. "°C"
    else
      feels = ""
    end
    wind = weather_text(entry.windspeedKmph)
    if wind ~= "" then
      wind = wind .. " km/h"
    end
  end
  local humidity = weather_text(entry.humidity)
  if humidity ~= "" then
    humidity = humidity .. "%"
  end
  return weather_clip(weather_join({ cond, temp, feels, wind, humidity }))
end

local function weather_hour(entry)
  if type(entry) ~= "table" then
    return ""
  end
  local cond = weather_cond(entry)
  local temp, feels
  if weather_units() == "imperial" then
    temp = weather_text(entry.tempF)
    if temp ~= "" then
      temp = temp .. "°F"
    end
    feels = weather_text(entry.FeelsLikeF)
    if feels ~= "" and feels ~= weather_text(entry.tempF) then
      feels = "feels " .. feels .. "°F"
    else
      feels = ""
    end
  else
    temp = weather_text(entry.tempC)
    if temp ~= "" then
      temp = temp .. "°C"
    end
    feels = weather_text(entry.FeelsLikeC)
    if feels ~= "" and feels ~= weather_text(entry.tempC) then
      feels = "feels " .. feels .. "°C"
    else
      feels = ""
    end
  end
  return weather_clip(weather_join({ cond, temp, feels }))
end

local function weather_day(day)
  if type(day) ~= "table" then
    return ""
  end
  local cond = weather_cond(weather_hourly_at(day.hourly, 12))
  local range = ""
  if weather_units() == "imperial" then
    local lo, hi = weather_text(day.mintempF), weather_text(day.maxtempF)
    if lo ~= "" and hi ~= "" then
      range = lo .. "°.." .. hi .. "°F"
    end
  else
    local lo, hi = weather_text(day.mintempC), weather_text(day.maxtempC)
    if lo ~= "" and hi ~= "" then
      range = lo .. "°.." .. hi .. "°C"
    end
  end
  return weather_clip(weather_join({ range, cond }))
end

local function fetch_weather()
  if not settings.weather then
    state.weather = nil
    return
  end
  local now = now_ms()
  if state.weather and state.weather.at and now - state.weather.at < WEATHER_TTL_MS then
    return
  end
  local loc = weather_location()
  local path = ""
  if loc ~= "" then
    path = "/" .. weather_encode(loc)
  end
  local url = WEATHER_URL .. path .. "?format=j1"
  state.weather = { at = now, loading = true }
  local res, err = gvim.http.get(url, { timeout_ms = 5000 })
  if not res then
    local text = tostring(err)
    local reason = t("weather.unavailable")
    if text:find("network is disabled", 1, true) then
      reason = t("weather.network_disabled")
    end
    state.weather = { at = now, unavailable = reason }
    return
  end
  if res.status ~= 200 then
    state.weather = { at = now, unavailable = t("weather.unavailable") }
    return
  end
  local ok, parsed = pcall(json.decode, res.body)
  if not ok or type(parsed) ~= "table" then
    state.weather = { at = now, unavailable = t("weather.invalid_response") }
    return
  end
  local current = type(parsed.current_condition) == "table" and parsed.current_condition[1] or nil
  local today_day = type(parsed.weather) == "table" and parsed.weather[1] or nil
  local tomorrow_day = type(parsed.weather) == "table" and parsed.weather[2] or nil
  local current_text = weather_current(current)
  local evening = weather_hour(weather_hourly_at(today_day and today_day.hourly or nil, 17))
  local tomorrow_text = weather_day(tomorrow_day)
  if current_text == "" and evening == "" and tomorrow_text == "" then
    state.weather = { at = now, unavailable = t("weather.invalid_response") }
    return
  end
  state.weather = {
    at = now,
    now_text = current_text,
    evening = evening,
    tomorrow = tomorrow_text,
  }
end

-- disk data --------------------------------------------------------------

-- disk_kb is a strict df -k cell parser: a plain non-negative integer string only.
-- nil means "unknown", never 0, so a malformed row cannot masquerade as a real volume.
local function disk_kb(value)
  if type(value) ~= "string" or not value:match("^%d+$") then
    return nil
  end
  local n = tonumber(value)
  if not n or n > 1e15 or n ~= math.floor(n) then
    return nil
  end
  return n
end

-- disk_pct parses df's percentage cell strictly: a non-negative integer with at most one
-- trailing %, e.g. "95" or "95%". Decimals, signs, whitespace and misplaced % are
-- unknown (nil); the caller clamps only a valid numeric result.
local function disk_pct(value)
  if type(value) ~= "string" or not value:match("^%d+%%?$") then
    return nil
  end
  local n = tonumber((value:gsub("%%$", "")))
  if not n or n ~= n or n == math.huge or n == -math.huge or n < 0 then
    return nil
  end
  return n
end

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
            local label = gvim.path.basename(mount) ~= "" and gvim.path.basename(mount) or mount
            -- Parse strict cells only (plain digits for capacity/used; digits plus an
            -- optional trailing % for pct). A bad df field is not zero capacity or 100%
            -- used: it is unavailable and its used/capacity cell is omitted. Valid
            -- numeric percentages still clamp into 0..100.
            local capacity = disk_kb(fields[#fields - 4])
            local used = disk_kb(fields[#fields - 3])
            local pct = disk_pct(fields[#fields - 1])
            if not capacity or capacity <= 0 or not used or used > capacity or not pct then
              rows[#rows + 1] = { mount = label, unavailable = true }
            else
              rows[#rows + 1] = {
                mount = label,
                pct = math.max(0, math.min(100, pct)),
                used = used,
                capacity = capacity,
              }
            end
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

-- disk_bar is Fresh's disk meter in ASCII: the bracket meter is the elastic cell, so a
-- terminal that gives block/line-drawing glyphs an East-Asian width cannot break the row.
-- width is clamped by the caller; pct is clamped here so malformed df output stays bounded.
local function disk_bar(pct, width)
  local inner = math.max(0, width - 2)
  local filled = math.max(0, math.min(inner, math.floor(math.max(0, math.min(100, pct)) / 100 * inner + 0.5)))
  return "[" .. string.rep("#", filled) .. string.rep("-", inner - filled) .. "]"
end

-- disk_size formats df -k's 1 KiB blocks for the used / capacity cell.
local function disk_size(kb)
  kb = tonumber(kb) or 0
  if kb >= 1024 * 1024 then
    return string.format("%.1fG", kb / (1024 * 1024))
  end
  if kb >= 1024 then
    return string.format("%.0fM", kb / 1024)
  end
  return string.format("%dK", kb)
end

local function external_string(value, max)
  local text = type(value) == "string" and value or ""
  if #text > 1024 then
    text = text:sub(1, 1024)
  end
  text = strip_pr_format(text):gsub("%s+", " ")
  text = text:match("^%s*(.-)%s*$") or ""
  if #text > max then
    text = gvim.text.truncate(text, max, "…")
  end
  return text
end

-- build_rows flattens the sections into display lines and the focusable rows.
local function build_rows(inner)
  local lines = {}
  local rows = {}
  local click_rows = {}
  state.click_rows = click_rows
  local function plain(text, style)
    lines[#lines + 1] = { segments = { { text = padded(text, inner), style = style } } }
  end
  local function heading(text)
    plain(text, STYLE.heading)
    lines[#lines + 1] = { segments = { { text = string.rep("─", inner), style = STYLE.frame } } }
  end
  local function kv(label, value, style, target)
    local label_w = 12
    local value_room = math.max(inner - label_w - 2, 4)
    lines[#lines + 1] = { segments = {
      { text = padded(" " .. label, label_w + 1), style = STYLE.muted },
      { text = " " .. clip(value, value_room), style = style or STYLE.value },
    } }
    if target then
      click_rows[#click_rows + 1] = { index = #lines, action = target }
    end
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
    kv("branch", state.git.branch, STYLE.title, state.git.branch_url and state.git.branch_url ~= "" and BRANCH_URL or nil)
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
    if state.git.repo_url ~= "" then
      kv("repo", state.git.repo_url, STYLE.muted, REPO_URL)
    elseif state.git.url ~= "" then
      kv("remote", state.git.url, STYLE.muted)
    end
    if command_available("git_log.show_git_log") then
      action_row("branch log", state.git.branch or "", { kind = "action", action = "git_log.show_git_log", target = BRANCH_LOG })
    else
      kv("branch log", "unavailable (enable git_log)", STYLE.muted)
    end
  elseif state.git and state.git.missing then
    heading("Git")
    kv("repository", "none in this workspace", STYLE.muted)
  end
  if settings.weather then
    if #lines > 0 then
      lines[#lines + 1] = { text = "" }
    end
    heading(t("weather.heading"))
    local w = state.weather
    if not w or w.loading then
      kv(t("weather.status_label"), t("weather.loading"), STYLE.muted)
    elseif w.unavailable then
      kv(t("weather.status_label"), w.unavailable, STYLE.muted)
    else
      kv(t("weather.now"), w.now_text ~= "" and w.now_text or "–", w.now_text ~= "" and STYLE.accent or STYLE.muted)
      if w.evening and w.evening ~= "" then
        kv(t("weather.evening"), w.evening, STYLE.value)
      end
      if w.tomorrow and w.tomorrow ~= "" then
        kv(t("weather.tomorrow"), w.tomorrow, STYLE.value)
      end
    end
  end
  if settings.github_prs then
    if #lines > 0 then
      lines[#lines + 1] = { text = "" }
    end
    heading("GitHub")
    local gh = state.github
    if not gh or gh.loading then
      kv("PRs", "loading…", STYLE.muted)
    elseif gh.unavailable then
      kv("PRs", gh.unavailable, STYLE.muted)
    elseif not gh.rows or #gh.rows == 0 then
      kv("PRs", "no open PRs", STYLE.muted)
    else
      for i, pr in ipairs(gh.rows) do
        local label = "#" .. tostring(pr.number)
        if settings.github_checks and pr.check_tag and pr.check_tag ~= "" then
          label = label .. " " .. pr.check_tag
        end
        local title = pr.draft and ("draft " .. pr.title) or pr.title
        local target = "dashboard.click_pr_" .. i
        action_row(label, title, { kind = "action", action = target, target = target })
      end
    end
  end
  for _, item in ipairs(state.external_order or {}) do
    local entry = item.entry
    if #lines > 0 then
      lines[#lines + 1] = { text = "" }
    end
    local suffix = ""
    if entry.error and entry.rows then
      suffix = " (" .. t("external.stale") .. ")"
    end
    heading((entry.title or "plugin") .. suffix)
    if entry.error and not entry.rows then
      kv("status", external_string(entry.error, 500), STYLE.muted)
    elseif not entry.rows then
      kv("status", t("external.loading"), STYLE.muted)
    elseif #entry.rows == 0 then
      kv("status", t("external.no_data"), STYLE.muted)
    else
      for _, row in ipairs(entry.rows) do
        if row.kind == "kv" then
          kv(row.label, row.value, row.style)
        elseif row.kind == "text" then
          plain("  " .. row.text, row.style)
        elseif row.kind == "error" then
          kv("error", row.text, STYLE.err)
        else
          lines[#lines + 1] = { text = "" }
        end
      end
    end
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
    action_row(ACTIONS[i].label, "", { kind = "action", action = ACTIONS[i].action, target = ACTIONS[i].target })
  end
  -- Keyboard-equivalent rows for the clickable URL lines (Fresh makes those rows
  -- clickable; gvim keeps them reachable from the dashboard mode too).
  if state.git and state.git.repo_url and state.git.repo_url ~= "" then
    action_row("Open Repository", "", { kind = "action", action = REPO_URL, target = REPO_URL })
    if state.git.branch_url and state.git.branch_url ~= "" then
      action_row("Open Branch", "", { kind = "action", action = BRANCH_URL, target = BRANCH_URL })
    end
  end
  if state.disk and #state.disk.rows > 0 then
    if #lines > 0 then
      lines[#lines + 1] = { text = "" }
    end
    heading("Disk")
    for _, row in ipairs(state.disk.rows) do
      local mount = strip_pr_format(tostring(row.mount or "")):gsub("%s+", " ")
      if row.unavailable then
        lines[#lines + 1] = { segments = {
          { text = padded(" " .. mount, 13), style = STYLE.muted },
          { text = " " .. t("disk.unavailable"), style = STYLE.muted },
        } }
      else
        local pct = math.max(0, math.min(100, row.pct or 0))
        -- Keep the percentage visible even at the minimum frame width: the meter may
        -- shrink to four cells, and the used/capacity cell is appended only when it fits.
        local meter = math.max(4, math.min(18, inner - 32))
        local detail = disk_bar(pct, meter) .. string.format(" %d%%", pct)
        local usage = disk_size(row.used) .. " / " .. disk_size(row.capacity)
        local value = detail
        local room = math.max(4, inner - 14)
        if room > #detail + #usage + 2 then
          value = detail .. "  " .. usage
        end
        local style = pct >= 90 and STYLE.err or pct >= 75 and STYLE.warn or STYLE.ok
        lines[#lines + 1] = { segments = {
          { text = padded(" " .. mount, 13), style = STYLE.muted },
          { text = " " .. clip(value, room), style = style },
        } }
      end
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
    if not row.line_index then
      goto continue
    end
    if row.kind == "recent" and row.path then
      targets[#targets + 1] = { line = top_pad + row.line_index + 1, path = row.path }
    elseif row.kind == "action" and row.target then
      targets[#targets + 1] = { line = top_pad + row.line_index + 1, action = row.target }
    end
    ::continue::
  end
  for _, row in ipairs(state.click_rows or {}) do
    targets[#targets + 1] = { line = top_pad + row.index + 1, action = row.action }
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


-- External dashboard sections ---------------------------------------------
-- Third-party plugins register through gvim.dashboard.register_section. The dashboard
-- discovers metadata from the host and calls the provider's exported proxy function as a
-- task with a per-section timeout; failures/stale data stay in that section and cannot
-- break the rest of the dashboard. Rows are plain data, validated and clipped here.

local function external_style(color)
  local styles = {
    muted = STYLE.muted, accent = STYLE.accent, value = STYLE.value,
    number = STYLE.number, ok = STYLE.ok, warn = STYLE.warn, err = STYLE.err,
    title = STYLE.title,
  }
  return styles[tostring(color or "value")] or STYLE.value
end

local function external_rows(result)
  if type(result) ~= "table" then
    return nil
  end
  local rows = {}
  for i, row in ipairs(result) do
    if i > EXTERNAL_MAX_ROWS then
      break
    end
    if type(row) == "table" then
      local kind = tostring(row.kind or "kv")
      if kind == "kv" then
        rows[#rows + 1] = {
          kind = "kv",
          label = external_string(row.label, 40),
          value = external_string(row.value, EXTERNAL_MAX_TEXT),
          style = external_style(row.color),
        }
      elseif kind == "text" then
        rows[#rows + 1] = {
          kind = "text",
          text = external_string(row.text, EXTERNAL_MAX_TEXT),
          style = external_style(row.color),
        }
      elseif kind == "error" then
        rows[#rows + 1] = {
          kind = "error",
          text = external_string(row.text or row.value or t("external.unavailable"), EXTERNAL_MAX_TEXT),
        }
      elseif kind == "blank" then
        rows[#rows + 1] = { kind = "blank" }
      end
    end
  end
  return rows
end

local function refresh_external_sections(force, inner)
  local ok, list = pcall(gvim.dashboard.sections)
  if not ok or type(list) ~= "table" then
    state.external = {}
    state.external_order = {}
    return
  end
  local now = now_ms()
  local prev = state.external or {}
  local next_entries = {}
  local order = {}
  for _, sec in ipairs(list) do
    if #order >= EXTERNAL_MAX_SECTIONS then
      break
    end
    if type(sec) == "table" and type(sec.plugin) == "string" and type(sec.name) == "string"
      and type(sec.key) == "string" then
      local id = sec.plugin .. "\1" .. sec.name
      local entry = prev[id]
      local ttl = tonumber(sec.ttl_ms) or 30000
      local timeout = tonumber(sec.timeout_ms) or 8000
      local title = external_string(sec.title, 120)
      if title == "" then
        title = external_string(sec.name, 120)
      end
      local due = force or not entry or now - (entry.at or 0) >= ttl
      if due then
        local rows, err
        local provider = gvim.plugins.get(sec.plugin)
        local fn = provider and provider[sec.key] or nil
        if type(fn) ~= "function" then
          err = t("external.unavailable")
        else
          local task = gvim.async.spawn(function()
            -- Cross-plugin proxies return their results directly (mkv strips the
            -- leading ok): a failed call is (nil, error), a success is (rows).
            local result, cerr = fn({ width = inner, plugin = sec.plugin, name = sec.name })
            if result == nil then
              return nil, tostring(cerr or t("external.invalid"))
            end
            return result
          end)
          if not task then
            gvim.log.debug("dashboard: external section " .. id .. ": spawn failed")
            err = t("external.unavailable")
          else
            local okw, result, werr = pcall(task.wait, task, { timeout_ms = timeout })
            if not okw then
              pcall(task.cancel, task)
              err = tostring(result)
            elseif werr ~= nil then
              pcall(task.cancel, task)
              err = tostring(werr)
            elseif result == nil then
              err = t("external.invalid")
            else
              rows = external_rows(result)
              if rows == nil then
                err = t("external.invalid")
              end
            end
            if err ~= nil then
              gvim.log.debug("dashboard: external section " .. id .. ": " .. tostring(err))
            end
            if err ~= nil then
              pcall(task.cancel, task)
            end
          end
        end
        if rows ~= nil then
          entry = { at = now, rows = rows }
        elseif entry ~= nil then
          entry = { at = now, rows = entry.rows, error = err or t("external.unavailable") }
        else
          entry = { at = now, error = err or t("external.unavailable") }
        end
      end
      if entry then
        entry.title = title
        entry.ttl = ttl
        entry.timeout = timeout
        entry.priority = tonumber(sec.priority) or 0
        next_entries[id] = entry
        order[#order + 1] = { id = id, entry = entry }
      end
    end
  end
  table.sort(order, function(a, b)
    if a.entry.priority ~= b.entry.priority then
      return a.entry.priority > b.entry.priority
    end
    return a.id < b.id
  end)
  state.external = next_entries
  state.external_order = order
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
  if settings.weather and (force or not state.weather or at - (state.weather.at or 0) > WEATHER_TTL_MS) then
    local ok, err = pcall(fetch_weather)
    if not ok then
      gvim.log.debug("dashboard: weather: " .. tostring(err))
      state.weather = { at = at, unavailable = t("weather.unavailable") }
    end
  end
  -- GitHub has its own TTL even for an explicit refresh: the unauthenticated REST
  -- budget must not be spent by rapid dashboard open/close cycles.
  if settings.github_prs and (not state.github or at - (state.github.at or 0) > GITHUB_TTL_MS) then
    local ok, err = pcall(fetch_github)
    if not ok then
      gvim.log.debug("dashboard: github: " .. tostring(err))
      state.github = { at = at, unavailable = "unavailable" }
    end
  end
  if force or not state.disk or at - (state.disk.at or 0) > DISK_TTL_MS then
    local ok, err = pcall(fetch_disk)
    if not ok then
      gvim.log.debug("dashboard: disk: " .. tostring(err))
      state.disk = { at = at, rows = {} }
    end
  end
  local size = gvim.ui.size()
  local inner = select(1, frame_width(size.width or 100))
  local ok, err = pcall(refresh_external_sections, force, inner)
  if not ok then
    gvim.log.debug("dashboard: external sections: " .. tostring(err))
    state.external = {}
    state.external_order = {}
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

-- auto_open_enabled is the session override set through gvim.dashboard.set_auto_open,
-- falling back to the user setting. The override only controls future automatic opens;
-- it never closes or opens an already-visible dashboard by itself.
local function auto_open_enabled()
  local override = gvim.dashboard.auto_open()
  if override ~= nil then
    return override
  end
  return settings.show_on_empty
end

local function open_if_empty()
  if not auto_open_enabled() then
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

-- Click targets run through hidden commands owned by this plugin. The action itself
-- still goes through gvim.command.run (the same dispatcher as the keyboard row), so a
-- stale/unavailable action is reported and nothing else is dispatched.
local function run_action(action)
  local ok, result, err = pcall(gvim.command.run, action, nil)
  if not ok then
    notify("Dashboard: " .. tostring(result))
  elseif not result then
    notify("Dashboard: " .. tostring(err))
  end
end

local function make_click(action)
  return function() run_action(action) end
end

for i, a in ipairs(ACTIONS) do
  register_hidden("click_" .. i, make_click(a.action))
end
register_hidden("click_branch_log", make_click("git_log.show_git_log"))

-- URL rows: gvim.url.open validates the scheme, always asks for confirmation, and
-- launches the platform opener without a shell. state.git holds the normalized URLs.
local function open_url(url)
  if not url or url == "" then
    notify("Dashboard: no repository URL")
    return
  end
  local ok, result, err = pcall(gvim.url.open, url)
  if not ok then
    notify("Dashboard: " .. tostring(result))
  elseif not result then
    notify("Dashboard: " .. tostring(err))
  end
end

register_hidden("click_repo_url", function()
  open_url(state.git and state.git.repo_url or "")
end)
register_hidden("click_branch_url", function()
  open_url(state.git and state.git.branch_url or "")
end)

-- One hidden command per PR row (the API caps the section at GITHUB_MAX_ROWS): a click
-- on the PR line or Enter on its keyboard row dispatches it through the same command
-- runner as the other dashboard rows, and it opens the URL gvim.url.open validated and
-- confirmed.
for i = 1, GITHUB_MAX_ROWS do
  register_hidden("click_pr_" .. i, function()
    local row = state.github and state.github.rows and state.github.rows[i]
    if not row or not row.url or row.url == "" then
      notify("Dashboard: no PR URL")
      return
    end
    open_url(row.url)
  end)
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
      if not has_file and auto_open_enabled() then
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
