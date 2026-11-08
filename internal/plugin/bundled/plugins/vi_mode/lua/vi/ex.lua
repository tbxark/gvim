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

local function line_range(x)
  local cur = D.cursor().line
  local first, last = x.first or cur, x.last or cur
  first = math.max(0, math.min(first, D.count() - 1))
  last = math.max(0, math.min(last, D.count() - 1))
  return first, last
end

-- write_lines writes the lines of a range to a file (:N,Mw file) or adds them to its end
-- (:w >> file). The buffer is not saved, and it keeps its name.
local function write_lines(x, path, append)
  if path == "" then
    bad(x.first and "E140: Use ! to write partial buffer" or "E471: Argument required")
  end
  local first, last = 0, D.count() - 1
  if x.first then
    first, last = line_range(x)
  end
  local ok, err = gvim.fs.write(resolve(path), table.concat(D.lines(first, last), "\n") .. "\n", { append = append })
  if not ok then
    bad("E212: Can't open file for writing: " .. tostring(err))
  end
  say(string.format('"%s" %d line%s %s', path, last - first + 1, last == first and "" or "s",
    append and "appended" or "written"))
end

cmd("write", 1, function(x)
  local b = buf_or_fail()
  local to = x.args:match("^>>%s*(.*)$")
  if to or x.first then
    write_lines(x, to or x.args, to ~= nil)
    return
  end
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

cmd("nohlsearch", 3, function()
  run_action("clear_search")
  Search.clear_highlight()
end)

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
-- show_lines lists lines in a popup (Esc or ctrl+c closes it): a status row holds one.
local function show_lines(title, lines)
  gvim.ui.popup { title = title, lines = lines, anchor = "center", width = 80 }
end

cmd("help", 1, function()
  show_lines("vi_mode", {
    "Files     :w :w file :w! :q :q! :wq :x :e file :e!",
    "          :r file :saveas :enew :bn :bp",
    "          :b N :ls :bd",
    "Windows   :sp :vs :new :vnew :only",
    "Lines     :d :y :j :> :< :m N :t N :sort",
    "          :normal is not supported",
    "Text      :s/pat/rep/flags :& :&& :g/pat/cmd",
    "          :v/pat/cmd   (flags g i I n c)",
    "Settings  :set number wrap ts= sw= et noet ic",
    "          [no]scs hls   :noh hides the matches",
    "Marks     :mark x :marks      Registers  :reg",
    "Ranges    :N :N,M :% :$ :'a,'b :/pat/ :.,+3",
    "          Esc or ctrl+c closes this list",
    "Not here  gn g; gq: not supported (each says so)",
    "More      vi_mode/README.md lists every command",
    "          and what differs from Vim",
  })
end)
cmd("registers", 3, function()
  local lines = { "Type  Name  Content" }
  for _, r in ipairs(Reg.list()) do
    local t = r.value.text:gsub("\n", "^J")
    if #t > 60 then
      t = t:sub(1, 60) .. "..."
    end
    local kind = r.value.kind == "line" and "l" or (r.value.kind == "block" and "b" or "c")
    lines[#lines + 1] = string.format('  %s   "%s    %s', kind, r.name, t)
  end
  if #lines == 1 then
    say("no registers")
    return
  end
  show_lines("Registers", lines)
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
    elseif bare == "hlsearch" or bare == "hls" then
      S.settings.hlsearch = on
      Search.show()
    elseif bare == "expandtab" or bare == "et" then
      S.settings.expand_tab = on
    elseif bare == "incsearch" or bare == "is" or bare == "autoindent" or bare == "ai" then
      bad("E: the option '" .. name .. "' is not supported (the editor decides)")
    else
      bad("E518: Unknown option: " .. name)
    end
  end
end)

-- ---------- line commands ----------

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
  D.set_cursor(D.first_nonblank(first))
end)

local function shift_cmd(dir)
  return function(x)
    local first, last = line_range(x)
    local n = 1 + #x.args:match("^[<>]*")
    Ops.shift(first, last, dir * n)
    D.set_cursor(D.first_nonblank(last))
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
-- delimiter. A backslash before the delimiter drops the backslash, except in the last part,
-- which is a command or flags that read their own escapes.
local function split_delimited(s, delim, n)
  local parts, cur = {}, {}
  local i = 1
  while i <= #s do
    local ch = s:sub(i, i)
    if ch == "\\" and i < #s then
      local nx = s:sub(i + 1, i + 1)
      if nx == delim and #parts < n - 1 then
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

-- rep_parts reads a vi replacement into parts: {s=text}, {g=n} for a group (0 is the
-- whole match) and {case=c} for a change of case (\u \U \l \L \e \E). A Go template cannot
-- change case, so a replacement that does is expanded by expand() for each match.
local function rep_parts(rep)
  local parts, lit = {}, {}
  local function flush()
    if #lit > 0 then
      parts[#parts + 1] = { s = table.concat(lit) }
      lit = {}
    end
  end
  local i = 1
  while i <= #rep do
    local ch = rep:sub(i, i)
    if ch == "\\" and i < #rep then
      local nx = rep:sub(i + 1, i + 1)
      i = i + 2
      if nx:find("%d") then
        flush()
        parts[#parts + 1] = { g = tonumber(nx) }
      elseif nx:find("[uUlLeE]") then
        flush()
        parts[#parts + 1] = { case = nx }
      elseif nx == "r" or nx == "n" then
        lit[#lit + 1] = "\n"
      elseif nx == "t" then
        lit[#lit + 1] = "\t"
      else
        lit[#lit + 1] = nx
      end
    elseif ch == "&" then
      flush()
      parts[#parts + 1] = { g = 0 }
      i = i + 1
    else
      lit[#lit + 1] = ch
      i = i + 1
    end
  end
  flush()
  return parts
end

-- expand builds the replacement of one match. caps are the groups of the pattern wrapped in
-- one more group, so the whole match comes first; a group that did not take part is false.
local function expand(parts, caps)
  local out, mode, once = {}, nil, nil
  local function emit(s)
    if s == "" then
      return
    end
    local n = T.next(s, 0)
    local head, tail = s:sub(1, n), s:sub(n + 1)
    if mode == "U" then
      tail = T.upper(tail)
    elseif mode == "L" then
      tail = T.lower(tail)
    end
    if once then
      head = once == "u" and T.upper(head) or T.lower(head)
      once = nil
    elseif mode == "U" then
      head = T.upper(head)
    elseif mode == "L" then
      head = T.lower(head)
    end
    out[#out + 1] = head .. tail
  end
  for _, p in ipairs(parts) do
    if p.case then
      if p.case == "u" or p.case == "l" then
        once = p.case
      elseif p.case == "U" or p.case == "L" then
        mode = p.case
      else
        mode, once = nil, nil
      end
    else
      emit(p.g and (caps[p.g + 1] or "") or p.s)
    end
  end
  return table.concat(out)
end

-- split_bar cuts a command line at its first "|" that is not escaped.
local function split_bar(s)
  local i = 1
  while i <= #s do
    local ch = s:sub(i, i)
    if ch == "\\" then
      i = i + 1
    elseif ch == "|" then
      return s:sub(1, i - 1), s:sub(i + 1)
    end
    i = i + 1
  end
  return s
end

-- substitute runs :s on the lines of the list (or first..last). It returns the command that
-- follows a "|" after the flags, or nil.
local function substitute(lines, args, bang)
  local delim = args:sub(1, 1)
  local pat, rep, flags
  if delim == "" or delim:find("[%w \"|&]") then
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
  local tail
  flags, tail = split_bar(flags)
  if flags:sub(1, 1) == "&" then
    flags = (S.last_sub and S.last_sub.flags or "") .. flags:sub(2)
  end
  if rep:sub(1, 2) == "\\=" then
    bad("E: a replacement that is an expression (\\=) is not supported")
  end
  local global, icase, count_only, confirm = false, nil, false, false
  for f in flags:gmatch("%a") do
    if f == "g" then
      global = true
    elseif f == "i" then
      icase = true
    elseif f == "I" then
      icase = false
    elseif f == "n" then
      count_only = true
    elseif f == "c" then
      confirm = true
    end
  end
  S.last_sub = { pattern = pat, rep = rep, flags = flags }
  S.last_search = { pattern = pat, forward = true }
  Search.highlight(pat)
  local re2, cs = Search.compile(pat)
  if icase ~= nil then
    cs = not icase
  end
  local parts = rep_parts(rep)
  local cased = false
  for _, p in ipairs(parts) do
    cased = cased or p.case ~= nil
  end
  -- most replacements are a Go template; the others are built match by match, with the
  -- pattern wrapped in a group of its own to hand the whole match over
  local plain = not (count_only or confirm or cased)
  local rx, err = gvim.re.compile(plain and re2 or ("(" .. re2 .. ")"), cs and "" or "i")
  if not rx then
    bad("E486: bad pattern: " .. tostring(err))
  end
  local tmpl = template(rep)
  local limit = global and -1 or 1
  D.preload(lines[1], lines[#lines])
  local edits, total, matched = {}, 0, 0
  local shift, cursor_line = 0, nil
  local function newlines(str)
    return select(2, str:gsub("\n", "\n"))
  end
  local yes_all, stop = false, false
  local function ask(l, whole)
    if stop then
      return false
    elseif yes_all then
      return true
    end
    D.set_cursor(P(l, 0))
    local id = gvim.ui.choose(string.format('replace "%s" with "%s" (line %d)?', whole, rep, l + 1), {
      { id = "y", label = "yes" }, { id = "n", label = "no" }, { id = "a", label = "all" },
      { id = "q", label = "quit" }, { id = "l", label = "last: replace this one and quit" },
    })
    if id == "y" then
      return true
    elseif id == "n" then
      return false
    elseif id == "a" then
      yes_all = true
      return true
    end
    stop = true
    return id == "l"
  end
  for _, l in ipairs(lines) do
    local s = D.line(l)
    local new, n
    if plain then
      new, n = rx:gsub(s, tmpl, limit)
      matched = matched + (n or 0)
    elseif count_only then
      new, n = rx:gsub(s, function() return false end, limit)
      if n and n > 0 then
        matched = matched + n
        total = total + 1
      end
      n = 0
    else
      local seen, yes = {}, {}
      local _, found = rx:gsub(s, function(whole)
        seen[#seen + 1] = whole
        return false
      end, limit)
      for i = 1, found or 0 do
        yes[i] = not confirm or ask(l, seen[i])
      end
      local k, done = 0, 0
      new = rx:gsub(s, function(...)
        k = k + 1
        if yes[k] then
          done = done + 1
          return expand(parts, { ... })
        end
        return false
      end, limit)
      matched = matched + (found or 0)
      n = done
    end
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
  if matched == 0 then
    bad("E486: Pattern not found: " .. pat)
  end
  if count_only then
    say(string.format("%d match%s on %d line%s", matched, matched == 1 and "" or "es", total, total == 1 and "" or "s"))
    return tail
  end
  if #edits > 0 then
    D.edit(edits)
  end
  if total > 0 then
    D.set_cursor(D.first_nonblank(math.min(cursor_line or lines[#lines], D.count() - 1)))
  end
  say(string.format("%d substitution%s on %d line%s", total, total == 1 and "" or "s", #edits, #edits == 1 and "" or "s"))
  return tail
end

-- run is M.run, which the commands that chain another one after a "|" call.
local run

local function substitute_cmd(x)
  local first, last = line_range(x)
  local lines = {}
  for l = first, last do
    lines[#lines + 1] = l
  end
  local tail = substitute(lines, x.args, x.bang)
  if tail and tail:find("%S") then
    run(tail)
  end
end

cmd("substitute", 1, substitute_cmd)
cmd("&", 1, substitute_cmd)

-- ---------- :global ----------

-- transfer moves (or copies) the lines that :global found to line 0, to the end or, for a
-- copy, after themselves. The lines are put back as they would be after the command had run
-- for each of them in turn.
local function transfer(found, copy, dest)
  dest = dest:match("^%s*(%S+)%s*$")
  if dest ~= "0" and dest ~= "$" and not (copy and dest == ".") then
    bad("E: :global moves and copies lines to 0 or $ only" .. (copy and " (or . for a copy)" or ""))
  end
  local n = D.count()
  local set = {}
  for _, l in ipairs(found) do
    set[l] = true
  end
  local lo = (dest == "0") and 0 or found[1]
  local hi = (dest == "0") and found[#found] or ((dest == "$") and n - 1 or found[#found])
  local old = D.lines(lo, hi)
  local new, moved, cursor = {}, {}, nil
  for l = lo, hi do
    local s = old[l - lo + 1]
    if set[l] then
      moved[#moved + 1] = s
    end
    if dest == "." then
      new[#new + 1] = s
      if set[l] then
        new[#new + 1] = s
        cursor = lo + #new - 1
      end
    elseif copy or not set[l] then
      new[#new + 1] = s
    end
  end
  if dest == "0" then
    local top = {}
    for i = #moved, 1, -1 do
      top[#top + 1] = moved[i]
    end
    for _, s in ipairs(new) do
      top[#top + 1] = s
    end
    new, cursor = top, 0
  elseif dest == "$" then
    for _, s in ipairs(moved) do
      new[#new + 1] = s
    end
    cursor = lo + #new - 1
  end
  D.replace(P(lo, 0), P(hi, D.len(hi)), table.concat(new, "\n"))
  D.set_cursor(D.first_nonblank(cursor))
end

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
  Search.highlight(pat)
  local first, last = 0, D.count() - 1
  if x.first then
    first, last = line_range(x)
  end
  local re2, cs = Search.compile(pat)
  local info = D.buf():info()
  if info and info.large then
    bad("no search in large-file mode")
  end
  local ok, found = pcall(function()
    local lines = {}
    -- find_lines scans in time-boxed chunks. It matches whole lines, so a pattern that
    -- matches nothing (^ $ ^$) finds them, which a list of match ranges would not.
    -- keyed from the first line: a table keyed by line 900000 is padded with nils up to it
    local hit = {}
    local from = first
    while from do
      local got, nxt = D.buf():find_lines(re2, { regex = true, case_sensitive = cs, max = 100000,
        from = from, to = last + 1, budget_ms = 100 })
      if not got then
        error({ find = Search.find_error(nxt) }, 0)
      end
      for _, l in ipairs(got) do
        hit[l - first + 1] = true
      end
      from = nxt
    end
    for l = first, last do
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
  -- global_name resolves the command of a :g line to the name the branches below test: the
  -- shortest names Vim takes inside :g (d, del, m, mo, t, co, j, jo, y, ya, p, s).
  local global_names = {{"delete", 1}, {"join", 2}, {"move", 2}, {"copy", 2}, {"yank", 2}, {"print", 1}, {"substitute", 1}}
  local function global_name(name)
    for _, g in ipairs(global_names) do
      if #name >= g[2] and g[1]:sub(1, #name) == name then
        return g[1]
      end
    end
    return name
  end
  local function apply(command)
    command = command:gsub("^%s+", "")
    local raw = command:match("^%a+") or command:match("^[<>]")
    if not raw then
      if command:find("%S") then
        -- an address (+1d, .d, 'a,'bd) or a symbol: the lines move while the command runs
        bad("E: :global does not run :" .. command)
      end
      raw = "p"
    end
    local name = global_name(raw)
    local rest = command:sub(#raw + 1)
    local chain
    if name == "s" or name == "substitute" then
      local tail = substitute(found, rest, false)
      if tail and tail:find("%S") then
        apply(tail)
      end
      return
    end
    rest, chain = split_bar(rest)
    if chain and chain:find("%S") and (name == "d" or name == "delete" or name == "j" or name == "join"
        or name == "m" or name == "move" or name == "t" or name == "copy") then
      bad("E: :global cannot run a command after :" .. name)
    end
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
        elseif D.trailing() then
          edits[#edits + 1] = { from = P(0, 0), to = P(b + 1, 0) }
        else
          edits[#edits + 1] = { from = P(0, 0), to = P(b, D.len(b)) }
        end
      end
      D.edit(edits)
      -- vim leaves the caret on the line that took the place of the last deleted one: that line
      -- was found[#found], and every line found before it is gone
      D.set_cursor(D.first_nonblank(math.min(found[#found] - (#found - 1), D.count() - 1)))
      say(string.format("%d fewer line%s", #found, #found == 1 and "" or "s"))
    elseif name == "y" or name == "yank" then
      local text = {}
      for _, l in ipairs(found) do
        text[#text + 1] = D.line(l)
      end
      Reg.set(nil, table.concat(text, "\n") .. "\n", "line", "y")
    elseif name == "j" or name == "join" then
      -- forward, as Vim does: a line that a join takes into the line before it is no longer a
      -- match; the caret ends on the last match, where it is after the joins before it
      local removed, swallowed, cursor = 0, {}, nil
      for _, l in ipairs(found) do
        local at = l - removed
        cursor = at
        if not swallowed[l] and at < D.count() - 1 then
          Ops.join(at, 2, true)
          removed = removed + 1
          swallowed[l + 1] = true
        end
      end
      if cursor then
        D.set_cursor(D.first_nonblank(math.min(cursor, D.count() - 1)))
      end
    elseif name == ">" or name == "<" then
      for _, l in ipairs(found) do
        Ops.shift(l, l, name == ">" and 1 or -1)
      end
    elseif name == "m" or name == "move" then
      transfer(found, false, rest)
    elseif name == "t" or name == "copy" then
      transfer(found, true, rest)
    elseif name == "p" or name == "print" then
      D.set_cursor(D.first_nonblank(found[#found]))
      say(string.format("%d matching line%s", #found, #found == 1 and "" or "s"))
    else
      bad("E: :global does not run :" .. name)
    end
    if chain and chain:find("%S") then
      apply(chain)
    end
  end
  apply(command)
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

-- the commands that read a "|" as part of their own argument
local keeps_bar = { substitute = true, ["&"] = true, global = true, vglobal = true }

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
  -- "|" ends a command and starts another, except in the commands that take the whole line
  local chain
  if not keeps_bar[c.name] then
    args, chain = split_bar(args)
  end
  c.fn({ first = first, last = last, bang = bang, args = args, name = c.name })
  if chain and chain:find("%S") then
    M.run(chain)
  end
end
run = M.run

return M
