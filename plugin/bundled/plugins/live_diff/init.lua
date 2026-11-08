-- Live Diff. A port of Fresh's live_diff plugin.
--
-- Renders a unified diff inside the live editable buffer: the old content of
-- deleted lines is shown above the line that follows them, added and modified
-- lines get a background and a gutter indicator, and every hunk gets a mark on
-- the scrollbar. The comparison reference is per buffer:
--
--   head    the file at git HEAD (the default)
--   disk    the file content currently on disk
--   branch  the file at a chosen git branch or ref
--
-- Differences from Fresh (recorded in docs/plugins/porting.md):
--   * Fresh diffs against a host-registered baseline with a patience diff; gvim
--     has no baseline API, so the reference is fetched here (git show, or a
--     disk read) and the diff is computed with gvim.diff (Myers). Hunk
--     boundaries can differ from git diff's.
--   * The word-level refinement runs here; it is bounded by the caps below and
--     yields between items so the UI thread is never blocked.
--   * Large-file mode is not diffed: the buffer text cannot be read cheaply, so
--     the plugin clears its decorations and says so once.
--   * gvim decorations have no "paint a foreground only where it collides with
--     the background" mode, so diff backgrounds use the theme background keys
--     only (no editor.diff_*_collision_fg).
--   * The old content of deleted lines is drawn as tier-1c virtual rows, which
--     need the host's virtual-line painting. Until the host reports
--     gvim.has("decor_virtual_paint") the plugin renders the reduced Fresh
--     view (backgrounds, new-side word emphasis, gutter, scrollbar); the row
--     building is already compiled in and switches on with the capability.
--
-- Opt-in: the plugin is off by default (plugin.json default_enabled = false),
-- and once loaded its global toggle also starts off, as Fresh's does. A vs-*
-- command force-enables the buffer.
local gvim = require "gvim"
local git = gvim.git

local settings = gvim.settings.define {
  default_mode = {
    type = "enum", values = { "head", "disk", "branch" }, default = "head",
    description = "Comparison reference for new buffers (branch uses default_branch)",
  },
  default_branch = {
    type = "string", default = "main",
    description = "Git ref used by default_mode = branch, and the default answer of Live Diff: vs Branch",
  },
  debounce_ms = {
    type = "integer", default = 75, minimum = 10, maximum = 5000, project = true,
    description = "Pause after typing before the diff is computed again, in milliseconds",
  },
  max_virtual_lines = {
    type = "integer", default = 2000, minimum = 0, maximum = 20000,
    description = "Skip the old-content lines above the diff when more lines than this were deleted",
  },
  max_overlay_lines = {
    type = "integer", default = 20000, minimum = 0, maximum = 50000,
    description = "Degrade to gutter and scrollbar only when more lines than this changed",
  },
}

local t = gvim.i18n.t

-- Namespaces. Fresh groups by role; the same split keeps each namespace under
-- the host's 20 000 decoration cap when a big diff is near the limits.
local ns_vline = gvim.decor.namespace("live_diff_vlines")
local ns_overlay = gvim.decor.namespace("live_diff_overlay")
local ns_gutter = gvim.decor.namespace("live_diff_gutter")
local ns_scroll = gvim.decor.namespace("live_diff_scroll")

local PRIORITY = 9
local SYMBOLS = { added = "+", modified = "~", removed = "-" }

-- Fixed gutter colors, as in Fresh: the glyph shape is the signal and the
-- colors stay readable on light and dark themes.
local GUTTER_COLOR = {
  added = { 80, 200, 120 },
  modified = { 220, 160, 90 },
  removed = { 220, 90, 90 },
}
local THEME = {
  addedBg = "editor.diff_add_bg",
  modifiedBg = "editor.diff_modify_bg",
  removedBg = "editor.diff_remove_bg",
  removedFg = "editor.diff_remove_collision_fg",
}

-- Caps (Fresh's values where the work is the same). MAX_REFINE_CELLS is in DP
-- cells per recompute; the word table is stored whole so it can be walked back,
-- so a single pair is also capped by MAX_WORD_DP_CELLS.
local MAX_VIRTUAL_LINES = 2000
local MAX_OVERLAY_LINES = 20000
local MAX_ITEMS = 20000
local MAX_LINES = 200000
local MAX_BASELINE_BYTES = 16 * 1024 * 1024
local MAX_LINE_LCS_CHARS = 2000
local MAX_WORD_TOKENS = 1000
local MAX_WORD_DP_CELLS = 1000000
local MAX_REFINE_CELLS = 10000000
local MAX_HUNKS = 20000
local MAX_VIRTUAL_ROWS_PER_LINE = 64
local YIELD_EVERY = 2000
local MAX_PASSES = 3

-- RENDER_VLINES switches the full Fresh rendering on: the old content of deleted
-- lines as virtual rows above the seam. The tier-1c decoration store and Lua
-- names are here (1c-A), but painting them is the remaining host increment, so
-- the plugin asks the host before using them. Everything else (line
-- backgrounds, word emphasis, gutter, scrollbar) works with the older kinds, so
-- until the host reports paint support the reduced Fresh view is shown.
local RENDER_VLINES = gvim.has("decor_virtual_paint") == true

-- State: one entry per buffer. The mode and the per-buffer enable override live
-- in the host's window state when 1c-C is available (get_window_state), so they
-- survive a plugin reload; the in-memory fields are the fallback and mirror.
local states = {}
local enabled = gvim.state.get("live_diff.enabled", false) == true
local default_override = gvim.state.get("live_diff.default_mode")
local last_branch = gvim.state.get("live_diff.last_branch")
local watchers = {}       -- watch key -> watcher
local watched_repos = {}  -- directory of a file -> git directory | false
local notice_buffers = {} -- buffer id -> the last degraded notice it got
local similarity_threshold = 0.95 -- refined pairs at or above this stay "modified"
local REFRESH_AFTER_MS = 5000 -- an activated buffer asks git again when its reference is older

local split_lines
local line_starts_for
local mode_key
local hunks_key_of

-- small helpers -------------------------------------------------------------

local function debug(fmt, ...)
  gvim.log.debug("live_diff: " .. fmt, ...)
end

local function buffer_path(b)
  local info = b:info()
  if not info or info.virtual or not info.path or info.path == "" then
    return nil, info
  end
  return info.path, info
end

-- split_lines is Fresh's splitLines: an empty text has no lines, and a trailing
-- newline does not make a last empty line ("foo\n" -> {"foo"}).
split_lines = function(text)
  if text == nil or #text == 0 then
    return {}
  end
  local lines = gvim.text.split(text, "\n", true)
  if #lines > 0 and lines[#lines] == "" then
    lines[#lines] = nil
  end
  return lines
end

-- line_starts_for returns the byte offset of every line start, plus one past
-- the last line start (a trailing newline counts one byte).
line_starts_for = function(lines)
  local starts = { 0 }
  local pos = 0
  for i = 1, #lines do
    pos = pos + #lines[i] + 1
    starts[i + 1] = pos
  end
  return starts
end

local function mode_to_text(mode)
  if mode.kind == "branch" then
    return "branch:" .. mode.ref
  end
  return mode.kind
end

mode_key = function(mode)
  return mode_to_text(mode)
end

local function text_to_mode(text)
  if text == "head" or text == nil or text == "" then
    return { kind = "head" }
  end
  if text == "disk" then
    return { kind = "disk" }
  end
  local branch = text:match("^branch:(.+)$")
  if branch and branch ~= "" then
    return { kind = "branch", ref = branch }
  end
  return nil
end

local function default_mode()
  local stored = default_override and text_to_mode(default_override) or nil
  if stored then
    return stored
  end
  if settings.default_mode == "disk" then
    return { kind = "disk" }
  end
  if settings.default_mode == "branch" then
    return { kind = "branch", ref = last_branch or settings.default_branch }
  end
  return { kind = "head" }
end

local function read_window_state(b, key)
  if not gvim.buf.get_window_state then
    return nil
  end
  local ok, value = pcall(gvim.buf.get_window_state, b, key)
  if ok then
    return value
  end
  return nil
end

local function write_window_state(b, key, value)
  if not gvim.buf.set_window_state then
    return
  end
  pcall(gvim.buf.set_window_state, b, key, value)
end

local function effective_enabled(st)
  if st.override ~= nil then
    return st.override
  end
  return enabled
end

-- UTF-8 and word helpers ----------------------------------------------------

-- next_char returns one code point starting at byte i, and the next index. A
-- malformed lead byte is taken as one byte, so the loop always advances.
local function next_char(s, i)
  local b = s:byte(i)
  local n = 1
  if b >= 0xF0 then
    n = 4
  elseif b >= 0xE0 then
    n = 3
  elseif b >= 0xC0 then
    n = 2
  end
  if i + n - 1 > #s then
    n = 1
  end
  return s:sub(i, i + n - 1), i + n
end

local function is_whitespace_char(ch)
  return #ch == 1 and ch:match("%s") ~= nil
end

local function is_word_char(ch)
  return #ch == 1 and ch:match("[%w_]") ~= nil
end

-- chars splits s into code points. It is only used for similarity.
local function chars(s)
  local out = {}
  local i = 1
  while i <= #s do
    local ch
    ch, i = next_char(s, i)
    out[#out + 1] = ch
  end
  return out
end

-- Similarity ----------------------------------------------------------------

-- prefix_suffix_lcs returns the number of equal characters trimmed from the
-- ends, and the LCS of the middle, using two rolling rows (O(min(m, n)) space).
local function prefix_suffix_lcs(a, b)
  local m, n = #a, #b
  local prefix = 0
  local min_len = math.min(m, n)
  while prefix < min_len and a[prefix + 1] == b[prefix + 1] do
    prefix = prefix + 1
  end
  local a_end, b_end = m, n
  while a_end > prefix and b_end > prefix and a[a_end] == b[b_end] do
    a_end = a_end - 1
    b_end = b_end - 1
  end
  local equal = prefix + (m - a_end)
  local am = a_end - prefix
  local bn = b_end - prefix
  if am == 0 or bn == 0 then
    return equal, 0
  end
  local prev, cur = {}, {}
  for j = 0, bn do
    prev[j] = 0
    cur[j] = 0
  end
  for i = 1, am do
    local ai = a[prefix + i]
    cur[0] = 0
    for j = 1, bn do
      if ai == b[prefix + j] then
        cur[j] = prev[j - 1] + 1
      elseif prev[j] >= cur[j - 1] then
        cur[j] = prev[j]
      else
        cur[j] = cur[j - 1]
      end
    end
    prev, cur = cur, prev
  end
  return equal, prev[bn]
end

-- line_similarity is Fresh's Sorensen-Dice ratio over the character LCS:
-- 2 * LCS / (len(a) + len(b)). 1.0 for two empty lines, 0.0 when one is empty
-- or a line is too long for the quadratic scan (minified JS, base64 blobs).
local function line_similarity(a, b)
  if #a == 0 and #b == 0 then
    return 1.0
  end
  if #a == 0 or #b == 0 then
    return 0.0
  end
  if #a > MAX_LINE_LCS_CHARS or #b > MAX_LINE_LCS_CHARS then
    return 0.0
  end
  local equal, middle = prefix_suffix_lcs(chars(a), chars(b))
  return (2 * (equal + middle)) / (#a + #b)
end

-- Word diff -----------------------------------------------------------------

-- A token is a run of word characters, a run of whitespace, or one other
-- character, with the byte range it occupies in its line.
local function tokenize(s)
  local tokens = {}
  local i = 1
  while i <= #s do
    local ch, after = next_char(s, i)
    local j = after
    if is_whitespace_char(ch) then
      while j <= #s do
        local c2, a2 = next_char(s, j)
        if not is_whitespace_char(c2) then
          break
        end
        j = a2
      end
    elseif is_word_char(ch) then
      while j <= #s do
        local c2, a2 = next_char(s, j)
        if not is_word_char(c2) then
          break
        end
        j = a2
      end
    end
    tokens[#tokens + 1] = { text = s:sub(i, j - 1), start = i, finish = j - 1 }
    i = j
  end
  return tokens
end

local function collapse_ranges(tokens)
  local out = {}
  for _, token in ipairs(tokens) do
    local last = out[#out]
    if last and last.finish + 1 == token.start then
      last.finish = token.finish
    else
      out[#out + 1] = { start = token.start, finish = token.finish }
    end
  end
  return out
end

local function non_whitespace(tokens)
  local out = {}
  for _, token in ipairs(tokens) do
    local first = token.text:sub(1, 1)
    if not is_whitespace_char(first) then
      out[#out + 1] = token
    end
  end
  return out
end

-- compute_word_diff is Fresh's computeWordDiff: a token LCS; the tokens not in
-- the common subsequence are the changed words. Whitespace-only changes are
-- never highlighted. If either side has too many tokens, every non-whitespace
-- token counts as changed instead of paying for the DP table.
local function compute_word_diff(old_s, new_s)
  local old_tokens = tokenize(old_s)
  local new_tokens = tokenize(new_s)
  local m, n = #old_tokens, #new_tokens
  local function everything(tokens)
    return collapse_ranges(non_whitespace(tokens))
  end
  if m == 0 and n == 0 then
    return {}, {}
  end
  if m > MAX_WORD_TOKENS or n > MAX_WORD_TOKENS or (m + 1) * (n + 1) > MAX_WORD_DP_CELLS then
    return everything(new_tokens), everything(old_tokens)
  end
  if m == 0 then
    return everything(new_tokens), {}
  end
  if n == 0 then
    return {}, everything(old_tokens)
  end
  local stride = n + 1
  local dp = {}
  for i = 0, m do
    local row = (i + 1) * stride
    for j = 0, n do
      dp[row + j] = 0
    end
  end
  for i = 1, m do
    local ot = old_tokens[i].text
    local row = (i + 1) * stride
    local prev = i * stride
    for j = 1, n do
      if ot == new_tokens[j].text then
        dp[row + j] = dp[prev + j - 1] + 1
      elseif dp[prev + j] >= dp[row + j - 1] then
        dp[row + j] = dp[prev + j]
      else
        dp[row + j] = dp[row + j - 1]
      end
    end
  end
  local matched_old, matched_new = {}, {}
  local i, j = m, n
  while i > 0 and j > 0 do
    if old_tokens[i].text == new_tokens[j].text then
      matched_old[i] = true
      matched_new[j] = true
      i, j = i - 1, j - 1
    elseif dp[i * stride + j] >= dp[(i + 1) * stride + j - 1] then
      j = j - 1
    else
      i = i - 1
    end
  end
  local function unmatched(tokens, matched)
    local out = {}
    for k, token in ipairs(tokens) do
      local first = token.text:sub(1, 1)
      if not matched[k] and not is_whitespace_char(first) then
        out[#out + 1] = token
      end
    end
    return collapse_ranges(out)
  end
  return unmatched(new_tokens, matched_new), unmatched(old_tokens, matched_old)
end

-- Hunk conversion and refinement --------------------------------------------

-- to_hunks maps gvim.diff's raw hunks ({kind,old_start,old_count,new_start,
-- new_count}) onto Fresh's shape. The old-side lines ride along when detail is
-- full, because the virtual lines need their text.
local function to_hunks(raw, old_lines, detail)
  local hunks = {}
  for _, h in ipairs(raw) do
    local kind = "modified"
    if h.old_count == 0 then
      kind = "added"
    elseif h.new_count == 0 then
      kind = "removed"
    end
    local old = {}
    if detail == "full" and h.old_count > 0 and old_lines then
      for i = 0, h.old_count - 1 do
        old[i + 1] = old_lines[h.old_start + i + 1] or ""
      end
    end
    hunks[#hunks + 1] = {
      kind = kind,
      newStart = h.new_start,
      newCount = h.new_count,
      oldLines = old,
    }
  end
  return hunks
end

-- refine_hunks is Fresh's refineHunks: only 1:1 modified pairs are refined.
-- High similarity (>= similarity_threshold) stays and gets word ranges on the
-- new line; below it the pair splits into a removed and an added hunk. A removed
-- hunk with unmatched old words also gets oldWordRanges for the deletion line.
local function refine_hunks(hunks, new_lines)
  local out = {}
  local budget = MAX_REFINE_CELLS
  for _, h in ipairs(hunks) do
    if h.kind ~= "modified" or #h.oldLines ~= h.newCount or h.newCount == 0 then
      out[#out + 1] = h
    else
      local cost = 0
      for i = 1, h.newCount do
        local new_len = #(new_lines[h.newStart + i] or "")
        cost = cost + #h.oldLines[i] * new_len + 1
      end
      if cost > budget then
        out[#out + 1] = h
      else
        budget = budget - cost
        for i = 1, h.newCount do
          local old_line = h.oldLines[i]
          local new_line = new_lines[h.newStart + i] or ""
          if line_similarity(old_line, new_line) >= similarity_threshold then
            local new_ranges, old_ranges = compute_word_diff(old_line, new_line)
            if #old_ranges > 0 then
              out[#out + 1] = {
                kind = "removed", newStart = h.newStart + i - 1, newCount = 0,
                oldLines = { old_line }, oldWordRanges = { old_ranges },
              }
            end
            out[#out + 1] = {
              kind = "modified", newStart = h.newStart + i - 1, newCount = 1,
              oldLines = {}, wordRanges = { new_ranges },
            }
          else
            local new_ranges, old_ranges = compute_word_diff(old_line, new_line)
            local removed = {
              kind = "removed", newStart = h.newStart + i - 1, newCount = 0,
              oldLines = { old_line },
            }
            if #old_ranges > 0 then
              removed.oldWordRanges = { old_ranges }
            end
            out[#out + 1] = removed
            local added = {
              kind = "added", newStart = h.newStart + i - 1, newCount = 1,
              oldLines = {},
            }
            if #new_ranges > 0 then
              added.wordRanges = { new_ranges }
            end
            out[#out + 1] = added
          end
        end
      end
    end
  end
  return out
end

-- detail_level picks the rendering detail. The caps degrade locally instead of
-- clearing the view: outline keeps the gutter and scrollbar, no-vlines adds the
-- line backgrounds but skips the old-content lines and the word refinement.
local function detail_level(raw)
  local max_overlay = math.min(settings.max_overlay_lines, MAX_OVERLAY_LINES)
  local max_virtual = math.min(settings.max_virtual_lines, MAX_VIRTUAL_LINES)
  local changed, virtual = 0, 0
  for _, h in ipairs(raw) do
    changed = changed + h.new_count
    virtual = virtual + h.old_count
  end
  if changed > max_overlay then
    return "outline"
  end
  if virtual > max_virtual then
    return "no-vlines"
  end
  return "full"
end

-- Baseline fetching ---------------------------------------------------------

-- resolve_default_branch is Fresh's resolveDefaultBranch: origin/HEAD, then
-- main, then master. A missing git binary falls back to main (git.run reports
-- unsupported and the fallback decision is the same as Fresh's last resort).
local function resolve_default_branch(path)
  local cwd = gvim.path.dirname(path)
  local r = git.run({ "symbolic-ref", "--short", "refs/remotes/origin/HEAD" }, { cwd = cwd })
  if r and r.ok then
    local trimmed = r.stdout:gsub("%s+$", "")
    if trimmed:match("^origin/") then
      return trimmed:sub(8)
    end
    if trimmed ~= "" then
      return trimmed
    end
  end
  local main = git.run({ "rev-parse", "--verify", "main" }, { cwd = cwd })
  if main and main.ok then
    return "main"
  end
  return "master"
end

-- read_disk reads the file the buffer is editing and decodes it with the
-- buffer's encoding, so the old side is compared as text, not bytes.
local function read_disk(path, encoding)
  local raw, err = gvim.fs.read(path, { max = MAX_BASELINE_BYTES })
  if not raw then
    return nil, err
  end
  if #raw > MAX_BASELINE_BYTES then
    return nil, "too_large: the file is larger than the live diff cap"
  end
  if encoding and encoding ~= "" and encoding ~= "UTF-8" and encoding ~= "utf-8" then
    local text, derr = gvim.text.decode(raw, encoding, { normalize = true })
    if not text then
      return nil, derr
    end
    return text
  end
  return gvim.text.decode(raw, "utf-8", { normalize = true })
end

-- fetch_reference fetches the old side for the buffer's mode. It returns nil
-- when no reference exists (untracked file, no repository, missing disk file):
-- the caller clears the view; a transient error is logged.
local function fetch_reference(b, st)
  local path, info = buffer_path(b)
  if not path or not info then
    return nil
  end
  local text
  local err
  if st.mode.kind == "disk" then
    text, err = read_disk(path, info.encoding)
  else
    local rev = "HEAD"
    if st.mode.kind == "branch" then
      rev = st.mode.ref
    end
    text, err = git.show(path, { rev = rev, for_buffer = b })
  end
  if text == nil then
    local code = gvim.err_code(err)
    if code ~= "not_found" and code ~= "unsupported" then
      debug("%s: %s", path, tostring(err))
    end
    return nil
  end
  return text
end

-- Rendering -----------------------------------------------------------------

-- clear removes every decoration this plugin has on b and forgets the diff.
local function clear(st, b)
  if st.painted or #st.hunks > 0 or st.last_key ~= "" then
    ns_overlay:set(b, {})
    ns_gutter:set(b, {})
    ns_scroll:set(b, {})
    ns_vline:set(b, {})
  end
  st.painted, st.hunks, st.last_key = false, {}, ""
  st.last_text = nil
end

-- notify_once shows a status message once per buffer and key, so a degraded
-- view announced on every keystroke does not drown the status row.
local function notify_once(st, key, message)
  if notice_buffers[st.id] == key then
    return
  end
  notice_buffers[st.id] = key
  gvim.notify(message)
end

-- paint_hunk adds the gutter, scrollbar and background items of one hunk.
local function paint_hunks(st, b, hunks, new_lines, detail, version)
  local gutter, scroll, overlay, vlines = {}, {}, {}, {}
  local dropped_words = false
  for _, h in ipairs(hunks) do
    if h.kind == "added" or h.kind == "modified" then
      local color = h.kind == "added" and GUTTER_COLOR.added or GUTTER_COLOR.modified
      local symbol = h.kind == "added" and SYMBOLS.added or SYMBOLS.modified
      local bg = h.kind == "added" and THEME.addedBg or THEME.modifiedBg
      for i = 0, h.newCount - 1 do
        local line = h.newStart + i
        if line >= #new_lines then
          break
        end
        if #gutter < MAX_ITEMS then
          gutter[#gutter + 1] = {
            kind = "gutter", line = line, text = symbol, priority = PRIORITY,
            style = { fg = color },
          }
        end
        if detail ~= "outline" then
          overlay[#overlay + 1] = { kind = "line", line = line, style = { bg = bg }, priority = PRIORITY }
        end
        if detail == "full" and h.wordRanges then
          local ranges = h.wordRanges[i + 1]
          if ranges then
            for _, range in ipairs(ranges) do
              if #overlay >= MAX_ITEMS then
                dropped_words = true
                break
              end
              overlay[#overlay + 1] = {
                kind = "highlight", priority = PRIORITY,
                range = gvim.range(line, range.start - 1, line, range.finish),
                style = { bold = true, underline = true },
              }
            end
          end
        end
        if (#overlay + #gutter) % YIELD_EVERY == 0 then
          gvim.timer.sleep(0)
        end
      end
      local first = h.newStart
      local last = math.min(h.newStart + h.newCount - 1, #new_lines - 1)
      if #scroll < MAX_HUNKS then
        if last >= first then
          scroll[#scroll + 1] = {
            kind = "scroll", priority = PRIORITY, color = color,
            range = gvim.range(first, 0, last + 1, 0),
          }
        else
          scroll[#scroll + 1] = { kind = "scroll", line = math.max(first - 1, 0), color = color, priority = PRIORITY }
        end
      end
    else
      -- Pure deletion: its content is gone, so the marker sits on the new line
      -- that follows the seam (the last line for a deletion at the end).
      local line = h.newStart
      if line >= #new_lines then
        line = math.max(#new_lines - 1, 0)
      end
      if #scroll < MAX_HUNKS then
        scroll[#scroll + 1] = { kind = "scroll", line = line, color = GUTTER_COLOR.removed, priority = PRIORITY }
      end
    end
  end
  -- The old-content virtual lines of deleted blocks. Kept behind RENDER_VLINES
  -- until the host paints them (1c-A B2); the rest of the rendering works now.
  if detail == "full" and RENDER_VLINES then
    for _, h in ipairs(hunks) do
      if h.kind == "removed" and #h.oldLines > 0 then
        local anchor_line = h.newStart
        local above = true
        if anchor_line >= #new_lines then
          anchor_line = math.max(#new_lines - 1, 0)
          above = false
        end
        -- The host draws at most 64 virtual rows at one anchor; a longer
        -- deletion block is cut with a summary line that says how many rows
        -- are not shown.
        local shown = #h.oldLines
        local truncated = 0
        if shown > MAX_VIRTUAL_ROWS_PER_LINE then
          truncated = shown - MAX_VIRTUAL_ROWS_PER_LINE
          shown = MAX_VIRTUAL_ROWS_PER_LINE
        end
        for i = 1, shown do
          local old_line = h.oldLines[i]
          local text_overlays = nil
          local ranges = h.oldWordRanges and h.oldWordRanges[i]
          if ranges then
            text_overlays = {}
            for _, range in ipairs(ranges) do
              text_overlays[#text_overlays + 1] = {
                start = range.start - 1, ["end"] = range.finish,
                style = { bold = true, underline = true },
              }
            end
          end
          vlines[#vlines + 1] = {
            kind = above and "virtual_line_above" or "virtual_line_below",
            line = anchor_line, text = old_line,
            style = { fg = THEME.removedFg, bg = THEME.removedBg },
            gutter_glyph = SYMBOLS.removed, gutter_color = GUTTER_COLOR.removed,
            text_overlays = text_overlays, priority = i - 1,
          }
        end
        if truncated > 0 then
          vlines[#vlines + 1] = {
            kind = above and "virtual_line_above" or "virtual_line_below",
            line = anchor_line, text = t("vline.more_deleted", { count = truncated }),
            style = { fg = THEME.removedFg, bg = THEME.removedBg },
            priority = MAX_VIRTUAL_ROWS_PER_LINE,
          }
        end
      end
    end
  end
  local ok = true
  local function set(ns, items)
    if not ok then
      return
    end
    local done, err = ns:set(b, items, { version = version })
    if not done then
      ok = false
      if gvim.err_code(err) ~= "closed" and gvim.err_code(err) ~= "stale" then
        debug("%s", tostring(err))
      end
    end
  end
  set(ns_gutter, gutter)
  set(ns_scroll, scroll)
  set(ns_overlay, overlay)
  set(ns_vline, vlines)
  if not ok then
    return false
  end
  if dropped_words then
    debug("word-level emphasis capped at %d items", MAX_ITEMS)
  end
  st.painted = #gutter > 0 or #scroll > 0 or #overlay > 0 or #vlines > 0
  return true
end

-- request/state -------------------------------------------------------------

local ensure_state
local request
local watch_git
local watch_file

-- drop_reference forces the next recompute to fetch the old side again.
local function drop_reference(st)
  st.reload_ref = true
  st.last_text = nil
  st.last_key = ""
end

-- recompute is one diff pass: fetch the reference if needed, read the buffer,
-- diff, refine and paint. Every await is followed by a validity check because
-- the buffer can change or close meanwhile.
local function recompute(b, st)
  if states[st.id] ~= st then
    return
  end
  if not effective_enabled(st) then
    clear(st, b)
    return
  end
  local info = b:info()
  if not info then
    return
  end
  if info.virtual or not info.path or info.path == "" then
    clear(st, b)
    return
  end
  if info.large or (info.line_count and info.line_count > MAX_LINES) then
    clear(st, b)
    notify_once(st, "large", t("status.large_file"))
    return
  end
  local key = mode_key(st.mode)
  if st.tracked == false and not st.reload_ref and st.ref_key == key then
    -- The reference is known not to exist (untracked, no repository): typing
    -- must not ask git again. A save, a watcher event or Refresh sets
    -- reload_ref and retries.
    clear(st, b)
    return
  end
  if st.reload_ref or st.ref_key ~= key or st.ref_text == nil then
    st.reload_ref = false
    -- A refreshed reference can change the diff even when the buffer text did
    -- not; do not let the unchanged-text skip below hide it (Fresh does the
    -- same with lastBufferText).
    st.last_text = nil
    local text = fetch_reference(b, st)
    if states[st.id] ~= st or not b:valid() then
      return
    end
    if text == nil then
      st.ref_key, st.ref_text, st.tracked = key, nil, false
      clear(st, b)
      return
    end
    st.ref_key, st.ref_text, st.tracked = key, text, true
    st.checked_at = gvim.time.monotonic_ms()
  end
  local new_text, terr = b:text()
  if states[st.id] ~= st or not b:valid() then
    return
  end
  if not new_text then
    clear(st, b)
    if gvim.err_code(terr) == "too_large" then
      notify_once(st, "large", t("status.large_file"))
    else
      debug("%s", tostring(terr))
    end
    return
  end
  if new_text == st.last_text and not st.force and st.last_key ~= "" then
    return
  end
  local new_lines = split_lines(new_text)
  if #new_lines > MAX_LINES then
    clear(st, b)
    notify_once(st, "large", t("status.large_file"))
    return
  end
  local raw, derr = gvim.diff.buffer(b, st.ref_text)
  if states[st.id] ~= st or not b:valid() then
    return
  end
  if not raw then
    clear(st, b)
    if gvim.err_code(derr) == "too_large" then
      notify_once(st, "large", t("status.large_file"))
    else
      debug("%s", tostring(derr))
    end
    return
  end
  local detail = detail_level(raw)
  local hunks
  if detail == "full" then
    local old_lines = split_lines(st.ref_text)
    hunks = refine_hunks(to_hunks(raw, old_lines, detail), new_lines)
  else
    hunks = to_hunks(raw, nil, detail)
  end
  local hunks_key = detail .. "|" .. hunks_key_of(hunks)
  if hunks_key == st.last_key and not st.force then
    st.hunks = hunks
    return
  end
  st.hunks = hunks
  st.last_text = new_text
  st.last_key = hunks_key
  st.force = false
  if not paint_hunks(st, b, hunks, new_lines, detail, raw.version) then
    st.last_key = ""
    st.again = true
    return
  end
  if detail ~= "full" and st.last_detail == "full" then
    gvim.notify(t("status.simplified"))
  end
  st.last_detail = detail
  if st.mode.kind == "disk" then
    watch_file(b, st)
  else
    watch_git(b, st)
  end
end

-- hunks_key_of is a compact string of a hunk list, used to skip a repaint when
-- the diff did not change.
hunks_key_of = function(hunks)
  local parts = {}
  for _, h in ipairs(hunks) do
    local words = {}
    if h.wordRanges then
      for _, list in ipairs(h.wordRanges) do
        local ranges = {}
        for _, range in ipairs(list) do
          ranges[#ranges + 1] = range.start .. "-" .. range.finish
        end
        words[#words + 1] = table.concat(ranges, ",")
      end
    end
    parts[#parts + 1] = table.concat({ h.kind, h.newStart, h.newCount, #h.oldLines, table.concat(words, ";") }, ":")
  end
  return table.concat(parts, "|")
end

request = function(b, opts)
  local st = ensure_state(b)
  if not st then
    return
  end
  if opts then
    if opts.fetch then
      st.reload_ref = true
    end
    if opts.force then
      st.force = true
      st.last_key = ""
    end
  end
  if st.running then
    st.again = true
    return
  end
  st.running = true
  local passes = 0
  repeat
    st.again = false
    local ok, err = pcall(recompute, b, st)
    if not ok then
      gvim.log.warn("live_diff: %s", tostring(err))
    end
    passes = passes + 1
  until not st.again or passes >= MAX_PASSES or states[st.id] ~= st
  st.running = false
end

-- ensure_state returns the state of b, creating it from the window state (or
-- the default mode) on the first sight of the buffer.
ensure_state = function(b)
  local path, info = buffer_path(b)
  if not path or not info then
    return nil
  end
  local st = states[b.id]
  if not st then
    local stored = read_window_state(b, "mode")
    local override = read_window_state(b, "enabled")
    st = {
      id = b.id,
      path = path,
      mode = text_to_mode(stored) or default_mode(),
      override = type(override) == "boolean" and override or nil,
      hunks = {},
      running = false,
      again = false,
      force = true,
      reload_ref = true,
      painted = false,
      last_text = nil,
      last_key = "",
      last_detail = "full",
      checked_at = nil,
      tracked = nil,
    }
    states[b.id] = st
  elseif st.path ~= path then
    st.path = path
    drop_reference(st)
    st.force = true
  end
  return st
end

-- Watchers ------------------------------------------------------------------

-- HEAD_FILES are the files of the git directory that move when HEAD's content
-- may change. The directory itself is watched, because git replaces them by
-- renaming lock files over them.
local HEAD_FILES = { HEAD = true, ORIG_HEAD = true, COMMIT_EDITMSG = true, index = true }

watch_git = function(b, st)
  local path = st.path
  if not path then
    return
  end
  local dir = gvim.path.dirname(path)
  if watched_repos[dir] ~= nil then
    return
  end
  watched_repos[dir] = false
  local repo = git.repo(dir)
  if not repo or not repo.head then
    return
  end
  local gitdir = gvim.path.dirname(repo.head)
  watched_repos[dir] = gitdir
  if watchers[gitdir] then
    return
  end
  watchers[gitdir] = gvim.fs.watch(gitdir, function(ev)
    if ev.kind == "error" or not HEAD_FILES[gvim.path.basename(ev.path or "")] then
      return
    end
    for _, s in pairs(states) do
      if s.mode.kind ~= "disk" and s.path and watched_repos[gvim.path.dirname(s.path)] == gitdir then
        s.reload_ref = true
        local bb = gvim.buf.get(s.id)
        if bb then
          request(bb, { fetch = true })
        end
      end
    end
  end, { debounce_ms = 200 })
end

-- watch_file watches the file on disk for disk mode, so an external writer (an
-- agent, a formatter) refreshes the diff even when the buffer is not saved.
watch_file = function(b, st)
  if st.file_watch then
    return
  end
  local path = st.path
  if not path then
    return
  end
  st.file_watch = gvim.fs.watch(path, function(ev)
    if ev.kind == "error" or ev.path ~= path then
      return
    end
    st.reload_ref = true
    local bb = gvim.buf.get(st.id)
    if bb then
      request(bb, { fetch = true, force = true })
    end
  end, { debounce_ms = 200 })
end

local function stop_file_watch(st)
  if st.file_watch then
    st.file_watch:stop()
    st.file_watch = nil
  end
end

-- Mode and enable state -----------------------------------------------------

-- set_mode switches the reference of the current buffer and force-enables it:
-- choosing a comparison is a clear "I want to see the diff" signal.
local function set_mode(b, mode)
  local st = ensure_state(b)
  if not st then
    gvim.notify(t("status.no_file"))
    return
  end
  st.mode = mode
  st.override = true
  write_window_state(b, "mode", mode_to_text(mode))
  write_window_state(b, "enabled", true)
  stop_file_watch(st)
  drop_reference(st)
  st.force = true
  request(b, { force = true })
end

-- sync_buffer reflects the effective enabled state: paint, or clear and forget.
local function sync_buffer(b, st)
  if effective_enabled(st) then
    request(b, { force = true })
  else
    clear(st, b)
    stop_file_watch(st)
  end
end

local function current_buffer()
  local b = gvim.buf.current()
  if not b or not b:path() then
    gvim.notify(t("status.no_file"))
    return nil
  end
  return b
end

-- Commands ------------------------------------------------------------------

-- The commands are Fresh's; none binds a default key (Fresh's
-- registerCommand calls pass nil), so the bundled plugin adds no chords.
gvim.command.register {
  name = "toggle_global",
  title = "%cmd.toggle_global",
  description = "%cmd.toggle_global_desc",
  run = function()
    enabled = not enabled
    gvim.state.set("live_diff.enabled", enabled)
    for id, st in pairs(states) do
      if st.override == nil then
        local b = gvim.buf.get(id)
        if b and b:valid() then
          sync_buffer(b, st)
        end
      end
    end
    gvim.notify(t(enabled and "status.global_enabled" or "status.global_disabled"))
  end,
}

gvim.command.register {
  name = "toggle_buffer",
  title = "%cmd.toggle_buffer",
  description = "%cmd.toggle_buffer_desc",
  run = function()
    local b = current_buffer()
    if not b then
      return
    end
    local st = ensure_state(b)
    if not st then
      gvim.notify(t("status.no_file"))
      return
    end
    local new_enabled = not effective_enabled(st)
    st.override = new_enabled
    write_window_state(b, "enabled", new_enabled)
    if new_enabled then
      drop_reference(st)
      st.force = true
    end
    sync_buffer(b, st)
    gvim.notify(t(new_enabled and "status.buffer_enabled" or "status.buffer_disabled"))
  end,
}

gvim.command.register {
  name = "vs_head",
  title = "%cmd.vs_head",
  description = "%cmd.vs_head_desc",
  run = function()
    local b = current_buffer()
    if not b then
      return
    end
    set_mode(b, { kind = "head" })
    gvim.notify(t("status.mode_head"))
  end,
}

gvim.command.register {
  name = "vs_disk",
  title = "%cmd.vs_disk",
  description = "%cmd.vs_disk_desc",
  run = function()
    local b = current_buffer()
    if not b then
      return
    end
    set_mode(b, { kind = "disk" })
    gvim.notify(t("status.mode_disk"))
  end,
}

gvim.command.register {
  name = "vs_branch",
  title = "%cmd.vs_branch",
  description = "%cmd.vs_branch_desc",
  run = function()
    local b = current_buffer()
    if not b then
      return
    end
    local default = last_branch or settings.default_branch
    local ref = gvim.ui.input { prompt = t("prompt.branch"), default = default, select_all = true }
    if not ref then
      return
    end
    ref = ref:gsub("^%s+", ""):gsub("%s+$", "")
    if ref == "" then
      return
    end
    last_branch = ref
    gvim.state.set("live_diff.last_branch", ref)
    set_mode(b, { kind = "branch", ref = ref })
    gvim.notify(t("status.mode_branch", { ref = ref }))
  end,
}

gvim.command.register {
  name = "vs_default_branch",
  title = "%cmd.vs_default_branch",
  description = "%cmd.vs_default_branch_desc",
  run = function()
    local b = current_buffer()
    if not b then
      return
    end
    local ref = resolve_default_branch(b:path())
    set_mode(b, { kind = "branch", ref = ref })
    gvim.notify(t("status.mode_branch", { ref = ref }))
  end,
}

gvim.command.register {
  name = "refresh",
  title = "%cmd.refresh",
  description = "%cmd.refresh_desc",
  run = function()
    local b = current_buffer()
    if not b then
      return
    end
    local st = ensure_state(b)
    if not st then
      return
    end
    drop_reference(st)
    st.force = true
    request(b, { force = true })
    gvim.notify(t("status.refreshed"))
  end,
}

gvim.command.register {
  name = "set_default",
  title = "%cmd.set_default",
  description = "%cmd.set_default_desc",
  run = function()
    local current = default_override or settings.default_mode
    local answer = gvim.ui.input { prompt = t("prompt.default_mode"), default = current, select_all = true }
    if not answer then
      return
    end
    answer = answer:gsub("^%s+", ""):gsub("%s+$", ""):lower()
    local mode = text_to_mode(answer)
    if not mode then
      gvim.notify(t("status.bad_default"))
      return
    end
    default_override = mode_to_text(mode)
    gvim.state.set("live_diff.default_mode", default_override)
    gvim.notify(t("status.default_set"))
  end,
}

-- Events --------------------------------------------------------------------

-- optional_event subscribes to an event only when the host knows it, so the
-- plugin works before the 1c-C event catalogue lands.
local function optional_event(name, fn)
  pcall(gvim.on, name, fn)
end

gvim.on("after_file_open", function(ev)
  if not ev.buf then
    return
  end
  local st = states[ev.buf.id]
  request(ev.buf, { fetch = st == nil or st.ref_key == nil })
end)

gvim.on("buffer_activated", function(ev)
  if not ev.buf then
    return
  end
  local st = states[ev.buf.id]
  local stale = not st or not st.checked_at or gvim.time.monotonic_ms() - st.checked_at > REFRESH_AFTER_MS
  request(ev.buf, { fetch = stale })
end)

for _, name in ipairs { "after_file_save", "after_file_revert" } do
  gvim.on(name, function(ev)
    local b = ev.buf
    if not b then
      return
    end
    local st = states[b.id]
    if not st then
      request(b, { fetch = true })
      return
    end
    local fetch = false
    if st.path ~= ev.path then
      st.path = ev.path
      drop_reference(st)
      fetch = true
    elseif st.mode.kind == "disk" then
      drop_reference(st)
      fetch = true
    end
    request(b, { fetch = fetch, force = true })
  end)
end

gvim.on("buffer_closed", function(ev)
  local st = states[ev.id]
  if st then
    stop_file_watch(st)
    states[ev.id] = nil
    notice_buffers[ev.id] = nil
  end
end)

-- A buffer text change (typing, undo, an external-file reload) recomputes after
-- the settings debounce. buffer_changed is the gvim equivalent of Fresh's
-- after_insert/after_delete/lines_changed trio.
gvim.on("buffer_changed", function(ev)
  local b = ev.buf
  if not b then
    return
  end
  local st = states[b.id]
  if st and effective_enabled(st) then
    request(b)
  end
end, { debounce = settings.debounce_ms })

optional_event("path_changed", function()
  -- A git HEAD move: refresh every head/branch baseline of that repository.
  for _, st in pairs(states) do
    if st.mode.kind ~= "disk" then
      st.reload_ref = true
    end
  end
  for id, st in pairs(states) do
    local b = gvim.buf.get(id)
    if b and b:valid() and st.mode.kind ~= "disk" then
      request(b, { fetch = true })
    end
  end
end)

optional_event("focus_gained", function()
  for id, st in pairs(states) do
    local b = gvim.buf.get(id)
    if b and b:valid() then
      request(b, { fetch = true })
    end
  end
end)

optional_event("active_window_changed", function()
  for id, st in pairs(states) do
    local b = gvim.buf.get(id)
    if b and b:valid() and st.again then
      request(b)
    end
  end
end)

-- Exports and unload --------------------------------------------------------

-- hunks returns the last diff of a buffer for other plugins (diff_nav and
-- friends): {kind, newStart, newCount}, no old-side text.
gvim.plugins.export {
  hunks = function(buf_id)
    local st = states[buf_id]
    if not st then
      return nil
    end
    local out = {}
    for i, h in ipairs(st.hunks) do
      out[i] = { kind = h.kind, newStart = h.newStart, newCount = h.newCount }
    end
    return out
  end,
  set_similarity_threshold = function(value)
    local clamped = tonumber(value) or 0.95
    clamped = math.max(0, math.min(1, clamped))
    similarity_threshold = clamped
    for id, st in pairs(states) do
      st.last_key = ""
      local b = gvim.buf.get(id)
      if b and b:valid() then
        request(b)
      end
    end
    return clamped
  end,
  get_similarity_threshold = function()
    return similarity_threshold
  end,
}

gvim.plugin.on_unload(function()
  for key, w in pairs(watchers) do
    w:stop()
    watchers[key] = nil
  end
  for _, st in pairs(states) do
    stop_file_watch(st)
  end
end)

-- Initialisation ------------------------------------------------------------

-- A plugin that loads late gets after_file_open/buffer_activated replayed for
-- the open buffers; scanning here as well makes initialisation idempotent.
for _, b in ipairs(gvim.buf.list()) do
  if b:path() then
    local st = ensure_state(b)
    if st then
      request(b, { fetch = true })
    end
  end
end

gvim.log.debug("live_diff: loaded, rendering %s", RENDER_VLINES and "full" or "reduced")
