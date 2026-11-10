-- Merge conflicts. A reduced port of Fresh's merge_conflict plugin: the conflict markers
-- git leaves in a file are highlighted in place, `Merge: Next Conflict` and `Previous
-- Conflict` move between them, and `Use Ours`, `Take Theirs` and `Use Both` resolve the
-- conflict under the caret with one edit (one undo step). `Merge: Start Resolution` turns on
-- a small key mode for the same actions (n/p, u/t/b, q). Fresh's three-panel merge view is
-- not ported.
--
-- A conflict is the block
--
--     <<<<<<< ours-label
--     our lines
--     ||||||| base-label        (only with merge.conflictStyle=diff3)
--     base lines
--     =======
--     their lines
--     >>>>>>> theirs-label
--
-- The plugin never reads a whole buffer. It finds the marker lines with b:find_lines (host
-- side, time-boxed, chunked here with gvim.timer.sleep(0)), keeps only those lines, and after
-- an edit shifts them by what the edit did and rescans just the lines the edit touched.
local gvim = require "gvim"

local DEBOUNCE_MS = 300
local MAX_MARKERS = 8000 -- marker lines kept per buffer
local MAX_CONFLICTS = 1000 -- conflicts decorated per buffer
local SHIFT_WORK_CAP = 20000 -- markers x edits above which the markers are rescanned instead
local HUGE_COL = 2 ^ 30 -- "the end of the line" for a clamped column
local MAX_ATTEMPTS = 4
local ITEM_CHUNK = 100 -- conflicts turned into decorations per slice
local SCAN_CHUNK = 300 -- marker lines examined per slice (each costs a b:line call)
local PARK_LIMIT_S = 5 -- a scan parked on a pause for longer than this is taken for dead

-- One line that is a conflict marker: seven of the marker character, then a space or the end
-- of the line (the separator has nothing after it).
local MARKER_PATTERN = [[^(?:(?:<{7}|\|{7}|>{7})(?: |$)|={7}$)]]

local ns = gvim.decor.namespace "conflicts"

local MARKER_STYLE = { bg = "editor.diff_modify_bg", bold = true }
local OURS_STYLE = { bg = "editor.diff_add_bg", extend = true }
local THEIRS_STYLE = { bg = "editor.diff_remove_bg", extend = true }

local states = {} -- buffer id -> state

-- ---------- markers and conflicts ----------

local function kind_of(text)
  local head, rest = text:sub(1, 7), text:sub(8, 8)
  if head == "=======" then
    return rest == "" and "mid" or nil
  end
  if rest ~= "" and rest ~= " " then
    return nil
  end
  if head == "<<<<<<<" then
    return "start"
  elseif head == "|||||||" then
    return "base"
  elseif head == ">>>>>>>" then
    return "finish"
  end
end

-- build turns the sorted marker lines into conflicts. A marker that does not fit (a stray
-- separator, a block that never closes) is skipped.
local function build(markers)
  local out, cur = {}, nil
  for _, m in ipairs(markers) do
    local k = m.kind
    if k == "start" then
      cur = { start = m.line }
    elseif cur and k == "base" and not cur.base and not cur.mid then
      cur.base = m.line
    elseif cur and k == "mid" and not cur.mid then
      cur.mid = m.line
    elseif cur and k == "finish" and cur.mid then
      cur.finish = m.line
      out[#out + 1] = cur
      cur = nil
      if #out >= MAX_CONFLICTS then
        break
      end
    end
  end
  return out
end

local function signature(conflicts)
  local parts = {}
  for i, c in ipairs(conflicts) do
    parts[i] = c.start .. "," .. (c.base or "") .. "," .. c.mid .. "," .. c.finish
  end
  return table.concat(parts, ";")
end

-- pause yields to the host between two chunks of a scan. While the task is parked here
-- st.parked says so: a task that runs out of its slice budget is killed without any code of
-- the plugin running, so the busy flag can only be trusted while the owner is parked.
local function pause(st)
  st.parked = os.clock()
  gvim.timer.sleep(0)
  st.parked = nil
end

-- busy reports whether a task is scanning the buffer of st. Handlers of a plugin run one at
-- a time, so when this runs the owner of st.running is either parked in pause or dead (it
-- was killed by a timeout or a slow host call, and cannot clear the flag): then the state
-- is reset and rescanned.
local function busy(st)
  if not st.running then
    return false
  end
  if st.parked and os.clock() - st.parked < PARK_LIMIT_S then
    return true
  end
  st.running, st.parked = nil, nil
  st.dirty = "all"
  return false
end

-- scan_markers returns the marker lines in [from, to) (to nil: the end) in order, chunk by
-- chunk. It gives up with "stale" when the buffer changes while it waits between chunks.
local function scan_markers(st, from, to, version)
  local b = st.buf
  local out, cur, truncated = {}, from, false
  while cur do
    local lines, nxt = b:find_lines(MARKER_PATTERN, {
      regex = true, case_sensitive = true, from = cur, to = to, max = SCAN_CHUNK,
    })
    if not lines then
      return nil, nxt
    end
    for _, n in ipairs(lines) do
      local text = b:line(n)
      local kind = text and kind_of(text)
      if kind then
        out[#out + 1] = { line = n, kind = kind }
      end
    end
    if #out > MAX_MARKERS then
      truncated = true
      break
    end
    cur = nxt
    if cur then
      pause(st)
      if not b:valid() or b:version() ~= version then
        return nil, "stale: the buffer changed during the scan"
      end
    end
  end
  return out, nil, truncated
end

-- ---------- decorations ----------

local function pos(line, col)
  return { line = line, col = col }
end

-- items_for turns the conflicts into decorations, yielding between chunks of them. It returns
-- nil when the buffer changed meanwhile.
local function items_for(st)
  local items = {}
  local function add(item)
    items[#items + 1] = item
  end
  for i, c in ipairs(st.conflicts) do
    if i % ITEM_CHUNK == 0 then
      pause(st)
      if not st.buf:valid() or st.buf:version() ~= st.version then
        return nil
      end
    end
    add { kind = "line", line = c.start, style = MARKER_STYLE }
    add { kind = "line", line = c.mid, style = MARKER_STYLE }
    add { kind = "line", line = c.finish, style = MARKER_STYLE }
    if c.base then
      add { kind = "line", line = c.base, style = MARKER_STYLE }
    end
    local ours_end = c.base or c.mid -- the first line after our lines
    if ours_end > c.start + 1 then
      add { kind = "highlight", range = { from = pos(c.start + 1, 0), to = pos(ours_end, 0) }, style = OURS_STYLE }
    end
    if c.finish > c.mid + 1 then
      add { kind = "highlight", range = { from = pos(c.mid + 1, 0), to = pos(c.finish, 0) }, style = THEIRS_STYLE }
    end
    add { kind = "scroll", line = c.start, color = "warning" }
  end
  return items
end

-- apply rebuilds the conflicts from the markers and redraws them when they changed. It
-- returns false when the host refused the decorations as stale.
local function apply(st)
  st.conflicts = build(st.markers)
  local sig = signature(st.conflicts)
  if sig == st.sig then
    return true
  end
  if #st.conflicts == 0 then
    ns:clear(st.buf)
    st.sig = sig
    return true
  end
  local items = items_for(st)
  if not items then
    st.sig = nil
    return false
  end
  local ok, err = ns:set(st.buf, items, { version = st.version })
  if not ok then
    st.sig = nil
    if gvim.err_code(err) ~= "stale" then
      gvim.log.warn("merge_conflict: cannot decorate %s: %s", st.buf:name() or "?", tostring(err))
    end
    return false
  end
  st.sig = sig
  return true
end

-- ---------- keeping the markers current ----------

-- refresh rescans d ("all", or {lo=, hi=} lines of the current text) and stores the markers.
local function refresh(st, d)
  local b = st.buf
  local v = b:version()
  local markers, err, truncated
  if d == "all" or st.truncated then
    markers, err, truncated = scan_markers(st, 0, nil, v)
    if not markers then
      return false, err
    end
    st.markers, st.truncated = markers, truncated
  else
    markers, err = scan_markers(st, d.lo, d.hi + 1, v)
    if not markers then
      return false, err
    end
    local merged = {}
    for _, m in ipairs(st.markers) do
      if m.line < d.lo or m.line > d.hi then
        merged[#merged + 1] = m
      end
    end
    for _, m in ipairs(markers) do
      merged[#merged + 1] = m
    end
    table.sort(merged, function(x, y)
      return x.line < y.line
    end)
    st.markers = merged
  end
  st.version = v
  st.lines = b:line_count()
  return true
end

-- update brings the state of a buffer up to date. One task at a time works on a buffer;
-- changes that arrive while it waits between chunks only mark the state dirty.
local function update(st)
  if busy(st) then
    return
  end
  st.running = true
  local ok, perr = pcall(function()
    for _ = 1, MAX_ATTEMPTS do
      local d = st.dirty
      if not d or not st.buf:valid() then
        return
      end
      st.dirty = nil
      local fine, err = refresh(st, d)
      if fine then
        if not st.dirty and not apply(st) then
          st.dirty = "all"
        end
      elseif gvim.err_code(err) == "stale" then
        st.dirty = "all"
      else
        gvim.log.warn("merge_conflict: cannot scan %s: %s", st.buf:name() or "?", tostring(err))
        st.markers, st.version, st.truncated = {}, nil, false
        apply(st)
        return
      end
    end
    if st.dirty and st.buf:valid() then
      gvim.timer.after(DEBOUNCE_MS, function()
        update(st)
      end)
    end
  end)
  st.running = nil
  if not ok then
    gvim.log.error("merge_conflict: %s", tostring(perr))
  end
end

local function state_for(b)
  if not b or not b:valid() then
    return nil
  end
  local st = states[b.id]
  if st then
    return st
  end
  local info = b:info()
  if not info or info.virtual or info.large then
    return nil
  end
  st = { buf = b, markers = {}, conflicts = {}, dirty = "all" }
  states[b.id] = st
  return st
end

-- shift applies the edits of a buffer_changed event, which are sequential, to the marker
-- lines: markers inside an edited range are dropped, later ones move by the lines the edit
-- added or removed. It returns the lines to rescan, or nil when a full rescan is cheaper.
local function shift(st, edits)
  if #st.markers * #edits > SHIFT_WORK_CAP then
    return nil
  end
  local markers, lines, lo, hi = st.markers, st.lines, nil, nil
  for _, e in ipairs(edits) do
    local first, last = e.range.from.line, e.range.to.line
    local _, nl = (e.text or ""):gsub("\n", "\n")
    local delta = nl - (last - first)
    lines = lines + delta
    local keep = {}
    for _, m in ipairs(markers) do
      if m.line < first then
        keep[#keep + 1] = m
      elseif m.line > last then
        m.line = m.line + delta
        keep[#keep + 1] = m
      end
    end
    markers = keep
    local nlast = first + nl
    if lo then
      if lo > last then
        lo = lo + delta
      elseif lo >= first then
        lo = first
      end
      if hi > last then
        hi = hi + delta
      elseif hi >= first then
        hi = nlast
      end
      lo, hi = math.min(lo, first), math.max(hi, nlast)
    else
      lo, hi = first, nlast
    end
  end
  st.markers = markers
  return { lo = math.max(lo, 0), hi = math.max(hi, 0) }, lines
end

local function on_open(ev)
  local st = state_for(ev.buf)
  if not st then
    return
  end
  if st.version == nil or st.version ~= st.buf:version() then
    st.dirty = "all"
  end
  if st.dirty then
    update(st)
  end
end

local function on_revert(ev)
  local st = state_for(ev.buf)
  if not st then
    return
  end
  st.sig = nil -- the host dropped the decorations with the old text
  st.dirty = "all"
  update(st)
end

local function on_changed(ev)
  local b = ev.buf
  local st = state_for(b)
  if not st then
    return
  end
  if busy(st) or ev.overflow or not ev.edits or not st.version or st.truncated or st.dirty == "all" then
    st.dirty = "all"
  elseif ev.version == st.version then
    return -- the markers already describe this text
  elseif ev.version ~= b:version() then
    st.dirty = "all" -- more edits are on their way; the markers cannot be shifted piecemeal
  else
    local hull, lines = shift(st, ev.edits)
    if not hull or lines ~= b:line_count() then
      st.dirty = "all"
    else
      st.lines = lines
      st.dirty = hull
    end
  end
  update(st)
end

gvim.on("after_file_open", on_open)
gvim.on("buffer_activated", on_open)
gvim.on("after_file_revert", on_revert)
gvim.on("buffer_changed", on_changed, { debounce = DEBOUNCE_MS })
gvim.on("buffer_closed", function(ev)
  states[ev.id] = nil
end)

-- ---------- commands ----------

-- ready returns the current buffer and its up-to-date state, or nil after telling the user.
local function ready()
  local b = gvim.buf.current()
  local st = state_for(b)
  if not st then
    gvim.notify("Merge: this buffer cannot have conflict markers")
    return nil
  end
  if busy(st) then
    gvim.notify("Merge: still scanning this file, try again in a moment")
    return nil
  end
  if st.dirty or st.version ~= b:version() then
    st.dirty = "all"
    update(st)
  end
  if st.version ~= b:version() then
    gvim.notify("Merge: the file changed while it was scanned, try again")
    return nil
  end
  return b, st
end

-- containing returns the index and the conflict whose lines include line.
local function containing(st, line)
  for i, c in ipairs(st.conflicts) do
    if line >= c.start and line <= c.finish then
      return i, c
    end
  end
end

local function goto_conflict(delta)
  local b, st = ready()
  if not b then
    return
  end
  local n = #st.conflicts
  if n == 0 then
    gvim.notify("No merge conflicts in this file")
    return
  end
  local line = b:cursor().line
  local idx = containing(st, line)
  if idx then
    idx = (idx - 1 + delta) % n + 1
  elseif delta > 0 then
    idx = 1
    for i, c in ipairs(st.conflicts) do
      if c.start > line then
        idx = i
        break
      end
    end
  else
    idx = n
    for i = n, 1, -1 do
      if st.conflicts[i].finish < line then
        idx = i
        break
      end
    end
  end
  b:set_cursor(pos(st.conflicts[idx].start, 0), { center = true })
  gvim.notify(("Conflict %d of %d"):format(idx, n))
end

-- Line ranges (inclusive) a resolution deletes. Everything else of the block stays as it is,
-- so no text is read or rewritten.
local function deletions(c, choice)
  local d
  if choice == "ours" then
    d = { { c.start, c.start }, { c.base or c.mid, c.finish } }
  elseif choice == "theirs" then
    d = { { c.start, c.mid }, { c.finish, c.finish } }
  else
    d = { { c.start, c.start }, c.base and { c.base, c.mid } or { c.mid, c.mid }, { c.finish, c.finish } }
  end
  local out = {}
  for _, iv in ipairs(d) do
    local last = out[#out]
    if last and iv[1] <= last[2] + 1 then
      last[2] = math.max(last[2], iv[2])
    else
      out[#out + 1] = { iv[1], iv[2] }
    end
  end
  return out
end

-- delete_lines is the edit that removes lines a..b with their line breaks.
local function delete_lines(a, b, count)
  if b + 1 < count then
    return { range = { from = pos(a, 0), to = pos(b + 1, 0) }, text = "" }
  elseif a > 0 then -- the last lines: take the break before them instead
    return { range = { from = pos(a - 1, HUGE_COL), to = pos(b, HUGE_COL) }, text = "" }
  end
  return { range = { from = pos(0, 0), to = pos(b, HUGE_COL) }, text = "" }
end

local CHOICES = {
  ours = "Use Ours",
  theirs = "Take Theirs",
  both = "Use Both",
}

local function leave_mode()
  if gvim.mode.current() == "resolve" then
    gvim.mode.leave()
  end
end

local function resolve(choice)
  local b, st = ready()
  if not b then
    return
  end
  local _, c = containing(st, b:cursor().line)
  if not c then
    gvim.notify("Merge: the caret is not inside a conflict")
    return
  end
  local count = b:line_count()
  local edits = {}
  for _, iv in ipairs(deletions(c, choice)) do
    edits[#edits + 1] = delete_lines(iv[1], iv[2], count)
  end
  local ok, err = b:edit(edits, { version = st.version })
  if not ok then
    gvim.notify("Merge: " .. CHOICES[choice] .. " failed: " .. tostring(err), "warn")
    return
  end
  st.dirty = "all"
  update(st)
  local nxt
  for _, k in ipairs(st.conflicts) do
    if k.start >= c.start then
      nxt = k
      break
    end
  end
  nxt = nxt or st.conflicts[1]
  if nxt then
    b:set_cursor(pos(nxt.start, 0), { center = true })
    gvim.notify(("%s: %d conflict%s remaining"):format(CHOICES[choice], #st.conflicts, #st.conflicts == 1 and "" or "s"))
  else
    b:set_cursor(pos(math.min(c.start, math.max(b:line_count() - 1, 0)), 0))
    gvim.notify("Merge: all conflicts resolved")
    leave_mode()
  end
end

local function pick()
  local b, st = ready()
  if not b then
    return
  end
  if not containing(st, b:cursor().line) then
    gvim.notify("Merge: the caret is not inside a conflict")
    return
  end
  local items = { "Use Ours", "Take Theirs", "Use Both" }
  local item = gvim.ui.select(items, { title = "Resolve conflict" })
  for choice, title in pairs(CHOICES) do
    if title == item then
      resolve(choice)
    end
  end
end

local HELP = "Merge: [n/p] Navigate | [u] Ours [t] Theirs [b] Both | [q] Quit"

local function start()
  local b, st = ready()
  if not b then
    return
  end
  local n = #st.conflicts
  if n == 0 then
    gvim.notify("No merge conflicts in this file")
    return
  end
  if not containing(st, b:cursor().line) then
    goto_conflict(1)
  end
  gvim.mode.enter "resolve"
  gvim.notify(("%d conflict%s. %s"):format(n, n == 1 and "" or "s", HELP))
end

gvim.mode.define {
  name = "resolve",
  text = "swallow",
  status = "-- MERGE --",
  bindings = {
    ["n"] = function() goto_conflict(1) end,
    ["]"] = function() goto_conflict(1) end,
    ["p"] = function() goto_conflict(-1) end,
    ["["] = function() goto_conflict(-1) end,
    ["u"] = function() resolve "ours" end,
    ["t"] = function() resolve "theirs" end,
    ["b"] = function() resolve "both" end,
    ["?"] = function() gvim.notify(HELP) end,
    ["q"] = leave_mode,
    ["escape"] = leave_mode,
  },
}

local function command(spec)
  gvim.command.register(spec)
end

command { name = "start", title = "Merge: Start Resolution", description = "Resolve the merge conflicts of this file with single keys", run = start }
command { name = "next", title = "Merge: Next Conflict", description = "Jump to the next merge conflict", run = function() goto_conflict(1) end }
command { name = "prev", title = "Merge: Previous Conflict", description = "Jump to the previous merge conflict", run = function() goto_conflict(-1) end }
command { name = "use_ours", title = "Merge: Use Ours", description = "Keep our side of the conflict under the caret", when = { "!read_only" }, run = function() resolve "ours" end }
command { name = "take_theirs", title = "Merge: Take Theirs", description = "Keep their side of the conflict under the caret", when = { "!read_only" }, run = function() resolve "theirs" end }
command { name = "use_both", title = "Merge: Use Both", description = "Keep both sides of the conflict under the caret", when = { "!read_only" }, run = function() resolve "both" end }
command { name = "resolve", title = "Merge: Resolve Conflict...", description = "Choose how to resolve the conflict under the caret", when = { "!read_only" }, run = pick }

gvim.keymap.set("alt+m", "merge_conflict.start", { desc = "Merge: Start Resolution" })
gvim.keymap.set("ctrl+alt+c", "merge_conflict.next", { desc = "Merge: Next Conflict" })
