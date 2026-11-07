-- Text objects: iw aw iW aW ip ap i( a( i[ a[ i{ a{ i< a< i" a" i' a' i` a` it at.
--
-- An object is a function of c = { p = caret, count = n } and include (true for "a"). It
-- returns { from = pos, to = pos (exclusive), kind = "char"|"line" }, or nil when there
-- is no such object at the caret. The word and paragraph objects are ports of vi's.
local D = require "vi.doc"
local T = require "vi.text"
local Mo = require "vi.motion"

local M = {}
M.table = {}

local function P(l, c) return { line = l, col = c } end

local function cls_at(p, big)
  return T.cls(D.char_at(p), big)
end

-- back_in_line goes back to the start of the run of one class the caret is in.
local function back_in_line(p, big)
  local sclass = cls_at(p, big)
  while p.col > 0 do
    local q = P(p.line, T.prev(D.line(p.line), p.col))
    if cls_at(q, big) ~= sclass then
      break
    end
    p = q
  end
  return p
end

-- oneleft moves one character left within the line; it fails at the first column.
local function oneleft(p)
  if p.col == 0 then
    return p, false
  end
  return P(p.line, T.prev(D.line(p.line), p.col)), true
end

local function after_char(p)
  local s = D.line(p.line)
  if p.col < #s then
    return P(p.line, T.next(s, p.col))
  end
  return p
end

-- ---------- words ----------

local function word(c, include, big)
  local p = back_in_line(c.p, big)
  local start = p
  local inclusive = true
  local include_white = false
  local count = c.count
  if (cls_at(p, big) == 0) == include then
    local q, ok = Mo.end_word(p, 1, big, true, true)
    if not ok then
      return nil
    end
    p = q
  else
    p = Mo.fwd_word(p, 1, big, true)
    if p.col == 0 then
      p = D.decl(p)
    else
      p = oneleft(p)
    end
    if include then
      include_white = true
    end
  end
  count = count - 1
  while count > 0 do
    inclusive = true
    local q, r = D.incl(p)
    if r == -1 then
      return nil
    end
    p = q
    if include ~= (cls_at(p, big) == 0) then
      local q2, ok = Mo.fwd_word(p, 1, big, true)
      if not ok and count > 1 then
        return nil
      end
      p = q2
      local q3, moved = oneleft(p)
      if not moved then
        inclusive = false
      else
        p = q3
      end
    else
      local q2, ok = Mo.end_word(p, 1, big, true, true)
      if not ok then
        return nil
      end
      p = q2
    end
    count = count - 1
  end
  if include_white and (cls_at(p, big) ~= 0 or (p.col == 0 and not inclusive)) then
    -- no white space after the word: take the white space before it, but not the indent
    local q, moved = oneleft(start)
    if moved then
      q = back_in_line(q, big)
      if cls_at(q, big) == 0 and q.col > 0 then
        start = q
      end
    end
  end
  local to = inclusive and after_char(p) or p
  return { from = start, to = to, kind = "char" }
end

M.table["w"] = function(c, include) return word(c, include, false) end
M.table["W"] = function(c, include) return word(c, include, true) end

-- ---------- paragraphs ----------

-- A line is "white" when it holds nothing but blanks; paragraphs are separated by them.
local function white(n) -- n is 1-based
  local s = D.line(n - 1)
  return s ~= nil and T.is_blank(s)
end

local function paragraph(c, include)
  local nl = D.count()
  local start_lnum = c.p.line + 1
  local white_in_front = white(start_lnum)
  while start_lnum > 1 do
    if white_in_front then
      if not white(start_lnum - 1) then
        break
      end
    elseif white(start_lnum - 1) then
      break
    end
    start_lnum = start_lnum - 1
  end
  local end_lnum = start_lnum
  while end_lnum <= nl and white(end_lnum) do
    end_lnum = end_lnum + 1
  end
  end_lnum = end_lnum - 1
  local i = c.count
  if not include and white_in_front then
    i = i - 1
  end
  local do_white = false
  while i > 0 do
    i = i - 1
    if end_lnum == nl then
      return nil
    end
    if not include then
      do_white = white(end_lnum + 1)
    end
    if include or not do_white then
      end_lnum = end_lnum + 1
      while end_lnum < nl and not white(end_lnum + 1) do
        end_lnum = end_lnum + 1
      end
    end
    if i == 0 and white_in_front and include then
      break
    end
    if include or do_white then
      while end_lnum < nl and white(end_lnum + 1) do
        end_lnum = end_lnum + 1
      end
    end
  end
  if not white_in_front and not white(end_lnum) and include then
    while start_lnum > 1 and white(start_lnum - 1) do
      start_lnum = start_lnum - 1
    end
  end
  return { from = P(start_lnum - 1, 0), to = P(end_lnum - 1, 0), kind = "line" }
end

M.table["p"] = paragraph

-- ---------- brackets ----------

local MAX_SCAN = 200000

-- find_open searches backward from before p for the bracket open that is not closed on
-- the way.
local function find_open(p, open, close)
  local depth, steps = 0, 0
  local q = p
  while true do
    local r
    q, r = D.dec(q)
    if r == -1 then
      return nil
    end
    steps = steps + 1
    if steps > MAX_SCAN then
      return nil
    end
    local ch = D.char_at(q)
    if ch == close then
      depth = depth + 1
    elseif ch == open then
      if depth == 0 then
        return q
      end
      depth = depth - 1
    end
  end
end

-- find_close searches forward from after p for the bracket close that closes p's block.
local function find_close(p, open, close)
  local depth, steps = 0, 0
  local q = p
  while true do
    local r
    q, r = D.inc(q)
    if r == -1 then
      return nil
    end
    steps = steps + 1
    if steps > MAX_SCAN then
      return nil
    end
    local ch = D.char_at(q)
    if ch == open then
      depth = depth + 1
    elseif ch == close then
      if depth == 0 then
        return q
      end
      depth = depth - 1
    end
  end
end

-- in_indent reports whether p is on a blank of the indent: the character at p and
-- everything before it is blank (vi's inindent(1)).
local function in_indent(p)
  local s = D.line(p.line)
  return T.is_blank(s:sub(1, p.col + 1))
end

local function block(c, include, open, close)
  local p = c.p
  if open == "{" then
    -- ignore the indent
    local s = D.line(p.line)
    if T.is_blank(s:sub(1, p.col)) and p.col < #s then
      local nb = s:find("[^ \t]")
      if nb then
        p = P(p.line, nb - 1)
      end
    end
  end
  if D.char_at(p) == open then
    p = P(p.line, p.col + 1) -- just after the bracket, so the search below finds it
  end
  local start_pos
  local cur = p
  for _ = 1, c.count do
    local q = find_open(cur, open, close)
    if not q then
      return nil
    end
    start_pos = q
    cur = q
  end
  local end_pos = find_close(start_pos, open, close)
  if not end_pos then
    return nil
  end
  if include then
    return { from = start_pos, to = after_char(end_pos), kind = "char", keep_end = true }
  end
  -- inner: without the brackets. When the opening bracket ends its line, start on the
  -- next line; when the closing one starts its line (after blanks), stop before the
  -- line break that precedes it.
  local from = D.incl(start_pos)
  local sol = end_pos.col == 0
  local cur_end = D.decl(end_pos)
  while in_indent(cur_end) do
    sol = true
    local q, r = D.decl(cur_end)
    cur_end = q
    if r ~= 0 then
      break
    end
  end
  local to
  if sol then
    to = D.incl(cur_end)
  elseif D.le(from, cur_end) then
    to = after_char(cur_end)
  else
    to = from -- nothing between the brackets
  end
  return { from = from, to = to, kind = "char" }
end

local function bracket(open, close)
  return function(c, include) return block(c, include, open, close) end
end

for _, k in ipairs { "(", ")", "b" } do
  M.table[k] = bracket("(", ")")
end
for _, k in ipairs { "{", "}", "B" } do
  M.table[k] = bracket("{", "}")
end
for _, k in ipairs { "[", "]" } do
  M.table[k] = bracket("[", "]")
end
for _, k in ipairs { "<", ">" } do
  M.table[k] = bracket("<", ">")
end

-- ---------- quotes ----------

-- quote_positions lists the columns of the quotes q in s that are not escaped.
local function quote_positions(s, q)
  local out = {}
  local col = 0
  while col < #s do
    local ch = T.at(s, col)
    if ch == "\\" then
      col = T.next(s, T.next(s, col))
    else
      if ch == q then
        out[#out + 1] = col
      end
      col = T.next(s, col)
    end
  end
  return out
end

local function quote(c, include, q)
  local s = D.line(c.p.line)
  local qs = quote_positions(s, q)
  local cur = c.p.col
  local a, b
  for i = 1, #qs - 1, 2 do
    local o, cl = qs[i], qs[i + 1]
    if cur >= o and cur <= cl then
      a, b = o, cl
      break
    end
    if cur < o then
      a, b = o, cl
      break
    end
  end
  if not a then
    return nil
  end
  local line = c.p.line
  if not include then
    if c.count >= 2 then
      return { from = P(line, a), to = P(line, T.next(s, b)), kind = "char" }
    end
    return { from = P(line, T.next(s, a)), to = P(line, b), kind = "char" }
  end
  local from, to = a, T.next(s, b)
  if s:sub(to + 1, to + 1):find("[ \t]") then
    while s:sub(to + 1, to + 1):find("[ \t]") do
      to = to + 1
    end
  else
    while from > 0 and s:sub(from, from):find("[ \t]") do
      from = from - 1
    end
  end
  return { from = P(line, from), to = P(line, to), kind = "char" }
end

for _, q in ipairs { '"', "'", "`" } do
  M.table[q] = function(c, include) return quote(c, include, q) end
end

-- ---------- tags ----------

-- tag_pairs lists the <name>...</name> pairs of lines first..last with the positions of the
-- four ends: open_from, open_to (just after ">"), close_from, close_to.
local function tag_pairs(first, last)
  local text = table.concat(D.lines(first, last), "\n")
  local pairs_, stack = {}, {}
  local pos = 1
  local line_starts = { 1 }
  for i in text:gmatch("\n()") do
    line_starts[#line_starts + 1] = i
  end
  local function to_pos(index)
    local l = #line_starts
    while line_starts[l] > index do
      l = l - 1
    end
    return P(first + l - 1, index - line_starts[l])
  end
  while true do
    local s, e, closing, name, rest = text:find("<(/?)([%w_:%.%-]+)([^<>]*)>", pos)
    if not s then
      break
    end
    pos = e + 1
    if closing == "/" then
      for i = #stack, 1, -1 do
        if stack[i].name == name then
          local o = stack[i]
          for j = #stack, i, -1 do
            stack[j] = nil
          end
          pairs_[#pairs_ + 1] = { from = to_pos(o.s), inner_from = to_pos(o.e + 1), inner_to = to_pos(s), to = to_pos(e + 1) }
          break
        end
      end
    elseif rest:sub(-1) ~= "/" then
      stack[#stack + 1] = { name = name, s = s, e = e }
    end
  end
  return pairs_
end

local function tag(c, include)
  local first = math.max(0, c.p.line - 300)
  local last = math.min(D.count() - 1, c.p.line + 300)
  local best
  local depth = c.count
  local list = tag_pairs(first, last)
  table.sort(list, function(a, b)
    -- innermost first: the later start wins
    return D.lt(b.from, a.from)
  end)
  for _, t in ipairs(list) do
    if D.le(t.from, c.p) and D.lt(c.p, t.to) then
      depth = depth - 1
      if depth == 0 then
        best = t
        break
      end
    end
  end
  if not best then
    return nil
  end
  if include then
    return { from = best.from, to = best.to, kind = "char", keep_end = true }
  end
  return { from = best.inner_from, to = best.inner_to, kind = "char", keep_end = true }
end

M.table["t"] = tag

return M
