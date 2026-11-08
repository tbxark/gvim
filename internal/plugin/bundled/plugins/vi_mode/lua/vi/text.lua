-- Characters and strings. Columns are 0-based byte offsets of the start of a character,
-- as the plugin API has them; a column equal to #s is the end of the line (where vi puts
-- the line break). Nothing here talks to the editor.
local gvim = require "gvim"

local M = {}

local byte, sub = string.byte, string.sub

-- mark_len is the length in bytes of the combining mark that starts at byte i (1-based), or
-- 0 when there is none: the accents of U+0300..036F, 1AB0..1AFF, 1DC0..1DFF, 20D0..20FF,
-- FE20..FE2F and the variation selectors FE00..FE0F.
local function mark_len(s, i)
  local b = byte(s, i)
  if not b or b < 0xCC then
    return 0
  end
  local b2, b3 = byte(s, i + 1), byte(s, i + 2)
  if b == 0xCC or (b == 0xCD and b2 and b2 <= 0xAF) then
    return 2
  end
  if not (b2 and b3) then
    return 0
  end
  if b == 0xE1 and ((b2 == 0xAA and b3 >= 0xB0) or b2 == 0xAB or b2 == 0xB7) then
    return 3
  elseif b == 0xE2 and b2 == 0x83 and b3 >= 0x90 then
    return 3
  elseif b == 0xEF and b2 == 0xB8 and (b3 <= 0x8F or (b3 >= 0xA0 and b3 <= 0xAF)) then
    return 3
  end
  return 0
end

-- next returns the column of the character after the one at col, or #s at the last one. A
-- character is a code point with the combining marks that follow it.
function M.next(s, col)
  local b = byte(s, col + 1)
  if not b then
    return col
  end
  local n = 1
  if b >= 0xF0 then
    n = 4
  elseif b >= 0xE0 then
    n = 3
  elseif b >= 0xC0 then
    n = 2
  end
  local c = col + n
  if c > #s then
    c = #s
  end
  while true do
    local m = mark_len(s, c + 1)
    if m == 0 then
      break
    end
    c = math.min(c + m, #s)
  end
  return c
end

-- prev_point is the column of the code point before col.
local function prev_point(s, col)
  local i = col -- 1-based index of the byte before col
  while i > 1 do
    local b = byte(s, i)
    if b < 0x80 or b >= 0xC0 then
      break
    end
    i = i - 1
  end
  return i - 1
end

-- prev returns the column of the character before col (0 at the start).
function M.prev(s, col)
  if col <= 0 then
    return 0
  end
  if col > #s then
    col = #s
  end
  local c = prev_point(s, col)
  while c > 0 and mark_len(s, c + 1) > 0 do
    c = prev_point(s, c)
  end
  return c
end

-- last returns the column of the last character of s, 0 for an empty line.
function M.last(s)
  if #s == 0 then
    return 0
  end
  return M.prev(s, #s)
end

-- at returns the character that starts at col, "" at the end of the line.
function M.at(s, col)
  if col >= #s or col < 0 then
    return ""
  end
  return sub(s, col + 1, M.next(s, col))
end

-- align moves a column that falls inside a character back to the start of it.
function M.align(s, col)
  if col >= #s then
    return #s
  end
  if col <= 0 then
    return 0
  end
  local i = col + 1
  while i > 1 do
    local b = byte(s, i)
    if b < 0x80 or b >= 0xC0 then
      break
    end
    i = i - 1
  end
  local c = i - 1
  while c > 0 and mark_len(s, c + 1) > 0 do
    c = prev_point(s, c)
  end
  return c
end

-- chars returns the characters of s in an array.
function M.chars(s)
  local out, col = {}, 0
  while col < #s do
    local nx = M.next(s, col)
    out[#out + 1] = sub(s, col + 1, nx)
    col = nx
  end
  return out
end

-- count returns how many characters start in [from, to).
function M.count(s, from, to)
  local n, col = 0, from
  to = math.min(to, #s)
  while col < to do
    col = M.next(s, col)
    n = n + 1
  end
  return n
end

-- advance moves n characters to the right of col (stopping at the end of the line).
function M.advance(s, col, n)
  while n > 0 and col < #s do
    col = M.next(s, col)
    n = n - 1
  end
  return col
end

-- retreat moves n characters to the left of col (stopping at 0).
function M.retreat(s, col, n)
  while n > 0 and col > 0 do
    col = M.prev(s, col)
    n = n - 1
  end
  return col
end

-- first_nonblank returns the column of the first character that is not a blank, or the
-- last character of a line that has none (vi's ^ on a blank line).
function M.first_nonblank(s)
  local i = s:find("[^ \t]")
  if not i then
    return M.last(s)
  end
  return i - 1
end

-- indent returns the leading blanks of s.
function M.indent(s)
  return s:match("^[ \t]*")
end

-- is_blank reports whether s is empty or only blanks.
function M.is_blank(s)
  return not s:find("[^ \t]")
end

-- cls is vi's character class: 0 for blanks and the end of the line, 2 for the characters
-- of words (letters, digits, "_", and everything that is not ASCII), 1 for the rest. With
-- big (a WORD) every non-blank character is class 1.
function M.cls(ch, big)
  if ch == "" or ch == " " or ch == "\t" then
    return 0
  end
  if big then
    return 1
  end
  local b = byte(ch)
  if b >= 0x80 or b == 95 or (b >= 48 and b <= 57) or (b >= 65 and b <= 90) or (b >= 97 and b <= 122) then
    return 2
  end
  return 1
end

local function ascii(s)
  return not s:find("[\128-\255]")
end

function M.upper(s)
  if ascii(s) then
    return s:upper()
  end
  return gvim.text.upper(s)
end

function M.lower(s)
  if ascii(s) then
    return s:lower()
  end
  return gvim.text.lower(s)
end

-- toggle swaps the case of every letter of s.
function M.toggle(s)
  if ascii(s) then
    return (s:gsub("%a", function(c)
      if c:find("%l") then
        return c:upper()
      end
      return c:lower()
    end))
  end
  local out = {}
  for i, c in ipairs(M.chars(s)) do
    local u = gvim.text.upper(c)
    if u == c then
      out[i] = gvim.text.lower(c)
    else
      out[i] = u
    end
  end
  return table.concat(out)
end

-- rot13 is g?.
function M.rot13(s)
  return (s:gsub("%a", function(c)
    local b = byte(c)
    local base = b >= 97 and 97 or 65
    return string.char((b - base + 13) % 26 + base)
  end))
end

-- split cuts s at "\n" and returns the lines. A trailing "\n" gives a last empty line.
function M.split(s)
  local out, pos = {}, 1
  while true do
    local i = s:find("\n", pos, true)
    if not i then
      out[#out + 1] = sub(s, pos)
      return out
    end
    out[#out + 1] = sub(s, pos, i - 1)
    pos = i + 1
  end
end

-- escape_re quotes the characters of s that RE2 reads as syntax.
function M.escape_re(s)
  return (s:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?%{%}%|\\]", "\\%0"))
end

return M
