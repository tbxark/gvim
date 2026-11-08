-- Motions. Each one is a function of a context
--
--   c = { p = start position, count = n (at least 1), has_count = bool,
--         op = "d"|"c"|"y"|... when an operator waits for the motion, else nil,
--         arg = the character f, t, ', ` ... asked for, visual = bool }
--
-- that returns a result { pos = target, kind = "exclusive"|"inclusive"|"line" }, or nil
-- when the motion fails (the command is abandoned, as vi beeps). A result may carry
-- jump = true (the old position is remembered for '' and ``), fail = true (a word motion
-- that ran into the end of the buffer after moving: an operator still uses the target),
-- to_eol = true ($ in visual mode selects the line break too), and act = the name of the
-- editor action that does the same move for the caret in normal mode.
--
-- The word motions are ports of vi's own (fwd_word, bck_word, end_word, bckend_word), so
-- they stop where vi stops: on empty lines, at the end of the buffer, and so on.
local gvim = require "gvim"
local D = require "vi.doc"
local T = require "vi.text"
local S = require "vi.state"

local M = {}
M.table = {}

local function P(l, c) return { line = l, col = c } end

-- cls_at is the character class of the character at p ("" at the end of a line is blank).
local function cls_at(p, big)
  return T.cls(D.char_at(p), big)
end

local function empty_line(p)
  return p.col == 0 and D.len(p.line) == 0
end

-- skip_chars moves while the class at p is cclass. It reports whether it ran into the
-- start or the end of the buffer.
local function skip_chars(p, cclass, forward, big)
  while cls_at(p, big) == cclass do
    local q, r
    if forward then
      q, r = D.inc(p)
    else
      q, r = D.dec(p)
    end
    if r == -1 then
      return p, true
    end
    p = q
  end
  return p, false
end

-- fwd_word: count words forward. eol: stop at the end of the line when the last word has
-- been passed (an operator is waiting). The second result is false when it failed.
function M.fwd_word(p, count, big, eol)
  while count > 0 do
    count = count - 1
    local sclass = cls_at(p, big)
    local last_line = p.line == D.count() - 1
    local q, i = D.inc(p)
    p = q
    if i == -1 or (i >= 1 and last_line) then
      return p, false
    end
    if i >= 1 and eol and count == 0 then
      return p, true
    end
    if sclass ~= 0 then
      while cls_at(p, big) == sclass do
        q, i = D.inc(p)
        p = q
        if i == -1 or (i >= 1 and eol and count == 0) then
          return p, true
        end
      end
    end
    while cls_at(p, big) == 0 do
      if empty_line(p) then
        break
      end
      q, i = D.inc(p)
      p = q
      if i == -1 or (i >= 1 and last_line) then
        return p, true
      end
    end
  end
  return p, true
end

-- bck_word: count words backward. stop: do not skip over blanks when starting at the start
-- of a word (used by cw-like callers).
function M.bck_word(p, count, big, stop)
  while count > 0 do
    count = count - 1
    local sclass = cls_at(p, big)
    local q, i = D.dec(p)
    if i == -1 then
      return p, false
    end
    p = q
    local finished = false
    if not stop or sclass == cls_at(p, big) or sclass == 0 then
      while cls_at(p, big) == 0 do
        if empty_line(p) then
          finished = true
          break
        end
        q, i = D.dec(p)
        if i == -1 then
          return p, true
        end
        p = q
      end
      if not finished then
        local hit
        p, hit = skip_chars(p, cls_at(p, big), false, big)
        if hit then
          return p, true
        end
      end
    end
    if not finished then
      p = D.inc(p)
    end
    stop = false
  end
  return p, true
end

-- end_word: count word ends forward. stop: stay on the word end when already there (cw).
-- empty: stop on empty lines.
function M.end_word(p, count, big, stop, empty)
  while count > 0 do
    count = count - 1
    local sclass = cls_at(p, big)
    local q, i = D.inc(p)
    if i == -1 then
      return p, false
    end
    p = q
    local finished = false
    if cls_at(p, big) == sclass and sclass ~= 0 then
      local hit
      p, hit = skip_chars(p, sclass, true, big)
      if hit then
        return p, false
      end
    elseif not stop or sclass == 0 then
      while cls_at(p, big) == 0 do
        if empty and empty_line(p) then
          finished = true
          break
        end
        q, i = D.inc(p)
        if i == -1 then
          return p, false
        end
        p = q
      end
      if not finished then
        local hit
        p, hit = skip_chars(p, cls_at(p, big), true, big)
        if hit then
          return p, false
        end
      end
    end
    if not finished then
      p = D.dec(p)
    end
    stop = false
  end
  return p, true
end

-- bckend_word: count word ends backward (ge). eol: stop at the end of a line.
function M.bckend_word(p, count, big, eol)
  while count > 0 do
    count = count - 1
    local sclass = cls_at(p, big)
    local q, i = D.dec(p)
    if i == -1 then
      return p, false
    end
    p = q
    if eol and i == 1 then
      return p, true
    end
    if sclass ~= 0 then
      while cls_at(p, big) == sclass do
        q, i = D.dec(p)
        if i == -1 or (eol and i == 1) then
          return q, true
        end
        p = q
      end
    end
    while cls_at(p, big) == 0 do
      if empty_line(p) then
        break
      end
      q, i = D.dec(p)
      if i == -1 or (eol and i == 1) then
        return q, true
      end
      p = q
    end
  end
  return p, true
end

local function result(pos, kind, extra)
  local r = extra or {}
  r.pos, r.kind = pos, kind
  return r
end

-- ---------- left, right, up, down ----------

M.table["h"] = function(c)
  if c.p.col == 0 then
    return nil
  end
  local s = D.line(c.p.line)
  return result(P(c.p.line, T.retreat(s, c.p.col, c.count)), "exclusive", { act = "move_left_in_line" })
end
M.table["left"] = M.table["h"]

M.table["l"] = function(c)
  local s = D.line(c.p.line)
  if #s == 0 then
    return nil
  end
  local last = T.last(s)
  if not c.op then
    if c.p.col >= last then
      return nil
    end
    local col = math.min(T.advance(s, c.p.col, c.count), last)
    return result(P(c.p.line, col), "exclusive", { act = "move_right_in_line" })
  end
  if c.p.col >= #s then
    return nil
  end
  return result(P(c.p.line, T.advance(s, c.p.col, c.count)), "exclusive")
end
M.table["right"] = M.table["l"]

-- space and backspace wrap over the line break.
M.table[" "] = function(c)
  local p = c.p
  for _ = 1, c.count do
    local q, r = D.inc(p)
    if r == -1 then
      if p.line == c.p.line and p.col == c.p.col then
        return nil
      end
      break
    end
    if r == 2 and not c.op then
      q = D.inc(q)
    end
    p = q
  end
  return result(p, "exclusive")
end

M.table["backspace"] = function(c)
  local p = c.p
  for _ = 1, c.count do
    local q, r = D.dec(p)
    if r == -1 then
      if D.eq(p, c.p) then
        return nil
      end
      break
    end
    if r == 1 and q.col > 0 and not c.op then
      q = D.dec(q)
    end
    p = q
  end
  return result(p, "exclusive")
end

local function vertical(c, delta, act)
  local last = D.count() - 1
  local l = c.p.line + delta * c.count
  if (delta > 0 and c.p.line >= last) or (delta < 0 and c.p.line <= 0) then
    -- j and k as plain moves are the editor's vi_move_* actions, which go by display rows: on
    -- a wrapped first or last line one can still move, so leave the decision to the action.
    -- An operator or the visual mode works on logical lines and fails at the ends.
    if act:find("^vi_move_") and not c.op and not c.visual then
      return result(c.p, "line", { act = act })
    end
    return nil
  end
  l = math.max(0, math.min(l, last))
  local s = D.line(l)
  return result(P(l, T.align(s, math.min(c.p.col, T.last(s)))), "line", { act = act })
end

M.table["j"] = function(c) return vertical(c, 1, "vi_move_down") end
M.table["down"] = M.table["j"]
M.table["k"] = function(c) return vertical(c, -1, "vi_move_up") end
M.table["up"] = M.table["k"]
-- gj and gk go by display rows, and so do the editor's vi_move_* actions that j and k run: they
-- are the same moves, including the first and last line of a wrapped buffer, where the action
-- can still move a row and stops at the end of the text.
M.table["gj"] = M.table["j"]
M.table["gk"] = M.table["k"]

-- first_nonblank_line goes delta lines (the count minus one for _) and lands on the first
-- non-blank character.
local function line_motion(c, delta, fail_at_end)
  local last = D.count() - 1
  local l = c.p.line + delta
  if fail_at_end and (l > last or l < 0) then
    return nil
  end
  l = math.max(0, math.min(l, last))
  return result(D.first_nonblank(l), "line")
end

M.table["+"] = function(c) return line_motion(c, c.count, true) end
M.table["enter"] = M.table["+"]
M.table["-"] = function(c) return line_motion(c, -c.count, true) end
M.table["_"] = function(c) return line_motion(c, c.count - 1, false) end

-- ---------- within the line ----------

M.table["0"] = function(c) return result(P(c.p.line, 0), "exclusive") end
M.table["home"] = M.table["0"]

M.table["^"] = function(c)
  return result(D.first_nonblank(c.p.line), "exclusive")
end

M.table["$"] = function(c)
  local last = D.count() - 1
  local l = c.p.line + c.count - 1
  if l > last then
    return nil
  end
  local s = D.line(l)
  return result(P(l, T.last(s)), "inclusive", { to_eol = true })
end
M.table["end"] = M.table["$"]

M.table["g_"] = function(c)
  local last = D.count() - 1
  local l = c.p.line + c.count - 1
  if l > last then
    return nil
  end
  local s = D.line(l)
  local col = #s
  while col > 0 do
    local pv = T.prev(s, col)
    local ch = s:sub(pv + 1, col)
    if ch ~= " " and ch ~= "\t" then
      return result(P(l, pv), "inclusive")
    end
    col = pv
  end
  return result(P(l, 0), "inclusive")
end

M.table["|"] = function(c)
  local s = D.line(c.p.line)
  local col = math.min(T.advance(s, 0, c.count - 1), T.last(s))
  return result(P(c.p.line, col), "exclusive")
end

-- ---------- lines of the buffer and the screen ----------

M.table["gg"] = function(c)
  local l = c.has_count and math.min(c.count - 1, D.count() - 1) or 0
  return result(D.first_nonblank(l), "line", { jump = true })
end

M.table["G"] = function(c)
  local l = c.has_count and math.min(c.count - 1, D.count() - 1) or D.count() - 1
  return result(D.first_nonblank(l), "line", { jump = true })
end

local function view()
  local w = gvim.win.current()
  if not w then
    return nil
  end
  local v = w:view()
  if type(v) ~= "table" then
    return nil
  end
  return v
end

local function screen_line(c, which)
  local v = view()
  local last = D.count() - 1
  local top, h = 0, last + 1
  if v then
    top, h = v.top_line, v.height
  end
  h = math.max(1, math.min(h, last + 1 - top))
  local l
  if which == "H" then
    l = top + math.min(c.count - 1, h - 1)
  elseif which == "L" then
    l = top + h - 1 - math.min(c.count - 1, h - 1)
  else
    l = top + math.floor((h - 1) / 2)
  end
  l = math.max(0, math.min(l, last))
  return result(D.first_nonblank(l), "line", { jump = true })
end

M.table["H"] = function(c) return screen_line(c, "H") end
M.table["M"] = function(c) return screen_line(c, "M") end
M.table["L"] = function(c) return screen_line(c, "L") end

-- ---------- words ----------

local function wordcmd(c, big, word_end)
  local p = c.p
  local inclusive = word_end
  local flag = false
  if not word_end and c.op == "c" then
    local ch = D.char_at(p)
    if ch ~= "" and ch ~= " " and ch ~= "\t" then
      -- cw is ce, unless on blanks
      inclusive, word_end, flag = true, true, true
    end
  end
  local q, ok
  if word_end then
    q, ok = M.end_word(p, c.count, big, flag, false)
  else
    q, ok = M.fwd_word(p, c.count, big, c.op ~= nil)
  end
  if D.lt(p, q) then
    -- do not leave the caret on the end of the line when it moved
    local s = D.line(q.line)
    if q.col > 0 and q.col >= #s then
      q = P(q.line, T.last(s))
      inclusive = true
    end
  end
  return result(q, inclusive and "inclusive" or "exclusive", { fail = not ok })
end

M.table["w"] = function(c) return wordcmd(c, false, false) end
M.table["W"] = function(c) return wordcmd(c, true, false) end
M.table["e"] = function(c) return wordcmd(c, false, true) end
M.table["E"] = function(c) return wordcmd(c, true, true) end

M.table["b"] = function(c)
  local q, ok = M.bck_word(c.p, c.count, false, false)
  if not ok and D.eq(q, c.p) then
    return nil
  end
  return result(q, "exclusive")
end
M.table["B"] = function(c)
  local q, ok = M.bck_word(c.p, c.count, true, false)
  if not ok and D.eq(q, c.p) then
    return nil
  end
  return result(q, "exclusive")
end
M.table["ge"] = function(c)
  local q, ok = M.bckend_word(c.p, c.count, false, false)
  if not ok and D.eq(q, c.p) then
    return nil
  end
  return result(q, "inclusive")
end
M.table["gE"] = function(c)
  local q, ok = M.bckend_word(c.p, c.count, true, false)
  if not ok and D.eq(q, c.p) then
    return nil
  end
  return result(q, "inclusive")
end

-- ---------- paragraphs ----------

local function para(c, forward)
  local last = D.count() - 1
  local cur = c.p.line
  local dir = forward and 1 or -1
  local count = c.count
  while count > 0 do
    count = count - 1
    local did_skip = false
    local first = true
    while true do
      local s = D.line(cur)
      if #s ~= 0 then
        did_skip = true
      end
      if not first and did_skip and (#s == 0 or s:sub(1, 1) == "\f") then
        break
      end
      cur = cur + dir
      if cur < 0 or cur > last then
        if count > 0 then
          return nil
        end
        cur = cur - dir
        break
      end
      first = false
    end
  end
  if forward and cur == last then
    local s = D.line(cur)
    if #s > 0 then
      return result(P(cur, T.last(s)), "inclusive", { jump = true })
    end
  end
  return result(P(cur, 0), "exclusive", { jump = true })
end

M.table["}"] = function(c) return para(c, true) end
M.table["{"] = function(c) return para(c, false) end

-- ---------- matching pairs ----------

local PAIRS = { ["("] = ")", ["["] = "]", ["{"] = "}", [")"] = "(", ["]"] = "[", ["}"] = "{" }
local OPENERS = { ["("] = true, ["["] = true, ["{"] = true }
local MAX_SCAN = 200000

-- match_pair finds the bracket that pairs with the one at p, counting nesting. It scans at
-- most MAX_SCAN characters.
function M.match_pair(p)
  local ch = D.char_at(p)
  local mate = PAIRS[ch]
  if not mate then
    return nil
  end
  local forward = OPENERS[ch]
  local depth, steps = 0, 0
  local q = p
  while true do
    local r
    if forward then
      q, r = D.inc(q)
    else
      q, r = D.dec(q)
    end
    if r == -1 then
      return nil
    end
    steps = steps + 1
    if steps > MAX_SCAN then
      return nil
    end
    local c2 = D.char_at(q)
    if c2 == ch then
      depth = depth + 1
    elseif c2 == mate then
      if depth == 0 then
        return q
      end
      depth = depth - 1
    end
  end
end

M.table["%"] = function(c)
  if c.has_count then
    -- N%: go to the line N percent of the way through the buffer
    if c.count > 100 then
      return nil
    end
    local l = math.floor((c.count * D.count() + 99) / 100) - 1
    l = math.max(0, math.min(l, D.count() - 1))
    return result(D.first_nonblank(l), "line", { jump = true })
  end
  local s = D.line(c.p.line)
  local col = c.p.col
  while col < #s and not PAIRS[T.at(s, col)] do
    col = T.next(s, col)
  end
  if col >= #s then
    return nil
  end
  local m = M.match_pair(P(c.p.line, col))
  if not m then
    return nil
  end
  return result(m, "inclusive", { jump = true })
end

-- ---------- f t F T ; , ----------

local function findchar(c, forward, till, repeating)
  local ch = c.arg
  if not ch or ch == "" then
    return nil
  end
  local s = D.line(c.p.line)
  local col = c.p.col
  local count = c.count
  if forward then
    local from = T.next(s, col)
    if till and repeating and count == 1 and T.at(s, from) == ch then
      from = T.next(s, from)
    end
    local found
    while count > 0 do
      local i = s:find(ch, from + 1, true)
      if not i then
        return nil
      end
      found = i - 1
      from = T.next(s, found)
      count = count - 1
    end
    if till then
      found = T.prev(s, found)
    end
    return result(P(c.p.line, found), "inclusive")
  end
  local pos = col
  if till and repeating and count == 1 and pos > 0 and T.at(s, T.prev(s, pos)) == ch then
    pos = T.prev(s, pos)
  end
  local found
  while count > 0 do
    local hit
    local q = pos
    while q > 0 do
      q = T.prev(s, q)
      if T.at(s, q) == ch then
        hit = q
        break
      end
    end
    if not hit then
      return nil
    end
    found = hit
    pos = hit
    count = count - 1
  end
  if till then
    found = T.next(s, found)
  end
  return result(P(c.p.line, found), "exclusive")
end

M.table["f"] = function(c)
  S.last_find = { ch = c.arg, forward = true, till = false }
  return findchar(c, true, false, false)
end
M.table["t"] = function(c)
  S.last_find = { ch = c.arg, forward = true, till = true }
  return findchar(c, true, true, false)
end
M.table["F"] = function(c)
  S.last_find = { ch = c.arg, forward = false, till = false }
  return findchar(c, false, false, false)
end
M.table["T"] = function(c)
  S.last_find = { ch = c.arg, forward = false, till = true }
  return findchar(c, false, true, false)
end
M.table[";"] = function(c)
  local f = S.last_find
  if not f then
    return nil
  end
  c.arg = f.ch
  return findchar(c, f.forward, f.till, true)
end
M.table[","] = function(c)
  local f = S.last_find
  if not f then
    return nil
  end
  c.arg = f.ch
  return findchar(c, not f.forward, f.till, true)
end

-- ---------- marks ----------

local function mark_pos(name)
  if name == "'" or name == "`" then
    return S.prev_pos
  end
  if name == "<" or name == ">" then
    local v = S.visual_marks
    if not v then
      return nil
    end
    return name == "<" and v.from or v.to
  end
  local m = S.marks[name]
  if m and m.buf == D.buf().id then
    return P(m.line, m.col)
  end
  return nil
end
M.mark_pos = mark_pos

-- Marks Vim sets itself (the last change, the last insert, ...) that vi_mode does not keep.
local UNKEPT_MARKS = { ["."] = true, ["["] = true, ["]"] = true, ["^"] = true, ['"'] = true, ["("] = true, [")"] = true, ["{"] = true, ["}"] = true }

-- no_mark ends a jump to a mark that is not there, saying why.
local function no_mark(name)
  if UNKEPT_MARKS[name] then
    D.fail("not supported: the mark " .. name .. " (a-z, A-Z, ', `, < and > are kept)")
  end
  D.fail("E20: Mark not set")
end

M.table["'"] = function(c)
  local m = mark_pos(c.arg)
  if not m then
    no_mark(c.arg)
  end
  local l = math.max(0, math.min(m.line, D.count() - 1))
  return result(D.first_nonblank(l), "line", { jump = true })
end

M.table["`"] = function(c)
  local m = mark_pos(c.arg)
  if not m then
    no_mark(c.arg)
  end
  return result(D.clamp(m), "exclusive", { jump = true })
end

return M
