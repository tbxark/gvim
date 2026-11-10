-- Review Diff (reduced audit_mode port). Fresh's audit_mode.ts is a magit-style
-- review session: a file sidebar, a unified/side-by-side diff stream, hunk and line
-- comments, staging/unstaging/discarding, exports, watches and range/stash modes.
-- This port is deliberately read-only and much smaller:
--
--   * Review Diff reads `git status --porcelain -z -uall` plus `git diff --numstat` and
--     `git diff --cached --numstat`, and lists staged, unstaged and untracked files with
--     their additions/deletions in a picker;
--   * selecting a file opens a read-only `*Review Diff: <path>*` virtual buffer holding
--     git's unified diff (or, for an untracked file, the working file shown as a new file);
--   * a diff row's line target opens the working file at that line (new-side lines only).
--   * Review Diff: Refresh re-reads the list; Review Diff: Close drops the list/diff.
--
-- No stage/unstage/discard/comment/export/range/stash action is registered: those need to
-- write the git index or an orchestrator/comment store, neither of which this port has.
-- All git reads go through the hardened gvim.git gateway (neutralized config, no external
-- diff, no textconv) and bounded output; no shell or argv is built from file text. Scans and
-- diffs are plugin-task awaits, never work in Update, and a closed picker/diff invalidates
-- the answer by generation. An already-issued git process is not killed; its result is
-- discarded.
local gvim = require "gvim"
local git = gvim.git

local t = gvim.i18n.t

local MAX_STATUS_BYTES = 1024 * 1024
local MAX_STAT_BYTES = 1024 * 1024
local MAX_DIFF_BYTES = 2 * 1024 * 1024
local MAX_FILES = 2000
local MAX_DIFF_LINES = 20000
local MAX_TARGETS = 10000

local state = {
  repo = nil,
  files = {},
  picker = nil,
  generation = 0,
  diff_buf = nil,
  diff_hunks = {},
  diff_generation = 0,
}

local function notice(msg, level)
  gvim.notify(msg, level or "info")
end

local function truncate_text(s, limit)
  if #s <= limit then
    return s, false
  end
  local cut = s:sub(1, limit)
  while #cut > 0 and cut:byte(-1) >= 128 do
    cut = cut:sub(1, #cut - 1)
  end
  return cut, true
end

-- git_run is one bounded read through the hardened git gateway.
local function git_run(repo, args, max_output)
  local result, err = git.run(args, {
    cwd = repo.root,
    max_output = max_output or MAX_STAT_BYTES,
  })
  if not result then
    return nil, err
  end
  if not result.ok then
    return nil, result.stderr ~= "" and result.stderr or ("git exited " .. tostring(result.code))
  end
  return result
end

local function repo_root()
  local buf = gvim.buf.current()
  local here = gvim.workspace.root()
  if buf and not buf:is_virtual() and buf:path() and buf:path() ~= "" then
    here = buf:path()
  end
  local repo = git.repo(here)
  if not repo and here ~= gvim.workspace.root() then
    repo = git.repo(gvim.workspace.root())
  end
  return repo
end

local CATEGORY_ORDER = { "staged", "unstaged", "untracked" }
local CATEGORY_LABEL = { staged = "section.staged", unstaged = "section.unstaged", untracked = "section.untracked" }

-- parse_status reads porcelain v1 -z -uall. A rename/copy consumes its next NUL field as
-- the original path. A path changed on both sides appears once in each category, exactly
-- as Fresh's parseGitStatusPorcelain does.
local function parse_status(raw)
  local files = {}
  local parts = {}
  if raw and raw ~= "" then
    parts = gvim.text.split(raw, "\0", true)
    -- split keeps a trailing empty field after the final NUL; it is not an entry.
    if parts[#parts] == "" then
      parts[#parts] = nil
    end
  end
  local i = 1
  while i <= #parts do
    local entry = parts[i]
    if #entry >= 3 then
      local x, y = entry:sub(1, 1), entry:sub(2, 2)
      local path = entry:sub(4)
      local orig
      if x == "R" or x == "C" or y == "R" or y == "C" then
        i = i + 1
        orig = parts[i]
      end
      if path ~= "" then
        if x == "?" and y == "?" then
          files[#files + 1] = { path = path, status = "?", category = "untracked" }
        elseif not (x == "!" and y == "!") then
          if x ~= " " and x ~= "?" then
            files[#files + 1] = { path = path, status = x, category = "staged", orig_path = orig }
          end
          if y ~= " " and y ~= "?" then
            files[#files + 1] = { path = path, status = y, category = "unstaged", orig_path = orig }
          end
        end
      end
    end
    i = i + 1
  end
  table.sort(files, function(a, b)
    local ao = (a.category == "staged" and 1) or (a.category == "unstaged" and 2) or 3
    local bo = (b.category == "staged" and 1) or (b.category == "unstaged" and 2) or 3
    if ao ~= bo then
      return ao < bo
    end
    return a.path < b.path
  end)
  return files
end

-- parse_numstat maps category + path -> {add, del}. The same path can have a staged and an
-- unstaged numstat entry; keeping only the path made one overwrite the other. Binary rows use
-- '-' and are left unknown.
local function parse_numstat(raw, category, into)
  for add, del, path in (raw or ""):gmatch("([^\t\n]*)\t([^\t\n]*)\t([^\n]+)\n?") do
    local nadd, ndel = tonumber(add), tonumber(del)
    if path ~= "" then
      into[category .. "\0" .. path] = { add = nadd or 0, del = ndel or 0, binary = nadd == nil or ndel == nil }
    end
  end
  return into
end

local function load_review()
  if not git.available() then
    return nil, t("status.no_git")
  end
  local repo = repo_root()
  if not repo then
    return nil, t("status.not_repo")
  end
  local status, err = git_run(repo, { "--no-optional-locks", "status", "--porcelain", "-z", "-uall" }, MAX_STATUS_BYTES)
  if not status then
    return nil, t("status.failed", { error = tostring(err) })
  end
  local files = parse_status(status.stdout)
  local truncated = status.truncated or #files > MAX_FILES
  if #files > MAX_FILES then
    local cut = {}
    for i = 1, MAX_FILES do
      cut[i] = files[i]
    end
    files = cut
  end
  local stats = {}
  local unstaged, uerr = git_run(repo, { "diff", "--no-color", "--numstat", "--" }, MAX_STAT_BYTES)
  if unstaged then
    parse_numstat(unstaged.stdout, "unstaged", stats)
    truncated = truncated or unstaged.truncated
  elseif uerr then
    gvim.log.debug("audit_mode: unstaged numstat: %s", tostring(uerr))
  end
  local staged, serr = git_run(repo, { "diff", "--no-color", "--cached", "--numstat", "--" }, MAX_STAT_BYTES)
  if staged then
    parse_numstat(staged.stdout, "staged", stats)
    truncated = truncated or staged.truncated
  elseif serr then
    gvim.log.debug("audit_mode: staged numstat: %s", tostring(serr))
  end
  local total_add, total_del = 0, 0
  for _, f in ipairs(files) do
    local st = stats[f.category .. "\0" .. f.path]
    if st and not st.binary then
      f.add, f.del = st.add, st.del
      total_add, total_del = total_add + st.add, total_del + st.del
    end
  end
  return { repo = repo, files = files, add = total_add, del = total_del, truncated = truncated }
end

-- parse_diff_targets maps each new-side (+ or context) row of a unified diff to the
-- working file's 0-based line. Old-side-only rows have no working-tree location.
local function parse_diff_targets(text, abs_path)
  local targets = {}
  local new_line = nil
  local row = 0
  for line in (text .. "\n"):gmatch("(.-)\n") do
    if row >= MAX_DIFF_LINES or #targets >= MAX_TARGETS then
      break
    end
    if line:sub(1, 2) == "@@" then
      local start = line:match("^@@ %-%d+[^ ]* %+(%d+)")
      new_line = tonumber(start)
    elseif new_line then
      local first = line:sub(1, 1)
      if first == "+" then
        targets[#targets + 1] = { line = row, path = abs_path, target = new_line - 1 }
        new_line = new_line + 1
      elseif first == " " then
        targets[#targets + 1] = { line = row, path = abs_path, target = new_line - 1 }
        new_line = new_line + 1
      elseif first == "-" then
        -- old side only: the working tree has no line to open
      elseif first ~= "\\" then
        new_line = nil
      end
    end
    row = row + 1
  end
  return targets
end

-- diff_hunk_rows returns the 0-based rows of the unified diff's @@ headers. It is the
-- navigation index for the Next/Previous Hunk commands; parse_diff_targets separately maps
-- new-side rows to the working file.
local function diff_hunk_rows(text)
  local rows = {}
  local row = 0
  for line in (text .. "\n"):gmatch("(.-)\n") do
    if row >= MAX_DIFF_LINES or #rows >= MAX_TARGETS then
      break
    end
    if line:sub(1, 2) == "@@" then
      rows[#rows + 1] = row
    end
    row = row + 1
  end
  return rows
end

local function close_previous_diff()
  if state.diff_buf and state.diff_buf:valid() then
    pcall(state.diff_buf.close, state.diff_buf)
  end
  state.diff_buf = nil
  state.diff_hunks = {}
end

local function open_virtual_diff(name, text, language)
  local buf, err = gvim.buf.create {
    name = name,
    text = text,
    language = language,
    read_only = true,
    focus = true,
  }
  if not buf then
    return nil, err
  end
  close_previous_diff()
  state.diff_buf = buf
  return buf
end

local function show_diff(file, repo, generation)
  local abs_path = gvim.path.join(repo.root, file.path)
  local args
  local pseudo
  if file.category == "staged" then
    args = { "diff", "--no-color", "--cached", "--unified=3", "--", file.path }
  elseif file.category == "unstaged" then
    args = { "diff", "--no-color", "--unified=3", "--", file.path }
  else
    local text, err = gvim.fs.read(abs_path, { max = MAX_DIFF_BYTES })
    if generation ~= state.diff_generation then
      return
    end
    if not text then
      notice(t("status.diff_failed", { path = file.path, error = tostring(err) }), "warn")
      return
    end
    local lines = {}
    for line in (text .. "\n"):gmatch("(.-)\n") do
      lines[#lines + 1] = "+" .. line
      if #lines >= MAX_DIFF_LINES then
        lines[#lines + 1] = t("diff.truncated")
        break
      end
    end
    pseudo = "--- /dev/null\n+++ b/" .. file.path .. "\n@@ -0,0 +1," .. tostring(#lines) .. " @@\n" .. table.concat(lines, "\n")
  end
  local text
  if pseudo then
    text = pseudo
  else
    local result, err = git_run(repo, args, MAX_DIFF_BYTES)
    if generation ~= state.diff_generation then
      return
    end
    if not result then
      notice(t("status.diff_failed", { path = file.path, error = tostring(err) }), "warn")
      return
    end
    text = result.stdout
    if result.truncated then
      notice(t("status.truncated"))
    end
    if text == "" then
      notice(t("status.diff_empty", { path = file.path }))
      return
    end
  end
  local language = "diff"
  local buf, err = open_virtual_diff("*Review Diff: " .. file.path .. "*", text, language)
  if not buf then
    notice(t("status.diff_failed", { path = file.path, error = tostring(err) }), "warn")
    return
  end
  -- Only a working-tree line that exists can be opened. A staged deletion (or a path
  -- removed by the worktree side) has no right-hand target; skip those rows rather than
  -- opening an empty buffer.
  if file.status ~= "D" and gvim.fs.exists(abs_path) then
    local targets = parse_diff_targets(text, abs_path)
    if #targets > 0 then
      local ok, terr = gvim.buf.set_line_targets(buf, targets)
      if not ok then
        gvim.log.debug("audit_mode: line targets: %s", tostring(terr))
      end
    end
  end
  state.diff_hunks = diff_hunk_rows(text)
  if file.category == "untracked" then
    notice(t("status.untracked", { path = file.path }))
  else
    notice(t("status.opened", { path = file.path }))
  end
end

local function close_state()
  state.generation = state.generation + 1
  state.diff_generation = state.diff_generation + 1
  state.picker = nil
  close_previous_diff()
end

local function fill(report)
  if not state.picker then
    return
  end
  -- A previous scan may have set the truncation footer; every result (including clean and
  -- error states) starts from no footer, and only a truncated result sets it again.
  state.picker:set_footer("")
  if not report then
    state.picker:set_items { { text = t("empty.not_repo"), disabled = true } }
    state.picker:set_status(t("status.not_repo"))
    return
  end
  local items = {}
  for _, category in ipairs(CATEGORY_ORDER) do
    local rows = {}
    for _, f in ipairs(report.files) do
      if f.category == category then
        rows[#rows + 1] = f
      end
    end
    if #rows > 0 then
      items[#items + 1] = { text = t(CATEGORY_LABEL[category], { count = #rows }), disabled = true }
      for _, f in ipairs(rows) do
        local detail = f.add and t("row.stats", { add = f.add, del = f.del }) or ""
        items[#items + 1] = {
          text = t("row.status", { status = f.status, path = f.path }),
          detail = detail,
          file = f,
        }
      end
    end
  end
  if #items == 0 then
    items[1] = { text = t("empty.clean"), disabled = true }
    state.picker:set_status(t("status.clean"))
  else
    state.picker:set_status(t("status.count", {
      files = #report.files, add = report.add, del = report.del,
    }))
    if report.truncated then
      state.picker:set_footer(t("status.truncated"))
    end
  end
  state.picker:set_items(items)
end

local refresh

local function on_select(item)
  local file = item and item.file
  if type(file) ~= "table" or not state.picker then
    return
  end
  local repo = state.repo
  close_state()
  state.diff_generation = state.diff_generation + 1
  local generation = state.diff_generation
  notice(t("status.diff_loading", { path = file.path }))
  show_diff(file, repo, generation)
end

local function close_picker()
  if state.picker then
    local picker = state.picker
    state.picker = nil
    pcall(picker.close, picker)
  end
  state.generation = state.generation + 1
end

refresh = function()
  if not state.picker then
    return
  end
  local generation = state.generation
  state.picker:set_status(t("status.loading"))
  local report, err = load_review()
  if generation ~= state.generation or not state.picker then
    return
  end
  if not report then
    notice(err or t("status.failed", { error = "unknown error" }), "warn")
    state.picker:set_footer("")
    state.picker:set_items { { text = tostring(err), disabled = true } }
    state.picker:set_status(tostring(err))
    return
  end
  state.repo = report.repo
  state.files = report.files
  fill(report)
end

local function show()
  if state.picker then
    refresh()
    return
  end
  state.generation = state.generation + 1
  local picker = gvim.ui.picker {
    title = t("cmd.review"),
    placeholder = t("cmd.review"),
    footer = "",
    filter = "fuzzy",
    items = { { text = t("status.loading"), disabled = true } },
    on_select = on_select,
    on_close = function()
      state.generation = state.generation + 1
      state.picker = nil
    end,
  }
  state.picker = picker
  picker:open()
  refresh()
end

gvim.command.register {
  name = "review",
  title = "%cmd.review",
  description = "%cmd.review_desc",
  run = show,
}

gvim.command.register {
  name = "refresh",
  title = "%cmd.refresh",
  description = "%cmd.refresh_desc",
  run = function()
    if state.picker then
      refresh()
    else
      show()
    end
  end,
}

local function navigate_hunk(direction)
  if not state.diff_buf or not state.diff_buf:valid() then
    notice(t("status.no_diff"))
    return
  end
  -- A command can be run from the palette while another tab is active. The old diff buffer
  -- may still exist and even be valid; never move a hidden buffer's cursor. Only the buffer
  -- on screen is a review diff the user is looking at.
  local current = gvim.buf.current()
  if not current or not current:valid() or current:info().id ~= state.diff_buf:info().id then
    notice(t("status.not_active"))
    return
  end
  local hunks = state.diff_hunks or {}
  if #hunks == 0 then
    notice(t("status.no_hunks"))
    return
  end
  local line = state.diff_buf:cursor().line
  local current = 0
  for i, row in ipairs(hunks) do
    if row <= line then
      current = i
    else
      break
    end
  end
  local target
  if direction > 0 then
    if current >= #hunks then
      notice(t("status.last_hunk"))
      return
    end
    target = current + 1
  else
    if current <= 1 then
      notice(t("status.first_hunk"))
      return
    end
    target = current - 1
  end
  state.diff_buf:set_cursor(gvim.pos(hunks[target], 0), { center = true })
  notice(t("status.hunk", { current = target, total = #hunks }))
end

gvim.command.register {
  name = "next_hunk",
  title = "%cmd.next_hunk",
  description = "%cmd.next_hunk_desc",
  run = function() navigate_hunk(1) end,
}

gvim.command.register {
  name = "prev_hunk",
  title = "%cmd.prev_hunk",
  description = "%cmd.prev_hunk_desc",
  run = function() navigate_hunk(-1) end,
}

gvim.command.register {
  name = "close",
  title = "%cmd.close",
  description = "%cmd.close_desc",
  run = function()
    close_picker()
    state.diff_generation = state.diff_generation + 1
    close_previous_diff()
  end,
}

gvim.plugin.on_unload(function()
  close_state()
end)

gvim.log.debug("review diff plugin loaded")
