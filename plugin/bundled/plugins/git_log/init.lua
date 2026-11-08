-- Git Log: a port of Fresh's plugins/git_log.ts (1268 lines), with the parts of
-- plugins/lib/git_history.ts (653 lines) and plugins/lib/git_repo.ts (272 lines) that it uses.
--
-- Ported from Fresh:
--   * a magit-style view: a commit log on the left, the selected commit's "git show
--     --stat --patch" on the right, and a one-row toolbar/hint strip above them
--   * the log rows (short hash, relative date, author initials, subject, [refs]) in Fresh's
--     column order and theme colours (gvim's theme has no syntax.number, so hash and tag use
--     syntax.constant)
--   * the detail stream: "git show --stat --patch", excluding per-file diffs over 2000
--     changed lines with Fresh's footer that names them, and a durable per-sha cache
--   * live selection: Up/Down/j/k (and Page/Home/End through the normal keymap) move the log
--     cursor, and the detail replaces itself after a short debounce; Enter focuses the detail
--     (Fresh's mode), and on the detail Enter opens the changed file at the diff cursor
--     ("git show <sha>:<path>")
--   * Fresh's four palette commands and their i18n keys/titles; its read-only mode keys
--     (q/Esc close, r refresh, y copy hash, Tab switch pane, RET enter)
--
-- Reduction vs Fresh (documented in docs/plugins/bundled.md too):
--   * Group-lite: a group is a layout of splits of the current tab, and the three panel
--     buffers are ordinary tabs too; Fresh's synthetic single group tab and the
--     setBufferGroupPanelBuffer retarget are not in this API level. The port therefore uses
--     ONE detail buffer and a disk cache per sha (Fresh kept one buffer per visited commit
--     and swapped it into the panel).
--   * The toolbar is a hint row, not a row of clickable Button widgets (group panel buffers
--     carry no widget panel layer); all actions stay on the mode keys and the palette.
--   * The log list is a styled buffer whose cursor is the selection, tinted with a line
--     decoration, not a described List widget; Fresh's enter/select event plumbing is
--     replaced by the cursor_moved event (debounced like Fresh's select).
--   * The detail is raw "git show" text with per-line decorations for +/- backgrounds and
--     header colours; gvim paints no .diff diff-background layer for an ordinary buffer.
--   * No HEAD watcher: r refreshes (Fresh has the same manual refresh; it watches nothing
--     for this view either).
--   * A historical file view takes its language from a small extension map: Lua has no
--     "detect the language from this file name" call.
--   * Layout: gvim's plugin group layout has ratio-only leaves (no fixed height), so the
--     toolbar's Fresh "fixed height 1" is accepted and ignored and it gets its ratio share
--     (about two rows). The file/hunk fold ranges are published with
--     gvim.decor.set_folding_ranges when the host has it: the pane paints the fold marker,
--     hides a collapsed body, shows the placeholder, and the normal fold action toggles it.
--   * View state: the detail's scroll line per commit is saved with
--     gvim.buf.get/set_window_state and restored when the commit is revisited (the seam is
--     in the shipped TUI; a host without it starts the commit at the top). State is
--     in-session only and cleared with the buffer.
--
-- Everything is bounded: at most 200 commits in the log, a 4 MiB/20,000-line diff stream
-- (killed with a footer when it would pass), at most 4000 decorations and 2000 fold ranges,
-- and the only whole-buffer read is the cache load (4 MiB). All git work runs through
-- gvim.proc.spawn/run callbacks; the UI thread never waits for git.
local gvim = require "gvim"

local t = gvim.i18n.t

-- ---------------------------------------------------------------------------
-- Caps and fixed sizes
-- ---------------------------------------------------------------------------
local MAX_COMMITS = 200
local MAX_LOG_OUTPUT = 4 * 1024 * 1024
local MAX_NUMSTAT_OUTPUT = 1024 * 1024
local MAX_DIFF_LINES_PER_FILE = 2000
local MAX_OMITTED_FILES = 200
local MAX_DIFF_BYTES = 4 * 1024 * 1024
local MAX_DIFF_LINES = 20000
local MAX_DECO = 4000
local MAX_FOLD_RANGES = 2000
local MAX_ENTER_SCAN_LINES = 5000
local MAX_FILE_VIEW_BYTES = 8 * 1024 * 1024
local MAX_SUBJECT_CELLS = 160
local MAX_REFS_CELLS = 120
local MAX_AUTHOR_CELLS = 80
local MAX_LINE_PARSE = 4096
local SELECT_DEBOUNCE_MS = 60
local GIT_TIMEOUT_MS = 15000
local CACHE_SUBDIR = "git_log"
local END_OF_LINE = 1073741824 -- 2^30: "the end of the line" for a Range column

local NUL = string.char(0)
local RS = string.char(30)

-- ---------------------------------------------------------------------------
-- Theme styles (Fresh's GIT_THEME, in gvim's theme keys/semantic names)
-- ---------------------------------------------------------------------------
local STYLE = {
  header = { fg = "syntax.keyword", bold = true },
  hash = { fg = "syntax.constant", bold = true },
  author = { fg = "syntax.function", bold = true },
  date = { fg = "syntax.string" },
  subject = { fg = "editor.fg" },
  muted = { fg = "editor.line_number_fg" },
  footer = { fg = "editor.line_number_fg", italic = true },
  ref_branch = { fg = "syntax.type", bold = true },
  ref_remote = { fg = "syntax.function", bold = true },
  ref_tag = { fg = "syntax.constant", bold = true },
  ref_head = { fg = "syntax.keyword", bold = true },
  diff_add = { fg = "diagnostic.info_fg", bg = "editor.diff_add_bg", extend = true },
  diff_remove = { fg = "diagnostic.error_fg", bg = "editor.diff_remove_bg", extend = true },
  diff_hunk = { fg = "syntax.type", bold = true },
  selection = { bg = "editor.selection_bg" },
}

-- ---------------------------------------------------------------------------
-- Hardened git command lines (the same rules as the bundled gvim.git module and
-- Fresh's diffArgs: a repository can never make git run a program for us, and the
-- patch format the parser reads is pinned back to its documented default).
-- ---------------------------------------------------------------------------
local NULL_DEVICE = gvim.platform == "windows" and "NUL" or "/dev/null"
local GIT_C = {
  "git",
  "-c", "core.fsmonitor=false",
  "-c", "core.pager=cat",
  "-c", "diff.external=",
  "-c", "core.attributesFile=" .. NULL_DEVICE,
  "-c", "protocol.ext.allow=never",
}
local DIFF_C = {
  "-c", "diff.noprefix=false",
  "-c", "diff.mnemonicPrefix=false",
  "-c", "diff.srcPrefix=a/",
  "-c", "diff.dstPrefix=b/",
  "-c", "core.quotepath=false",
  "-c", "diff.suppressBlankEmpty=false",
  "-c", "diff.relative=false",
  "-c", "diff.submodule=short",
  "-c", "core.bigFileThreshold=1m",
}
local GIT_ENV = {
  GIT_OPTIONAL_LOCKS = "0",
  GIT_TERMINAL_PROMPT = "0",
  GIT_CONFIG_NOSYSTEM = "1",
}

-- git_argv builds a full git command line: the hardened -c settings, the subcommand,
-- the caller's options, the pinned diff flags when this command's output is parsed as
-- a patch, then the operands.
local function git_argv(sub, opts, operands, pinned)
  local out = {}
  for _, v in ipairs(GIT_C) do
    out[#out + 1] = v
  end
  if pinned then
    out[#out + 1] = "--no-optional-locks"
    for _, v in ipairs(DIFF_C) do
      out[#out + 1] = v
    end
  end
  for _, v in ipairs(sub) do
    out[#out + 1] = v
  end
  for _, v in ipairs(opts or {}) do
    out[#out + 1] = v
  end
  if pinned then
    out[#out + 1] = "--no-ext-diff"
    out[#out + 1] = "--no-textconv"
    out[#out + 1] = "--no-color"
  end
  for _, v in ipairs(operands or {}) do
    out[#out + 1] = v
  end
  return out
end

local function git_run(sub, opts, operands, opt)
  opt = opt or {}
  return gvim.proc.run(git_argv(sub, opts, operands, opt.pinned), {
    cwd = opt.cwd,
    env = GIT_ENV,
    timeout_ms = GIT_TIMEOUT_MS,
    max_output = opt.max_output or MAX_NUMSTAT_OUTPUT,
  })
end

local function git_spawn(sub, opts, operands, opt)
  opt = opt or {}
  return gvim.proc.spawn(git_argv(sub, opts, operands, opt.pinned), {
    cwd = opt.cwd,
    env = GIT_ENV,
    timeout_ms = GIT_TIMEOUT_MS,
    on_stdout = opt.on_stdout,
    on_stderr = opt.on_stderr,
    on_exit = opt.on_exit,
  })
end

-- ---------------------------------------------------------------------------
-- Small helpers
-- ---------------------------------------------------------------------------
local function cells(s, n)
  return gvim.text.truncate(tostring(s or ""), n)
end

local function clip(s, n)
  s = tostring(s or "")
  if #s <= n then
    return s
  end
  local cut = s:sub(1, n)
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
  return cut
end

local function first_line(s)
  s = tostring(s or "")
  return (s:match("^[^\r\n]*")) or ""
end

local function clamp(v, lo, hi)
  if v < lo then
    return lo
  end
  if v > hi then
    return hi
  end
  return v
end

-- Fresh's authorInitials: "Ann Author" -> "AA", one word -> its first two letters.
local function author_initials(author)
  local cleaned = author:gsub("[<>].*", ""):gsub("^%s+", ""):gsub("%s+$", "")
  local parts = {}
  for word in cleaned:gmatch("%S+") do
    parts[#parts + 1] = word
    if #parts >= 8 then
      break
    end
  end
  if #parts == 0 then
    return "??"
  end
  if #parts == 1 then
    return clip(parts[1]:upper(), 2)
  end
  local first = parts[1]:sub(1, 1)
  local last = parts[#parts]:sub(1, 1)
  return (first .. last):upper()
end

local function short_hash(hash)
  if type(hash) ~= "string" then
    return ""
  end
  return hash:sub(1, 7)
end

-- ---------------------------------------------------------------------------
-- State
-- ---------------------------------------------------------------------------
local state = {
  open = false,
  group = nil, -- buffer group id
  repo = nil, -- resolved repository root
  commits = {},
  selected = 1,
  path_filter = nil,
  ids = {}, -- numeric buffer ids: log/detail/toolbar
  bufs = {}, -- Buffer handles
  cursor_sub = nil,
  closed_sub = nil,
}

-- The detail panel's stream state. gen numbers a stream; callbacks from an older
-- generation are dropped, so a killed git can never write into a newer commit.
local detail = {
  gen = 0,
  proc = nil,
  sha = nil,
  bytes = 0,
  lines = 0,
  partial = "",
  skipping = false,
  deco = 0,
  truncated = false,
  done = false,
  run = nil, -- the open +/- decoration run: {class=, start_line=, style=}
  hunks = {}, -- {line=, file=, new_start=}
  folds = {}, -- {start_line, end_line}
  file = nil, -- the diff file the stream is in
  fold_file_start = nil,
  fold_hunk_start = nil,
  last_line = -1,
  stderr = "",
}

local ns_log = gvim.decor.namespace("git_log")
local ns_detail = gvim.decor.namespace("git_log_diff")

-- Forward declarations (the mode actions and the debounced loader refer to each other).
local load_detail
local close_log
local open_log
local detail_open_at_cursor

-- Debounced selection -> detail (Fresh's SELECT_DEBOUNCE_MS).
local schedule_detail = gvim.timer.debounce(SELECT_DEBOUNCE_MS, function(index)
  if state.open then
    load_detail(index)
  end
end)

-- ---------------------------------------------------------------------------
-- Log fetching and parsing (git_history.fetchGitLog)
-- ---------------------------------------------------------------------------
-- The record format is git's OWN %x00/%x1e escapes: an argv element cannot carry a real
-- NUL byte (the host refuses it). The stdout it produces does contain them.
local LOG_FORMAT = "%H%x00%h%x00%an%x00%ae%x00%ai%x00%ar%x00%D%x00%s%x1e"

local function fetch_log(root, path_filter)
  local operands = {}
  if path_filter then
    local rel = gvim.path.relative(root, path_filter)
    if rel and rel ~= "" then
      path_filter = rel
    end
    operands = { "--", path_filter }
  end
  local r, err = git_run({ "log" }, {
    "--no-show-signature",
    "--format=" .. LOG_FORMAT,
    "-n" .. MAX_COMMITS,
  }, operands, { cwd = root, max_output = MAX_LOG_OUTPUT })
  if not r then
    return nil, err
  end
  if not r.ok then
    -- Fresh treats a failing log as "no commits" (a repository with no HEAD, a bad
    -- pathspec); the caller shows status.no_commits.
    return {}, nil
  end
  local commits = {}
  for _, record in ipairs(gvim.text.split(r.stdout, RS, true)) do
    if #commits >= MAX_COMMITS then
      break
    end
    if record:find("%S") then
      local parts = gvim.text.split(record, NUL, true)
      if #parts >= 8 then
        commits[#commits + 1] = {
          hash = clip(gvim.text.trim(parts[1]), 64),
          short_hash = clip(gvim.text.trim(parts[2]), 16),
          author = cells(gvim.text.trim(parts[3]), MAX_AUTHOR_CELLS),
          email = clip(gvim.text.trim(parts[4]), 128),
          date = clip(gvim.text.trim(parts[5]), 40),
          relative_date = cells(gvim.text.trim(parts[6]), 24),
          refs = cells(gvim.text.trim(parts[7]), MAX_REFS_CELLS),
          subject = cells(gvim.text.trim(parts[8]), MAX_SUBJECT_CELLS),
        }
      end
    end
  end
  return commits, nil
end

-- ---------------------------------------------------------------------------
-- Log rendering (git_history.buildCommitLogEntries)
-- ---------------------------------------------------------------------------
local function ref_style(token)
  local s = gvim.text.trim(token)
  if s:sub(1, 4) == "tag:" then
    return STYLE.ref_tag
  end
  if s:sub(1, 4) == "HEAD" then
    return STYLE.ref_head
  end
  if s:find("/", 1, true) then
    return STYLE.ref_remote
  end
  return STYLE.ref_branch
end

local function log_column_widths(commits)
  local hash_w, date_w, author_w = 7, 10, 2
  for _, c in ipairs(commits) do
    hash_w = math.max(hash_w, #c.short_hash)
    date_w = math.max(date_w, gvim.text.width(c.relative_date))
    author_w = math.max(author_w, #author_initials(c.author))
  end
  return math.min(hash_w, 12), math.min(date_w, 16), math.min(author_w, 4)
end

local function pad(s, n)
  local w = gvim.text.width(s)
  if w >= n then
    return s
  end
  return s .. string.rep(" ", n - w)
end

-- build_log_entries turns the commits into one styled content row each.
local function build_log_entries()
  local commits = state.commits
  if #commits == 0 then
    return {
      { text = "\n" },
      { text = t("panel.no_commits") .. "\n", style = STYLE.muted },
    }
  end
  local hash_w, date_w, author_w = log_column_widths(commits)
  local entries = {}
  for i, commit in ipairs(commits) do
    local segments = {
      { text = " " },
      { text = pad(commit.short_hash, hash_w), style = STYLE.hash },
      { text = "  " },
      { text = pad(commit.relative_date, date_w), style = STYLE.date },
      { text = "  (" },
      { text = pad(author_initials(commit.author), author_w), style = STYLE.author },
      { text = ") " },
      { text = commit.subject, style = STYLE.subject },
    }
    if commit.refs ~= "" then
      segments[#segments + 1] = { text = "  " }
      local tokens = gvim.text.split(commit.refs, ",", true)
      local shown = 0
      for _, token in ipairs(tokens) do
        token = gvim.text.trim(token)
        if token ~= "" and shown < 12 then
          if shown > 0 then
            segments[#segments + 1] = { text = " " }
          end
          segments[#segments + 1] = { text = "[" .. token .. "]", style = ref_style(token) }
          shown = shown + 1
        end
      end
    end
    segments[#segments + 1] = { text = "\n" }
    entries[#entries + 1] = {
      segments = segments,
      props = { type = "git_log_commit", index = i, hash = commit.hash },
    }
  end
  return entries
end

local function scope_label()
  if state.path_filter then
    return t("panel.scope_file")
  end
  return t("panel.scope_repo")
end

local function build_toolbar_entries()
  local hint = t("panel.log_hint", { count = #state.commits, scope = scope_label() })
  return {
    {
      segments = {
        { text = t("panel.commits_header"), style = STYLE.header },
        { text = " " .. #state.commits .. "  ", style = STYLE.muted },
        { text = clip(hint, 400), style = STYLE.muted },
      },
      props = { type = "git_log_toolbar" },
    },
  }
end

local function set_cursor(buf, line, reveal)
  if not buf then
    return
  end
  buf:set_cursor(gvim.pos(line, 0), { reveal = reveal ~= false })
end

local function paint_selection()
  local buf = state.bufs.log
  if not buf then
    return
  end
  local line = math.max(state.selected - 1, 0)
  ns_log:set(buf, { { kind = "line", line = line, style = STYLE.selection } })
end

local function render_log()
  local buf = state.bufs.log
  if not buf then
    return
  end
  buf:set_content(build_log_entries())
  paint_selection()
  set_cursor(buf, math.max(state.selected - 1, 0), true)
end

local function render_toolbar()
  if state.bufs.toolbar then
    state.bufs.toolbar:set_content(build_toolbar_entries())
  end
end

-- ---------------------------------------------------------------------------
-- Detail stream
-- ---------------------------------------------------------------------------
local cache_dir_value

local function cache_dir()
  if cache_dir_value then
    return cache_dir_value
  end
  local dir = gvim.path.join(gvim.dirs.data(), CACHE_SUBDIR)
  gvim.fs.mkdir(dir, { parents = true })
  cache_dir_value = dir
  return dir
end

local function cache_paths(sha)
  local base = gvim.path.join(cache_dir(), sha)
  return base .. ".diff", base .. ".json", base .. ".done"
end

-- close_run flushes one +/- run: one highlight decoration covers all of its lines, so a
-- huge diff costs a few hundred host calls, not one per line.
local function close_run(end_line)
  local run = detail.run
  if not run then
    return
  end
  detail.run = nil
  if end_line < run.start_line or detail.deco >= MAX_DECO then
    return
  end
  local buf = state.bufs.detail
  if not buf then
    return
  end
  detail.deco = detail.deco + 1
  ns_detail:add(buf, {
    kind = "highlight",
    range = gvim.range(run.start_line, 0, end_line, END_OF_LINE),
    style = run.style,
  })
end

local function add_deco(line, style, class)
  if class == "add" or class == "del" then
    local run = detail.run
    if run and run.class == class then
      return
    end
    close_run(line - 1)
    detail.run = { class = class, start_line = line, style = style }
    return
  end
  close_run(line - 1)
  if detail.deco >= MAX_DECO then
    return
  end
  local buf = state.bufs.detail
  if not buf then
    return
  end
  detail.deco = detail.deco + 1
  ns_detail:add(buf, { kind = "line", line = line, style = style })
end

-- close_fold_hunk/close_fold_file close the open fold ranges at end_line (inclusive).
local function close_fold_hunk(end_line)
  if detail.fold_hunk_start and end_line > detail.fold_hunk_start then
    detail.folds[#detail.folds + 1] = { detail.fold_hunk_start, end_line }
  end
  detail.fold_hunk_start = nil
end

local function close_fold_file(end_line)
  close_fold_hunk(end_line)
  if detail.fold_file_start and end_line > detail.fold_file_start then
    detail.folds[#detail.folds + 1] = { detail.fold_file_start, end_line }
  end
  detail.fold_file_start = nil
end

-- diff_file_from_header returns the new-side path of a "diff --git a/X b/Y" line.
local function diff_file_from_header(line)
  local a, b = line:match("^diff %-%-git a/(.*) b/(.*)$")
  if b then
    return b
  end
  return a
end

local META_FIELDS = {
  Author = true, Date = true, Commit = true, Merge = true,
  AuthorDate = true, CommitDate = true, Encoding = true,
}

local function is_meta_line(line)
  if line:sub(1, 7) == "commit " then
    return true
  end
  local name = line:match("^(%a+):")
  return name ~= nil and META_FIELDS[name] == true
end

-- classify_one_line parses one diff line into the fold ranges, the hunk anchors and
-- the per-line decorations.
local function classify_one_line(line, idx)
  if line:sub(1, 11) == "diff --git " then
    close_fold_file(idx - 1)
    detail.file = diff_file_from_header(line)
    detail.fold_file_start = idx
    add_deco(idx, STYLE.header, "hdr")
    return
  end
  if line:sub(1, 6) == "+++ b/" then
    detail.file = gvim.text.trim(line:sub(7))
    add_deco(idx, STYLE.header, "hdr2")
    return
  end
  if line:sub(1, 4) == "+++ " then
    -- /dev/null: a deletion section has no new-side file to open.
    detail.file = nil
    add_deco(idx, STYLE.muted, "hdr2")
    return
  end
  if line:sub(1, 4) == "--- " or line:sub(1, 6) == "index " then
    add_deco(idx, STYLE.muted, "hdr2")
    return
  end
  if line:sub(1, 3) == "@@ " then
    close_fold_hunk(idx - 1)
    local new_start = line:match("^@@ %-%d+[^ ]* %+(%d+)")
    detail.hunks[#detail.hunks + 1] = {
      line = idx, file = detail.file, new_start = tonumber(new_start) or 1,
    }
    detail.fold_hunk_start = idx
    add_deco(idx, STYLE.diff_hunk, "hunk")
    return
  end
  if is_meta_line(line) then
    add_deco(idx, STYLE.muted, "meta")
    return
  end
  local first = line:sub(1, 1)
  if first == "+" then
    add_deco(idx, STYLE.diff_add, "add")
  elseif first == "-" then
    add_deco(idx, STYLE.diff_remove, "del")
  elseif first == " " or line == "" then
    add_deco(idx, nil, "ctx")
  else
    add_deco(idx, nil, "text")
  end
end

-- feed_stream parses one stdout chunk line by line. The partial line is carried across
-- chunks; a single line longer than MAX_LINE_PARSE is classified by its start and its
-- remainder skipped (the parser only ever needs headers at the start of a line).
local function feed_stream(chunk)
  local buf = detail.partial .. chunk
  local start = 1
  if detail.skipping then
    local nl = buf:find("\n", 1, true)
    if not nl then
      detail.partial = ""
      return
    end
    detail.skipping = false
    start = nl + 1
  end
  while true do
    local nl = buf:find("\n", start, true)
    if not nl then
      break
    end
    if detail.lines >= MAX_DIFF_LINES then
      detail.truncated = true
      detail.partial = ""
      return
    end
    local line = buf:sub(start, nl - 1)
    start = nl + 1
    local idx = detail.lines
    detail.lines = detail.lines + 1
    detail.last_line = idx
    classify_one_line(line, idx)
  end
  detail.partial = buf:sub(start)
  if #detail.partial > MAX_LINE_PARSE then
    local idx = detail.lines
    detail.lines = detail.lines + 1
    detail.last_line = idx
    classify_one_line(detail.partial, idx)
    detail.partial = ""
    detail.skipping = true
  end
end

-- publish_folds hands the file/hunk ranges to the fold engine when tier 1c-A is
-- present (the port stays green on hosts without it).
local function publish_folds()
  local setter = gvim.decor and gvim.decor.set_folding_ranges
  local buf = state.bufs.detail
  if type(setter) ~= "function" or not buf then
    return
  end
  local ranges = {}
  for _, r in ipairs(detail.folds) do
    if #ranges >= MAX_FOLD_RANGES then
      break
    end
    ranges[#ranges + 1] = { start_line = r[1], end_line = r[2], kind = "region" }
  end
  pcall(setter, buf, ranges, { namespace = "git_log" })
end

-- View state (tier 1c-C): remember the detail scroll line per commit for this session.
local function save_window_state()
  if not detail.sha then
    return
  end
  local get, put = gvim.buf.get_window_state, gvim.buf.set_window_state
  local buf = state.bufs.detail
  if type(get) ~= "function" or type(put) ~= "function" or not buf then
    return
  end
  pcall(put, buf, "scroll:" .. detail.sha, buf:cursor().line)
end

local function restore_window_state(sha)
  if not sha then
    return
  end
  local get = gvim.buf.get_window_state
  local buf = state.bufs.detail
  if type(get) ~= "function" or not buf then
    return
  end
  local ok, line = pcall(get, buf, "scroll:" .. sha)
  if ok and type(line) == "number" and line > 0 then
    set_cursor(buf, line, false)
  end
end

-- append_chunk appends to the detail buffer and tees to the cache file. The buffer is
-- writable only for the life of the stream (a read-only virtual buffer refuses append).
local function append_chunk(chunk, diff_path)
  local buf = state.bufs.detail
  if not buf then
    return
  end
  buf:append(chunk)
  if diff_path then
    gvim.fs.write(diff_path, chunk, { append = true, atomic = false })
  end
end

-- finalize_stream finishes a stream exactly once: flushes the last line/run/folds,
-- writes the cache files on a clean exit, and updates the status row.
local function finalize_stream(clean, err_text, diff_path, oversized)
  if detail.done then
    return
  end
  detail.done = true
  if detail.partial ~= "" then
    local idx = detail.lines
    detail.lines = detail.lines + 1
    detail.last_line = idx
    classify_one_line(detail.partial, idx)
    detail.partial = ""
  end
  local last = math.max(detail.last_line, 0)
  close_run(last)
  close_fold_file(last)
  local buf = state.bufs.detail
  if not buf then
    return
  end
  local function seal()
    pcall(buf.set_read_only, buf, true)
  end
  if detail.truncated then
    local note = t("panel.truncated", { lines = MAX_DIFF_LINES })
    append_chunk("\n" .. note .. "\n", nil)
    seal()
    gvim.notify(t("status.error_fetching_diff", { error = note }), "warn")
    return
  end
  if not clean then
    local note = t("status.error_fetching_diff", { error = first_line(err_text) })
    append_chunk("\n" .. note .. "\n", nil)
    seal()
    gvim.notify(note, "warn")
    return
  end
  if oversized and #oversized > 0 then
    append_chunk("\n", diff_path)
    append_chunk(t("panel.omitted_header", {
      count = #oversized, lines = MAX_DIFF_LINES_PER_FILE,
    }) .. "\n", diff_path)
    for _, path in ipairs(oversized) do
      append_chunk("  " .. path .. "\n", diff_path)
    end
    append_chunk(t("panel.omitted_footer", { hash = short_hash(detail.sha) }) .. "\n", diff_path)
  end
  seal()
  local sha = detail.sha
  if sha then
    local _, meta_path, done_path = cache_paths(sha)
    local meta = { hunks = {}, folds = {}, lines = detail.lines }
    for _, h in ipairs(detail.hunks) do
      meta.hunks[#meta.hunks + 1] = { line = h.line, file = h.file or "", new_start = h.new_start or 1 }
    end
    for _, f in ipairs(detail.folds) do
      meta.folds[#meta.folds + 1] = { f[1], f[2] }
    end
    gvim.fs.write(meta_path, json.encode(meta))
    gvim.fs.write(done_path, "")
  end
  publish_folds()
  restore_window_state(sha)
  gvim.notify(t("status.commit_ready", { hash = short_hash(sha) }))
end

-- apply_meta rebuilds the hunk anchors and fold ranges from the cached metadata.
local function apply_meta(sha)
  local _, meta_path = cache_paths(sha)
  local raw = gvim.fs.read(meta_path, { max = 2 * 1024 * 1024 })
  if not raw then
    return
  end
  local ok, meta = pcall(json.decode, raw, { jsonc = false })
  if not ok or type(meta) ~= "table" then
    return
  end
  if type(meta.hunks) == "table" then
    for _, h in ipairs(meta.hunks) do
      if type(h) == "table" and type(h.line) == "number" then
        detail.hunks[#detail.hunks + 1] = {
          line = h.line, file = h.file or nil, new_start = h.new_start or 1,
        }
      end
    end
  end
  if type(meta.folds) == "table" then
    for _, f in ipairs(meta.folds) do
      if type(f) == "table" and type(f[1]) == "number" and type(f[2]) == "number" then
        detail.folds[#detail.folds + 1] = { f[1], f[2] }
      end
    end
  end
  publish_folds()
end

local function detail_placeholder(text)
  local buf = state.bufs.detail
  if not buf then
    return
  end
  buf:set_read_only(true)
  buf:set_content({
    { text = "\n" },
    { text = "  " .. text .. "\n", style = STYLE.muted },
  })
end

local function detail_clear()
  local buf = state.bufs.detail
  if not buf then
    return
  end
  ns_detail:clear(buf)
  local clear = gvim.decor and gvim.decor.clear_folds
  if type(clear) == "function" then
    pcall(clear, buf, { namespace = "git_log" })
  end
  buf:set_read_only(false)
  buf:set_text("")
  buf:set_read_only(true)
end

local function stream_detail(commit, gen)
  local sha = commit.hash
  local diff_path = cache_paths(sha)
  gvim.fs.write(diff_path, "")
  local oversized = {}
  local num, num_err = git_run({ "show" }, { "--numstat", "--format=" }, { sha }, {
    cwd = state.repo, pinned = true, max_output = MAX_NUMSTAT_OUTPUT,
  })
  if num and num.ok then
    for _, line in ipairs(gvim.text.split(num.stdout, "\n", true)) do
      if #oversized < MAX_OMITTED_FILES then
        local added, removed, path = line:match("^(%S+)\t(%S+)\t(.*)$")
        if path and #path > 0 then
          local n = (tonumber(added) or 0) + (tonumber(removed) or 0)
          if n > MAX_DIFF_LINES_PER_FILE then
            oversized[#oversized + 1] = path
          end
        end
      end
    end
  elseif num_err then
    gvim.log.debug("git_log: numstat: %s", tostring(num_err))
  end
  local operands = { sha }
  if #oversized > 0 then
    operands[#operands + 1] = "--"
    operands[#operands + 1] = "."
    for _, path in ipairs(oversized) do
      operands[#operands + 1] = ":(exclude,top)" .. path
    end
  end
  local proc, err = git_spawn({ "show" }, { "--stat", "--patch" }, operands, {
    cwd = state.repo,
    pinned = true,
    on_stdout = function(chunk)
      if gen ~= detail.gen or detail.done then
        return
      end
      detail.bytes = detail.bytes + #chunk
      if detail.bytes > MAX_DIFF_BYTES then
        local room = MAX_DIFF_BYTES - (detail.bytes - #chunk)
        if room > 0 then
          append_chunk(chunk:sub(1, room), diff_path)
          feed_stream(chunk:sub(1, room))
        end
        detail.truncated = true
        if detail.proc then
          pcall(detail.proc.kill, detail.proc, "TERM")
          detail.proc = nil
        end
        finalize_stream(false, "too_large", diff_path, oversized)
        return
      end
      append_chunk(chunk, diff_path)
      feed_stream(chunk)
      if detail.truncated and not detail.done then
        if detail.proc then
          pcall(detail.proc.kill, detail.proc, "TERM")
          detail.proc = nil
        end
        finalize_stream(false, "too_large", diff_path, oversized)
      end
    end,
    on_stderr = function(chunk)
      if gen ~= detail.gen then
        return
      end
      if #detail.stderr < 512 then
        detail.stderr = detail.stderr .. chunk
      end
    end,
    on_exit = function(result)
      if gen ~= detail.gen then
        return
      end
      detail.proc = nil
      -- The exit callback may be delivered before the queued on_stdout tasks of the same
      -- process, so finishing waits one tick: every chunk is appended and parsed first.
      gvim.timer.after(1, function()
        if gen ~= detail.gen or detail.done then
          return
        end
        local clean = result ~= nil and result.ok == true and not detail.truncated
        finalize_stream(clean, detail.stderr, diff_path, oversized)
      end)
    end,
  })
  if not proc then
    detail.done = true
    detail_placeholder(t("status.failed_open_details"))
    gvim.notify(t("status.failed_open_details"), "warn")
    gvim.log.debug("git_log: spawn: %s", tostring(err))
    return
  end
  detail.proc = proc
  gvim.notify(t("status.loading_commit", { hash = commit.short_hash }))
end

load_detail = function(index)
  if not state.open or not state.bufs.detail then
    return
  end
  local commit = state.commits[index]
  if not commit then
    return
  end
  state.selected = index
  paint_selection()
  -- Kill the previous stream: its callbacks are dropped by the generation bump.
  if detail.proc then
    pcall(detail.proc.kill, detail.proc, "TERM")
    detail.proc = nil
  end
  detail.gen = detail.gen + 1
  local gen = detail.gen
  save_window_state()
  local sha = commit.hash
  detail.sha = sha
  detail.bytes, detail.lines, detail.deco = 0, 0, 0
  detail.partial, detail.skipping, detail.truncated, detail.done = "", false, false, false
  detail.run, detail.hunks, detail.folds = nil, {}, {}
  detail.file = nil
  detail.fold_file_start, detail.fold_hunk_start = nil, nil
  detail.last_line, detail.stderr = -1, ""
  detail_clear()
  local diff_path, meta_path, done_path = cache_paths(sha)
  if gvim.fs.exists(done_path) and gvim.fs.exists(diff_path) then
    local text = gvim.fs.read(diff_path, { max = MAX_DIFF_BYTES })
    if text then
      local buf = state.bufs.detail
      buf:set_read_only(false)
      buf:set_text(text)
      buf:set_read_only(true)
      detail.lines = 0
      for _ in text:gmatch("\n") do
        detail.lines = detail.lines + 1
      end
      detail.done = true
      apply_meta(sha)
      restore_window_state(sha)
      gvim.notify(t("status.commit_ready", { hash = commit.short_hash }))
      return
    end
  end
  local buf = state.bufs.detail
  if buf then
    buf:set_read_only(false)
  end
  stream_detail(commit, gen)
end

-- hunk_at returns the last hunk header at or before line (binary search).
local function hunk_at(line)
  local hunks = detail.hunks
  local lo, hi, found = 1, #hunks, nil
  while lo <= hi do
    local mid = math.floor((lo + hi) / 2)
    if hunks[mid].line <= line then
      found = hunks[mid]
      lo = mid + 1
    else
      hi = mid - 1
    end
  end
  return found
end

-- language_for is the small extension map of a historical file view.
local LANGUAGE_BY_EXT = {
  go = "go", rs = "rust", py = "python", js = "javascript", jsx = "javascript",
  ts = "typescript", tsx = "typescript", c = "c", h = "c", cc = "cpp", cpp = "cpp",
  hpp = "cpp", java = "java", kt = "kotlin", rb = "ruby", php = "php", cs = "csharp",
  json = "json", yaml = "yaml", yml = "yaml", toml = "toml", md = "markdown",
  sh = "bash", bash = "bash", zsh = "bash", lua = "lua", sql = "sql", html = "html",
  css = "css", xml = "xml", swift = "swift",
}

local function language_for(path)
  local ext = gvim.path.extname(path or ""):lower()
  if ext:sub(1, 1) == "." then
    ext = ext:sub(2)
  end
  return LANGUAGE_BY_EXT[ext]
end

-- detail_open_at_cursor is Enter on the detail: it opens the new-side line the diff row
-- under the cursor addresses, as a read-only virtual buffer of "<sha>:<path>".
detail_open_at_cursor = function()
  local buf = state.bufs.detail
  if not state.open or not buf or not detail.sha then
    return
  end
  local cursor = buf:cursor()
  local hunk = hunk_at(cursor.line)
  if not hunk or not hunk.file or hunk.file == "" then
    gvim.notify(t("status.move_to_diff_with_context"))
    return
  end
  local sha = detail.sha
  local file = hunk.file
  local count = cursor.line - hunk.line - 1
  if count > MAX_ENTER_SCAN_LINES then
    gvim.notify(t("status.move_to_diff"))
    return
  end
  local target = hunk.new_start
  if count > 0 then
    local lines = buf:lines(hunk.line + 1, count)
    for _, line in ipairs(lines or {}) do
      local c = line:sub(1, 1)
      if c == "+" or c == " " or line == "" then
        target = target + 1
      end
    end
  end
  local commit = state.commits[state.selected]
  local short = commit and commit.short_hash or short_hash(sha)
  gvim.notify(t("status.file_loading", { file = file, hash = short }))
  local r, err = git_run({ "show" }, {}, { sha .. ":" .. file }, {
    cwd = state.repo, max_output = MAX_FILE_VIEW_BYTES,
  })
  if not r or not r.ok or r.truncated then
    gvim.notify(t("status.file_not_found", { file = file, hash = short }), "warn")
    gvim.log.debug("git_log: show file: %s", tostring(err))
    return
  end
  local lines = gvim.text.split(r.stdout, "\n", true)
  if #lines > 1 and lines[#lines] == "" then
    lines[#lines] = nil
  end
  local view, create_err = gvim.buf.create {
    name = "*" .. short .. ":" .. file .. "*",
    text = table.concat(lines, "\n"),
    language = language_for(file),
    read_only = true,
    mode = "git_log_file_view",
    focus = true,
  }
  if not view then
    gvim.notify(t("status.failed_open_file", { file = file }), "warn")
    gvim.log.debug("git_log: file view: %s", tostring(create_err))
    return
  end
  set_cursor(view, math.max(target - 1, 0), true)
  gvim.notify(t("status.file_view_ready", { file = file, hash = short, line = target }))
end

-- ---------------------------------------------------------------------------
-- Repository/open/close
-- ---------------------------------------------------------------------------
local function resolve_repo(path)
  if not gvim.has("git") then
    return nil, "git binary not found"
  end
  local probe = path
  if not probe or probe == "" then
    probe = gvim.workspace.root()
  end
  if not probe or probe == "" then
    return nil, nil
  end
  local repo, err = gvim.git.repo(probe)
  if repo and repo.root and repo.root ~= "" then
    return repo.root
  end
  if err then
    return nil, err
  end
  local root = gvim.workspace.root()
  if root ~= "" and root ~= probe then
    repo, err = gvim.git.repo(root)
    if repo and repo.root and repo.root ~= "" then
      return repo.root
    end
  end
  return nil, err
end

local function close_detail_stream()
  detail.gen = detail.gen + 1
  if detail.proc then
    pcall(detail.proc.kill, detail.proc, "TERM")
    detail.proc = nil
  end
  detail.done = true
end

local function unsubscribe()
  if state.cursor_sub then
    state.cursor_sub:off()
    state.cursor_sub = nil
  end
  if state.closed_sub then
    state.closed_sub:off()
    state.closed_sub = nil
  end
end

local function on_buffer_closed(ev)
  if not state.open then
    return
  end
  local id = ev.id
  if id == state.ids.log or id == state.ids.detail or id == state.ids.toolbar then
    close_log(true)
    gvim.notify(t("status.closed"))
  end
end

close_log = function(quiet)
  if not state.open then
    return
  end
  state.open = false
  save_window_state()
  close_detail_stream()
  unsubscribe()
  local group = state.group
  local bufs = state.bufs
  state.group = nil
  state.bufs = {}
  state.ids = {}
  if group then
    gvim.buf.close_group(group)
  end
  for _, key in ipairs({ "log", "detail", "toolbar" }) do
    local buf = bufs[key]
    if buf then
      pcall(buf.close, buf)
    end
  end
  state.commits = {}
  state.selected = 1
  state.path_filter = nil
  state.repo = nil
  detail.sha = nil
  if not quiet then
    gvim.notify(t("status.closed"))
  end
end

local function make_buffers()
  local log_buf, err = gvim.buf.create {
    name = "*Git Log*", read_only = true, focus = false,
  }
  if not log_buf then
    return nil, err
  end
  local detail_buf, derr = gvim.buf.create {
    name = "*Git Log: Detail*", read_only = true, focus = false, language = "diff",
  }
  if not detail_buf then
    pcall(log_buf.close, log_buf)
    return nil, derr
  end
  local toolbar_buf, terr = gvim.buf.create {
    name = "*Git Log: Hints*", read_only = true, focus = false,
  }
  if not toolbar_buf then
    pcall(log_buf.close, log_buf)
    pcall(detail_buf.close, detail_buf)
    return nil, terr
  end
  return { log = log_buf, detail = detail_buf, toolbar = toolbar_buf }
end

local GROUP_LAYOUT = '{"type":"split","direction":"v","ratio":0.07,' ..
  '"first":{"type":"fixed","id":"toolbar","height":1,"bufferId":%d},' ..
  '"second":{"type":"split","direction":"h","ratio":0.6,' ..
  '"first":{"type":"scrollable","id":"log","bufferId":%d},' ..
  '"second":{"type":"scrollable","id":"detail","bufferId":%d}}}'

local function bind_group(group, bufs)
  state.bufs = bufs
  state.ids = {
    log = bufs.log.id,
    detail = bufs.detail.id,
    toolbar = bufs.toolbar.id,
  }
  bufs.log:set_mode("git_log")
  bufs.detail:set_mode("git_log_detail")
  state.cursor_sub = gvim.on("cursor_moved", function(ev)
    if not state.open or ev.buf == nil or ev.buf.id ~= state.ids.log then
      return
    end
    local line = ev.pos.line + 1
    if line < 1 or line > #state.commits or line == state.selected then
      return
    end
    state.selected = line
    paint_selection()
    gvim.notify(t("status.commit_position", { current = line, total = #state.commits }))
    schedule_detail(line)
  end, { buf = bufs.log })
  state.closed_sub = gvim.on("buffer_closed", on_buffer_closed)
  gvim.buf.set_group_title(group, {
    { text = "Git Log" },
    { text = state.path_filter and (": " .. gvim.path.basename(state.path_filter)) or "" },
  })
  gvim.buf.focus_panel(group, "log")
  render_toolbar()
  render_log()
  gvim.notify(t("status.log_ready", { count = #state.commits }))
  if #state.commits > 0 then
    load_detail(1)
  else
    detail_placeholder(t("panel.select"))
  end
end

open_log = function(path_filter)
  if state.open then
    if state.path_filter ~= path_filter then
      close_log(true)
    else
      if state.group then
        gvim.buf.focus_panel(state.group, "log")
      end
      gvim.notify(t("status.already_open"))
      return
    end
  end
  gvim.notify(t("status.loading"))
  local repo, repo_err = resolve_repo(path_filter)
  if not repo then
    if repo_err then
      gvim.notify(t("status.git_error", { error = first_line(repo_err) }), "warn")
    else
      gvim.notify(t("status.no_commits"))
    end
    return
  end
  local commits, fetch_err = fetch_log(repo, path_filter)
  if not commits then
    gvim.notify(t("status.git_error", { error = first_line(fetch_err) or "cannot read the history" }), "warn")
    return
  end
  if #commits == 0 then
    gvim.notify(t("status.no_commits"))
    return
  end
  state.repo = repo
  state.commits = commits
  state.selected = 1
  state.path_filter = path_filter
  local bufs, berr = make_buffers()
  if not bufs then
    gvim.notify(t("status.failed_open"), "warn")
    gvim.log.debug("git_log: create buffers: %s", tostring(berr))
    return
  end
  local layout = GROUP_LAYOUT:format(bufs.toolbar.id, bufs.log.id, bufs.detail.id)
  local group, gerr = gvim.buf.create_group("Git Log", layout)
  if not group then
    for _, key in ipairs({ "log", "detail", "toolbar" }) do
      pcall(bufs[key].close, bufs[key])
    end
    gvim.notify(t("status.failed_open"), "warn")
    gvim.log.debug("git_log: create group: %s", tostring(gerr))
    return
  end
  state.group = group
  state.open = true
  bind_group(group, bufs)
end

-- ---------------------------------------------------------------------------
-- Mode actions (the hidden commands the buffer-local modes bind)
-- ---------------------------------------------------------------------------
local function active_id()
  local buf = gvim.buf.current()
  return buf and buf.id or 0
end

local function selected_commit()
  return state.commits[state.selected]
end

local function move_selection(delta)
  if not state.open or not state.bufs.log then
    return
  end
  local n = #state.commits
  if n == 0 then
    return
  end
  local line = state.bufs.log:cursor().line
  local next_line = clamp(line + delta, 0, n - 1)
  if next_line ~= line then
    state.selected = next_line + 1
    set_cursor(state.bufs.log, next_line, true)
    paint_selection()
    gvim.notify(t("status.commit_position", { current = state.selected, total = n }))
    schedule_detail(state.selected)
  end
end

local function step_detail(delta)
  if not state.open then
    return
  end
  gvim.command.run(delta < 0 and "move_up" or "move_down")
end

local function enter_pane()
  if not state.open then
    return
  end
  if active_id() == state.ids.detail then
    detail_open_at_cursor()
    return
  end
  if state.group then
    gvim.buf.focus_panel(state.group, "detail")
  end
  local commit = selected_commit()
  if commit then
    gvim.notify(t("status.commit_ready", { hash = commit.short_hash }))
  end
end

local function switch_pane()
  if not state.open or not state.group then
    return
  end
  if active_id() == state.ids.detail then
    gvim.buf.focus_panel(state.group, "log")
  else
    gvim.buf.focus_panel(state.group, "detail")
  end
end

local function copy_hash()
  local commit = selected_commit()
  if not commit then
    gvim.notify(t("status.move_to_commit"))
    return
  end
  gvim.clipboard.set(commit.hash)
  gvim.notify(t("status.hash_copied", { short = commit.short_hash, full = commit.hash }))
end

local function log_window_rows()
  local group = state.group
  if not group then
    return 10
  end
  local panels = gvim.buf.group_panels(group)
  for _, panel in ipairs(panels or {}) do
    if panel.name == "log" and panel.window then
      local ok, size = pcall(function() return panel.window:size() end)
      if ok and type(size) == "table" and type(size.height) == "number" and size.height > 0 then
        return size.height
      end
    end
  end
  return 10
end

local function move_page(delta)
  if not state.open or not state.bufs.log or #state.commits == 0 then
    return
  end
  local rows = math.max(log_window_rows(), 1)
  local line = state.bufs.log:cursor().line
  set_cursor(state.bufs.log, clamp(line + delta * rows, 0, #state.commits - 1), true)
end

local function move_edge(last)
  if not state.open or not state.bufs.log or #state.commits == 0 then
    return
  end
  set_cursor(state.bufs.log, last and (#state.commits - 1) or 0, true)
end

local function file_view_close()
  local buf = gvim.buf.current()
  if buf then
    pcall(buf.close, buf)
  end
end

local function refresh_log()
  if not state.open then
    return
  end
  gvim.notify(t("status.refreshing"))
  close_detail_stream()
  local commits = fetch_log(state.repo, state.path_filter)
  if not commits then
    gvim.notify(t("status.git_error", { error = "cannot read the history" }), "warn")
    return
  end
  state.commits = commits
  if #commits == 0 then
    state.selected = 1
    render_toolbar()
    render_log()
    detail_clear()
    detail_placeholder(t("panel.select"))
    gvim.notify(t("status.refreshed", { count = 0 }))
    return
  end
  state.selected = clamp(state.selected, 1, #commits)
  render_toolbar()
  render_log()
  load_detail(state.selected)
  gvim.notify(t("status.refreshed", { count = #commits }))
end

-- ---------------------------------------------------------------------------
-- Modes (Fresh's git-log mode, split into the log, detail and file-view buffers)
-- ---------------------------------------------------------------------------
local function action(name)
  return "git_log." .. name
end

local MODE_LOG = {
  name = "git_log",
  status = t("panel.mode_log"),
  text = "swallow",
  paste = "swallow",
  fallthrough = true,
  bindings = {
    j = action("git_log_next"),
    k = action("git_log_prev"),
    enter = action("git_log_enter"),
    tab = action("git_log_tab"),
    q = action("git_log_close"),
    escape = action("git_log_close"),
    r = action("git_log_refresh"),
    y = action("git_log_copy_hash"),
  },
}

local MODE_DETAIL = {
  name = "git_log_detail",
  status = t("panel.mode_detail"),
  text = "swallow",
  paste = "swallow",
  fallthrough = true,
  bindings = {
    j = action("detail_down"),
    k = action("detail_up"),
    enter = action("git_log_enter"),
    tab = action("git_log_tab"),
    q = action("git_log_close"),
    escape = action("git_log_close"),
    r = action("git_log_refresh"),
    y = action("git_log_copy_hash"),
  },
}

local MODE_FILE = {
  name = "git_log_file_view",
  status = t("panel.mode_file"),
  text = "swallow",
  paste = "swallow",
  fallthrough = true,
  bindings = {
    q = action("git_log_file_view_close"),
    escape = action("git_log_file_view_close"),
  },
}

-- ---------------------------------------------------------------------------
-- Commands (Fresh's four palette rows + hidden action commands for the modes)
-- ---------------------------------------------------------------------------
local function register_commands()
  gvim.command.register {
    name = "show_git_log",
    title = "%cmd.git_log",
    description = "%cmd.git_log_desc",
    run = function() open_log(nil) end,
  }
  gvim.command.register {
    name = "show_git_log_current_file",
    title = "%cmd.git_log_current_file",
    description = "%cmd.git_log_current_file_desc",
    when = { "has_buffer" },
    run = function()
      local buf = gvim.buf.current()
      local path = buf and not buf:is_virtual() and buf:path() or nil
      if not path then
        gvim.notify(t("status.no_file"))
        return
      end
      open_log(path)
    end,
  }
  gvim.command.register {
    name = "git_log_close",
    title = "%cmd.git_log_close",
    description = "%cmd.git_log_close_desc",
    run = function() close_log(false) end,
  }
  gvim.command.register {
    name = "git_log_refresh",
    title = "%cmd.git_log_refresh",
    description = "%cmd.git_log_refresh_desc",
    run = function() refresh_log() end,
  }
  local hidden = {
    { "git_log_prev", function() move_selection(-1) end },
    { "git_log_next", function() move_selection(1) end },
    { "git_log_page_up", function() move_page(-1) end },
    { "git_log_page_down", function() move_page(1) end },
    { "git_log_first", function() move_edge(false) end },
    { "git_log_last", function() move_edge(true) end },
    { "git_log_enter", function() enter_pane() end },
    { "git_log_tab", function() switch_pane() end },
    { "git_log_copy_hash", function() copy_hash() end },
    { "git_log_file_view_close", function() file_view_close() end },
    { "git_log_detail_open_file", function() detail_open_at_cursor() end },
    { "detail_up", function() step_detail(-1) end },
    { "detail_down", function() step_detail(1) end },
  }
  for _, item in ipairs(hidden) do
    gvim.command.register {
      name = item[1],
      title = item[1],
      hidden = true,
      run = item[2],
    }
  end
end

gvim.mode.define(MODE_LOG)
gvim.mode.define(MODE_DETAIL)
gvim.mode.define(MODE_FILE)
register_commands()

gvim.log.debug("git log plugin loaded (group-lite port)")
