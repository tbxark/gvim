-- Git gutter. A port of Fresh's git_gutter and diff_nav plugins.
--
-- The open buffer is compared with the file at HEAD. Added and modified lines get a bar in
-- the gutter and a mark on the scrollbar, a place where lines were deleted gets a triangle.
-- "Next Change" and "Previous Change" move the caret between the changed regions.
--
-- Differences from Fresh: Fresh compares the file on disk with HEAD and leaves unsaved text
-- to the editor's own change bars; here the buffer text is compared, so the marks follow
-- what you type (after a short pause). The HEAD text is read once per file with `git show`,
-- kept (bounded) and compared with the buffer by the host: no line arrays are built in Lua,
-- and a large buffer is never scanned by this plugin (the host reads it in one piece, or
-- refuses it). Outside a repository, or for an untracked file, the plugin stays idle.
--
-- Exports for other plugins: hunks(buf_id) -> { {type = "added"|"modified"|"deleted",
-- line = first line (0-based), count = lines}, ... } for a buffer that is tracked by git.
local gvim = require "gvim"
local git = gvim.git

local settings = gvim.settings.define {
  debounce_ms = {
    type = "integer", default = 300, minimum = 50, maximum = 5000, project = true,
    description = "Pause after typing before the markers are computed again, in milliseconds",
  },
}

local ns_gutter = gvim.decor.namespace("git_gutter")
local ns_scroll = gvim.decor.namespace("git_gutter_scroll")

local REFRESH_AFTER_MS = 5000          -- an activated buffer asks git again when its last answer is older
local MAX_CACHE_BYTES = 32 * 1024 * 1024 -- HEAD text kept for all buffers together
local MAX_ITEMS = 10000                -- bars per buffer (the host allows 50 000 decorations per plugin)
local YIELD_EVERY = 2000           -- bars built per turn of the scheduler
local MAX_PASSES = 3                   -- recomputations of one request when the buffer keeps changing

local states = {}      -- buffer id -> state
local cache_bytes = 0

-- A state: { id, path, head = HEAD text | nil, tracked = nil (unknown) | true | false,
--            checked_at, running, again, want_fetch, marked, hunks, regions, at_version }
local function track(b)
  local st = states[b.id]
  if not st then
    st = { id = b.id, hunks = {}, regions = {} }
    states[b.id] = st
  end
  return st
end

local function set_head(st, text)
  if st.head then
    cache_bytes = cache_bytes - #st.head
  end
  st.head = text
  if text then
    cache_bytes = cache_bytes + #text
  end
end

-- evict forgets the HEAD text of buffers other than keep until the cache fits again; their
-- markers stay, and the text is read again when they change.
local function evict(keep)
  if cache_bytes <= MAX_CACHE_BYTES then
    return
  end
  for _, st in pairs(states) do
    if st ~= keep and st.head and not st.running then
      set_head(st, nil)
      if cache_bytes <= MAX_CACHE_BYTES then
        return
      end
    end
  end
end

-- to_gutter_hunks turns the line diff (old and new ranges) into the marks Fresh draws: the
-- lines a replacement shares are "modified", the surplus is "added", or "deleted" for lines
-- that are gone. A deleted run is marked on the line before the seam.
local function to_gutter_hunks(raw)
  local out = {}
  for _, h in ipairs(raw) do
    local paired = math.min(h.old_count, h.new_count)
    if paired > 0 then
      out[#out + 1] = { type = "modified", line = h.new_start, count = paired }
    end
    if h.new_count > h.old_count then
      out[#out + 1] = { type = "added", line = h.new_start + paired, count = h.new_count - h.old_count }
    elseif h.old_count > h.new_count then
      out[#out + 1] = {
        type = "deleted",
        line = paired > 0 and h.new_start or math.max(h.new_start - 1, 0),
        count = h.old_count - h.new_count,
      }
    end
  end
  return out
end

-- region_lines is the first line of every changed region, ascending and without repeats.
local function region_lines(raw)
  local out = {}
  for _, h in ipairs(raw) do
    local line = h.new_count > 0 and h.new_start or math.max(h.new_start - 1, 0)
    if out[#out] ~= line then
      out[#out + 1] = line
    end
  end
  return out
end

-- decorations builds the gutter bars and the scrollbar marks of the hunks. A hunk is one
-- scrollbar mark however many lines it has; the bars are capped by the host's limit. It may
-- yield (the buffer can change meanwhile: the host then refuses the stale result).
local function decorations(hunks)
  local gutter, scroll = {}, {}
  for _, h in ipairs(hunks) do
    if h.type == "deleted" then
      if #gutter < MAX_ITEMS then
        gutter[#gutter + 1] = { kind = "gutter", line = h.line, text = "▾", style = { fg = "delete" }, priority = 9 }
      end
      if #scroll < MAX_ITEMS then
        scroll[#scroll + 1] = { kind = "scroll", line = h.line, color = "delete", priority = 9 }
      end
    else
      local color = h.type == "added" and "add" or "change"
      for i = 0, math.min(h.count, MAX_ITEMS - #gutter) - 1 do
        gutter[#gutter + 1] = { kind = "gutter", line = h.line + i, text = "│", style = { fg = color }, priority = 10 }
        if #gutter % YIELD_EVERY == 0 then
          gvim.timer.sleep(0) -- a long run of bars is built over several turns, within the time budget
        end
      end
      if #scroll < MAX_ITEMS then
        scroll[#scroll + 1] = {
          kind = "scroll", range = gvim.range(h.line, 0, h.line + h.count, 0), color = color, priority = 10,
        }
      end
    end
  end
  return gutter, scroll
end

local function clear_marks(b, st)
  if st.marked then
    ns_gutter:clear(b)
    ns_scroll:clear(b)
    st.marked = false
  end
  st.hunks, st.regions, st.at_version = {}, {}, nil
end

-- update brings the markers of buffer b up to date. With fetch it asks git for the HEAD
-- text again; otherwise it uses the text it has. It awaits, so the buffer may be edited or
-- closed meanwhile: after each await the state is checked, and a snapshot that is out of
-- date is not shown (the host refuses it as stale).
local function update(b, st, fetch)
  local info = b:info()
  local path = info and info.path or ""
  if not info or path == "" or info.virtual or info.large then
    set_head(st, nil)
    st.tracked, st.path = nil, nil
    clear_marks(b, st)
    return
  end
  if path ~= st.path then
    st.path = path
    set_head(st, nil)
    st.tracked, fetch = nil, true
  end
  if st.tracked == false and not fetch then
    return
  end
  if st.head == nil or fetch then
    local text, err = git.show(path, { for_buffer = b })
    if states[st.id] ~= st or not b:valid() then
      return
    end
    st.checked_at = gvim.time.monotonic_ms()
    if not text then
      set_head(st, nil)
      st.tracked = false
      clear_marks(b, st)
      if gvim.err_code(err) ~= "not_found" then
        gvim.log.debug("git_gutter: %s: %s", path, tostring(err))
      end
      return
    end
    set_head(st, text)
    st.tracked = true
    evict(st)
  end
  local raw, err = gvim.diff.buffer(b, st.head)
  if states[st.id] ~= st or not b:valid() then
    return
  end
  if not raw then
    gvim.log.debug("git_gutter: %s: %s", path, tostring(err))
    clear_marks(b, st)
    return
  end
  local hunks = to_gutter_hunks(raw)
  local gutter, scroll = decorations(hunks)
  if #gutter > 0 or #scroll > 0 or st.marked then
    local opts = { version = raw.version }
    local ok, serr = ns_gutter:set(b, gutter, opts)
    if ok then
      ok, serr = ns_scroll:set(b, scroll, opts)
    end
    if not ok then
      if gvim.err_code(serr) == "stale" then
        st.again = true -- the buffer changed after the snapshot: compute again
      elseif gvim.err_code(serr) ~= "closed" then
        gvim.log.debug("git_gutter: %s", tostring(serr))
      end
      return
    end
    st.marked = #gutter > 0 or #scroll > 0
  end
  st.hunks, st.regions, st.at_version = hunks, region_lines(raw), raw.version
end

-- request recomputes the markers of b. At most one computation runs per buffer: a request
-- that arrives meanwhile makes the running one go round again. It returns when its own
-- computation is done.
local function request(b, fetch)
  local st = track(b)
  st.want_fetch = st.want_fetch or fetch
  if st.running then
    st.again = true
    return
  end
  st.running = true
  local passes = 0
  repeat
    st.again = false
    local f = st.want_fetch
    st.want_fetch = false
    local ok, err = pcall(update, b, st, f)
    if not ok then
      gvim.log.warn("git_gutter: %s", tostring(err))
    end
    passes = passes + 1
  until not st.again or passes >= MAX_PASSES or states[st.id] ~= st
  st.running = false
end

-- ---------- events ----------

gvim.on("after_file_open", function(ev)
  if ev.buf and (ev.path or "") ~= "" then
    request(ev.buf, true)
  end
end)

gvim.on("buffer_activated", function(ev)
  local b = ev.buf
  if not b or not b:path() then
    return
  end
  local st = states[b.id]
  if not st or not st.checked_at or gvim.time.monotonic_ms() - st.checked_at > REFRESH_AFTER_MS then
    request(b, true)
  end
end)

-- After a save the path may be new (save as) and HEAD may have moved since the last look.
for _, name in ipairs { "after_file_save", "after_file_revert" } do
  gvim.on(name, function(ev)
    if ev.buf and (ev.path or "") ~= "" then
      request(ev.buf, true)
    end
  end)
end

gvim.on("buffer_closed", function(ev)
  local st = states[ev.id]
  if st then
    set_head(st, nil)
    states[ev.id] = nil
  end
end)

gvim.on("buffer_changed", function(ev)
  local st = ev.buf and states[ev.buf.id]
  if st and st.tracked then
    request(ev.buf, false)
  end
end, { debounce = settings.debounce_ms })

-- ---------- Next and Previous Change (Fresh's diff_nav) ----------

-- change_lines returns the first line of each changed region of b: against HEAD for a file
-- git tracks, else against the saved file.
local function change_lines(b)
  local st = states[b.id]
  if st and st.tracked then
    if st.at_version ~= tostring(b:version()) then
      request(b, false)
    end
    return st.regions
  end
  local raw = b:diff_saved()
  return raw and region_lines(raw) or {}
end

local function jump(direction)
  local b = gvim.buf.current()
  if not b then
    return
  end
  local lines = change_lines(b)
  if #lines == 0 then
    gvim.notify("No changes")
    return
  end
  local cur = b:cursor().line
  local idx, wrapped
  if direction > 0 then
    for i, l in ipairs(lines) do
      if l > cur then
        idx = i
        break
      end
    end
    if not idx then
      idx, wrapped = 1, true
    end
  else
    for i = #lines, 1, -1 do
      if lines[i] < cur then
        idx = i
        break
      end
    end
    if not idx then
      idx, wrapped = #lines, true
    end
  end
  b:set_cursor({ line = lines[idx], col = 0 }, { center = true })
  gvim.notify(string.format("Change %d/%d%s", idx, #lines, wrapped and " [wrapped]" or ""))
end

-- ---------- commands and exports ----------

gvim.command.register {
  name = "refresh",
  title = "Git Gutter: Refresh",
  description = "Refresh git gutter indicators for the current buffer",
  run = function()
    local b = gvim.buf.current()
    if not b or not b:path() then
      gvim.notify("Git Gutter: No file open")
      return
    end
    request(b, true)
    local st = states[b.id]
    gvim.notify(string.format("Git Gutter: %d change(s) detected", st and #st.regions or 0))
  end,
}

gvim.command.register {
  name = "next_change",
  title = "Next Change",
  description = "Jump to the next changed region",
  run = function() jump(1) end,
}

gvim.command.register {
  name = "prev_change",
  title = "Previous Change",
  description = "Jump to the previous changed region",
  run = function() jump(-1) end,
}

gvim.plugins.export {
  hunks = function(buf_id)
    local st = states[buf_id]
    if not st or not st.tracked then
      return nil
    end
    local out = {}
    for i, h in ipairs(st.hunks) do
      out[i] = { type = h.type, line = h.line, count = h.count }
    end
    return out
  end,
}
