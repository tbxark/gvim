-- Diff navigation: a port of Fresh's plugins/diff_nav.ts.
--
-- One pair of commands merges the three change sources Fresh uses: the hunks exported by
-- git_gutter (git/HEAD changes), the hunks exported by live_diff (HEAD/disk/branch
-- comparisons) and the line hunks of the buffer's saved text against the current text
-- (unsaved changes, b:diff_saved). Targets are sorted by position, deduplicated by line,
-- and wrapped; the status row uses Fresh's Change n/total and [wrapped] messages.
--
-- git_gutter is default-on, but this plugin still works when it is disabled, has no git
-- binary, or has not computed hunks yet: every source is optional and a missing/failing
-- export or diff simply contributes nothing. live_diff and the saved diff are likewise
-- independent. All host calls are awaits inside a plugin task, never in Update.
local gvim = require "gvim"

local t = gvim.i18n.t

-- exported_hunks returns the hunks a provider plugin exported, or an empty table. The
-- provider may be unloaded, may not export hunks (an older/other build), or its export
-- may raise; in every case this source degrades to nothing instead of failing navigation.
local function exported_hunks(name, buf)
  local provider = gvim.plugins.get(name)
  if not provider then
    return {}
  end
  local ok, fn = pcall(function()
    return provider.hunks
  end)
  if not ok or type(fn) ~= "function" then
    return {}
  end
  local called, hunks = pcall(fn, buf.id)
  if not called or type(hunks) ~= "table" then
    return {}
  end
  return hunks
end

-- target_for clips a saved line to the buffer and returns {line, bytePos}. The byte
-- position is the jump/sort key Fresh uses; a host that cannot answer it (large-file
-- buffers refuse offset_of) leaves it nil and the line is used instead.
local function target_for(buf, line, line_count)
  if type(line) ~= "number" or line ~= line or line < 0 then
    return nil
  end
  if line_count and line_count > 0 and line >= line_count then
    line = line_count - 1
  end
  local byte_pos = nil
  local ok, pos = pcall(buf.offset_of, buf, gvim.pos(line, 0))
  if ok and type(pos) == "number" then
    byte_pos = pos
  end
  return { line = line, bytePos = byte_pos }
end

local function collect_targets(buf)
  local targets = {}
  local line_count = buf:line_count()
  local function add(line)
    local target = target_for(buf, line, line_count)
    if target then
      targets[#targets + 1] = target
    end
  end

  -- Source 1: git_gutter's exported hunk list ({type, line, count}, 0-based line).
  for _, h in ipairs(exported_hunks("git_gutter", buf)) do
    if type(h) == "table" then
      add(h.line)
    end
  end

  -- Source 2: live_diff's exported hunk list ({kind, newStart, newCount}, 0-based).
  for _, h in ipairs(exported_hunks("live_diff", buf)) do
    if type(h) == "table" then
      add(h.newStart)
    end
  end

  -- Source 3: the saved text against the current text ({kind, new_start, ...}).
  -- For an empty new side (a deletion) the jump belongs on the line before the seam,
  -- exactly like git_gutter's merged navigation; using new_start would jump past the
  -- deleted region. new_count > 0 uses the first new line.
  local saved = buf:diff_saved()
  if type(saved) == "table" then
    for _, h in ipairs(saved) do
      if type(h) == "table" then
        local start = tonumber(h.new_start)
        if start and start >= 0 then
          local count = tonumber(h.new_count) or 0
          add(count > 0 and start or math.max(start - 1, 0))
        end
      end
    end
  end

  if #targets == 0 then
    return targets
  end

  -- Sort by byte position when both targets have one, otherwise by line. Deduplicate by
  -- line: Fresh keeps the first target of two different sources on the same line.
  table.sort(targets, function(a, b)
    if a.bytePos and b.bytePos then
      if a.bytePos ~= b.bytePos then
        return a.bytePos < b.bytePos
      end
      return a.line < b.line
    end
    return a.line < b.line
  end)
  local deduped = {}
  local seen_lines = {}
  for _, target in ipairs(targets) do
    if not seen_lines[target.line] then
      seen_lines[target.line] = true
      deduped[#deduped + 1] = target
    end
  end
  return deduped
end

local function cursor_byte(buf)
  local ok, pos = pcall(buf.offset_of, buf, buf:cursor())
  if ok and type(pos) == "number" then
    return pos
  end
  return nil
end

-- after reports whether target lies after the cursor in Fresh's byte order. When the
-- cursor byte or the target byte is unavailable (large buffers), the line comparison is
-- the fallback.
local function after(buf, target, cursor)
  local cbyte = cursor_byte(buf)
  if cbyte and target.bytePos then
    return target.bytePos > cbyte
  end
  return target.line > cursor.line
end

local function before(buf, target, cursor)
  local cbyte = cursor_byte(buf)
  if cbyte and target.bytePos then
    return target.bytePos < cbyte
  end
  return target.line < cursor.line
end

local function jump(buf, target)
  local ok, err = buf:set_cursor(gvim.pos(target.line, 0), { center = true })
  if not ok then
    gvim.status(t("status.jump_failed", { error = tostring(err) }))
    return false
  end
  return true
end

local function report(index, total, wrapped)
  local key = wrapped and "status.change_wrapped" or "status.change"
  gvim.status(t(key, { n = tostring(index), total = tostring(total) }))
end

local function next_change()
  local buf = gvim.buf.current()
  if not buf then
    gvim.status(t("status.no_changes"))
    return
  end
  local targets = collect_targets(buf)
  if #targets == 0 then
    gvim.status(t("status.no_changes"))
    return
  end
  local cursor = buf:cursor()
  local index = nil
  for i, target in ipairs(targets) do
    if after(buf, target, cursor) then
      index = i
      break
    end
  end
  local wrapped = false
  if not index then
    index = 1
    wrapped = true
  end
  if jump(buf, targets[index]) then
    report(index, #targets, wrapped)
  end
end

local function prev_change()
  local buf = gvim.buf.current()
  if not buf then
    gvim.status(t("status.no_changes"))
    return
  end
  local targets = collect_targets(buf)
  if #targets == 0 then
    gvim.status(t("status.no_changes"))
    return
  end
  local cursor = buf:cursor()
  local index = nil
  for i = #targets, 1, -1 do
    if before(buf, targets[i], cursor) then
      index = i
      break
    end
  end
  local wrapped = false
  if not index then
    index = #targets
    wrapped = true
  end
  if jump(buf, targets[index]) then
    report(index, #targets, wrapped)
  end
end

gvim.command.register({
  name = "next_change",
  title = t("cmd.next_change"),
  description = t("cmd.next_change_desc"),
  run = function()
    next_change()
  end,
})

gvim.command.register({
  name = "prev_change",
  title = t("cmd.prev_change"),
  description = t("cmd.prev_change_desc"),
  run = function()
    prev_change()
  end,
})

gvim.log.debug("diff_nav: Plugin loaded")
