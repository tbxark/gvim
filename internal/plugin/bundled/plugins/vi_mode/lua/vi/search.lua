-- Searching: / ? n N * #. A pattern is written in vi's syntax (\< \> \( \) \| \+ \? ...)
-- and translated to Go's RE2, which the buffer search uses.
local gvim = require "gvim"
local D = require "vi.doc"
local T = require "vi.text"
local S = require "vi.state"
local Mo = require "vi.motion"

local M = {}

local function P(l, c) return { line = l, col = c } end

-- interval reads the rest of an interval quantifier (\{n,m} \{n,} \{,m} \{n} \{} and the lazy
-- \{-n,m}) at pat[i], just after the "{". It returns the RE2 quantifier and the index after
-- the closing "}" (or "\}"), or nil when the interval is not well formed.
local function interval(pat, i)
  local close = pat:find("}", i, true)
  if not close then
    return nil
  end
  local body = pat:sub(i, close - 1)
  local next_i = close + 1
  if body:sub(-1) == "\\" then
    body = body:sub(1, -2)
  end
  local lazy = body:sub(1, 1) == "-"
  if lazy then
    body = body:sub(2)
  end
  local lo, comma, hi = body:match("^(%d*)(,?)(%d*)$")
  if not lo then
    return nil
  end
  local q
  if comma == "" then
    q = lo == "" and "*" or ("{" .. lo .. "}")
  elseif lo == "" and hi == "" then
    q = "*"
  elseif hi == "" then
    q = "{" .. lo .. ",}"
  else
    q = "{" .. (lo == "" and "0" or lo) .. "," .. hi .. "}"
  end
  return q .. (lazy and "?" or ""), next_i
end

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
      local q, after
      if c == "{" and not very then
        q, after = interval(pat, i)
      end
      if q then
        out[#out + 1] = q
        i = after
      elseif c == "(" or c == ")" or c == "|" or c == "+" or c == "?" or c == "{" or c == "}" then
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
    elseif very and ch == "{" and interval(pat, i) then
      out[#out + 1], i = interval(pat, i)
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
-- settings ignore_case and smart_case. nosmart leaves smart_case out, as vi does for the
-- patterns of * and #.
function M.case_sensitive(pat, force, nosmart)
  if force ~= nil then
    return force
  end
  local st = S.settings
  if not st.ignore_case then
    return true
  end
  if st.smart_case and not nosmart then
    local plain = pat:gsub("\\.", "")
    if plain:find("%u") then
      return true
    end
  end
  return false
end

-- compile returns what b:find needs: the RE2 pattern and the case flag.
function M.compile(pat, nosmart)
  local re2, force = M.translate(pat)
  return re2, M.case_sensitive(pat, force, nosmart)
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
    return r[1].from, r[1], false
  end
  if D.cmp(start, P(0, 0)) > 0 then
    r = find(re2, cs, P(0, 0), D.max(start, P(0, 0)))
    if #r > 0 then
      return r[1].from, r[1], true
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
      return best.from, best, false
    end
    if top == 0 then
      break
    end
    window = window * 4
  end
  local r = find(re2, cs, p, D.last_pos(), 100000)
  if #r > 0 then
    return r[#r].from, r[#r], true
  end
  return nil
end

-- motion moves count matches of pat from p. forward says the direction. It returns the
-- motion result, or nil when the pattern is not found.
function M.motion(pat, forward, count, p, nosmart)
  local re2, cs = M.compile(pat, nosmart)
  local cur = p
  local wrapped = false
  for _ = 1, count do
    local m, _, wrap
    if forward then
      m, _, wrap = next_match(re2, cs, cur, false)
    else
      m, _, wrap = prev_match(re2, cs, cur)
    end
    if not m then
      D.fail("E486: Pattern not found: " .. pat)
    end
    wrapped = wrapped or wrap
    cur = m
  end
  if wrapped then
    gvim.notify(forward and "search hit BOTTOM, continuing at TOP" or "search hit TOP, continuing at BOTTOM")
  end
  M.highlight(pat, nosmart)
  return { pos = cur, kind = "exclusive", jump = true }
end

-- ---------- highlighting the matches ----------

local hl_ns -- the decoration namespace of the highlight, made on first use

-- highlight starts showing the matches of the vi pattern pat (the last search): the setting
-- hlsearch, on by default, turns it off, and :noh hides it until the next search.
function M.highlight(pat, nosmart)
  S.hl = { pattern = pat, nosmart = nosmart }
  M.show()
end

-- clear_highlight hides the matches in every buffer (:noh).
function M.clear_highlight()
  S.hl = nil
  if hl_ns then
    pcall(function() hl_ns:clear() end)
  end
end

-- hide removes the painted matches from every buffer and keeps the pattern: vi turned off or stepped
-- aside, or a buffer that is left, shows none of them.
function M.hide()
  if hl_ns then
    pcall(function() hl_ns:clear() end)
  end
end

-- show paints the matches of the highlighted pattern in the lines the window shows and some
-- beyond, in the active buffer. The decorations follow edits; this runs again when the window
-- scrolls or the text changes.
function M.show()
  local hl = S.hl
  if not S.settings.hlsearch or not hl or not S.enabled then
    if hl_ns and not S.settings.hlsearch then
      pcall(function() hl_ns:clear() end)
    end
    return
  end
  local buf = gvim.buf.current()
  if not buf then
    return
  end
  pcall(function()
    hl_ns = hl_ns or gvim.decor.namespace("search")
    local info = buf:info()
    if info and (info.large or info.virtual) then
      hl_ns:clear(buf)
      return
    end
    local top, height = 0, 60
    local w = gvim.win.current()
    local v = w and w:view()
    if type(v) == "table" then
      top, height = v.top_line or 0, v.height or 60
    end
    local n = buf:line_count() or 0
    local from, to = math.max(0, top - height), math.min(n, top + 2 * height)
    local re2, cs = M.compile(hl.pattern, hl.nosmart)
    local ranges = buf:find(re2, { regex = true, case_sensitive = cs, max = 1000,
      within = { from = P(from, 0), to = P(to, 0) } })
    local items = {}
    for _, r in ipairs(ranges or {}) do
      if D.lt(r.from, r.to) then
        items[#items + 1] = { kind = "highlight", range = r, style = { bg = "search.match_bg", fg = "search.match_fg" } }
      end
    end
    hl_ns:set(buf, items)
  end)
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
  return M.motion(ls.pattern, forward, c.count, c.p, ls.nosmart)
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
