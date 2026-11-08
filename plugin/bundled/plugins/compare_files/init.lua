-- Compare Files. A port of Fresh's compare_files plugin.
--
-- Shows two arbitrary files side by side in one tab, both editable, with the
-- differences aligned and highlighted - what `vimdiff A B` or a GUI diff tool
-- does. The comparison is a group-lite of two existing file buffers: the files
-- are opened as buffers if they are not open already, and the group shows them
-- as two splits of the current tab. What the plugin adds:
--
--   - a background on every changed line, and on the changed span of a line
--     edited in place;
--   - a gutter glyph per changed line and a scrollbar mark per change;
--   - filler rows on the shorter side of a change when the host paints virtual
--     lines, so the unchanged lines sit on the same screen row in both panels;
--   - scroll sync between the panels, anchored at the end of every change;
--   - a fresh diff after every edit to either side (debounced);
--   - Next/Previous difference, Switch Side, Copy Change to Other Side
--     (vimdiff's dp) and Take Change from Other Side (vimdiff's do), the last
--     two through host-side compare offsets and replace_range, one edit and one
--     undo step per merge.
--
-- The diff and every offset come from the host (gvim.diff.compare_buffers): no
-- file text is copied into the plugin.
--
-- Reductions from Fresh (recorded in docs/plugins/porting.md):
--   * No synthetic group tab: the group title segments are stored but not
--     painted, and the two files are shown in splits of the current tab.
--   * The two files are opened as ordinary tabs (gvim has no tabless view of a
--     real file buffer). Files the comparison opened are closed again when the
--     comparison closes if they are unmodified; files that were already open
--     keep their tabs.
--   * No line-ending-only hunks: gvim normalises line endings when a file is
--     read, so compare_buffers never reports an eol-only hunk and the plugin
--     shows no line-ending notes.
--   * Filler rows need the host's virtual-line painting (gvim.has
--     "decor_virtual_paint"); without it the panes are level only where the
--     files have the same number of lines at a change.
--   * Lines are paired positionally within a hunk (Fresh's documented fallback
--     when it has no per-line ops).
--   * The command body paths are read with gvim.ui.input (the path_complete
--     plugin gives completion); Fresh's file picker is not available.
--   * Compare: Close leaves the tabs of the files open; close them as usual.
--   * No `fresh --diff LEFT RIGHT` launch hook and no script-facing
--     exportPluginApi: gvim exports plain data, and the editor's own --diff /
--     Compare view still opens the native comparison for those launches.
local gvim = require "gvim"

local t = gvim.i18n.t

-- Namespaces. Fresh groups by role; the same split keeps an eye on the host's
-- 20 000 item cap per namespace when a big diff is near the limits.
local ns_lines = gvim.decor.namespace("compare_files_lines")
local ns_spans = gvim.decor.namespace("compare_files_spans")
local ns_gutter = gvim.decor.namespace("compare_files_gutter")
local ns_scroll = gvim.decor.namespace("compare_files_scroll")
local ns_fill = gvim.decor.namespace("compare_files_fill")

local PRIORITY = 20 -- above git_gutter (10) and live_diff (9)

local THEME = {
  removedBg = "editor.diff_remove_bg",
  removedSpanBg = "editor.diff_remove_highlight_bg",
  addedBg = "editor.diff_add_bg",
  addedSpanBg = "editor.diff_add_highlight_bg",
  modifiedBg = "editor.diff_modify_bg",
}

-- Gutter glyphs and scrollbar marks take RGB only; the same palette as
-- live_diff so a change reads the same in either plugin.
local COLORS = {
  added = { 80, 200, 120 },
  modified = { 220, 160, 90 },
  removed = { 220, 90, 90 },
}

local DEBOUNCE_MS = 75
local MAX_ITEMS = 20000

-- Filler rows are virtual lines; the host reports the feature when it paints
-- them (plugin API tier 1c). Until then the reduced view has no fillers.
local RENDER_FILL = gvim.has("decor_virtual_paint") == true

-- One comparison, keyed by the ids of its two buffers. Comparisons are few;
-- a plain array keeps the iteration order stable.
local comparisons = {}
local sync_seq = 0 -- the next scroll-sync id; the host wants a positive one

local function other(side)
  return side == "left" and "right" or "left"
end

local function comparison_of(buf_id)
  for _, c in ipairs(comparisons) do
    if c.left_id == buf_id or c.right_id == buf_id then
      return c
    end
  end
  return nil
end

local function buffer_of(c, side)
  return side == "left" and c.left or c.right
end

local function side_of(c, buf_id)
  return c.left_id == buf_id and "left" or "right"
end

local function base_name(path)
  return gvim.path.basename(path)
end

-- absolute resolves a prompt path the way gvim.buf.open does: a relative path
-- is against the workspace root.
local function absolute(path)
  if path:sub(1, 1) == "/" then
    return path
  end
  return gvim.path.join(gvim.workspace.root(), path)
end

-- The comparison the user is in: the one showing the focused buffer.
local function active_comparison()
  local b = gvim.buf.current()
  if not b then
    return nil
  end
  local c = comparison_of(b.id)
  if not c then
    return nil
  end
  return c, side_of(c, b.id)
end

-- clear removes every decoration this plugin paints on one of the buffers.
local function clear_decorations(buf)
  ns_lines:set(buf, {})
  ns_spans:set(buf, {})
  ns_gutter:set(buf, {})
  ns_scroll:set(buf, {})
  ns_fill:set(buf, {})
end

-- pos_of converts an absolute byte offset to a Pos on the buffer.
local function pos_of(buf, offset)
  local p, err = buf:pos_of(offset)
  if not p then
    return nil, err
  end
  return p
end

-- paint_hunks paints one side: line backgrounds, changed spans, gutter glyphs
-- and scrollbar marks, plus the filler rows for the lines only the other side
-- has. Everything is computed from the host's CompareResult offsets.
local function paint_hunks(buf, res, side, clear)
  if clear then
    clear_decorations(buf)
  end
  local left = side == "left"
  local lines, spans, gutter, scroll, fill = {}, {}, {}, {}, {}
  local lone_bg = left and THEME.removedBg or THEME.addedBg
  local span_bg = left and THEME.removedSpanBg or THEME.addedSpanBg
  local lone_color = left and COLORS.removed or COLORS.added
  local fill_bg = left and THEME.addedBg or THEME.removedBg
  local len = left and res.left_len or res.right_len

  for _, h in ipairs(res.hunks) do
    local mine = h[side]
    local theirs = h[other(side)]
    local paired = math.min(mine.count, theirs.count)
    for i = 1, mine.count do
      local start = mine.lines[i][1]
      local finish = mine.lines[i][2]
      local is_paired = i <= paired
      if #lines < MAX_ITEMS then
        lines[#lines + 1] = {
          kind = "line", line = mine.start + i - 1, priority = PRIORITY,
          style = { bg = is_paired and THEME.modifiedBg or lone_bg, extend = true },
        }
      end
      local span = is_paired and mine.spans[i] or false
      if span and span[2] > span[1] and #spans < MAX_ITEMS then
        local p1 = pos_of(buf, span[1])
        local p2 = pos_of(buf, span[2])
        if p1 and p2 then
          spans[#spans + 1] = {
            kind = "highlight", priority = PRIORITY,
            range = gvim.range(p1.line, p1.col, p2.line, p2.col),
            style = { bg = span_bg },
          }
        end
      end
      if #gutter < MAX_ITEMS then
        gutter[#gutter + 1] = {
          kind = "gutter", line = mine.start + i - 1, priority = PRIORITY,
          text = is_paired and "~" or (left and "-" or "+"),
          style = { fg = is_paired and COLORS.modified or lone_color },
        }
      end
    end
    -- One scrollbar mark per change: a range over this side's lines, or a seam
    -- mark where this side has none.
    if #scroll < MAX_ITEMS then
      local position = math.min(mine.from, math.max(len - 1, 0))
      local color = mine.count == 0 and (left and COLORS.added or COLORS.removed)
        or paired > 0 and COLORS.modified or lone_color
      if mine.count > 0 then
        scroll[#scroll + 1] = {
          kind = "scroll", priority = PRIORITY, color = color,
          range = gvim.range(mine.start, 0, mine.start + mine.count, 0),
        }
      else
        scroll[#scroll + 1] = { kind = "scroll", line = position, color = color, priority = PRIORITY }
      end
    end
    -- Filler rows for the lines the other side has here and this one doesn't.
    local missing = theirs.count - mine.count
    if RENDER_FILL and missing > 0 then
      local above = false
      local anchor
      if mine.count > 0 then
        anchor = mine.start + mine.count - 1
      elseif mine.from > 0 then
        local p = pos_of(buf, mine.from - 1)
        anchor = p and p.line or 0
      else
        anchor = 0
        above = true
      end
      for i = 1, missing do
        fill[#fill + 1] = {
          kind = above and "virtual_line_above" or "virtual_line_below",
          line = anchor, text = "",
          style = { bg = fill_bg, extend = true },
          priority = i - 1,
        }
      end
    end
  end
  ns_lines:set(buf, lines)
  ns_spans:set(buf, spans)
  ns_gutter:set(buf, gutter)
  ns_scroll:set(buf, scroll)
  ns_fill:set(buf, fill)
end

-- sync_anchors pairs the two sides at the end of every change: the filler rows
-- keep the rows between two changes' ends equal, so the ends hold the panes
-- together while an edit waits for its diff.
local function sync_anchors(res)
  local out = {}
  for _, h in ipairs(res.hunks) do
    out[#out + 1] = { h.left.start + h.left.count, h.right.start + h.right.count }
  end
  return out
end

local function summary(res)
  local n = #res.hunks
  if n == 0 then
    return t("status.identical")
  end
  return t("status.differences", { n = tostring(n) })
end

-- apply stores a result and paints both sides.
local function apply(c, res)
  c.result = res
  paint_hunks(c.left, res, "left", true)
  paint_hunks(c.right, res, "right", true)
  gvim.win.set_scroll_sync_anchors(c.sync_id, sync_anchors(res))
end

-- rediff recomputes the diff and paints it; a rerun asked for while a diff is
-- on its way is folded into one more pass.
local function rediff(c)
  if c.running then
    c.rerun = true
    return
  end
  c.running = true
  local ok = false
  repeat
    c.rerun = false
    local res, err = gvim.diff.compare_buffers(c.left, c.right)
    if not res then
      gvim.notify(t("status.failed", { reason = tostring(err) }), "warn")
      break
    end
    if not c.closed then
      apply(c, res)
    end
    ok = true
  until not c.rerun
  c.running = false
  return ok
end

local function schedule_rediff(c)
  if c.closed or not c.result then
    return
  end
  rediff(c)
end

-- jump_to puts both cursors at the hunk and centers it in the active side's
-- window; the other panel follows by scroll sync.
local function jump_to(c, h, side)
  local left = pos_of(c.left, h.left.jump)
  local right = pos_of(c.right, h.right.jump)
  if left then
    c.left:set_cursor(left, { reveal = true })
  end
  if right then
    c.right:set_cursor(right, { reveal = true })
  end
  local s = h[side]
  local line = s.count == 0 and s.start > 0 and s.start - 1 or s.start
  local win = c.windows[side]
  if win then
    win:scroll_to(line, { center = true })
  end
end

-- hunk_at_cursor is the change the cursor is in on one side, or the one at the
-- seam when the side has no lines there.
local function hunk_at_cursor(res, side, line)
  for _, h in ipairs(res.hunks) do
    local s = h[side]
    if s.count > 0 then
      if line >= s.start and line < s.start + s.count then
        return h
      end
    elseif line == s.start or line == s.start - 1 then
      return h
    end
  end
  return nil
end

-- cursor_line is the focused buffer's line.
local function cursor_line(buf)
  local p = buf:cursor()
  return p and p.line or 0
end

local function close_group(c)
  c.closed = true
  for i = #comparisons, 1, -1 do
    if comparisons[i] == c then
      table.remove(comparisons, i)
    end
  end
  gvim.win.remove_scroll_sync(c.sync_id)
  if c.group then
    gvim.buf.close_group(c.group)
  end
  for _, b in ipairs({ c.left, c.right }) do
    if b:valid() then
      clear_decorations(b)
      -- Restore the wrap setting the tabs had before the comparison.
      b:set_option("wrap", c.wrap[b.id] ~= false)
    end
  end
  -- Files this comparison opened are closed again when they have no unsaved
  -- changes; a file that was open before keeps its tab.
  for _, b in ipairs(c.opened or {}) do
    if b:valid() and not b:is_modified() then
      pcall(b.close, b)
    end
  end
  if #comparisons == 0 then
    gvim.context.set("compare-active", false)
  end
  gvim.notify(t("status.closed"))
end

local function close_comparison_of(buf_id)
  local c = comparison_of(buf_id)
  if c then
    close_group(c)
  end
end

-- open_buffers returns the two file buffers, opening the ones that are not
-- open yet and recording those so close_group can close them again.
local function open_buffers(left_path, right_path)
  local opened = {}
  local function one(path)
    local b = gvim.buf.find(path)
    if b then
      return b
    end
    local buf, err = gvim.buf.open(path, { background = true })
    if not buf then
      return nil, err
    end
    opened[#opened + 1] = buf
    return buf
  end
  local left, lerr = one(left_path)
  if not left then
    for _, b in ipairs(opened) do
      pcall(b.close, b, { force = true })
    end
    return nil, lerr
  end
  local right, rerr = one(right_path)
  if not right then
    for _, b in ipairs(opened) do
      pcall(b.close, b, { force = true })
    end
    return nil, rerr
  end
  return left, right, opened
end

-- title_segments builds Fresh's title from the localized template, with a
-- placeholder per file so each file's segment names its panel.
local function title_segments(labels)
  local mark = "\1"
  local filled = t("tab_name", { left = mark .. "left" .. mark, right = mark .. "right" .. mark })
  local segments = {}
  local i = 0
  for part in (filled .. mark):gmatch("([^" .. mark .. "]*)" .. mark) do
    i = i + 1
    if i % 2 == 0 and (part == "left" or part == "right") then
      segments[#segments + 1] = { text = labels[part], panel = part }
    elseif part ~= "" then
      segments[#segments + 1] = { text = part }
    end
  end
  return segments
end

local function tab_labels(left_path, right_path)
  local left, right = base_name(left_path), base_name(right_path)
  if left ~= right then
    return { left = left, right = right }
  end
  local function with_dir(path, name)
    local dir = gvim.path.basename(gvim.path.dirname(path))
    return dir ~= "" and (dir .. "/" .. name) or name
  end
  return { left = with_dir(left_path, left), right = with_dir(right_path, right) }
end

local function group_layout(left_id, right_id)
  return '{"type":"split","direction":"h","ratio":0.5,"first":' ..
    '{"type":"scrollable","id":"left","bufferId":' .. tostring(left_id) .. '},' ..
    '"second":{"type":"scrollable","id":"right","bufferId":' .. tostring(right_id) .. '}}'
end

-- compare shows two files side by side. It returns true when the comparison
-- is up.
local function compare(left_arg, right_arg)
  local left_path, right_path = absolute(left_arg), absolute(right_arg)
  if not gvim.fs.exists(left_path) then
    gvim.notify(t("status.no_such_file", { path = left_path }), "warn")
    return false
  end
  if not gvim.fs.exists(right_path) then
    gvim.notify(t("status.no_such_file", { path = right_path }), "warn")
    return false
  end
  if left_path == right_path then
    gvim.notify(t("status.same_file"))
    return false
  end
  -- A buffer is in one comparison at a time: its decorations are its own.
  for _, path in ipairs({ left_path, right_path }) do
    local b = gvim.buf.find(path)
    if b then
      close_comparison_of(b.id)
    end
  end

  local left, right, opened = open_buffers(left_path, right_path)
  if not left then
    gvim.notify(t("status.failed", { reason = tostring(opened) }), "warn")
    return false
  end
  if left.id == right.id then
    for _, b in ipairs(opened) do
      pcall(b.close, b, { force = true })
    end
    gvim.notify(t("status.same_file"))
    return false
  end

  local labels = tab_labels(left_path, right_path)
  local group, gerr = gvim.buf.create_group(t("tab_name", labels), group_layout(left.id, right.id))
  if not group then
    for _, b in ipairs(opened) do
      pcall(b.close, b, { force = true })
    end
    gvim.notify(t("status.failed", { reason = tostring(gerr) }), "warn")
    return false
  end

  sync_seq = sync_seq + 1
  local c = {
    group = group,
    left = left, right = right,
    left_id = left.id, right_id = right.id,
    sync_id = sync_seq,
    opened = opened,
    wrap = {},
    windows = {},
    result = nil,
    running = false,
    rerun = false,
    closed = false,
  }

  local panels = gvim.buf.group_panels(group)
  if not panels then
    gvim.buf.close_group(group)
    return false
  end
  for _, p in ipairs(panels) do
    c.windows[p.name] = p.window
  end
  if not c.windows.left or not c.windows.right then
    gvim.buf.close_group(group)
    return false
  end

  -- Fillers keep rows level only while one buffer line is one screen row.
  for _, side in ipairs({ "left", "right" }) do
    local b = buffer_of(c, side)
    local current = b:get_option("wrap")
    c.wrap[b.id] = current == nil and true or current
    b:set_option("wrap", false)
  end

  local res, cerr = gvim.diff.compare_buffers(left, right)
  if not res then
    -- A binary file, or one too large to diff: nothing to show.
    gvim.buf.close_group(group)
    for _, b in ipairs(opened) do
      pcall(b.close, b, { force = true })
    end
    for _, side in ipairs({ "left", "right" }) do
      local b = buffer_of(c, side)
      if b:valid() then
        b:set_option("wrap", c.wrap[b.id])
      end
    end
    gvim.notify(t("status.failed", { reason = tostring(cerr) }), "warn")
    return false
  end

  comparisons[#comparisons + 1] = c
  gvim.context.set("compare-active", true)
  gvim.buf.set_group_title(group, title_segments(labels))
  gvim.win.create_scroll_sync(c.sync_id, c.windows.left, c.windows.right)
  gvim.buf.focus_panel(group, "left")
  apply(c, res)
  if #res.hunks > 0 then
    jump_to(c, res.hunks[1], "left")
  end
  gvim.notify(summary(res))
  return true
end

-- Commands ------------------------------------------------------------------

-- The commands are Fresh's. Fresh's registerCommand calls pass no key
-- (the fourth argument is the context, not a chord), so the bundled plugin
-- adds no default bindings.
local function register(spec)
  gvim.command.register(spec)
end

local function prompt_path(prompt_text, default)
  return gvim.ui.input { prompt = prompt_text, default = default, select_all = true }
end

register {
  name = "compare_files",
  title = "%cmd.compare_files",
  description = "%cmd.compare_files_desc",
  run = function()
    local left = prompt_path(t("prompt.left"))
    if not left then
      return
    end
    local right = prompt_path(t("prompt.right"))
    if not right then
      return
    end
    compare(left, right)
  end,
}

register {
  name = "compare_with_file",
  title = "%cmd.compare_with",
  description = "%cmd.compare_with_desc",
  run = function()
    local b = gvim.buf.current()
    local path = b and b:path()
    if not path then
      gvim.notify(t("status.no_file"))
      return
    end
    local right = prompt_path(t("prompt.with", { name = base_name(path) }))
    if not right then
      return
    end
    compare(path, right)
  end,
}

register {
  name = "next_difference",
  title = "%cmd.next",
  description = "%cmd.next_desc",
  when = { "compare-active" },
  run = function()
    local c, side = active_comparison()
    if not c then
      gvim.notify(t("status.not_comparing"))
      return
    end
    local res = c.result
    if not res or #res.hunks == 0 then
      gvim.notify(t("status.identical"))
      return
    end
    local pos = c[side == "left" and "left" or "right"]:offset_of(c[side]:cursor())
    pos = pos or 0
    local n = #res.hunks
    local index = -1
    for i, h in ipairs(res.hunks) do
      if h[side].jump > pos then
        index = i
        break
      end
    end
    local wrapped = index < 0
    if wrapped then
      index = 1
    end
    jump_to(c, res.hunks[index], side)
    gvim.notify(t(wrapped and "status.difference_wrapped" or "status.difference", {
      n = tostring(index), total = tostring(n),
    }))
  end,
}

register {
  name = "prev_difference",
  title = "%cmd.prev",
  description = "%cmd.prev_desc",
  when = { "compare-active" },
  run = function()
    local c, side = active_comparison()
    if not c then
      gvim.notify(t("status.not_comparing"))
      return
    end
    local res = c.result
    if not res or #res.hunks == 0 then
      gvim.notify(t("status.identical"))
      return
    end
    local pos = c[side == "left" and "left" or "right"]:offset_of(c[side]:cursor())
    pos = pos or 0
    local n = #res.hunks
    local index = -1
    for i = n, 1, -1 do
      if res.hunks[i][side].jump < pos then
        index = i
        break
      end
    end
    local wrapped = index < 0
    if wrapped then
      index = n
    end
    jump_to(c, res.hunks[index], side)
    gvim.notify(t(wrapped and "status.difference_wrapped" or "status.difference", {
      n = tostring(index), total = tostring(n),
    }))
  end,
}

register {
  name = "switch_side",
  title = "%cmd.switch",
  description = "%cmd.switch_desc",
  when = { "compare-active" },
  run = function()
    local c, side = active_comparison()
    if not c then
      gvim.notify(t("status.not_comparing"))
      return
    end
    gvim.buf.focus_panel(c.group, other(side))
  end,
}

-- merge makes the destination side's lines of the change under the cursor the
-- bytes of the source side's, through replace_range: one edit, one undo step,
-- and only if the destination has not changed since the diff it was measured
-- from.
local function merge(direction)
  local c, side = active_comparison()
  if not c then
    gvim.notify(t("status.not_comparing"))
    return
  end
  local res, err = gvim.diff.compare_buffers(c.left, c.right)
  if not res then
    gvim.notify(t("status.failed", { reason = tostring(err) }), "warn")
    return
  end
  apply(c, res)
  local b = c[side]
  local h = hunk_at_cursor(res, side, cursor_line(b))
  if not h then
    gvim.notify(t("status.no_change_here"))
    return
  end
  local from = direction == "copy" and side or other(side)
  local to = other(from)
  local src = h[from]
  local dst = h[to]
  local from_buf = buffer_of(c, from)
  local to_buf = buffer_of(c, to)
  local text = ""
  if src.to > src.from then
    local p1 = pos_of(from_buf, src.from)
    local p2 = pos_of(from_buf, src.to)
    if not p1 or not p2 then
      gvim.notify(t("status.failed", { reason = "cannot read the change" }), "warn")
      return
    end
    text = from_buf:text(gvim.range(p1.line, p1.col, p2.line, p2.col)) or ""
  end
  local version = to == "left" and res.left_version or res.right_version
  local applied = gvim.diff.replace_range(to_buf, dst.from, dst.to, text, version)
  -- The plugin's own edit fires no buffer_modified, so diff again here.
  rediff(c)
  if not applied then
    gvim.notify(t("status.not_applied", { name = base_name(to_buf:path() or "") }))
  elseif c.result then
    gvim.notify(summary(c.result))
  end
end

register {
  name = "copy_change",
  title = "%cmd.copy",
  description = "%cmd.copy_desc",
  when = { "compare-active" },
  run = function()
    merge("copy")
  end,
}

register {
  name = "take_change",
  title = "%cmd.take",
  description = "%cmd.take_desc",
  when = { "compare-active" },
  run = function()
    merge("take")
  end,
}

register {
  name = "close",
  title = "%cmd.close",
  description = "%cmd.close_desc",
  when = { "compare-active" },
  run = function()
    local c = active_comparison()
    if not c then
      gvim.notify(t("status.not_comparing"))
      return
    end
    close_group(c)
  end,
}

-- Events --------------------------------------------------------------------

-- The context is declared at load so the palette can evaluate the commands'
-- when clauses before any comparison exists.
gvim.context.set("compare-active", false)

-- An edit to either side schedules a fresh diff; the debounce merges an edit
-- burst (typing, paste, undo) into one diff.
gvim.on("buffer_modified", function(ev)
  local c = comparison_of(ev.buf.id)
  if c then
    schedule_rediff(c)
  end
end, { debounce = DEBOUNCE_MS })

gvim.on("after_file_revert", function(ev)
  local c = comparison_of(ev.buf.id)
  if c then
    schedule_rediff(c)
  end
end)

-- A comparison whose buffer closed (a tab closed, a buffer force-closed) is
-- gone; drop it without touching the closed buffer again.
gvim.on("buffer_closed", function(ev)
  local id = ev.id
  for i = #comparisons, 1, -1 do
    local c = comparisons[i]
    if c.left_id == id or c.right_id == id then
      c.closed = true
      table.remove(comparisons, i)
      gvim.win.remove_scroll_sync(c.sync_id)
      if c.group then
        gvim.buf.close_group(c.group)
      end
      if #comparisons == 0 then
        gvim.context.set("compare-active", false)
      end
    end
  end
end)
