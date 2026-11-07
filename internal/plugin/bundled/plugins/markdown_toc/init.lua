-- Markdown outline. A reduced port of Fresh's markdown_toc plugin: Fresh draws the table of
-- contents in a sidebar section with folding; gvim has no widgets, so the outline is a
-- read-only virtual buffer in a split on the left. Enter on a row jumps to the heading, q
-- closes the outline, Esc goes back to the Markdown buffer and leaves it open.
--
-- The outline follows the buffer you work in: it is refreshed 300 ms after an edit and it
-- switches to the next Markdown buffer you activate. The selected row follows the cursor
-- (or, with the setting follow = "scroll", the top of the window). Fenced code is skipped,
-- so a "# comment" in a shell block is not a heading.
--
-- Big documents: the plugin never reads the whole buffer. b:find_lines picks the lines that
-- look like a heading or a fence, in chunks that yield in between, and only those lines are
-- read. An edit event re-scans only the lines the edit touched: the editor reports edits in
-- sequence, so the plugin keeps the markers (headings and fences) of the document up to
-- date by shifting them and scanning the new lines. Whether a heading sits inside a fence
-- is worked out from the markers when the outline is drawn, so an edit that opens or closes
-- a fence needs no wider re-scan.
local gvim = require "gvim"

local settings = gvim.settings.define {
  max_depth = {
    type = "integer", default = 6, minimum = 1, maximum = 6,
    description = "Deepest heading level listed in the outline (1 is '#', 6 is '######').",
  },
  follow = {
    type = "enum", values = { "cursor", "scroll" }, default = "cursor",
    description = "Which heading the outline selects: the one the cursor is under, or the one at the top of the window.",
  },
}

local MODE = "markdown_outline"
local OUTLINE_NAME = "Outline"
local OUTLINE_SHARE = 0.3 -- width of the outline split
local MAX_MARKERS = 20000 -- headings and fences kept per document
local MAX_ROWS = 5000     -- rows drawn
local CHUNK = 2000        -- candidate lines per b:find_lines call
local MAX_DIRTY = 64      -- scan ranges kept before the document is scanned afresh
local RESYNC_MS = 1000
-- Lines worth reading: an ATX heading (up to three spaces of indent) or a fence.
local CANDIDATES = [[^(?: {0,3}#{1,6}[ \t]+\S|\s*(?:`{3,}|~{3,}))]]

-- st is the state of the one outline. src is the Markdown buffer shown, out the outline
-- buffer and win its window.
local st = {
  src = nil, src_win = nil, out = nil, win = nil,
  markers = {},     -- headings and fences of src, sorted by line
  dirty = {},       -- line ranges {lo, hi} (hi exclusive) the markers do not cover yet
  version = nil,    -- version of src the markers belong to
  line_count = nil, -- line count of src they belong to
  capped = false,   -- more markers than MAX_MARKERS
  too_large = false,
  changed = false,  -- markers moved since the outline was drawn
  rows = {},        -- the headings drawn, one per outline line
  followed = nil,   -- row the outline cursor was last put on
  gen = 0,          -- bumped by every event; a running scan that sees it move stops
  subs = {},
}

-- ---------- Which buffers ----------

local EXTENSIONS = { md = true, markdown = true, mdown = true, mkd = true, mdx = true }

local function is_markdown(b)
  if not b or b:is_virtual() then
    return false
  end
  local lang = b:language()
  if lang == "markdown" or lang == "mdx" then
    return true
  end
  local ext = (b:path() or b:name() or ""):match("%.(%w+)$")
  return ext ~= nil and EXTENSIONS[ext:lower()] == true
end

local function alive(b)
  return b ~= nil and b:valid()
end

-- ---------- Markers ----------

local MAX_TITLE_BYTES = 1024

local function is_space(c)
  return c == 32 or (c >= 9 and c <= 13)
end

-- trim_end returns the length of s up to i without trailing whitespace.
local function trim_end(s, i)
  while i > 0 and is_space(s:byte(i)) do
    i = i - 1
  end
  return i
end

-- classify reads the text of a candidate line: a fence marker, a heading marker, or nil.
local function classify(n, text)
  local ticks = text:match("^%s*(`+)") or text:match("^%s*(~+)")
  if ticks and #ticks >= 3 then
    local rest = text:match("^%s*[`~]+(.*)$") or ""
    -- a closing fence has no info string
    return { line = n, kind = "f", ch = ticks:sub(1, 1), n = #ticks, bare = rest:match("^%s*$") ~= nil }
  end
  local hashes, p = text:match("^ ? ? ?(#+)()[ \t]")
  if hashes and #hashes <= 6 then
    -- Whitespace runs can be arbitrarily long, and a backtracking pattern over them is
    -- quadratic, so the title is cut out by position and trimmed with bounded loops.
    local s = text:find("%S", p)
    if s then
      local title = text:sub(s, s + MAX_TITLE_BYTES - 1)
      local e = trim_end(title, #title)
      local h = e
      while h > 0 and title:byte(h) == 35 do
        h = h - 1
      end
      -- closing hashes count only after whitespace, and not when they are the whole title
      if h > 0 and h < e and is_space(title:byte(h)) then
        e = trim_end(title, h)
      end
      title = title:sub(1, e)
      if title ~= "" then
        return { line = n, kind = "h", level = #hashes, title = title }
      end
    end
  end
  return nil
end

-- headings returns the markers that are headings outside fenced code. A fence closes at a
-- line of the same character, at least as long, with nothing after it.
local function headings()
  local out, open = {}, nil
  for _, m in ipairs(st.markers) do
    if m.kind == "f" then
      if not open then
        open = m
      elseif m.ch == open.ch and m.n >= open.n and m.bare then
        open = nil
      end
    elseif not open then
      out[#out + 1] = m
    end
  end
  return out
end

-- lower_bound is the index of the first marker at or after line.
local function lower_bound(list, line)
  local lo, hi = 1, #list + 1
  while lo < hi do
    local mid = math.floor((lo + hi) / 2)
    if list[mid].line < line then
      lo = mid + 1
    else
      hi = mid
    end
  end
  return lo
end

-- merge adds the markers of one scanned chunk, which are in line order.
local function merge(new)
  local old = st.markers
  if #new == 0 then
    return
  end
  if #old == 0 or old[#old].line < new[1].line then
    for _, m in ipairs(new) do
      old[#old + 1] = m
    end
    return
  end
  local out, i, j = {}, 1, 1
  while i <= #old or j <= #new do
    if j > #new or (i <= #old and old[i].line < new[j].line) then
      out[#out + 1] = old[i]
      i = i + 1
    else
      out[#out + 1] = new[j]
      j = j + 1
    end
  end
  st.markers = out
end

-- ---------- Dirty ranges ----------

local function reset_full()
  local b = st.src
  st.markers, st.capped, st.changed = {}, false, true
  st.too_large = b:is_large()
  st.version, st.line_count = b:version(), b:line_count()
  st.dirty = st.too_large and {} or { { lo = 0, hi = math.huge } }
end

-- normalise sorts the dirty ranges and joins the ones that touch.
local function normalise()
  table.sort(st.dirty, function(a, b) return a.lo < b.lo end)
  local out = {}
  for _, iv in ipairs(st.dirty) do
    local last = out[#out]
    if last and iv.lo <= last.hi then
      last.hi = math.max(last.hi, iv.hi)
    else
      out[#out + 1] = iv
    end
  end
  st.dirty = out
end

-- apply_edit follows one change record of the document: it drops the markers on the lines
-- that were replaced, shifts the markers below, and marks the new lines for scanning. The
-- range is in the coordinates of the text before this record.
local function apply_edit(e)
  local from, to = e.range.from.line, e.range.to.line
  local _, newlines = e.text:gsub("\n", "")
  local delta = newlines - (to - from)
  local ms = st.markers
  local i = lower_bound(ms, from)
  local j = i
  while ms[j] and ms[j].line <= to do
    j = j + 1
  end
  if j > i then
    local out = {}
    for k = 1, i - 1 do
      out[k] = ms[k]
    end
    for k = j, #ms do
      ms[k].line = ms[k].line + delta
      out[#out + 1] = ms[k]
    end
    st.markers, st.changed = out, true
  elseif delta ~= 0 and ms[i] then
    for k = i, #ms do
      ms[k].line = ms[k].line + delta
    end
    st.changed = true
  end
  st.line_count = st.line_count + delta
  for _, iv in ipairs(st.dirty) do
    if iv.lo > to then
      iv.lo, iv.hi = iv.lo + delta, iv.hi + delta
    elseif iv.hi > from then -- overlaps the replaced lines
      iv.lo = math.min(iv.lo, from)
      iv.hi = iv.hi > to + 1 and iv.hi + delta or from + newlines + 1
    end
  end
  st.dirty[#st.dirty + 1] = { lo = from, hi = from + newlines + 1 }
end

-- ---------- Drawing ----------

local function style_of(level)
  if level <= 2 then
    return { bold = true }
  elseif level >= 4 then
    return { fg = "dim" }
  end
  return {}
end

local function render()
  st.changed, st.followed, st.scanning = false, nil, false
  if not alive(st.out) then
    return
  end
  local shown, base, truncated = {}, 7, false
  if st.src and not st.too_large then
    for _, h in ipairs(headings()) do
      if h.level <= settings.max_depth then
        if #shown >= MAX_ROWS then
          truncated = true
          break
        end
        shown[#shown + 1] = h
        base = math.min(base, h.level)
      end
    end
  end
  st.rows = shown
  local entries = {}
  if #shown == 0 then
    local msg = "(no headings)"
    if st.too_large then
      msg = "(the file is too large for an outline)"
    elseif not st.src then
      msg = "(no Markdown buffer)"
    end
    entries[1] = { text = msg, style = { fg = "dim" } }
  end
  for i, h in ipairs(shown) do
    local text = string.rep("  ", h.level - base) .. gvim.text.truncate(h.title, 120, "…")
    if i < #shown then
      text = text .. "\n"
    end
    entries[i] = { text = text, style = style_of(h.level), props = { line = h.line, level = h.level } }
  end
  if truncated or st.capped then
    entries[#entries + 1] = { text = "\n… the outline is cut short", style = { fg = "dim" } }
  end
  st.out:set_content(entries)
end

-- current_line is the line of the source the outline follows.
local function current_line()
  if settings.follow == "scroll" and alive(st.src_win) and st.src_win:buffer() == st.src then
    local rows = st.src_win:visible()
    if rows and rows[1] then
      return rows[1].line
    end
  end
  local c = st.src:cursor()
  return c and c.line
end

-- follow puts the outline cursor on the last heading at or above line.
local function follow(line)
  if line == nil or not alive(st.out) or #st.rows == 0 then
    return
  end
  local lo, hi, found = 1, #st.rows, 0
  while lo <= hi do
    local mid = math.floor((lo + hi) / 2)
    if st.rows[mid].line <= line then
      found, lo = mid, mid + 1
    else
      hi = mid - 1
    end
  end
  if found > 0 and found ~= st.followed then
    st.followed = found
    st.out:set_cursor(gvim.pos(found - 1, 0), { reveal = true })
  end
end

-- ---------- Scanning ----------

local resync

-- drain scans the dirty ranges, a chunk at a time, and draws the outline when it is done.
-- It stops when a newer event took over (gen moved) or the buffer moved on without an
-- event yet; the event brings the scan back.
local function drain(gen)
  local b = st.src
  while #st.dirty > 0 do
    local iv = st.dirty[1]
    local opts = { regex = true, from = iv.lo, max = CHUNK }
    if iv.hi ~= math.huge then
      opts.to = iv.hi
    end
    local lines, nxt = b:find_lines(CANDIDATES, opts)
    if not lines then
      return
    end
    if b:version() ~= st.version then
      resync()
      return
    end
    -- read no more lines than the markers have room for
    local room = MAX_MARKERS - #st.markers
    local new = {}
    for i = 1, math.min(#lines, room) do
      local text = b:line(lines[i])
      -- one bad line must not blank the outline
      local ok, m = false, nil
      if text then
        ok, m = pcall(classify, lines[i], text)
      end
      if ok and m then
        new[#new + 1] = m
      end
    end
    if #lines > room then
      st.capped, st.dirty, st.changed = true, {}, true
      merge(new)
      break
    end
    merge(new)
    if #new > 0 then
      st.changed = true
    end
    if nxt then
      iv.lo = nxt
      -- a huge first scan takes several chunks: say so rather than show an empty window
      if #st.rows == 0 and not st.scanning and alive(st.out) then
        st.scanning, st.changed = true, true
        st.out:set_content({ { text = "(scanning…)", style = { fg = "dim" } } })
      end
      gvim.timer.sleep(0)
      if gen ~= st.gen then
        return
      end
    else
      table.remove(st.dirty, 1)
    end
  end
  if st.changed then
    render()
  end
  follow(current_line())
end

local function next_gen()
  st.gen = st.gen + 1
  return st.gen
end

-- rescan scans the source afresh.
local function rescan()
  reset_full()
  drain(next_gen())
end

resync = gvim.timer.debounce(RESYNC_MS, function()
  if alive(st.src) and st.src:version() ~= st.version then
    rescan()
  end
end)

-- ---------- Windows ----------

-- focus_source gives the keyboard back to the window of the Markdown buffer.
local function focus_source()
  if not alive(st.src) then
    return
  end
  local w = st.src_win
  if not alive(w) or w == st.win then
    w = nil
    for _, x in ipairs(gvim.win.list()) do
      if x ~= st.win then
        w = x
        break
      end
    end
  end
  if not w then
    st.src:show()
    return
  end
  st.src_win = w
  if w:buffer() ~= st.src then
    w:set_buffer(st.src)
  end
  w:focus()
end

-- forget drops the state of the outline; the windows and buffers are the caller's.
local function forget()
  for _, s in ipairs(st.subs) do
    s:off()
  end
  st.subs = {}
  next_gen()
  st.src, st.src_win, st.out, st.win = nil, nil, nil, nil
  st.markers, st.dirty, st.rows, st.followed, st.scanning = {}, {}, {}, nil, false
  st.version, st.line_count, st.capped, st.too_large = nil, nil, false, false
end

local function close_outline()
  local win, out = st.win, st.out
  forget()
  if alive(win) then
    win:close()
  end
  if alive(out) then
    out:close { force = true }
  end
end

local function jump()
  if gvim.buf.current() ~= st.out or not alive(st.src) then
    return
  end
  local props = st.out:props_at()
  local line = props and props.line
  if not line then
    return
  end
  focus_source()
  st.src:set_cursor(gvim.pos(line, 0), { center = true })
end

-- ---------- Events ----------

local function on_changed(ev)
  local b = st.src
  if ev.buf ~= b or not alive(b) then
    return
  end
  local gen = next_gen()
  if st.capped or ev.overflow or not ev.edits or not st.line_count or #ev.edits * #st.markers > 400000 then
    reset_full()
  else
    for _, e in ipairs(ev.edits) do
      apply_edit(e)
    end
    normalise()
    if #st.dirty > MAX_DIRTY then
      reset_full()
    end
  end
  st.version = ev.version
  -- events that were dropped show as a document of another length than we expect
  if ev.version == b:version() and st.line_count ~= b:line_count() then
    reset_full()
  end
  drain(gen)
end

local function on_activated(ev)
  local b = ev.buf
  if b == st.out or not alive(st.out) then
    return
  end
  if b == st.src then
    st.src_win = gvim.win.current()
    if b:version() ~= st.version then
      rescan()
    end
  elseif is_markdown(b) then
    st.src, st.src_win = b, gvim.win.current()
    rescan()
  end
end

local function on_closed(ev)
  if st.src and ev.id == st.src.id then
    close_outline()
  elseif st.out and ev.id == st.out.id then
    local win = st.win
    forget()
    if alive(win) then
      win:close()
    end
  end
end

local function on_cursor(ev)
  if settings.follow == "cursor" and ev.buf == st.src then
    follow(ev.pos.line)
  end
end

local function on_viewport(ev)
  if settings.follow == "scroll" and ev.buf == st.src and alive(st.src) then
    follow(current_line())
  end
end

local function subscribe()
  local function on(name, fn, opts)
    st.subs[#st.subs + 1] = gvim.on(name, fn, opts)
  end
  on("buffer_changed", on_changed, { debounce = 300 })
  on("buffer_activated", on_activated)
  on("buffer_closed", on_closed)
  on("cursor_moved", on_cursor, { debounce = 80 })
  on("viewport_changed", on_viewport, { debounce = 80 })
end

-- ---------- Commands ----------

gvim.mode.define {
  name = MODE,
  text = "swallow",
  status = "OUTLINE  enter: jump  esc: back  q: close",
  bindings = {
    enter = jump,
    escape = focus_source,
    q = close_outline,
  },
}

-- open_window shows the outline buffer in a split on the left of the window of the source.
local function open_window()
  local from = alive(st.src_win) and st.src_win or gvim.win.current()
  local win, err = from:split { dir = "right", before = true, buf = st.out, ratio = OUTLINE_SHARE, focus = true }
  if win then
    st.win = win
  end
  return win, err
end

local function show()
  local cur = gvim.buf.current()
  if alive(st.out) then
    if cur ~= st.out and cur ~= st.src and is_markdown(cur) then
      st.src, st.src_win = cur, gvim.win.current()
      rescan()
    end
    if alive(st.win) then
      st.win:focus()
    else
      local win, err = open_window()
      if not win then
        gvim.notify("Cannot open the outline: " .. tostring(err), "warn")
        return
      end
    end
    render()
    return
  end
  if not is_markdown(cur) then
    gvim.notify("Not a Markdown buffer")
    return
  end
  local out, err = gvim.buf.create { name = OUTLINE_NAME, read_only = true, mode = MODE }
  if not out then
    gvim.notify("Cannot open the outline: " .. tostring(err), "warn")
    return
  end
  st.src, st.src_win, st.out = cur, gvim.win.current(), out
  local win, werr = open_window()
  if not win then
    forget()
    out:close { force = true }
    gvim.notify("Cannot open the outline: " .. tostring(werr), "warn")
    return
  end
  subscribe()
  rescan()
end

gvim.command.register {
  name = "show",
  title = "Markdown: Show Outline",
  description = "List the headings of the Markdown buffer in a split; Enter jumps to one",
  run = show,
}

gvim.command.register {
  name = "close",
  title = "Markdown: Close Outline",
  description = "Close the Markdown outline",
  run = function()
    if alive(st.out) then
      close_outline()
    end
  end,
}
