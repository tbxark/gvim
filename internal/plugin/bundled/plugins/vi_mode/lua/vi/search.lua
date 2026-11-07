-- Searching: / ? n N * #. A pattern is written in vi's syntax (\< \> \( \) \| \+ \? ...)
-- and translated to Go's RE2, which the buffer search uses.
local gvim = require "gvim"
local D = require "vi.doc"
local T = require "vi.text"
local S = require "vi.state"
local Mo = require "vi.motion"

local M = {}

local function P(l, c) return { line = l, col = c } end

-- translate turns a vi pattern into an RE2 pattern. Its second result is true or false
-- when the pattern forces the case (\c, \C), nil otherwise.
function M.translate(pat)
  local out = {}
  local force
  local very = false
  local i = 1
  local n = #pat
  while i <= n do
    local ch = pat:sub(i, i)
    if ch == "\\" and i < n then
      local c = pat:sub(i + 1, i + 1)
      i = i + 2
      if c == "(" or c == ")" or c == "|" or c == "+" or c == "?" or c == "{" or c == "}" then
        out[#out + 1] = very and ("\\" .. c) or c
      elseif c == "=" then
        out[#out + 1] = "?"
      elseif c == "<" or c == ">" then
        out[#out + 1] = "\\b"
      elseif c == "n" then
        out[#out + 1] = "\\n"
      elseif c == "t" then
        out[#out + 1] = "\\t"
      elseif c == "e" then
        out[#out + 1] = "\\x1b"
      elseif c == "a" then
        out[#out + 1] = "[A-Za-z]"
      elseif c == "l" then
        out[#out + 1] = "[a-z]"
      elseif c == "u" then
        out[#out + 1] = "[A-Z]"
      elseif c == "x" then
        out[#out + 1] = "[0-9A-Fa-f]"
      elseif c == "c" then
        force = false
      elseif c == "C" then
        force = true
      elseif c == "v" then
        very = true
      elseif c == "V" or c == "m" or c == "M" then
        very = false
      elseif c == "s" or c == "S" or c == "d" or c == "D" or c == "w" or c == "W" then
        out[#out + 1] = "\\" .. c
      elseif c == "/" then
        out[#out + 1] = "/"
      elseif c:find("[%.%*%[%]%^%$~\\]") then
        out[#out + 1] = c == "~" and "~" or ("\\" .. c)
      else
        out[#out + 1] = "\\" .. c
      end
    elseif ch == "[" then
      local close = pat:find("]", i + 2, true)
      if close then
        out[#out + 1] = pat:sub(i, close)
        i = close + 1
      else
        out[#out + 1] = "\\["
        i = i + 1
      end
    elseif not very and (ch == "(" or ch == ")" or ch == "|" or ch == "+" or ch == "?" or ch == "{" or ch == "}") then
      out[#out + 1] = "\\" .. ch
      i = i + 1
    elseif very and (ch == "<" or ch == ">") then
      out[#out + 1] = "\\b"
      i = i + 1
    elseif very and ch == "=" then
      out[#out + 1] = "?"
      i = i + 1
    else
      out[#out + 1] = ch
      i = i + 1
    end
  end
  return table.concat(out), force
end

-- case_sensitive decides whether a pattern matches case: \c and \C decide, then the
-- settings ignore_case and smart_case.
function M.case_sensitive(pat, force)
  if force ~= nil then
    return force
  end
  local st = S.settings
  if not st.ignore_case then
    return true
  end
  if st.smart_case then
    local plain = pat:gsub("\\.", "")
    if plain:find("%u") then
      return true
    end
  end
  return false
end

-- compile returns what b:find needs: the RE2 pattern and the case flag.
function M.compile(pat)
  local re2, force = M.translate(pat)
  return re2, M.case_sensitive(pat, force)
end

-- find_error is the message for the error b:find returned. The editor's search does not
-- work through b:find on a large file (its error code is too_large or unsupported);
-- anything else is a pattern that does not compile.
function M.find_error(err)
  err = tostring(err)
  if err:find("^too_large:") or err:find("^unsupported:") then
    return "no search in large-file mode"
  end
  return "E486: bad pattern: " .. err
end

local function find(re2, cs, from, to, max)
  local b = D.buf()
  local ranges, truncated = b:find(re2, {
    regex = true, case_sensitive = cs, max = max or 1,
    within = { from = from, to = to },
  })
  if not ranges then
    D.fail(M.find_error(truncated))
  end
  return ranges, truncated
end

-- next_match finds the first match that starts after p (or at p when inclusive), wrapping
-- around the end of the buffer. It returns the match start or nil.
local function next_match(re2, cs, p, inclusive)
  local start = p
  if not inclusive then
    start = D.after(p)
    if D.eq(start, p) then
      start = P(0, 0)
    end
  end
  local last = D.last_pos()
  local r = find(re2, cs, start, last)
  if #r > 0 then
    return r[1].from, r[1]
  end
  if D.cmp(start, P(0, 0)) > 0 then
    r = find(re2, cs, P(0, 0), D.max(start, P(0, 0)))
    if #r > 0 then
      return r[1].from, r[1]
    end
  end
  return nil
end

-- prev_match finds the last match that starts before p, wrapping around the start. It
-- looks in windows of lines that grow towards the start of the buffer, so a big buffer
-- with many matches is not listed whole.
local function prev_match(re2, cs, p)
  local function last_before(from, to)
    local r = find(re2, cs, from, to, 100000)
    local best
    for _, m in ipairs(r) do
      if D.lt(m.from, p) then
        best = m
      end
    end
    return best
  end
  local window, top = 200, p.line
  while true do
    top = math.max(0, top - window)
    local best = last_before(P(top, 0), p)
    if best then
      return best.from, best
    end
    if top == 0 then
      break
    end
    window = window * 4
  end
  local r = find(re2, cs, p, D.last_pos(), 100000)
  if #r > 0 then
    return r[#r].from, r[#r]
  end
  return nil
end

-- motion moves count matches of pat from p. forward says the direction. It returns the
-- motion result, or nil when the pattern is not found.
function M.motion(pat, forward, count, p)
  local re2, cs = M.compile(pat)
  local cur = p
  for _ = 1, count do
    local m
    if forward then
      m = next_match(re2, cs, cur, false)
    else
      m = prev_match(re2, cs, cur)
    end
    if not m then
      D.fail("E486: Pattern not found: " .. pat)
    end
    cur = m
  end
  return { pos = cur, kind = "exclusive", jump = true }
end

-- last_search_motion is n (same direction) or N (opposite).
function M.repeat_motion(c, same)
  local ls = S.last_search
  if not ls then
    D.fail("E35: No previous regular expression")
  end
  local forward = ls.forward
  if not same then
    forward = not forward
  end
  return M.motion(ls.pattern, forward, c.count, c.p)
end

-- word_pattern builds the pattern * and # search for: the word under or after the caret,
-- or the run of other characters when there is no word.
function M.word_under(p)
  local s = D.line(p.line)
  local col = p.col
  local function class(c) return T.cls(T.at(s, c), false) end
  -- the first word character at or after the caret, else the first non-blank
  local at = col
  while at < #s and class(at) ~= 2 do
    at = T.next(s, at)
  end
  local want = 2
  if at >= #s then
    at = col
    while at < #s and class(at) == 0 do
      at = T.next(s, at)
    end
    if at >= #s then
      return nil
    end
    want = 1
  end
  local a, b = at, at
  while a > 0 and class(T.prev(s, a)) == want do
    a = T.prev(s, a)
  end
  while b < #s and class(b) == want do
    b = T.next(s, b)
  end
  return s:sub(a + 1, b), a, want
end

-- star_pattern is the vi pattern for the word under the caret, whole-word when it can be.
function M.star_pattern(word, want)
  local esc = (word:gsub("[\\/%.%*%[%]%^%$~]", "\\%0"))
  if want == 2 and not word:find("[\128-\255]") then
    return "\\<" .. esc .. "\\>"
  end
  return esc
end

return M
