-- The : command line. run(line) parses an optional range and a command name (any
-- unambiguous prefix works, as in vi), then runs the command. Commands that touch a file
-- or the editor go through the editor's own actions, so they behave like the menu items:
-- :w saves, :q closes the tab (and quits on the last one), :q! discards it, :qa! asks.
local gvim = require "gvim"
local D = require "vi.doc"
local T = require "vi.text"
local Reg = require "vi.reg"
local Ops = require "vi.ops"
local Search = require "vi.search"
local S = require "vi.state"

local M = {}

local function P(l, c) return { line = l, col = c } end

local function say(msg)
  gvim.notify(msg)
end

-- bad reports an error of the command line and abandons it.
local function bad(msg)
  gvim.notify(msg, "warn")
  error(D.CANCEL, 0)
end

local function run_action(action, args)
  local ok, err = gvim.command.run(action, args)
  if not ok then
    bad(string.format("E: %s: %s", action, tostring(err)))
  end
end

-- ---------- ranges ----------

local function mark_line(name)
  local Mo = require "vi.motion"
  local p = Mo.mark_pos(name)
  if not p then
    bad("E20: Mark not set")
  end
  return p.line
end

-- parse_address reads one address at s[pos]. It returns the 0-based line (nil when there
-- is none), "%" for the whole buffer, and the new position.
local function parse_address(s, pos, cur)
  local function skip()
    while s:sub(pos, pos):find("[ \t]") do
      pos = pos + 1
    end
  end
  skip()
  local c = s:sub(pos, pos)
  local base
  if c == "%" then
    return "%", pos + 1
  elseif c == "." then
    base, pos = cur, pos + 1
  elseif c == "$" then
    base, pos = D.count() - 1, pos + 1
  elseif c:find("%d") then
    local digits = s:match("^%d+", pos)
    base, pos = tonumber(digits) - 1, pos + #digits
  elseif c == "'" then
    base, pos = mark_line(s:sub(pos + 1, pos + 1)), pos + 2
  elseif c == "/" or c == "?" then
    local close = pos + 1
    while close <= #s and s:sub(close, close) ~= c do
      if s:sub(close, close) == "\\" then
        close = close + 1
      end
      close = close + 1
    end
    local pat = s:sub(pos + 1, close - 1)
    pos = close + 1
    if pat == "" then
      pat = S.last_search and S.last_search.pattern or ""
    end
    if pat == "" then
      bad("E35: No previous regular expression")
    end
    local start = c == "/" and P(cur, D.len(cur)) or P(cur, 0)
    local r = Search.motion(pat, c == "/", 1, start)
    base = r.pos.line
    S.last_search = { pattern = pat, forward = c == "/" }
  end
  while true do
    skip()
    local sign = s:sub(pos, pos)
    if sign ~= "+" and sign ~= "-" then
      break
    end
    local digits = s:match("^%d+", pos + 1)
    local n = digits and tonumber(digits) or 1
    pos = pos + 1 + (digits and #digits or 0)
    base = (base or cur) + (sign == "+" and n or -n)
  end
  return base, pos
end

-- parse_range reads the range at the start of s. It returns first, last (0-based, nil when
-- there is no range), the rest of the line, and whether the range was given.
local function parse_range(s, cur)
  local first, last
  local pos = 1
  local given = false
  local a, p = parse_address(s, pos, cur)
  if a == "%" then
    return 0, D.count() - 1, s:sub(p), true
  end
  pos = p
  if a then
    first, last, given = a, a, true
  end
  while s:sub(pos, pos) == "," or s:sub(pos, pos) == ";" do
    if s:sub(pos, pos) == ";" and first then
      cur = first
    end
    local b
    b, pos = parse_address(s, pos + 1, cur)
    if b == "%" then
      b = D.count() - 1
    end
    if not given then
      first = cur
    end
    last = b or cur
    given = true
  end
  if first and last and first > last then
    local tmp = first
    first = last
    last = tmp
  end
  return first, last, s:sub(pos), given
end

-- ---------- commands ----------

local commands = {}

-- cmd registers a command with its shortest abbreviation.
local function cmd(name, min, fn, default_all)
  commands[#commands + 1] = { name = name, min = min, fn = fn, all = default_all }
end

local function resolve(path)
  if path:sub(1, 1) == "/" or path:match("^%a:[\\/]") then
    return path
  end
  if path:sub(1, 2) == "~/" then
    return gvim.dirs.home() .. path:sub(2)
  end
  return gvim.dirs.cwd() .. "/" .. path
end

local function buf_or_fail()
  local b = gvim.buf.current()
  if not b then
    bad("E: no buffer")
  end
  return b
end

-- finish leaves the editor when the last tab goes: the tab is closed (force discards its
-- changes) and a quit follows when no other tab is left.
local function close_current(force)
  local b = buf_or_fail()
  local tabs = gvim.buf.list()
  if #tabs > 1 then
    if force then
      run_action("close_tab", { force = true })
    else
      local ok, err = b:close()
      if not ok then
        bad("E37: No write since last change (add ! to override): " .. tostring(err))
      end
    end
  else
    if force then
      run_action("close_tab", { force = true })
    elseif b:is_modified() then
      bad("E37: No write since last change (add ! to override)")
    end
    run_action("quit")
  end
end

local function any_modified()
  for _, b in ipairs(gvim.buf.list()) do
    if b:is_modified() and not b:is_virtual() then
      return b
    end
  end
  return nil
end

local function write(b, path)
  local ok, err
  if path and path ~= "" then
    ok, err = b:save(resolve(path))
  else
    if (b:path() or "") == "" and not path then
      bad("E32: No file name")
    end
    ok, err = b:save()
  end
  if not ok then
    bad("E: " .. tostring(err))
  end
  return true
end

cmd("write", 1, function(x)
  local b = buf_or_fail()
  write(b, x.args ~= "" and x.args or nil)
  say('"' .. b:name() .. '" written')
end, true)

cmd("update", 2, function(x)
  local b = buf_or_fail()
  if b:is_modified() then
    write(b, nil)
    say('"' .. b:name() .. '" written')
  end
end)

cmd("quit", 1, function(x)
  close_current(x.bang)
end)

cmd("close", 3, function(x)
  run_action("close_split")
end)

cmd("wq", 2, function(x)
  local b = buf_or_fail()
  write(b, x.args ~= "" and x.args or nil)
  close_current(false)
end, true)

cmd("xit", 1, function(x)
  local b = buf_or_fail()
  if b:is_modified() then
    write(b, x.args ~= "" and x.args or nil)
  end
  close_current(false)
end, true)

cmd("exit", 3, function(x)
  local b = buf_or_fail()
  if b:is_modified() then
    write(b, nil)
  end
  close_current(false)
end, true)

cmd("wall", 2, function()
  for _, b in ipairs(gvim.buf.list()) do
    if b:is_modified() and (b:path() or "") ~= "" then
      write(b, nil)
    end
  end
  say("all files written")
end)

cmd("qall", 2, function(x)
  local dirty = any_modified()
  if x.bang then
    if dirty then
      run_action("quit_discard")
    else
      run_action("quit")
    end
    return
  end
  if dirty then
    bad('E37: No write since last change (add ! to override): "' .. dirty:name() .. '"')
  end
  run_action("quit")
end)

cmd("wqall", 3, function()
  for _, b in ipairs(gvim.buf.list()) do
    if b:is_modified() and (b:path() or "") ~= "" then
      write(b, nil)
    end
  end
  local dirty = any_modified()
  if dirty then
    bad('E37: No write since last change: "' .. dirty:name() .. '" has no file name')
  end
  run_action("quit")
end)

cmd("xall", 2, function()
  commands.by_name.wqall.fn({ args = "" })
end)

cmd("edit", 1, function(x)
  local b = buf_or_fail()
  if x.args == "" then
    if b:is_modified() and not x.bang then
      bad("E37: No write since last change (add ! to override)")
    end
    local ok, err = b:revert()
    if not ok then
      bad("E: " .. tostring(err))
    end
    return
  end
  local ok, err = gvim.buf.open(resolve(x.args))
  if not ok then
    bad("E: " .. tostring(err))
  end
end)

cmd("enew", 3, function() run_action("new") end)

cmd("saveas", 3, function(x)
  if x.args == "" then
    bad("E471: Argument required")
  end
  write(buf_or_fail(), x.args)
end)

cmd("read", 1, function(x)
  if x.args == "" then
    bad("E471: Argument required")
  end
  local text, err = gvim.fs.read(resolve(x.args))
  if not text then
    bad("E484: Can't open file " .. x.args .. ": " .. tostring(err))
  end
  text = text:gsub("\r\n", "\n"):gsub("\n$", "")
  local line = x.last or D.cursor().line
  local at = P(line, D.len(line))
  D.replace(at, at, "\n" .. text)
  D.set_cursor(D.first_nonblank(line + 1))
end)

cmd("next", 1, function() run_action("next_buffer") end)
cmd("Next", 1, function() run_action("prev_buffer") end)
cmd("previous", 4, function() run_action("prev_buffer") end)
cmd("bnext", 2, function() run_action("next_buffer") end)
cmd("bprevious", 2, function() run_action("prev_buffer") end)
cmd("bNext", 2, function() run_action("prev_buffer") end)
cmd("tabnext", 4, function() run_action("next_buffer") end)
cmd("tabprevious", 4, function() run_action("prev_buffer") end)
cmd("tabnew", 4, function(x)
  run_action("new")
  if x.args ~= "" then
    gvim.buf.open(resolve(x.args))
  end
end)
cmd("tabedit", 4, function(x)
  commands.by_name.edit.fn({ args = x.args, bang = false })
end)
cmd("tabclose", 4, function(x) close_current(x.bang) end)
cmd("bdelete", 2, function(x)
  local b = buf_or_fail()
  local ok, err = b:close({ force = x.bang })
  if not ok then
    bad("E89: No write since last change (add ! to override): " .. tostring(err))
  end
end)

cmd("buffer", 1, function(x)
  local tabs = gvim.buf.list()
  if x.args == "" then
    local b = buf_or_fail()
    say(string.format('"%s" buffer %d', b:name(), b.id))
    return
  end
  local n = tonumber(x.args)
  local want
  if n then
    for _, b in ipairs(tabs) do
      if b.id == n then
        want = b
      end
    end
    want = want or tabs[n]
    if not want then
      bad("E86: Buffer " .. x.args .. " does not exist")
    end
  else
    local matches = {}
    for _, b in ipairs(tabs) do
      if b:name():lower():find(x.args:lower(), 1, true) then
        matches[#matches + 1] = b
      end
    end
    if #matches == 0 then
      bad("E94: No matching buffer for " .. x.args)
    elseif #matches > 1 then
      bad("E93: More than one match for " .. x.args)
    end
    want = matches[1]
  end
  want:show()
end)

cmd("ls", 2, function()
  local cur = gvim.buf.current()
  local parts = {}
  for i, b in ipairs(gvim.buf.list()) do
    parts[#parts + 1] = string.format("%s%d: %s%s", b == cur and "%" or " ", i, b:name(), b:is_modified() and " [+]" or "")
  end
  say(table.concat(parts, " | "))
end)
commands[#commands + 1] = { name = "buffers", min = 7, fn = commands[#commands].fn }
commands[#commands + 1] = { name = "files", min = 3, fn = commands[#commands].fn }

cmd("split", 2, function(x)
  run_action("split_horizontal")
  if x.args ~= "" then
    gvim.buf.open(resolve(x.args))
  end
end)
cmd("vsplit", 2, function(x)
  run_action("split_vertical")
  if x.args ~= "" then
    gvim.buf.open(resolve(x.args))
  end
end)
cmd("new", 3, function(x)
  run_action("split_horizontal")
  run_action("new")
  if x.args ~= "" then
    gvim.buf.open(resolve(x.args))
  end
end)
cmd("vnew", 3, function(x)
  run_action("split_vertical")
  run_action("new")
  if x.args ~= "" then
    gvim.buf.open(resolve(x.args))
  end
end)
cmd("only", 2, function() run_action("toggle_maximize_split") end)

cmd("nohlsearch", 3, function() run_action("clear_search") end)

cmd("undo", 1, function()
  local b = buf_or_fail()
  if not b:undo() then
    bad("Already at oldest change")
  end
end)
cmd("redo", 3, function()
  local b = buf_or_fail()
  if not b:redo() then
    bad("Already at newest change")
  end
end)

cmd("pwd", 2, function() say(gvim.dirs.cwd()) end)
cmd("file", 1, function()
  local b = buf_or_fail()
  local pos = D.cursor()
  say(string.format('"%s"%s %d lines --%d%%--', (b:path() or "") ~= "" and b:path() or "[No Name]",
    b:is_modified() and " [Modified]" or "", D.count(), math.floor((pos.line + 1) * 100 / D.count())))
end)
cmd("version", 3, function() say("gvim vi_mode") end)
cmd("help", 1, function()
  say("vi_mode: :w :q :wq :x :e :bn :bp :sp :vs :s/pat/rep/ :g/pat/d :d :y :j :> :< :m :t :sort :set :noh")
end)
cmd("registers", 3, function()
  local parts = {}
  for _, r in ipairs(Reg.list()) do
    local t = r.value.text:gsub("\n", "^J")
    parts[#parts + 1] = string.format('"%s %s', r.name, #t > 20 and t:sub(1, 20) .. "..." or t)
  end
  say(#parts > 0 and table.concat(parts, " | ") or "no registers")
end)
commands[#commands + 1] = { name = "display", min = 2, fn = commands[#commands].fn }

cmd("marks", 5, function()
  local parts = {}
  for name, m in pairs(S.marks) do
    parts[#parts + 1] = string.format("%s %d:%d", name, m.line + 1, m.col)
  end
  table.sort(parts)
  say(#parts > 0 and table.concat(parts, " | ") or "no marks")
end)

cmd("mark", 2, function(x)
  local name = x.args:match("^%a")
  if not name then
    bad("E191: Argument must be a letter or forward/backward quote")
  end
  S.marks[name] = { buf = D.buf().id, line = x.last or D.cursor().line, col = 0 }
end)
commands[#commands + 1] = { name = "k", min = 1, fn = commands[#commands].fn }

cmd("syntax", 2, function() say("syntax highlighting is always on") end)

cmd("set", 2, function(x)
  local b = buf_or_fail()
  for opt in x.args:gmatch("%S+") do
    local name, value = opt:match("^(%w+)=(.*)$")
    name = name or opt
    local on = true
    local bare = name
    if bare:sub(1, 2) == "no" then
      on, bare = false, bare:sub(3)
    end
    if bare == "number" or bare == "nu" then
      local ok, err = b:set_option("line_numbers", on)
      if not ok then
        bad("E: " .. tostring(err))
      end
    elseif bare == "wrap" then
      b:set_option("wrap", on)
    elseif bare == "tabstop" or bare == "ts" or bare == "shiftwidth" or bare == "sw" then
      local n = tonumber(value)
      if not n or n < 1 or n > 16 then
        bad("E487: Argument must be positive: " .. opt)
      end
      if bare:sub(1, 1) == "t" then
        b:set_option("tab_size", n)
      else
        S.settings.shift_width = n
      end
    elseif bare == "ignorecase" or bare == "ic" then
      S.settings.ignore_case = on
    elseif bare == "smartcase" or bare == "scs" then
      S.settings.smart_case = on
    elseif bare == "hlsearch" or bare == "hls" or bare == "incsearch" or bare == "is"
        or bare == "expandtab" or bare == "et" or bare == "autoindent" or bare == "ai" then
      -- accepted, nothing to change
    else
      bad("E518: Unknown option: " .. name)
    end
  end
end)

-- ---------- line commands ----------

local function line_range(x)
  local cur = D.cursor().line
  local first, last = x.first or cur, x.last or cur
  first = math.max(0, math.min(first, D.count() - 1))
  last = math.max(0, math.min(last, D.count() - 1))
  return first, last
end

cmd("delete", 1, function(x)
  local first, last = line_range(x)
  local name = x.args:match("^[%a\"_0-9%-+*]")
  if name and x.args:match("^%d") then
    last = math.min(D.count() - 1, last + tonumber(x.args:match("^%d+")) - 1)
    name = nil
  end
  Ops.delete({ kind = "line", first = first, last = last }, name, false)
end)

cmd("yank", 1, function(x)
  local first, last = line_range(x)
  local name = x.args:match("^%a")
  Ops.yank({ kind = "line", first = first, last = last }, name)
end)

cmd("join", 1, function(x)
  local first, last = line_range(x)
  if first == last then
    last = first + 1
  end
  if not Ops.join(first, last - first + 1, not x.bang) then
    bad("E: cannot join")
  end
end)

local function shift_cmd(dir)
  return function(x)
    local first, last = line_range(x)
    local n = 1 + #x.args:match("^[<>]*")
    Ops.shift(first, last, dir * n)
  end
end
cmd(">", 1, shift_cmd(1))
cmd("<", 1, shift_cmd(-1))

-- destination reads the address after :m and :t.
local function destination(x)
  if x.args == "" then
    bad("E14: Invalid address")
  end
  local a = parse_address(x.args, 1, D.cursor().line)
  if a == "%" then
    a = D.count() - 1
  end
  if not a then
    bad("E14: Invalid address")
  end
  return a
end

cmd("copy", 2, function(x)
  local first, last = line_range(x)
  local dest = destination(x)
  local text = table.concat(D.lines(first, last), "\n")
  if dest < 0 then
    D.replace(P(0, 0), P(0, 0), text .. "\n")
    D.set_cursor(D.first_nonblank(last - first))
  else
    local e = P(dest, D.len(dest))
    D.replace(e, e, "\n" .. text)
    D.set_cursor(D.first_nonblank(dest + 1 + last - first))
  end
end)
commands[#commands + 1] = { name = "t", min = 1, fn = commands[#commands].fn }

cmd("move", 1, function(x)
  local first, last = line_range(x)
  local dest = destination(x)
  if dest >= first and dest < last then
    bad("E134: Cannot move a range of lines into itself")
  end
  if dest == last or dest == first - 1 then
    return
  end
  local text = table.concat(D.lines(first, last), "\n")
  local n = last - first + 1
  local edits = {}
  -- delete the lines with one of their line breaks
  if last < D.count() - 1 then
    edits[#edits + 1] = { from = P(first, 0), to = P(last + 1, 0) }
  else
    edits[#edits + 1] = { from = P(first - 1, D.len(first - 1)), to = P(last, D.len(last)) }
  end
  if dest < 0 then
    edits[#edits + 1] = { from = P(0, 0), to = P(0, 0), text = text .. "\n" }
  else
    local e = P(dest, D.len(dest))
    edits[#edits + 1] = { from = e, to = e, text = "\n" .. text }
  end
  D.edit(edits)
  local top = dest < first and (dest + 1) or (dest - n + 1)
  D.set_cursor(D.first_nonblank(math.max(0, top + n - 1)))
end)

cmd("sort", 3, function(x)
  local first, last = line_range(x)
  local whole = x.first == nil
  if whole then
    first, last = 0, D.count() - 1
  end
  local lines = D.lines(first, last)
  local numeric, fold, unique = false, false, false
  for flag in x.args:gmatch("%a") do
    if flag == "n" then
      numeric = true
    elseif flag == "i" then
      fold = true
    elseif flag == "u" then
      unique = true
    end
  end
  local function key(s)
    if numeric then
      return tonumber(s:match("%-?%d+")) or -math.huge
    end
    return fold and s:lower() or s
  end
  local idx = {}
  for i, s in ipairs(lines) do
    idx[i] = { s = s, k = key(s), i = i }
  end
  table.sort(idx, function(a, b)
    if a.k ~= b.k then
      if x.bang then
        return a.k > b.k
      end
      return a.k < b.k
    end
    return a.i < b.i
  end)
  local out = {}
  for _, e in ipairs(idx) do
    if not (unique and #out > 0 and (fold and out[#out]:lower() == e.s:lower() or out[#out] == e.s)) then
      out[#out + 1] = e.s
    end
  end
  D.replace(P(first, 0), P(last, D.len(last)), table.concat(out, "\n"))
  D.set_cursor(D.first_nonblank(first))
end, true)

-- ---------- substitute ----------

-- split_delimited cuts s (after its delimiter) into at most n parts at the unescaped
-- delimiter. A backslash before the delimiter drops the backslash.
local function split_delimited(s, delim, n)
  local parts, cur = {}, {}
  local i = 1
  while i <= #s do
    local ch = s:sub(i, i)
    if ch == "\\" and i < #s then
      local nx = s:sub(i + 1, i + 1)
      if nx == delim then
        cur[#cur + 1] = delim
      else
        cur[#cur + 1] = ch .. nx
      end
      i = i + 2
    elseif ch == delim and #parts < n - 1 then
      parts[#parts + 1] = table.concat(cur)
      cur = {}
      i = i + 1
    else
      cur[#cur + 1] = ch
      i = i + 1
    end
  end
  parts[#parts + 1] = table.concat(cur)
  return parts
end

-- template turns a vi replacement (& \1 \r \t \\ \&) into a Go template.
local function template(rep)
  local out = {}
  local i = 1
  while i <= #rep do
    local ch = rep:sub(i, i)
    if ch == "\\" and i < #rep then
      local nx = rep:sub(i + 1, i + 1)
      i = i + 2
      if nx:find("%d") then
        out[#out + 1] = "${" .. nx .. "}"
      elseif nx == "r" or nx == "n" then
        out[#out + 1] = "\n"
      elseif nx == "t" then
        out[#out + 1] = "\t"
      elseif nx == "&" then
        out[#out + 1] = "&"
      elseif nx == "$" then
        out[#out + 1] = "$$"
      else
        out[#out + 1] = nx
      end
    elseif ch == "&" then
      out[#out + 1] = "${0}"
      i = i + 1
    elseif ch == "$" then
      out[#out + 1] = "$$"
      i = i + 1
    else
      out[#out + 1] = ch
      i = i + 1
    end
  end
  return table.concat(out)
end

-- substitute runs :s on the lines of the list (or first..last).
local function substitute(lines, args, bang)
  local delim = args:sub(1, 1)
  local pat, rep, flags
  if delim == "" or delim:find("[%w \"|]") then
    local ls = S.last_sub
    if not ls then
      bad("E35: No previous regular expression")
    end
    pat, rep, flags = ls.pattern, ls.rep, args
  else
    local parts = split_delimited(args:sub(2), delim, 3)
    pat, rep, flags = parts[1], parts[2] or "", parts[3] or ""
    if pat == "" then
      pat = S.last_search and S.last_search.pattern
      if not pat then
        bad("E35: No previous regular expression")
      end
    end
  end
  local global, icase = false, nil
  for f in flags:gmatch("%a") do
    if f == "g" then
      global = true
    elseif f == "i" then
      icase = true
    elseif f == "I" then
      icase = false
    end
  end
  S.last_sub = { pattern = pat, rep = rep }
  S.last_search = { pattern = pat, forward = true }
  local re2, cs = Search.compile(pat)
  if icase ~= nil then
    cs = not icase
  end
  local rx, err = gvim.re.compile(re2, cs and "" or "i")
  if not rx then
    bad("E486: bad pattern: " .. tostring(err))
  end
  local tmpl = template(rep)
  D.preload(lines[1], lines[#lines])
  local edits, total = {}, 0
  local shift, cursor_line = 0, nil
  local function newlines(str)
    return select(2, str:gsub("\n", "\n"))
  end
  for _, l in ipairs(lines) do
    local s = D.line(l)
    local new, n = rx:gsub(s, tmpl, global and -1 or 1)
    if n and n > 0 then
      total = total + n
      if new ~= s then
        edits[#edits + 1] = { from = P(l, 0), to = P(l, #s), text = new }
        cursor_line = l + shift + newlines(new)
        shift = shift + newlines(new)
      else
        cursor_line = l + shift
      end
    end
  end
  if total == 0 then
    bad("E486: Pattern not found: " .. pat)
  end
  if #edits > 0 then
    D.edit(edits)
  end
  D.set_cursor(D.first_nonblank(math.min(cursor_line or lines[#lines], D.count() - 1)))
  say(string.format("%d substitution%s on %d line%s", total, total == 1 and "" or "s", #edits, #edits == 1 and "" or "s"))
end

cmd("substitute", 1, function(x)
  local first, last = line_range(x)
  local lines = {}
  for l = first, last do
    lines[#lines + 1] = l
  end
  substitute(lines, x.args, x.bang)
end)
cmd("&", 1, function(x)
  local first, last = line_range(x)
  local lines = {}
  for l = first, last do
    lines[#lines + 1] = l
  end
  substitute(lines, x.args, x.bang)
end)

-- ---------- :global ----------

local function global(x, invert)
  local delim = x.args:sub(1, 1)
  if delim == "" or delim:find("[%w \"|]") then
    bad("E35: No previous regular expression")
  end
  local parts = split_delimited(x.args:sub(2), delim, 2)
  local pat, command = parts[1], parts[2] or "p"
  if pat == "" then
    pat = S.last_search and S.last_search.pattern or ""
  end
  if pat == "" then
    bad("E35: No previous regular expression")
  end
  S.last_search = { pattern = pat, forward = true }
  local first, last = 0, D.count() - 1
  if x.first then
    first, last = line_range(x)
  end
  local re2, cs = Search.compile(pat)
  local ok, found = pcall(function()
    local lines = {}
    local from = first
    -- find_lines scans in time-boxed chunks; the plain search lists the matches of the range
    local ranges, err = D.buf():find(re2, { regex = true, case_sensitive = cs, max = 100000,
      within = { from = P(first, 0), to = P(last, D.len(last)) } })
    if not ranges then
      error({ find = Search.find_error(err) }, 0)
    end
    -- keyed from the first line: a table keyed by line 900000 is padded with nils up to it
    local hit = {}
    for _, r in ipairs(ranges) do
      hit[r.from.line - first + 1] = true
    end
    for l = from, last do
      if (hit[l - first + 1] == true) ~= invert then
        lines[#lines + 1] = l
      end
    end
    return lines
  end)
  if not ok then
    bad(type(found) == "table" and found.find or "E: " .. tostring(found))
  end
  if #found == 0 then
    bad("E486: Pattern not found: " .. pat)
  end
  command = command:gsub("^%s+", "")
  local name = command:match("^%a+") or command:match("^[<>]") or "p"
  local rest = command:sub(#name + 1)
  if name == "d" or name == "delete" then
    local text = {}
    for _, l in ipairs(found) do
      text[#text + 1] = D.line(l)
    end
    Reg.set(nil, table.concat(text, "\n") .. "\n", "line", "d")
    -- one edit for each run of neighbouring lines
    local n = D.count()
    local edits = {}
    local i = 1
    while i <= #found do
      local a = found[i]
      local b = a
      while found[i + 1] == b + 1 do
        i = i + 1
        b = found[i]
      end
      i = i + 1
      if b < n - 1 then
        edits[#edits + 1] = { from = P(a, 0), to = P(b + 1, 0) }
      elseif a > 0 then
        edits[#edits + 1] = { from = P(a - 1, D.len(a - 1)), to = P(b, D.len(b)) }
      else
        edits[#edits + 1] = { from = P(0, 0), to = P(b, D.len(b)) }
      end
    end
    D.edit(edits)
    D.set_cursor(D.first_nonblank(math.min(found[1], D.count() - 1)))
    say(string.format("%d fewer line%s", #found, #found == 1 and "" or "s"))
  elseif name == "s" or name == "substitute" then
    substitute(found, rest, false)
  elseif name == "y" or name == "yank" then
    local text = {}
    for _, l in ipairs(found) do
      text[#text + 1] = D.line(l)
    end
    Reg.set(nil, table.concat(text, "\n") .. "\n", "line", "y")
  elseif name == "j" or name == "join" then
    for i = #found, 1, -1 do
      Ops.join(found[i], 2, true)
    end
  elseif name == ">" or name == "<" then
    for _, l in ipairs(found) do
      Ops.shift(l, l, name == ">" and 1 or -1)
    end
  elseif name == "p" or name == "print" then
    D.set_cursor(D.first_nonblank(found[#found]))
    say(string.format("%d matching line%s", #found, #found == 1 and "" or "s"))
  else
    bad("E: :global does not run :" .. name)
  end
end

cmd("global", 1, function(x) global(x, x.bang) end, true)
cmd("vglobal", 1, function(x) global(x, true) end, true)

-- ---------- dispatch ----------

local by_name = {}
commands.by_name = by_name
for _, c in ipairs(commands) do
  by_name[c.name] = c
end

-- find resolves a command name: an exact name, then the abbreviation the table allows. The
-- classic one-letter aliases win over ambiguity.
local function find(name)
  if by_name[name] then
    return by_name[name]
  end
  local match
  local n = 0
  for _, c in ipairs(commands) do
    if #name >= c.min and c.name:sub(1, #name) == name then
      match = c
      n = n + 1
    end
  end
  if n == 1 then
    return match
  end
  return nil
end

-- run executes one command line (without the colon).
function M.run(line)
  line = line:gsub("^[%s:]+", "")
  if line == "" then
    return
  end
  local cur = D.cursor().line
  local first, last, rest, given = parse_range(line, cur)
  rest = rest:gsub("^%s+", "")
  if rest == "" then
    if given and last then
      D.set_cursor(D.first_nonblank(math.max(0, math.min(last, D.count() - 1))))
    end
    return
  end
  local name, tail
  if rest:find("^[<>&=#!]") then
    name, tail = rest:sub(1, 1), rest:sub(2)
  else
    name = rest:match("^%a+")
    if not name then
      bad("E492: Not an editor command: " .. rest)
    end
    tail = rest:sub(#name + 1)
  end
  if name == "!" then
    bad("E: shell commands are not supported")
  end
  local c = find(name)
  if not c then
    bad("E492: Not an editor command: " .. name)
  end
  local bang = false
  if tail:sub(1, 1) == "!" then
    bang, tail = true, tail:sub(2)
  end
  local args = tail:gsub("^%s+", "")
  if c.all and not given then
    first, last = nil, nil
  end
  c.fn({ first = first, last = last, bang = bang, args = args, name = c.name })
end

return M
