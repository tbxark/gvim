-- The document vi works on: the active buffer, its lines (cached for the length of one
-- key press), positions and the edits. A position is {line=, col=}: 0-based line and
-- 0-based byte column of the start of a character, as the plugin API has it. A column
-- equal to the length of the line is the end of the line, where the line break is.
local gvim = require "gvim"
local T = require "vi.text"

local M = {}

-- CANCEL is thrown (with error) to abandon the command being read. The reader catches it.
M.CANCEL = setmetatable({}, { __tostring = function() return "vi: cancelled" end })

local buf, cache, nlines, total

-- A text that ends in a line break has an empty line after it in the host. It is not a line
-- of the file (vi never lands on it, and what crosses it would eat the line break), so
-- the count leaves it out (line() still answers for it, as the caret can be put there by
-- the editor, and gives an empty line). keep_last is set when the caret is on it: an insert that opened
-- a line at the end of a text that had no line break makes a real last line.
local keep_last = false

local function settle(n)
  total = n
  if n and n > 1 and not keep_last and buf:line(n - 1) == "" then
    return n - 1
  end
  return n
end

-- The line cache is a table of chunks of CHUNK lines, not a table keyed by the line number:
-- gopher-lua pads the array part of a table with nils up to an integer key, so a cache keyed
-- by line 900000 costs tens of MiB on every key press. A chunk is indexed by line // CHUNK.
local CHUNK = 1024

local function cached(l)
  local c = cache[math.floor(l / CHUNK) + 1]
  return c and c[l % CHUNK + 1]
end

local function store(l, s)
  local k = math.floor(l / CHUNK) + 1
  local c = cache[k]
  if not c then
    c = {}
    cache[k] = c
  end
  c[l % CHUNK + 1] = s
end

-- fail abandons the command after telling the user why.
function M.fail(msg)
  if msg then
    gvim.notify(msg, "warn")
  end
  error(M.CANCEL, 0)
end

-- begin binds the active buffer and forgets the cached lines. Every key press starts with
-- it, because anything may have edited the buffer since the last one. It returns the buffer,
-- or nil when there is none (or its size is not known yet).
function M.begin()
  buf = gvim.buf.current()
  cache = {}
  nlines = nil
  keep_last = false
  if buf then
    nlines = buf:line_count()
    local c = nlines and buf:cursor()
    keep_last = c ~= nil and c.line >= nlines - 1
    nlines = settle(nlines)
  end
  if not buf or not nlines then
    return nil
  end
  return buf
end

function M.buf() return buf end

-- count is the number of lines (at least 1).
function M.count() return nlines end

-- invalidate forgets the cached lines after an edit.
function M.invalidate()
  cache = {}
  nlines = settle(buf:line_count()) or nlines
end

-- trailing reports whether the text ends in a line break, which has the empty line of the
-- host after it.
function M.trailing() return total ~= nil and total > nlines end

-- line returns the text of line l (0-based), or nil past the ends.
function M.line(l)
  if l < 0 or l >= (total or nlines) then
    return nil
  end
  local s = cached(l)
  if s == nil then
    s = buf:line(l) or ""
    store(l, s)
  end
  return s
end

-- len is the length in bytes of line l.
function M.len(l)
  local s = M.line(l)
  return s and #s or 0
end

-- preload reads the lines first..last (inclusive) into the cache with a few calls, so a
-- command that goes over many lines does not call the host once for each.
function M.preload(first, last)
  first, last = math.max(first, 0), math.min(last, nlines - 1)
  local chunk = 5000
  local l = first
  while l <= last do
    -- skip the lines that are cached already
    while l <= last and cached(l) ~= nil do
      l = l + 1
    end
    if l > last then
      break
    end
    local n = math.min(chunk, last - l + 1)
    local got = buf:lines(l, n)
    if not got then
      return
    end
    for i, s in ipairs(got) do
      store(l + i - 1, s)
    end
    l = l + n
  end
end

-- lines returns the lines first..last (inclusive) as an array.
function M.lines(first, last)
  if last - first >= 2 then
    M.preload(first, last)
  end
  local out = {}
  for l = first, last do
    out[#out + 1] = M.line(l) or ""
  end
  return out
end

function M.pos(l, c) return { line = l, col = c } end

function M.cmp(a, b)
  if a.line ~= b.line then
    return a.line < b.line and -1 or 1
  end
  if a.col ~= b.col then
    return a.col < b.col and -1 or 1
  end
  return 0
end

function M.lt(a, b) return M.cmp(a, b) < 0 end
function M.le(a, b) return M.cmp(a, b) <= 0 end
function M.eq(a, b) return M.cmp(a, b) == 0 end
function M.min(a, b) return M.cmp(a, b) <= 0 and a or b end
function M.max(a, b) return M.cmp(a, b) >= 0 and a or b end

-- clamp puts a position on the text: a line inside the buffer, a column on a character or
-- at the end of the line.
function M.clamp(p)
  local l = math.max(0, math.min(p.line, nlines - 1))
  local s = M.line(l)
  local c = T.align(s, math.max(0, math.min(p.col, #s)))
  return { line = l, col = c }
end

-- on_char puts a position on a character of its line, like the normal-mode cursor: the
-- last character of the line when the position is at its end.
function M.on_char(p)
  p = M.clamp(p)
  local s = M.line(p.line)
  if p.col >= #s and #s > 0 then
    p = { line = p.line, col = T.last(s) }
  end
  return p
end

-- inc steps one character forward: onto the end of the line when the last character is
-- passed, then onto the next line. The second result is vi's: 0 within the line, 2 when
-- the step reached the end of the line, 1 when it went to the next line, -1 at the end of
-- the buffer (and p does not move).
function M.inc(p)
  local s = M.line(p.line)
  if p.col < #s then
    local c = T.next(s, p.col)
    return { line = p.line, col = c }, c < #s and 0 or 2
  end
  if p.line + 1 < nlines then
    return { line = p.line + 1, col = 0 }, 1
  end
  return p, -1
end

-- dec steps one character back: from the start of a line to the end of the previous one
-- (the second result is 1 then), -1 at the start of the buffer.
function M.dec(p)
  if p.col > 0 then
    return { line = p.line, col = T.prev(M.line(p.line), p.col) }, 0
  end
  if p.line > 0 then
    return { line = p.line - 1, col = M.len(p.line - 1) }, 1
  end
  return p, -1
end

-- incl is inc that skips the end of the line (vi's incl).
function M.incl(p)
  local q, r = M.inc(p)
  if r >= 1 and q.col > 0 then
    q, r = M.inc(q)
  end
  return q, r
end

-- decl is dec that skips the end of the line (vi's decl).
function M.decl(p)
  local q, r = M.dec(p)
  if r == 1 and q.col > 0 then
    q, r = M.dec(q)
  end
  return q, r
end

-- char_at is the character at p ("" at the end of a line).
function M.char_at(p)
  return T.at(M.line(p.line) or "", p.col)
end

-- after returns the position just after the character at p; at the end of a line that is
-- the start of the next line (or p itself on the last one).
function M.after(p)
  local s = M.line(p.line)
  if p.col < #s then
    return { line = p.line, col = T.next(s, p.col) }
  end
  if p.line + 1 < nlines then
    return { line = p.line + 1, col = 0 }
  end
  return p
end

-- first_nonblank returns the position of the first non-blank of line l.
function M.first_nonblank(l)
  return { line = l, col = T.first_nonblank(M.line(l) or "") }
end

-- advance returns the position after the text s was inserted at p.
function M.advance(p, s)
  local lines = T.split(s)
  if #lines == 1 then
    return { line = p.line, col = p.col + #s }
  end
  return { line = p.line + #lines - 1, col = #lines[#lines] }
end

-- text returns the text in [from, to).
function M.text(from, to)
  if M.cmp(from, to) >= 0 then
    return ""
  end
  local t, err = buf:text({ from = from, to = to })
  if not t then
    M.fail("vi: cannot read the text: " .. tostring(err))
  end
  return t
end

-- cursor returns the position of the caret.
function M.cursor()
  local c = buf:cursor()
  return { line = c.line, col = c.col }
end

-- set_cursor moves the caret and scrolls it into view.
function M.set_cursor(p, reveal)
  buf:set_cursor(p, { reveal = reveal ~= false })
end

-- edit applies the edits ({from=, to=, text=}) as one undo step. The caret is left where
-- it is; the caller places it.
function M.edit(edits)
  local list = {}
  for i, e in ipairs(edits) do
    list[i] = { range = { from = e.from, to = e.to }, text = e.text or "" }
  end
  local ok, err = buf:edit(list, { cursor = "keep" })
  M.invalidate()
  if not ok then
    local code = gvim.err_code(err)
    if code == "read_only" then
      M.fail("E45: 'readonly' option is set")
    end
    M.fail("vi: " .. tostring(err))
  end
end

-- replace edits one range.
function M.replace(from, to, text)
  M.edit({ { from = from, to = to, text = text } })
end

-- delete_range removes [from, to).
function M.delete_range(from, to)
  M.edit({ { from = from, to = to } })
end

-- last_pos is the end of the buffer.
function M.last_pos()
  return { line = nlines - 1, col = M.len(nlines - 1) }
end

return M
