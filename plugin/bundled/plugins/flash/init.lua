-- Flash. A port of Fresh's flash plugin (flash.nvim in spirit): label-based jump to any
-- visible match.
--
--   1. `Flash: Jump` starts a session. With nothing typed, every visible word start gets a
--      one-letter label.
--   2. Each character typed extends a literal pattern. Every visible match in every window
--      gets a label; the match is highlighted and the label is drawn over the character that
--      follows it. The buffer is never touched: labels are decorations.
--   3. A label key jumps to its match (focusing the window it is in). Backspace shortens the
--      pattern, Enter jumps to the nearest match of the focused window, Escape, a timeout or
--      any other key (arrows, control keys) ends the session where the cursor was.
--
-- Labels follow flash.nvim: nearest matches get the most comfortable keys, a label that
-- survives a longer pattern keeps its letter, and a letter that is also the next character
-- after a match is not used as a label (so a key press is never both "jump" and "extend the
-- pattern").
local gvim = require "gvim"

local DEFAULT_LABELS = "asdfghjklqwertyuiopzxcvbnm"
local MAX_MATCHES = 2000 -- matches highlighted per redraw; labels are limited by the pool anyway
local KEY_WAIT_MS = 60000
-- Distance proxy: a line counts as this many columns, so the order is "same line first,
-- then by column" without the byte offsets the host does not hand out cheaply.
local LINE_WEIGHT = 4096

local settings = gvim.settings.define {
  labels = {
    type = "string",
    default = DEFAULT_LABELS,
    description = "Characters used as jump labels, in comfort order. The nearest matches get the first ones.",
  },
  skip_rule = {
    type = "boolean",
    default = true,
    description = "Do not use a label that is also the character after a match (it would be ambiguous with typing on).",
  },
  case_sensitive = {
    type = "boolean",
    default = true,
    description = "Match the typed pattern with its case.",
  },
}

local ns_match = gvim.decor.namespace "match"
local ns_label = gvim.decor.namespace "label"

local MATCH_STYLE = { bg = "search.match_bg", fg = "search.match_fg", bold = true }
local LABEL_STYLE = { bg = "search.label_bg", fg = "search.label_fg", bold = true }

-- The session in progress: nil, or { waiting = os.clock() while it is parked on a key }.
-- A session killed by the slice budget (a timeout, a slow host call) never reaches the code
-- after its pcall, so `running` cannot be a bare flag: tasks of a plugin run one at a time,
-- so a session that is not parked on a key while another handler runs is dead.
local running = nil
local watchdog = nil -- timer that cleans up after a dead session
local WATCH_MS = 1000
local WAIT_GRACE_S = 5 -- a parked session whose key wait should have ended this long ago is dead
local touched = {} -- buffer id -> Buffer: buffers carrying our decorations

local function is_word_byte(s, i)
  return s:find("^[%w_]", i) ~= nil
end

-- char_at returns the UTF-8 character that starts at byte i of s, or nil past the end.
local function char_at(s, i)
  local b = s:byte(i)
  if not b then
    return nil
  end
  local n = b < 0xC0 and 1 or b < 0xE0 and 2 or b < 0xF0 and 3 or 4
  return s:sub(i, i + n - 1)
end

-- pool_chars splits the label setting into distinct characters.
local function pool_chars()
  local raw = settings.labels
  if type(raw) ~= "string" or raw == "" then
    raw = DEFAULT_LABELS
  end
  local out, seen, i = {}, {}, 1
  while i <= #raw do
    local ch = char_at(raw, i)
    i = i + #ch
    if not seen[ch] and not ch:find("^%s") then
      seen[ch] = true
      out[#out + 1] = ch
    end
  end
  return out
end

-- ---------- reading what is on screen ----------

-- read_views returns one entry per window: its buffer, its painted rows and the text of the
-- lines they show (texts[line - base + 1]: gopher-lua pads a table with nils up to an integer
-- key, so a table keyed by line 900000 costs tens of MiB). The focused window comes first.
local function read_views()
  local out = {}
  local current = gvim.win.current()
  for _, w in ipairs(gvim.win.list()) do
    local buf = w:buffer()
    local rows = w:visible()
    if buf and rows and #rows > 0 then
      -- Lines the rows show, fetched in runs of consecutive lines (a fold leaves gaps).
      local base = math.huge
      for _, r in ipairs(rows) do
        base = math.min(base, r.line)
      end
      local texts, first, count = {}, nil, 0
      local function flush()
        if first then
          local got = buf:lines(first, count)
          for k, s in ipairs(got or {}) do
            texts[first + k - base] = s
          end
        end
        first, count = nil, 0
      end
      for _, r in ipairs(rows) do
        if first and r.line == first + count - 1 then
          -- a soft-wrapped line again
        elseif first and r.line == first + count then
          count = count + 1
        else
          flush()
          first, count = r.line, 1
        end
      end
      flush()
      out[#out + 1] = { win = w, buf = buf, rows = rows, texts = texts, base = base, focused = (w == current) }
    end
  end
  table.sort(out, function(a, b)
    return a.focused and not b.focused
  end)
  return out
end

-- ---------- matching ----------

local function add_match(list, seen, v, row, text, col, ecol, skip_at)
  local key = v.buf.id .. ":" .. row.line .. ":" .. col
  if seen[key] then
    return
  end
  seen[key] = true
  list[#list + 1] = {
    key = key,
    view = v,
    line = row.line,
    col = col,
    ecol = ecol,
    after = char_at(text, ecol + 1), -- the character a label is drawn over
    skip = char_at(text, skip_at + 1), -- the character that must not be a label
  }
end

-- find_matches lists the matches of pattern ("" labels every word start) in all views.
local function find_matches(vs, pattern)
  local list, seen = {}, {}
  local fold = not settings.case_sensitive
  local needle = pattern
  if fold then
    needle = pattern:lower()
  end
  for _, v in ipairs(vs) do
    local lowered = {}
    for _, row in ipairs(v.rows) do
      local text = v.texts[row.line - v.base + 1]
      if text and #list < MAX_MATCHES then
        local from, to = row.col_from + 1, row.col_to -- 1-based inclusive
        if pattern == "" then
          local init = from
          while true do
            local s, e = text:find("[%w_]+", init)
            if not s or s > to then
              break
            end
            if not (s == from and s > 1 and is_word_byte(text, s - 1)) then
              add_match(list, seen, v, row, text, s - 1, s, s - 1)
            end
            init = e + 1
          end
        else
          local hay, pat = text, pattern
          if fold then
            local l = lowered[row.line - v.base + 1]
            if l == nil then
              l = text:lower()
              if #l ~= #text then
                l = false -- lower-casing changed the byte length: columns would shift
              end
              lowered[row.line - v.base + 1] = l
            end
            if l then
              hay, pat = l, needle
            end
          end
          local init = from
          while #list < MAX_MATCHES do
            local s, e = hay:find(pat, init, true)
            if not s or e > to then
              break
            end
            add_match(list, seen, v, row, text, s - 1, e, e)
            init = s + #char_at(text, s)
          end
        end
      end
    end
  end
  return list
end

-- ---------- labels ----------

-- assign_labels gives matches a label: first the letter a match already had, then the
-- comfortable letters to the nearest matches. The focused window's matches come first, by
-- distance from the cursor; the other windows follow in window order.
local function assign_labels(matches, cursor, prev)
  local pool = pool_chars()
  local skip = {}
  if settings.skip_rule then
    for _, m in ipairs(matches) do
      if m.skip then
        skip[m.skip] = true
        skip[m.skip:lower()] = true
      end
    end
  end
  local free = {}
  for _, ch in ipairs(pool) do
    if not skip[ch] then
      free[ch] = true
    end
  end

  local order = {}
  for i, m in ipairs(matches) do
    order[i] = m
    m.rank = m.view.focused and 0 or 1
    m.dist = math.abs(m.line - cursor.line) * LINE_WEIGHT + math.abs(m.col - cursor.col)
  end
  table.sort(order, function(a, b)
    if a.rank ~= b.rank then
      return a.rank < b.rank
    end
    if a.rank == 0 and a.dist ~= b.dist then
      return a.dist < b.dist
    end
    if a.view ~= b.view then
      return a.view.win.id < b.view.win.id
    end
    if a.line ~= b.line then
      return a.line < b.line
    end
    return a.col < b.col
  end)

  for _, m in ipairs(order) do -- stability
    local old = prev[m.key]
    if old and free[old] then
      m.label = old
      free[old] = nil
    end
  end
  local k = 1
  for _, m in ipairs(order) do -- proximity
    if not m.label then
      while pool[k] and not free[pool[k]] do
        k = k + 1
      end
      if not pool[k] then
        break
      end
      m.label = pool[k]
      free[pool[k]] = nil
    end
  end
  return order
end

-- ---------- drawing ----------

local function clear_all()
  for id, buf in pairs(touched) do
    if buf:valid() then
      ns_match:clear(buf)
      ns_label:clear(buf)
    end
    touched[id] = nil
  end
end

local function pos(line, col)
  return { line = line, col = col }
end

-- draw replaces the decorations of the last frame: a highlight for each match and the label
-- over the character after it (or, at the end of a line, after the last character).
local function draw(matches)
  local per = {}
  for _, m in ipairs(matches) do
    local buf = m.view.buf
    local d = per[buf.id]
    if not d then
      d = { buf = buf, match = {}, label = {} }
      per[buf.id] = d
    end
    d.match[#d.match + 1] = {
      kind = "highlight",
      range = { from = pos(m.line, m.col), to = pos(m.line, m.ecol) },
      style = MATCH_STYLE,
    }
    if m.label then
      if m.after then
        d.label[#d.label + 1] = {
          kind = "replace", pos = pos(m.line, m.ecol), text = m.label, style = LABEL_STYLE, priority = 10,
        }
      else
        d.label[#d.label + 1] = { kind = "eol", line = m.line, text = m.label, style = LABEL_STYLE, priority = 10 }
      end
    end
  end
  for id, buf in pairs(touched) do
    if not per[id] then
      ns_match:clear(buf)
      ns_label:clear(buf)
      touched[id] = nil
    end
  end
  for id, d in pairs(per) do
    touched[id] = d.buf
    ns_match:set(d.buf, d.match)
    ns_label:set(d.buf, d.label)
  end
end

-- ---------- the session ----------

local function jump(m, from)
  local view = m.view
  if view.win ~= from then
    view.win:focus()
  end
  local ok, err = view.buf:set_cursor(pos(m.line, m.col))
  if not ok then
    gvim.log.warn("flash: cannot move the cursor: %s", tostring(err))
  end
end

local function session(me)
  local start_win = gvim.win.current()
  local start_buf = gvim.buf.current()
  if not start_win or not start_buf then
    return
  end
  local cursor = start_buf:cursor() or pos(0, 0)
  local pattern = ""
  local prev = {}
  while true do
    local vs = read_views()
    local ordered = assign_labels(find_matches(vs, pattern), cursor, prev)
    prev = {}
    local by_label = {}
    for _, m in ipairs(ordered) do
      if m.label then
        prev[m.key] = m.label
        by_label[m.label] = m
      end
    end
    draw(ordered)
    gvim.status("Flash[" .. pattern .. "]")

    me.waiting = os.clock()
    local k, err = gvim.input.next_key { timeout_ms = KEY_WAIT_MS }
    me.waiting = nil
    if not k then
      if gvim.err_code(err) ~= "timeout" and gvim.err_code(err) ~= "cancelled" then
        gvim.log.warn("flash: %s", tostring(err))
      end
      return
    end
    local key = k.key
    if key == "escape" then
      return
    elseif key == "enter" then
      if ordered[1] then
        clear_all()
        jump(ordered[1], start_win)
      end
      return
    elseif key == "backspace" then
      if #pattern > 0 then
        local cut = #pattern
        while cut > 1 and pattern:byte(cut) >= 0x80 and pattern:byte(cut) < 0xC0 do
          cut = cut - 1
        end
        pattern = pattern:sub(1, cut - 1)
      end
    elseif k.text and k.text ~= "" and not k.ctrl and not k.alt and not k.super then
      local hit = by_label[k.text]
      if hit then
        clear_all()
        jump(hit, start_win)
        return
      end
      pattern = pattern .. k.text
    else
      return -- an arrow, a function key, a chord: end the session in place
    end
  end
end

-- alive reports whether the running session is still waiting for keys.
local function alive()
  local w = running and running.waiting
  return w ~= nil and os.clock() - w < KEY_WAIT_MS / 1000 + WAIT_GRACE_S
end

local function stop_watchdog()
  if watchdog then
    watchdog:cancel()
    watchdog = nil
  end
end

-- reap removes what a dead session left on screen.
local function reap()
  stop_watchdog()
  running = nil
  clear_all()
  gvim.status("")
end

local function watch(me)
  watchdog = gvim.timer.after(WATCH_MS, function()
    watchdog = nil
    if running ~= me then
      return
    end
    if alive() then
      watch(me)
    else
      reap()
    end
  end)
end

local function flash_jump()
  if running then
    if alive() then
      return
    end
    reap() -- the previous session was killed: take over
  end
  local me = {}
  running = me
  clear_all()
  watch(me)
  local ok, err = pcall(session, me)
  if running == me then
    stop_watchdog()
    running = nil
    clear_all()
    gvim.status("")
  end
  if not ok then
    gvim.log.error("flash: %s", tostring(err))
  end
end

gvim.plugin.on_unload(function()
  clear_all()
end)

gvim.command.register {
  name = "jump",
  title = "Flash: Jump",
  description = "Jump to any visible match across every window",
  run = flash_jump,
}
