-- Operators and the commands that edit: delete, change, yank, shift, case, join, put,
-- replace, increment. They work on ranges:
--
--   { kind = "char", from = pos, to = pos }      half-open, from <= to
--   { kind = "line", first = n, last = n }       whole lines, both ends included
--   { kind = "block", first = n, last = n, left = c, right = c }
--                                                a column of characters (character
--                                                indexes, right exclusive; right = nil
--                                                runs to the end of each line)
--
-- Every edit is a single call of b:edit, so one command is one undo step.
local gvim = require "gvim"
local D = require "vi.doc"
local T = require "vi.text"
local Reg = require "vi.reg"
local S = require "vi.state"

local M = {}

local function P(l, c) return { line = l, col = c } end

-- ---------- ranges ----------

-- before_first_nonblank: only blanks lie before p on its line (vi's inindent).
local function in_indent(p)
  return T.is_blank(D.line(p.line):sub(1, p.col))
end

-- from_motion turns the result r of a motion started at start into a range. An exclusive
-- motion that ends in the first column of a later line stops at the end of the line before
-- it, and becomes linewise when it also starts at or before the first non-blank (vi's
-- o_v rule: "dw" on the last word of a line does not eat the line break).
function M.from_motion(start, r)
  local a, b = start, r.pos
  if r.kind == "line" then
    return { kind = "line", first = math.min(a.line, b.line), last = math.max(a.line, b.line) }
  end
  if D.lt(b, a) then
    -- not "a, b = b, a": gopher-lua assigns multiple locals one after the other
    local tmp = a
    a = b
    b = tmp
  end
  if r.kind == "inclusive" then
    -- on an empty line (or past the end of one) there is no character to include: D.after
    -- would reach over the line break into the next line
    if b.col >= D.len(b.line) then
      return { kind = "char", from = a, to = b }
    end
    return { kind = "char", from = a, to = D.after(b) }
  end
  return M.adjust({ kind = "char", from = a, to = b })
end

-- adjust applies the o_v rule to an exclusive character range.
function M.adjust(rng)
  local a, b = rng.from, rng.to
  if b.col == 0 and b.line > a.line then
    local prev = b.line - 1
    if in_indent(a) then
      return { kind = "line", first = a.line, last = prev }
    end
    return { kind = "char", from = a, to = P(prev, D.len(prev)) }
  end
  return rng
end

-- from_object turns a text object into a range.
function M.from_object(o)
  if o.kind == "line" then
    return { kind = "line", first = o.from.line, last = o.to.line }
  end
  return M.adjust({ kind = "char", from = o.from, to = o.to })
end

-- text returns the text a range covers (linewise text ends in "\n").
function M.text(rng)
  if rng.kind == "line" then
    return table.concat(D.lines(rng.first, rng.last), "\n") .. "\n"
  end
  if rng.kind == "block" then
    local parts = {}
    for l = rng.first, rng.last do
      local s = D.line(l)
      local a = T.advance(s, 0, rng.left)
      local b = rng.right and T.advance(s, 0, rng.right) or #s
      parts[#parts + 1] = s:sub(a + 1, math.max(a, b))
    end
    return table.concat(parts, "\n")
  end
  return D.text(rng.from, rng.to)
end

local function reg_kind(rng)
  if rng.kind == "line" then
    return "line"
  end
  return rng.kind == "block" and "block" or "char"
end

-- ---------- indent ----------

local function tab_size()
  local ts = gvim.config.get("tab_size")
  if type(ts) ~= "number" or ts < 1 then
    ts = 4
  end
  return ts
end

local function shift_width()
  local sw = S.settings.shift_width
  if type(sw) == "number" and sw > 0 then
    return sw
  end
  return tab_size()
end

local function indent_string(width)
  local spaces = gvim.config.get("insert_spaces")
  if spaces == false then
    local ts = tab_size()
    return string.rep("\t", math.floor(width / ts)) .. string.rep(" ", width % ts)
  end
  return string.rep(" ", width)
end

-- indent_width is the width of the leading blanks of s, with tabs at the tab size.
local function indent_width(s)
  local ts, w = tab_size(), 0
  for i = 1, #s do
    local ch = s:sub(i, i)
    if ch == " " then
      w = w + 1
    elseif ch == "\t" then
      w = w + ts - w % ts
    else
      break
    end
  end
  return w
end
M.indent_width = indent_width
M.indent_string = indent_string

-- shift moves the lines first..last by amount shift widths (positive: right). Blank lines
-- are not indented.
function M.shift(first, last, amount)
  local sw = shift_width()
  local edits = {}
  for l = first, last do
    local s = D.line(l)
    local ind = T.indent(s)
    if amount < 0 or not T.is_blank(s) then
      local w = math.max(0, indent_width(ind) + amount * sw)
      local new = indent_string(w)
      if new ~= ind then
        edits[#edits + 1] = { from = P(l, 0), to = P(l, #ind), text = new }
      end
    end
  end
  if #edits > 0 then
    D.edit(edits)
  end
  D.set_cursor(D.first_nonblank(first))
  return last - first + 1
end

-- ---------- delete, change, yank ----------

-- yank copies the range into the register. The caret goes to the start of the range, but
-- stays on its column for a linewise one.
function M.yank(rng, name, start)
  local text = M.text(rng)
  Reg.set(name, text, reg_kind(rng), "y")
  if rng.kind == "char" then
    D.set_cursor(D.on_char(rng.from))
  elseif rng.kind == "line" then
    if start and rng.first < start.line then
      D.set_cursor(D.on_char(P(rng.first, start.col)))
    end
  elseif rng.kind == "block" then
    local s = D.line(rng.first)
    D.set_cursor(D.on_char(P(rng.first, T.advance(s, 0, rng.left))))
  end
  return text
end

-- delete removes the range, after copying it into the register. With change, a linewise
-- range leaves one line holding its indent, and the caret may stand at the end of a line
-- (the caller enters insert mode there).
function M.delete(rng, name, change)
  local text = M.text(rng)
  Reg.set(name, text, reg_kind(rng), "d")
  if rng.kind == "char" then
    if D.cmp(rng.from, rng.to) < 0 then
      D.replace(rng.from, rng.to, "")
    end
    if change then
      D.set_cursor(D.clamp(rng.from))
    else
      D.set_cursor(D.on_char(rng.from))
    end
    return
  end
  if rng.kind == "block" then
    local edits = {}
    for l = rng.first, rng.last do
      local s = D.line(l)
      local a = T.advance(s, 0, rng.left)
      local b = rng.right and T.advance(s, 0, rng.right) or #s
      if b > a then
        edits[#edits + 1] = { from = P(l, a), to = P(l, b) }
      end
    end
    if #edits > 0 then
      D.edit(edits)
    end
    local s = D.line(rng.first)
    local p = P(rng.first, T.advance(s, 0, rng.left))
    D.set_cursor(change and D.clamp(p) or D.on_char(p))
    return
  end
  local first, last = rng.first, rng.last
  local n = D.count()
  if change then
    local ind = T.indent(D.line(first))
    D.replace(P(first, 0), P(last, D.len(last)), ind)
    D.set_cursor(P(first, #ind))
    return
  end
  if last < n - 1 then
    D.replace(P(first, 0), P(last + 1, 0), "")
  elseif first > 0 then
    D.replace(P(first - 1, D.len(first - 1)), P(last, D.len(last)), "")
  else
    D.replace(P(0, 0), P(last, D.len(last)), "")
  end
  local l = math.min(first, D.count() - 1)
  D.set_cursor(D.first_nonblank(l))
end

-- ---------- case ----------

local function convert(op, s)
  if op == "g~" then
    return T.toggle(s)
  elseif op == "gu" then
    return T.lower(s)
  elseif op == "gU" then
    return T.upper(s)
  end
  return T.rot13(s)
end

-- case changes the case of the range; op is "g~", "gu", "gU" or "g?".
function M.case(rng, op, start)
  if rng.kind == "char" then
    local old = D.text(rng.from, rng.to)
    local new = convert(op, old)
    if new ~= old then
      D.replace(rng.from, rng.to, new)
    end
    D.set_cursor(D.on_char(rng.from))
    return
  end
  local edits = {}
  local first, last, left, right = rng.first, rng.last, 0, nil
  if rng.kind == "block" then
    left, right = rng.left, rng.right
  end
  for l = first, last do
    local s = D.line(l)
    local a = T.advance(s, 0, left)
    local b = right and T.advance(s, 0, right) or #s
    local old = s:sub(a + 1, b)
    local new = convert(op, old)
    if new ~= old then
      edits[#edits + 1] = { from = P(l, a), to = P(l, b), text = new }
    end
  end
  if #edits > 0 then
    D.edit(edits)
  end
  if rng.kind == "block" then
    D.set_cursor(D.on_char(P(first, T.advance(D.line(first), 0, left))))
  else
    D.set_cursor(D.on_char(P(first, start and math.min(start.col, T.last(D.line(first))) or 0)))
  end
end

-- ---------- join ----------

-- join joins n lines starting at line first (at least two). With spaces, the blanks at the
-- start of each joined line go and one space takes the place of the line break, unless
-- the next line starts with ")" or the line before ends in a blank. It reports whether
-- anything was joined.
function M.join(first, n, spaces)
  local count = D.count()
  if first + n - 1 > count - 1 then
    if n <= 2 then
      return false
    end
    n = count - first
  end
  if n < 2 then
    return false
  end
  local last = first + n - 1
  local acc = D.line(first)
  local at = #acc
  for l = first + 1, last do
    local s = D.line(l)
    at = #acc
    if spaces then
      s = s:gsub("^[ \t]+", "")
      local prev_end = acc:sub(-1)
      if s ~= "" and s:sub(1, 1) ~= ")" and #acc > 0 and prev_end ~= "\t" then
        if prev_end ~= " " then
          acc = acc .. " "
        end
      end
    end
    acc = acc .. s
  end
  D.replace(P(first, 0), P(last, D.len(last)), acc)
  D.set_cursor(D.on_char(P(first, at)))
  return true
end

-- ---------- put ----------

-- put pastes the register name (nil: the unnamed one) count times, before the caret or
-- after it. With after_text (gp, gP) the caret ends after the new text. The text of the
-- system clipboard cannot be read, so for "+ and "* the editor's own paste runs.
function M.put(name, count, before, after_text)
  if Reg.is_clip(name) then
    if not before then
      local cur = D.cursor()
      local s = D.line(cur.line)
      if #s > 0 then
        D.set_cursor(P(cur.line, T.next(s, cur.col)))
      end
    end
    local ok, err = gvim.command.run("paste")
    if not ok then
      D.fail("vi: " .. tostring(err))
    end
    return
  end
  local v = Reg.get(name)
  if not v then
    D.fail("E353: Nothing in register " .. (name or '"'))
  end
  local cur = D.cursor()
  if v.kind == "line" then
    local body = v.text:sub(1, -2)
    local one = body
    local parts = {}
    for _ = 1, count do
      parts[#parts + 1] = one
    end
    local text = table.concat(parts, "\n")
    local nlines = #T.split(text)
    local first
    if before then
      D.replace(P(cur.line, 0), P(cur.line, 0), text .. "\n")
      first = cur.line
    elseif cur.line < D.count() - 1 then
      D.replace(P(cur.line + 1, 0), P(cur.line + 1, 0), text .. "\n")
      first = cur.line + 1
    else
      local e = P(cur.line, D.len(cur.line))
      D.replace(e, e, "\n" .. text)
      first = cur.line + 1
    end
    if after_text then
      D.set_cursor(D.on_char(P(math.min(first + nlines, D.count() - 1), 0)))
    else
      D.set_cursor(D.first_nonblank(first))
    end
    return
  end
  if v.kind == "block" then
    M.put_block(v, count, before, cur)
    return
  end
  local text = string.rep(v.text, count)
  local s = D.line(cur.line)
  local at = cur
  if not before and #s > 0 then
    at = P(cur.line, T.next(s, cur.col))
  end
  D.replace(at, at, text)
  local e = D.advance(at, text)
  if after_text then
    D.set_cursor(D.clamp(e))
  elseif text:find("\n", 1, true) then
    D.set_cursor(D.on_char(at))
  else
    local line = D.line(e.line)
    D.set_cursor(D.on_char(P(e.line, T.prev(line, e.col))))
  end
end

-- put_block pastes a column of text at the caret: row i of the block goes into line
-- cur.line + i - 1 at the same character column, short lines being padded with blanks and
-- missing lines added at the end of the buffer.
function M.put_block(v, count, before, cur)
  local rows = T.split(v.text)
  local s = D.line(cur.line)
  local col_chars = T.count(s, 0, cur.col)
  if not before and #s > 0 then
    col_chars = col_chars + 1
  end
  local width = 0
  for _, r in ipairs(rows) do
    width = math.max(width, T.count(r, 0, #r))
  end
  local edits, extra = {}, {}
  for i, r in ipairs(rows) do
    local l = cur.line + i - 1
    local piece = string.rep(r .. string.rep(" ", width - T.count(r, 0, #r)), count)
    if l < D.count() then
      local line = D.line(l)
      local have = T.count(line, 0, #line)
      local a, ins = T.advance(line, 0, col_chars), piece
      if have < col_chars then
        a, ins = #line, string.rep(" ", col_chars - have) .. piece
      end
      if a >= #line then
        ins = ins:gsub("%s+$", "")
      end
      if ins ~= "" then
        edits[#edits + 1] = { from = P(l, a), to = P(l, a), text = ins }
      end
    else
      extra[#extra + 1] = (string.rep(" ", col_chars) .. piece):gsub("%s+$", "")
    end
  end
  if #extra > 0 then
    local e = P(D.count() - 1, D.len(D.count() - 1))
    edits[#edits + 1] = { from = e, to = e, text = "\n" .. table.concat(extra, "\n") }
  end
  if #edits > 0 then
    D.edit(edits)
  end
  local line = D.line(cur.line)
  D.set_cursor(D.on_char(P(cur.line, T.advance(line, 0, col_chars))))
end

-- ---------- replace ----------

-- replace_chars replaces count characters from the caret with ch (r). A line break as ch
-- replaces them with one line break.
function M.replace_chars(ch, count, pos)
  pos = pos or D.cursor()
  local s = D.line(pos.line)
  if T.count(s, pos.col, #s) < count then
    D.fail()
  end
  local e = P(pos.line, T.advance(s, pos.col, count))
  if ch == "\n" then
    D.replace(pos, e, "\n")
    D.set_cursor(P(pos.line + 1, 0))
    return
  end
  D.replace(pos, e, string.rep(ch, count))
  D.set_cursor(P(pos.line, pos.col + (count - 1) * #ch))
end

-- ---------- increment ----------

-- increment adds amount to the number at or after the caret on its line.
function M.increment(amount, pos)
  pos = pos or D.cursor()
  local s = D.line(pos.line)
  local at = pos.col + 1
  local found
  local i = 1
  while i <= #s do
    local hs, he = s:find("^0[xX]%x+", i)
    local ds, de = s:find("^%d+", i)
    if hs then
      if he >= at then
        found = { kind = "hex", s = hs, e = he }
        break
      end
      i = he + 1
    elseif ds then
      if de >= at then
        found = { kind = "dec", s = ds, e = de }
        break
      end
      i = de + 1
    else
      i = i + 1
    end
  end
  if not found then
    D.fail()
  end
  local text
  local from, to = found.s, found.e
  if found.kind == "hex" then
    local digits = s:sub(found.s + 2, found.e)
    local n = tonumber(digits, 16) + amount
    if n < 0 then
      n = 0
    end
    local upper = digits:find("%u") and not digits:find("%l")
    local fmt = upper and "%X" or "%x"
    local out = string.format(fmt, n)
    if #out < #digits then
      out = string.rep("0", #digits - #out) .. out
    end
    text = s:sub(found.s, found.s + 1) .. out
  else
    local digits = s:sub(found.s, found.e)
    local negative = false
    if found.s > 1 and s:sub(found.s - 1, found.s - 1) == "-" then
      local before = s:sub(found.s - 2, found.s - 2)
      if before == "" or not before:find("[%w_]") then
        negative = true
        from = found.s - 1
      end
    end
    local n = tonumber(digits)
    if negative then
      n = -n
    end
    n = n + amount
    local out = string.format("%d", math.abs(n))
    if digits:sub(1, 1) == "0" and #digits > 1 then
      if #out < #digits then
        out = string.rep("0", #digits - #out) .. out
      end
    end
    text = (n < 0 and "-" or "") .. out
  end
  D.replace(P(pos.line, from - 1), P(pos.line, to), text)
  D.set_cursor(P(pos.line, from - 1 + #text - 1))
end

return M
