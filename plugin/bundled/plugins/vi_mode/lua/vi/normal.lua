-- The vi command grammar. Every key press the modes hand to the plugin goes through feed.
-- The commands are read by a coroutine, so a command that needs more keys (an operator
-- and its motion, a count, f and its character, a register, a text object) is written as
-- a straight line of getkey calls instead of a state machine: the coroutine simply waits
-- for the next key. A command is parsed into a table and then run by exec, which is also
-- what . runs again.
local gvim = require "gvim"
local D = require "vi.doc"
local T = require "vi.text"
local Reg = require "vi.reg"
local Ops = require "vi.ops"
local Mo = require "vi.motion"
local Obj = require "vi.textobj"
local Search = require "vi.search"
local Ex = require "vi.ex"
local S = require "vi.state"

local M = {}

local CANCEL = D.CANCEL
local MAX_COUNT = 100000
local MAX_ACTION = 10000 -- the most gvim.command.run repeats an action

-- run_action runs an editor action count times. A count over what command.run takes is cut
-- down: moving by 99999999 lines ends at the last line all the same.
local function run_action(action, count)
  local args
  if count and count > 1 then
    args = { count = math.min(count, MAX_ACTION) }
  end
  return gvim.command.run(action, args)
end

local function P(l, c) return { line = l, col = c } end

-- hooks are filled in by init.lua: set_mode(name) switches the editor's input mode and
-- status() redraws the status row.
M.hooks = {
  set_mode = function() end,
  status = function() end,
}

local function cancel() error(CANCEL, 0) end

-- fail abandons the command quietly (vi beeps).
local function fail(msg) D.fail(msg) end

-- ---------- reading keys ----------

local reader -- the coroutine that reads commands
local busy = false -- the reader is waiting for a prompt or the editor, not for a key
local busy_since = 0 -- when it started to wait (gvim.time.monotonic_ms)
local BUSY_LIMIT = 120000 -- a wait that never ends is given up after this many ms

-- display is how a key shows in the status row while the command is pending.
local function display(k)
  if #k == 1 or T.count(k, 0, #k) == 1 then
    return k
  end
  local name = k:gsub("^ctrl%+", "^")
  if name == "escape" then
    return "<Esc>"
  end
  return "<" .. name .. ">"
end

local function getkey()
  local k = coroutine.yield()
  if not D.begin() then
    S.pending = ""
    if gvim.buf.current() then
      gvim.notify("vi: this file is too big to edit until its index is ready", "warn")
    end
    cancel()
  end
  S.pending = S.pending .. display(k)
  return k
end

local function is_char(k)
  return T.count(k, 0, #k) == 1
end

-- getchar reads a key that must be a character (for f, t, r, marks, registers).
local function getchar(allow_enter)
  local k = getkey()
  if k == "escape" or k == "ctrl+[" then
    cancel()
  end
  if allow_enter and k == "enter" then
    return "\n"
  end
  if not is_char(k) then
    cancel()
  end
  return k
end

-- await runs fn, which may wait (for a prompt, for a save to finish), with the reader
-- marked busy: keys that arrive meanwhile are dropped, because resuming the coroutine with a
-- key would hand the key to the wait instead of to the command. Anything that can wait must
-- run through here.
local function await(fn, ...)
  busy = true
  busy_since = gvim.time.monotonic_ms()
  local ok, a, b = pcall(fn, ...)
  busy = false
  if not ok then
    error(a, 0)
  end
  return a, b
end

-- ex runs a : command line. Commands save, open and revert, which wait for the editor.
local function ex(line)
  await(Ex.run, line)
  D.begin()
end

local MAX_HISTORY = 100

-- remember adds a line to the history of the prompts of kind ("ex" or "search"): a line that is
-- already there moves to the end.
local function remember(kind, text)
  local list = S.history[kind]
  for i = #list, 1, -1 do
    if list[i] == text then
      table.remove(list, i)
    end
  end
  list[#list + 1] = text
  while #list > MAX_HISTORY do
    table.remove(list, 1)
  end
end

-- prompt reads a line from the user. nil: cancelled. kind names the history Up and Down walk.
local function prompt(label, default, kind)
  local text = await(function()
    return gvim.ui.input { prompt = label, default = default or "", select_all = false, history = S.history[kind] }
  end)
  D.begin()
  if text and text ~= "" and text ~= default then
    remember(kind, text)
  end
  return text
end

-- read_count reads the digits of a count that starts with k and returns the count and the
-- key after it.
local function read_count(k, count)
  while k:find("^[1-9]$") or (k == "0" and count) do
    count = math.min((count or 0) * 10 + tonumber(k), MAX_COUNT)
    k = getkey()
  end
  return count, k
end

-- ---------- insert mode ----------

-- clamp_cursor puts the normal-mode caret on a character.
local function clamp_cursor()
  local cur = D.cursor()
  local s = D.line(cur.line)
  if s and cur.col >= #s and #s > 0 then
    D.set_cursor(P(cur.line, T.last(s)), false)
  end
end

-- apply_text inserts text count times at the caret and leaves the caret on its last
-- character (what escaping from an insert does).
local function apply_text(text, count)
  if not text or text == "" then
    return
  end
  local all = string.rep(text, count)
  local cur = D.cursor()
  D.replace(cur, cur, all)
  local e = D.advance(cur, all)
  local s = D.line(e.line)
  D.set_cursor(P(e.line, T.prev(s, e.col)))
end

-- open_below opens a line under the caret's line and puts the caret in it. It uses the
-- editor's own newline, which indents the new line the way typing Enter would.
local function open_below()
  local cur = D.cursor()
  D.set_cursor(P(cur.line, D.len(cur.line)))
  local ok, err = gvim.command.run("insert_newline")
  if not ok then
    D.fail("vi: " .. tostring(err))
  end
  D.begin()
end

local function open_above()
  local cur = D.cursor()
  local ind = T.indent(D.line(cur.line))
  D.replace(P(cur.line, 0), P(cur.line, 0), ind .. "\n")
  D.set_cursor(P(cur.line, #ind))
end

-- enter_insert starts an insert. cmd is the command that asked for it, which learns what
-- was typed when the insert ends. When a change is being replayed the text is put in
-- directly and the mode stays normal.
local function enter_insert(cmd, count, opts)
  count = count or 1
  opts = opts or {}
  if S.replay then
    local text = S.replay.text
    if text and text ~= "" then
      if opts.line_open then
        apply_text(text, 1)
        for _ = 2, count do
          if opts.line_open == "below" then
            open_below()
          else
            open_above()
          end
          apply_text(text, 1)
        end
      else
        apply_text(text, count)
        if opts.block then
          M.finish_block_insert({ block = opts.block }, text)
        end
      end
    else
      if opts.line_open then
        for _ = 2, count do
          if opts.line_open == "below" then
            open_below()
          else
            open_above()
          end
        end
      end
      local cur = D.cursor()
      local s = D.line(cur.line)
      if cur.col > 0 and cur.col >= #s then
        D.set_cursor(P(cur.line, T.prev(s, cur.col)))
      end
    end
    return
  end
  local cur = D.cursor()
  local s = D.line(cur.line)
  S.ins = {
    cmd = cmd and (cmd.owner or cmd), count = count, start = cur, head = s:sub(1, cur.col), tail = s:sub(cur.col + 1),
    line_open = opts.line_open, block = opts.block,
  }
  S.mode = "insert"
  M.hooks.set_mode("vi_insert")
  M.hooks.status()
end

-- insert_broken tells the insert that the caret moved by other means than typing.
function M.insert_broken()
  if S.ins then
    S.ins.broken = true
  end
end

-- leave_insert ends the insert: it works out what was typed (for ., counts and block
-- inserts), steps the caret left like vi does, and goes back to normal mode.
function M.leave_insert()
  local ins = S.ins
  S.ins = nil
  S.mode = "normal"
  if not D.begin() then
    M.hooks.set_mode("vi_normal")
    M.hooks.status()
    return
  end
  local ok, err = pcall(function()
    local cur = D.cursor()
    local text
    if ins and not ins.broken and D.le(ins.start, cur) then
      local s = D.line(cur.line)
      local head_ok = D.line(ins.start.line):sub(1, ins.start.col) == ins.head
      local tail_ok = ins.tail == "" or s:sub(-#ins.tail) == ins.tail
      if head_ok and tail_ok then
        text = D.text(ins.start, cur)
      end
    end
    if text and text ~= "" and ins then
      if ins.cmd then
        ins.cmd.insert_text = text
      end
      if ins.block then
        M.finish_block_insert(ins, text)
        return
      end
      if ins.count > 1 then
        if ins.line_open then
          for _ = 2, ins.count do
            if ins.line_open == "below" then
              open_below()
            else
              open_above()
            end
            local c2 = D.cursor()
            D.replace(c2, c2, text)
            D.set_cursor(D.advance(c2, text))
          end
        else
          local more = string.rep(text, ins.count - 1)
          D.replace(cur, cur, more)
          D.set_cursor(D.advance(cur, more))
        end
      end
    elseif ins and ins.line_open and ins.count > 1 and text == "" then
      -- 3o<Esc>: nothing was typed, and the lines are opened all the same
      for _ = 2, ins.count do
        if ins.line_open == "below" then
          open_below()
        else
          open_above()
        end
      end
    elseif ins and ins.cmd and not text then
      ins.cmd.insert_text = nil
    end
    local c = D.cursor()
    S.last_insert = c
    local line = D.line(c.line)
    if c.col > 0 then
      D.set_cursor(P(c.line, T.prev(line, math.min(c.col, #line))))
    end
  end)
  M.hooks.set_mode("vi_normal")
  M.hooks.status()
  if not ok and err ~= CANCEL then
    error(err, 0)
  end
end

-- finish_block_insert copies the text typed on the first line of a block to the others.
function M.finish_block_insert(ins, text)
  local b = ins.block
  if text:find("\n", 1, true) then
    return
  end
  local edits = {}
  for l = b.first + 1, b.last do
    local s = D.line(l)
    local col
    if b.append then
      if b.to_eol then
        col = #s
      else
        local have = T.count(s, 0, #s)
        if have < b.right then
          edits[#edits + 1] = { from = P(l, #s), to = P(l, #s), text = string.rep(" ", b.right - have) .. text }
          col = nil
        else
          col = T.advance(s, 0, b.right)
        end
      end
    else
      if T.count(s, 0, #s) >= b.left then
        col = T.advance(s, 0, b.left)
      end
    end
    if col then
      edits[#edits + 1] = { from = P(l, col), to = P(l, col), text = text }
    end
  end
  if #edits > 0 then
    D.edit(edits)
  end
  local first = P(b.first, T.advance(D.line(b.first), 0, b.left))
  D.set_cursor(D.on_char(first))
end

-- ---------- visual mode ----------

local VNAME = { v = "VISUAL", V = "VISUAL LINE", b = "VISUAL BLOCK" }
M.VNAME = VNAME

local function char_index(l, col)
  local s = D.line(l)
  return T.count(s, 0, math.min(col, #s))
end

-- visual_range is the range the visual area covers.
local function visual_range()
  local V = S.vis
  local a, h = V.anchor, V.head
  local lo, hi = D.min(a, h), D.max(a, h)
  if V.mode == "V" then
    return { kind = "line", first = lo.line, last = hi.line }, lo, hi
  elseif V.mode == "b" then
    local ca, ch = char_index(a.line, a.col), char_index(h.line, h.col)
    local left, right = math.min(ca, ch), math.max(ca, ch) + 1
    if V.to_eol then
      right = nil
    end
    return { kind = "block", first = lo.line, last = hi.line, left = left, right = right }, lo, hi
  end
  return { kind = "char", from = lo, to = D.after(hi) }, lo, hi
end

local block_ns

-- show_block paints the block area with highlights (the editor has no block selection).
local function show_block(rng)
  local ok = pcall(function()
    if not block_ns then
      block_ns = gvim.decor.namespace("block")
    end
    if not rng then
      block_ns:clear()
      return
    end
    local items = {}
    for l = rng.first, rng.last do
      local s = D.line(l)
      local a = T.advance(s, 0, rng.left)
      local b = rng.right and T.advance(s, 0, rng.right) or #s
      if b > a then
        items[#items + 1] = { kind = "highlight", range = { from = P(l, a), to = P(l, b) }, style = { bg = "editor.selection_bg" } }
      end
    end
    block_ns:set(D.buf(), items)
  end)
  return ok
end

-- apply_visual shows the visual area in the editor: a selection, or highlights for a block.
local function apply_visual()
  local V = S.vis
  local a, h = V.anchor, V.head
  local buf = D.buf()
  if V.mode == "b" then
    local rng = visual_range()
    show_block(rng)
    D.set_cursor(h)
    return
  end
  show_block(nil)
  if V.mode == "V" then
    if a.line <= h.line then
      buf:set_cursor(P(a.line, 0), { reveal = false })
      buf:set_cursor(P(h.line, D.len(h.line)), { extend = true })
    else
      buf:set_cursor(P(a.line, D.len(a.line)), { reveal = false })
      buf:set_cursor(P(h.line, 0), { extend = true })
    end
    return
  end
  if D.le(a, h) then
    buf:set_cursor(a, { reveal = false })
    buf:set_cursor(D.after(h), { extend = true })
  else
    buf:set_cursor(D.after(a), { reveal = false })
    buf:set_cursor(h, { extend = true })
  end
end

local function enter_visual(mode)
  local cur = D.cursor()
  S.vis = { mode = mode, anchor = cur, head = cur }
  S.mode = "visual"
  M.hooks.set_mode("vi_visual")
  apply_visual()
end

-- leave_visual ends the visual mode and puts the caret where the head was.
local function leave_visual(keep_cursor)
  local V = S.vis
  if V then
    S.last_visual = { mode = V.mode, anchor = V.anchor, head = V.head, to_eol = V.to_eol }
    local lo, hi = D.min(V.anchor, V.head), D.max(V.anchor, V.head)
    if V.mode == "V" then
      S.visual_marks = { from = P(lo.line, 0), to = P(hi.line, D.len(hi.line)) }
    else
      S.visual_marks = { from = lo, to = hi }
    end
  end
  show_block(nil)
  S.vis = nil
  S.mode = "normal"
  if not keep_cursor and V and D.buf() then
    D.set_cursor(D.on_char(V.head))
  end
  M.hooks.set_mode("vi_normal")
end

-- ---------- motions ----------

local function motion_fn(cmd)
  local key = cmd.key
  if key == "n" or key == "N" then
    return function(c) return Search.repeat_motion(c, key == "n") end
  end
  if cmd.pattern then
    return function(c)
      S.last_search = { pattern = cmd.pattern, forward = cmd.forward, nosmart = cmd.nosmart }
      return Search.motion(cmd.pattern, cmd.forward, c.count, c.p, cmd.nosmart)
    end
  end
  return Mo.table[key]
end

-- read_pattern asks for the pattern of / and ?.
local function read_pattern(forward)
  local text = prompt(forward and "/" or "?", "", "search")
  if text == nil then
    cancel()
  end
  if text == "" then
    if not S.last_search then
      fail("E35: No previous regular expression")
    end
    return S.last_search.pattern
  end
  return text
end

local function star_cmd(key, count)
  local word, _, want = Search.word_under(D.cursor())
  if not word then
    fail("E348: No string under cursor")
  end
  local pat
  if key == "*" or key == "#" then
    pat = Search.star_pattern(word, want)
  else
    pat = Search.star_pattern(word, 1) -- g* and g#: no word boundaries
  end
  return {
    kind = "motion", key = key, pattern = pat, forward = key == "*" or key == "g*", count = count, nosmart = true,
  }
end

local MOTION_KEYS = {}
for k in pairs(Mo.table) do
  MOTION_KEYS[k] = true
end
for _, k in ipairs { "n", "N", "/", "?", "*", "#", "g*", "g#" } do
  MOTION_KEYS[k] = true
end

-- parse_motion reads the rest of the motion that starts with key k. It returns the motion
-- command, or nil when k is not a motion.
local function parse_motion(k, count)
  if k == "g" then
    local k2 = getkey()
    k = "g" .. k2
    if k2 == "escape" then
      cancel()
    end
  end
  if not MOTION_KEYS[k] then
    return nil, k
  end
  if S.settings.search_word_under_cursor == false and (k == "*" or k == "#" or k == "g*" or k == "g#") then
    return nil, k
  end
  local cmd = { kind = "motion", key = k, count = count }
  if k == "f" or k == "F" or k == "t" or k == "T" or k == "'" or k == "`" then
    cmd.arg = getchar(false)
  elseif k == "/" or k == "?" then
    cmd.forward = k == "/"
    cmd.pattern = read_pattern(cmd.forward)
  elseif k == "*" or k == "#" or k == "g*" or k == "g#" then
    cmd = star_cmd(k, count)
  end
  return cmd
end

-- do_motion moves the caret. sticky is the position $ left the caret at when the command
-- before this one was $ or a j or k that followed it: vi's j and k then keep to the end of
-- the line, whatever its length.
local function do_motion(cmd, sticky)
  local cur = D.cursor()
  local c = { p = cur, count = cmd.count or 1, has_count = cmd.count ~= nil, arg = cmd.arg }
  local fn = motion_fn(cmd)
  local r = fn(c)
  if not r then
    fail()
  end
  local vertical = r.act == "vi_move_down" or r.act == "vi_move_up"
  if sticky and vertical and D.eq(sticky, cur) and r.pos.line ~= cur.line then
    local l = r.pos.line
    local p = D.on_char(P(l, #D.line(l)))
    D.set_cursor(p)
    S.sticky_eol = p
    return
  end
  if r.act then
    if not run_action(r.act, c.count) then
      fail()
    end
    return
  end
  if r.jump then
    S.prev_pos = cur
  end
  D.set_cursor(D.on_char(r.pos))
  if cmd.key == "$" or cmd.key == "end" then
    S.sticky_eol = D.cursor()
  end
end

-- ---------- operators ----------

local OPERATORS = { d = true, c = true, y = true, [">"] = true, ["<"] = true }
local GOPERATORS = { ["g~"] = true, gu = true, gU = true, ["g?"] = true }

-- Commands of Vim that vi_mode does not have are named in a message, and the keys that belong
-- to the command are read and dropped: left to the grammar, "cgnX<Esc>" would run X as a
-- command and "qa" would insert text. Both are defined below parse_operator, which they use.
local not_ported, swallow_text, NOT_PORTED

-- text_object reads the key after i or a and returns the object command.
local function parse_object(include)
  local k = getkey()
  if k == "escape" or k == "ctrl+[" or k == "ctrl+c" then
    cancel()
  end
  if not Obj.table[k] then
    fail("not a text object: " .. (include and "a" or "i") .. display(k))
  end
  return { include = include, key = k }
end

-- parse_operator reads the motion, text object or doubled key after an operator.
local function parse_operator(op, count1, reg)
  local k = getkey()
  local force
  while k == "v" or k == "V" or k == "ctrl+v" do
    force = k
    k = getkey()
  end
  local count2
  count2, k = read_count(k, nil)
  local count
  if count1 or count2 then
    count = math.min((count1 or 1) * (count2 or 1), MAX_COUNT)
  end
  local cmd = { kind = "op", op = op, reg = reg, count = count, force = force }
  -- dd, cc, yy, >>, <<, g~~, g~g~ ...
  local last = op:sub(-1)
  if k == (#op == 1 and op or last) then
    cmd.line = true
    return cmd
  end
  if #op == 2 and k == "g" then
    local k2 = getkey()
    if k2 == last then
      cmd.line = true
      return cmd
    end
    k = "g" .. k2
  end
  if k == "i" or k == "a" then
    cmd.obj = parse_object(k == "a")
    return cmd
  end
  local mc, named = parse_motion(k, count)
  if not mc then
    if named == "escape" or named == "ctrl+[" or named == "ctrl+c" then
      cancel()
    end
    if NOT_PORTED[named] then
      -- a change operator is followed by the text it replaces with: it is not run as commands
      return not_ported(named, count, nil, false, op == "c")
    end
    fail("not a motion: " .. display(named))
  end
  cmd.motion = mc
  return cmd
end

-- NOT_PORTED are the commands of Vim that vi_mode does not have. follow says which keys belong
-- to the command: "motion" (an operator: a motion, an object or the doubled key), "char" (a
-- register name) or "text" (what is typed until Esc).
local MACRO_HINT = "use the editor's Record Macro and Play Last Macro commands"
NOT_PORTED = {
  R = { what = "R (replace mode)", follow = "text" },
  q = { what = "q (recording a macro)", follow = "char", hint = MACRO_HINT },
  ["@"] = { what = "@ (running a macro)", follow = "char", hint = MACRO_HINT },
  ["="] = { what = "= (re-indenting)", follow = "motion" },
  ["!"] = { what = "! (filtering through a program)", follow = "motion" },
  gq = { what = "gq (formatting)", follow = "motion" },
  gw = { what = "gw (formatting)", follow = "motion" },
  ["g@"] = { what = "g@ (operatorfunc)", follow = "motion" },
  gn = { what = "gn (selecting the next match)" },
  gN = { what = "gN (selecting the previous match)" },
  ["g;"] = { what = "g; (the change list)" },
  ["g,"] = { what = "g, (the change list)" },
  ["g&"] = { what = "g& (repeating :s on all lines)" },
  ga = { what = "ga (the code of the character)" },
  g8 = { what = "g8 (the bytes of the character)" },
  gs = { what = "gs (sleeping)" },
  gx = { what = "gx (opening the link)" },
  K = { what = "K (looking up the word)" },
  Q = { what = "Q (Ex mode)" },
  U = { what = "U (undoing the changes of a line)" },
  tab = { what = "Tab (the jump list)" },
  zf = { what = "zf (creating a fold)", follow = "motion", hint = "vi_mode has no folds" },
}

-- swallow_text drops the keys that follow a command that vi_mode does not have, until Esc. The
-- notice says so, and it is said again for every key that is dropped: the editor clears the status
-- row on each key, so a notice said once would be gone after the first one.
function swallow_text(what, notice)
  S.pending = what
  M.hooks.status()
  while true do
    local k = getkey()
    if k == "escape" or k == "ctrl+[" or k == "ctrl+c" then
      cancel()
    end
    S.pending = what
    gvim.notify(notice, "warn")
    M.hooks.status()
  end
end

-- not_ported says that the command key is not supported, and reads the keys that belong to it.
-- A visual command takes no motion. text asks to swallow what is typed after it, for a change
-- operator.
function not_ported(key, count, reg, visual, text)
  local def = NOT_PORTED[key]
  local msg = "not supported: " .. def.what
  if def.hint then
    msg = msg .. ": " .. def.hint
  end
  local follow = def.follow
  if text then
    follow = "text"
  elseif visual and follow == "motion" then
    follow = nil
  end
  if follow == "text" then
    gvim.notify(msg .. "; keys are ignored until Esc", "warn")
    swallow_text(display(key), msg .. "; keys are ignored until Esc")
  end
  gvim.notify(msg, "warn")
  if follow == "motion" then
    parse_operator(key, count, reg)
  elseif follow == "char" then
    getkey()
  end
  cancel()
end

-- change_range deletes the range and starts an insert at its start.
local function change_range(rng, cmd)
  Ops.delete(rng, cmd.reg, true)
  if rng.kind == "block" then
    -- what is typed goes into every line of the block
    enter_insert(cmd, 1, { block = { first = rng.first, last = rng.last, left = rng.left } })
  else
    enter_insert(cmd, 1)
  end
end

-- apply_op runs the operator on the range.
local function apply_op(op, rng, cmd, start)
  if op == "d" then
    if rng.kind == "char" and D.eq(rng.from, rng.to) then
      fail()
    end
    Ops.delete(rng, cmd.reg, false)
  elseif op == "c" then
    change_range(rng, cmd)
  elseif op == "y" then
    Ops.yank(rng, cmd.reg, start)
  elseif op == ">" or op == "<" then
    local first, last
    if rng.kind == "char" then
      first, last = rng.from.line, rng.to.line
      if rng.to.col == 0 and last > first then
        last = last - 1
      end
    else
      first, last = rng.first, rng.last
    end
    Ops.shift(first, last, (op == ">" and 1 or -1) * (cmd.times or 1))
  else
    Ops.case(rng, op, start)
  end
end

local function exec_op(cmd)
  local start = D.cursor()
  local op = cmd.op
  local count = cmd.count
  local rng
  if cmd.line then
    local n = D.count()
    local c1 = count or 1
    if c1 > 1 and start.line >= n - 1 then
      fail()
    end
    rng = { kind = "line", first = start.line, last = math.min(start.line + c1 - 1, n - 1) }
  elseif cmd.obj then
    local fn = Obj.table[cmd.obj.key]
    local o = fn({ p = start, count = count or 1 }, cmd.obj.include)
    if not o then
      fail()
    end
    rng = Ops.from_object(o)
  else
    local m = cmd.motion
    local fn = motion_fn(m)
    local c = { p = start, count = count or 1, has_count = count ~= nil, arg = m.arg, op = op }
    local r = fn(c)
    if not r then
      fail()
    end
    if cmd.force == "V" then
      r = { pos = r.pos, kind = "line" }
    elseif cmd.force == "v" then
      if r.kind == "line" then
        r = { pos = r.pos, kind = "exclusive" }
      elseif r.kind == "inclusive" then
        r = { pos = r.pos, kind = "exclusive" }
      else
        r = { pos = r.pos, kind = "inclusive" }
      end
    end
    rng = Ops.from_motion(start, r)
    if r.jump then
      S.prev_pos = start
    end
  end
  if rng.kind == "char" and D.eq(rng.from, rng.to) and op ~= "c" then
    fail()
  end
  apply_op(op, rng, cmd, start)
end

-- ---------- simple commands ----------

-- op_cmd builds the operator command that x, D, s ... stand for.
local function op_cmd(op, key, count, reg, owner)
  return { kind = "op", op = op, reg = reg, count = count, motion = { kind = "motion", key = key }, owner = owner }
end

local function undo_redo(redo, count)
  local b = D.buf()
  local n = 0
  for _ = 1, count do
    local ok
    if redo then
      ok = b:redo()
    else
      ok = b:undo()
    end
    if not ok then
      break
    end
    n = n + 1
  end
  D.invalidate()
  if n == 0 then
    gvim.notify(redo and "Already at newest change" or "Already at oldest change")
  end
  clamp_cursor()
end

local function scroll(cmd)
  local k, count = cmd.key, cmd.count
  local w = gvim.win.current()
  local v = w and w:view()
  local height = type(v) == "table" and v.height or 20
  local half = count or math.max(1, math.floor(height / 2))
  if k == "ctrl+e" then
    run_action("scroll_down", count)
    local top = type(v) == "table" and v.top_line or 0
    local cur = D.cursor()
    if cur.line < top + 1 then
      D.set_cursor(D.on_char(P(math.min(top + 1, D.count() - 1), cur.col)), false)
    end
  elseif k == "ctrl+y" then
    run_action("scroll_up", count)
  elseif k == "ctrl+d" or k == "ctrl+u" then
    -- The window scrolls half a page and the caret moves as many lines. The scroll drags the
    -- caret along to keep it inside the window, so the move is what is still missing.
    local down = k == "ctrl+d"
    local from = D.cursor().line
    run_action(down and "scroll_down" or "scroll_up", half)
    local want = down and math.min(from + half, D.count() - 1) or math.max(from - half, 0)
    local now = D.cursor().line
    if want > now then
      run_action("vi_move_down", want - now)
    elseif want < now then
      run_action("vi_move_up", now - want)
    end
  elseif k == "ctrl+f" or k == "pagedown" then
    for _ = 1, count or 1 do
      gvim.command.run("move_page_down")
    end
  else
    for _ = 1, count or 1 do
      gvim.command.run("move_page_up")
    end
  end
end

local function set_mark(name)
  if not name:find("^%a$") and name ~= "'" and name ~= "`" then
    fail("E191: Argument must be a letter or forward/backward quote")
  end
  local cur = D.cursor()
  if name == "'" or name == "`" then
    S.prev_pos = cur
  else
    S.marks[name] = { buf = D.buf().id, line = cur.line, col = cur.col }
  end
end

-- exec runs a parsed command.
local function exec(cmd)
  local kind = cmd.kind
  local sticky = S.sticky_eol
  S.sticky_eol = nil
  if kind == "motion" then
    do_motion(cmd, sticky)
  elseif kind == "op" then
    exec_op(cmd)
  elseif kind == "insert" then
    local k, count = cmd.key, cmd.count or 1
    local cur = D.cursor()
    local s = D.line(cur.line)
    if k == "a" then
      if #s > 0 then
        D.set_cursor(P(cur.line, T.next(s, cur.col)))
      end
      enter_insert(cmd, count)
    elseif k == "i" then
      enter_insert(cmd, count)
    elseif k == "I" then
      D.set_cursor(P(cur.line, #s == 0 and 0 or (s:find("[^ \t]") and s:find("[^ \t]") - 1 or #s)))
      enter_insert(cmd, count)
    elseif k == "gI" then
      D.set_cursor(P(cur.line, 0))
      enter_insert(cmd, count)
    elseif k == "A" then
      D.set_cursor(P(cur.line, #s))
      enter_insert(cmd, count)
    elseif k == "gi" then
      local p = S.last_insert
      if p then
        D.set_cursor(D.clamp(p))
      end
      enter_insert(cmd, count)
    elseif k == "o" then
      open_below()
      enter_insert(cmd, count, { line_open = "below" })
    else -- "O"
      open_above()
      enter_insert(cmd, count, { line_open = "above" })
    end
  elseif kind == "simple" then
    local k, count = cmd.key, cmd.count
    if k == "x" or k == "delete" then
      exec_op(op_cmd("d", "l", count, cmd.reg, cmd))
    elseif k == "X" then
      exec_op(op_cmd("d", "h", count, cmd.reg, cmd))
    elseif k == "D" then
      exec_op(op_cmd("d", "$", count, cmd.reg, cmd))
    elseif k == "C" then
      exec_op(op_cmd("c", "$", count, cmd.reg, cmd))
    elseif k == "s" then
      local cur = D.cursor()
      if D.len(cur.line) == 0 then
        enter_insert(cmd, 1)
      else
        exec_op(op_cmd("c", "l", count, cmd.reg, cmd))
      end
    elseif k == "S" then
      exec_op({ kind = "op", op = "c", reg = cmd.reg, count = count, line = true, owner = cmd })
    elseif k == "Y" then
      exec_op({ kind = "op", op = "y", reg = cmd.reg, count = count, line = true, owner = cmd })
    elseif k == "p" or k == "P" or k == "gp" or k == "gP" then
      Ops.put(cmd.reg, count or 1, k == "P" or k == "gP", k == "gp" or k == "gP")
    elseif k == "J" or k == "gJ" then
      local cur = D.cursor()
      if not Ops.join(cur.line, math.max(2, count or 2), k == "J") then
        fail()
      end
    elseif k == "~" then
      local ok, err = run_action("toggle_case", count)
      if not ok then
        fail("vi: " .. tostring(err))
      end
      D.begin()
      clamp_cursor()
    elseif k == "r" then
      Ops.replace_chars(cmd.arg, count or 1)
    elseif k == "ctrl+a" or k == "ctrl+x" then
      Ops.increment((k == "ctrl+a" and 1 or -1) * (count or 1))
    elseif k == "&" then
      ex("s")
    elseif k == "m" then
      set_mark(cmd.arg)
    end
  end
end

-- repeatable tells whether . repeats the command.
local function repeatable(cmd)
  if cmd.kind == "insert" then
    return true
  elseif cmd.kind == "op" then
    return cmd.op ~= "y"
  elseif cmd.kind == "simple" then
    local k = cmd.key
    return k == "x" or k == "X" or k == "D" or k == "C" or k == "s" or k == "S" or k == "p" or k == "P"
        or k == "gp" or k == "gP" or k == "J" or k == "gJ" or k == "~" or k == "r" or k == "ctrl+a"
        or k == "ctrl+x" or k == "delete"
  elseif cmd.kind == "visual_op" then
    return cmd.op ~= "y"
  end
  return false
end

-- run_cmd executes a command read from the keys and remembers it for . when it changes
-- text. A command that fails is not remembered.
local function run_cmd(cmd)
  exec(cmd)
  if repeatable(cmd) then
    S.last_change = cmd
  end
end

-- dot repeats the last change count times (a count replaces the old one).
local function dot(count)
  local last = S.last_change
  if not last then
    fail()
  end
  if count then
    last.count = count
  end
  local copy = {}
  for k, v in pairs(last) do
    copy[k] = v
  end
  S.replay = { text = last.insert_text }
  local ok, err = pcall(function()
    if copy.kind == "visual_op" then
      M.exec_visual_op(copy)
    elseif copy.kind == "block_insert" then
      M.replay_block_insert(copy)
    else
      exec(copy)
    end
  end)
  S.replay = nil
  if not ok then
    error(err, 0)
  end
end

-- ---------- reading a normal-mode command ----------

local SIMPLE = {
  x = true, X = true, D = true, C = true, s = true, S = true, Y = true, p = true, P = true,
  J = true, ["~"] = true, ["ctrl+a"] = true, ["ctrl+x"] = true, ["&"] = true, delete = true,
}

local SCROLL = {
  ["ctrl+d"] = true, ["ctrl+u"] = true, ["ctrl+f"] = true, ["ctrl+b"] = true, ["ctrl+e"] = true,
  ["ctrl+y"] = true, pageup = true, pagedown = true,
}

-- FOLD_KEYS are the keys after z that work on folds.
local FOLD_KEYS = {}
for k in ("fFdDoOcCaAvxXmMrRnNiEjk"):gmatch(".") do
  FOLD_KEYS[k] = true
end

local function normal_command()
  S.pending = ""
  local k = getkey()
  clamp_cursor()
  local count, reg
  while true do
    if k == '"' then
      reg = getchar(false)
      if not Reg.valid(reg) then
        cancel()
      end
      k = getkey()
    elseif k:find("^[1-9]$") or (k == "0" and count) then
      count, k = read_count(k, count)
    else
      break
    end
  end

  if k == "escape" or k == "ctrl+[" or k == "ctrl+c" then
    return
  end

  if OPERATORS[k] then
    return run_cmd(parse_operator(k, count, reg))
  end
  if k == "g" then
    local k2 = getkey()
    local kk = "g" .. k2
    if GOPERATORS[kk] then
      return run_cmd(parse_operator(kk, count, reg))
    elseif kk == "gJ" or kk == "gp" or kk == "gP" then
      return run_cmd({ kind = "simple", key = kk, count = count, reg = reg })
    elseif kk == "gi" or kk == "gI" then
      return run_cmd({ kind = "insert", key = kk, count = count })
    elseif kk == "gv" then
      local lv = S.last_visual
      if not lv then
        fail()
      end
      S.vis = { mode = lv.mode, anchor = D.clamp(lv.anchor), head = D.clamp(lv.head), to_eol = lv.to_eol }
      S.mode = "visual"
      M.hooks.set_mode("vi_visual")
      apply_visual()
      return
    elseif NOT_PORTED[kk] then
      return not_ported(kk, count, reg)
    end
    local cmd = parse_motion(kk, count)
    if not cmd then
      if k2 == "escape" or k2 == "ctrl+[" or k2 == "ctrl+c" then
        cancel()
      end
      fail("not a vi command: " .. kk)
    end
    cmd.count = count
    return exec(cmd)
  end
  if k == "i" or k == "a" or k == "I" or k == "A" or k == "o" or k == "O" then
    return run_cmd({ kind = "insert", key = k, count = count })
  end
  if SIMPLE[k] then
    return run_cmd({ kind = "simple", key = k, count = count, reg = reg })
  end
  if k == "r" then
    local ch = getchar(true)
    return run_cmd({ kind = "simple", key = "r", arg = ch, count = count })
  end
  if k == "m" then
    local name = getchar(false)
    return exec({ kind = "simple", key = "m", arg = name })
  end
  if k == "." then
    return dot(count)
  end
  if k == "u" then
    return undo_redo(false, count or 1)
  end
  if k == "ctrl+r" then
    return undo_redo(true, count or 1)
  end
  if k == "v" or k == "V" or k == "ctrl+v" then
    return enter_visual(k == "v" and "v" or (k == "V" and "V" or "b"))
  end
  if k == ":" then
    local text = prompt(":", "", "ex")
    if text and text ~= "" then
      ex(text)
    end
    return
  end
  if k == "Z" then
    local k2 = getkey()
    if k2 == "Z" then
      ex("x")
    elseif k2 == "Q" then
      ex("q!")
    elseif k2 ~= "escape" and k2 ~= "ctrl+[" and k2 ~= "ctrl+c" then
      fail("not a vi command: Z" .. display(k2) .. " (ZZ and ZQ are)")
    end
    return
  end
  if k == "z" then
    local k2 = getkey()
    if NOT_PORTED["z" .. k2] then
      return not_ported("z" .. k2, count, reg)
    end
    if FOLD_KEYS[k2] then
      fail("not supported: z" .. k2 .. " (folds)")
    end
    if k2 == "z" or k2 == "." then
      gvim.command.run("recenter")
    elseif k2 == "t" or k2 == "enter" then
      local w = gvim.win.current()
      if w then
        w:scroll_to(D.cursor().line)
      end
    elseif k2 == "b" or k2 == "-" then
      local w = gvim.win.current()
      local v = w and w:view()
      if w and type(v) == "table" then
        w:scroll_to(math.max(0, D.cursor().line - v.height + 1))
      end
    elseif k2 ~= "escape" and k2 ~= "ctrl+[" and k2 ~= "ctrl+c" then
      fail("not a vi command: z" .. display(k2))
    end
    return
  end
  if SCROLL[k] then
    return scroll({ key = k, count = count })
  end
  if k == "ctrl+g" then
    return ex("file")
  end
  if NOT_PORTED[k] then
    return not_ported(k, count, reg)
  end
  if k == "insert" or k == "ctrl+l" then
    return
  end

  -- a motion; a key that is no command at all says so (vi beeps)
  local mc = parse_motion(k, count)
  if mc then
    return exec(mc)
  end
  fail("not a vi command: " .. display(k))
end

-- ---------- visual-mode commands ----------

-- visual_motion moves the head with a motion.
local function visual_motion(cmd)
  local V = S.vis
  local c = { p = V.head, count = cmd.count or 1, has_count = cmd.count ~= nil, arg = cmd.arg, visual = true }
  local fn = motion_fn(cmd)
  local r = fn(c)
  if not r then
    fail()
  end
  if r.act == "vi_move_down" or r.act == "vi_move_up" then
    -- j and k go by logical lines here and aim at the wanted column. The editor cannot keep
    -- it: the head is set from the plugin on every key, and the short line it may have
    -- crossed would become the new column.
    local line = r.pos.line
    if V.to_eol then
      V.head = P(line, D.len(line))
      return
    end
    local s = D.line(line)
    local want = V.want or char_index(V.head.line, V.head.col)
    V.want = want
    V.head = P(line, math.min(T.advance(s, 0, want), T.last(s)))
    return
  end
  V.want = nil
  if r.act then
    -- the editor's own vertical move (gj, gk): put the caret on the head, move it, and read
    -- it back
    D.set_cursor(V.head, false)
    if not run_action(r.act, c.count) then
      fail()
    end
    D.begin()
    V.head = D.cursor()
    V.to_eol = false
    return
  end
  if r.jump then
    S.prev_pos = V.head
  end
  if r.to_eol then
    -- $ selects the line break too: the head stands on the end of the line
    V.head = P(r.pos.line, D.len(r.pos.line))
    V.to_eol = true
  else
    V.head = D.on_char(r.pos)
    V.to_eol = false
  end
end

-- extend_object is a text object asked for again while an area of more than one character
-- is selected: words and paragraphs add the next one to the area, the other objects take
-- the one around the area. It returns the object, or nil when the area is not one to
-- extend (then the object is the one around the caret).
local function extend_object(fn, key, include, count)
  local V = S.vis
  local a, h = V.anchor, V.head
  if D.cmp(a, h) >= 0 then
    return nil
  end
  local linewise = V.mode == "V"
  local sel_to = linewise and P(h.line + 1, 0) or D.after(h)
  if key == "p" or ((key == "w" or key == "W") and not linewise) then
    if linewise and h.line + 1 >= D.count() or D.cmp(sel_to, h) <= 0 then
      return nil
    end
    local o = fn({ p = sel_to, count = count }, include)
    if not o then
      return nil
    end
    return { from = linewise and P(a.line, 0) or a, to = o.to, kind = o.kind }
  end
  if linewise then
    return nil
  end
  for k = count, count + 8 do
    local o = fn({ p = h, count = k }, include)
    if not o then
      return nil
    end
    if D.le(o.from, a) and D.le(sel_to, o.to) and not (D.eq(o.from, a) and D.eq(o.to, sel_to)) then
      return o
    end
  end
  return nil
end

-- visual_object selects a text object.
local function visual_object(cmd, include)
  local V = S.vis
  local fn = Obj.table[cmd.key]
  local count = cmd.count or 1
  local o = extend_object(fn, cmd.key, include, count) or fn({ p = V.head, count = count }, include)
  if not o then
    fail()
  end
  V.want = nil
  local rng = Ops.from_object(o)
  if rng.kind == "line" then
    V.mode = "V"
    V.anchor = P(rng.first, 0)
    V.head = P(rng.last, 0)
    return
  end
  local from, to = rng.from, rng.to
  V.anchor = from
  -- the head is the last character of the object
  local lastc = to
  if D.cmp(to, from) > 0 then
    local q = D.dec(to)
    lastc = D.on_char(q)
  end
  V.head = lastc
end

local function swap_ends()
  local V = S.vis
  V.anchor, V.head = V.head, V.anchor
  V.to_eol = false
  V.want = nil
end

-- put_over replaces the visual area with a register (p; P leaves the register alone).
local pasting -- {text=, kind=} while a paste replaces the area: put_over puts it, not a register

local function put_over(rng, cmd, keep_register)
  if not pasting and Reg.is_clip(cmd.reg) then
    Ops.delete(rng, "_", false)
    gvim.command.run("paste")
    return
  end
  local v = pasting or Reg.get(cmd.reg)
  if not v then
    fail("E353: Nothing in register " .. (cmd.reg or '"'))
  end
  local replaced = Ops.text(rng)
  local count = cmd.count or 1
  local body = v.text
  if v.kind == "line" then
    body = v.text:sub(1, -2)
  end
  local lines = {}
  for _ = 1, count do
    lines[#lines + 1] = body
  end
  local newtext = table.concat(lines, v.kind == "line" and "\n" or "")
  if rng.kind == "line" then
    D.replace(P(rng.first, 0), P(rng.last, D.len(rng.last)), newtext)
    D.set_cursor(D.first_nonblank(rng.first))
  elseif rng.kind == "char" then
    local text = newtext
    if v.kind == "line" then
      text = "\n" .. newtext .. "\n" -- the line is split around the new lines
    end
    D.replace(rng.from, rng.to, text)
    if v.kind == "line" then
      D.set_cursor(D.first_nonblank(rng.from.line + 1))
    else
      local e = D.advance(rng.from, text)
      D.set_cursor(D.on_char(P(e.line, T.prev(D.line(e.line), e.col))))
    end
  else
    Ops.delete(rng, "_", false)
    local at = D.cursor()
    D.replace(at, at, newtext)
    D.set_cursor(D.on_char(at))
  end
  if not keep_register then
    Reg.set(nil, replaced, rng.kind == "line" and "line" or (rng.kind == "block" and "block" or "char"), "d")
  end
end

-- replace_selection is r in visual mode: every character of the area becomes ch.
local function replace_selection(rng, ch)
  local edits = {}
  local function fill(l, a, b)
    local s = D.line(l)
    local n = T.count(s, a, b)
    if n > 0 then
      edits[#edits + 1] = { from = P(l, a), to = P(l, b), text = string.rep(ch, n) }
    end
  end
  if rng.kind == "line" then
    for l = rng.first, rng.last do
      fill(l, 0, D.len(l))
    end
  elseif rng.kind == "block" then
    for l = rng.first, rng.last do
      local s = D.line(l)
      fill(l, T.advance(s, 0, rng.left), rng.right and T.advance(s, 0, rng.right) or #s)
    end
  else
    for l = rng.from.line, rng.to.line do
      local a = l == rng.from.line and rng.from.col or 0
      local b = l == rng.to.line and rng.to.col or D.len(l)
      fill(l, a, math.min(b, D.len(l)))
    end
  end
  if #edits > 0 then
    D.edit(edits)
  end
end

-- exec_visual_op runs an operator on an area: cmd.rng, or, when . repeats it, the area of
-- the same size (cmd.shape) from the caret.
function M.exec_visual_op(cmd)
  local rng
  local cur = D.cursor()
  if cmd.shape and S.replay then
    local sh = cmd.shape
    if sh.mode == "V" then
      rng = { kind = "line", first = cur.line, last = math.min(cur.line + sh.lines - 1, D.count() - 1) }
    elseif sh.mode == "b" then
      local left = char_index(cur.line, cur.col)
      rng = { kind = "block", first = cur.line, last = math.min(cur.line + sh.lines - 1, D.count() - 1),
        left = left, right = sh.eol and nil or left + sh.width }
    else
      local to
      if sh.lines <= 1 then
        local s = D.line(cur.line)
        to = P(cur.line, T.advance(s, cur.col, sh.chars))
      else
        local l = math.min(cur.line + sh.lines - 1, D.count() - 1)
        local s = D.line(l)
        to = P(l, T.advance(s, 0, sh.chars))
      end
      rng = { kind = "char", from = cur, to = to }
    end
  else
    rng = cmd.rng
  end
  local op = cmd.op
  if op == "d" then
    Ops.delete(rng, cmd.reg, false)
  elseif op == "c" then
    change_range(rng, cmd)
  elseif op == "y" then
    Ops.yank(rng, cmd.reg)
  elseif op == ">" or op == "<" then
    local first, last = rng.first or rng.from.line, rng.last or rng.to.line
    if rng.kind == "char" and rng.to.col == 0 and last > first then
      last = last - 1
    end
    Ops.shift(first, last, (op == ">" and 1 or -1) * (cmd.times or 1))
  elseif op == "J" or op == "gJ" then
    local first, last = rng.first or rng.from.line, rng.last or rng.to.line
    if rng.kind == "char" and rng.to.col == 0 and last > first then
      last = last - 1
    end
    if not Ops.join(first, math.max(2, last - first + 1), op == "J") then
      fail()
    end
  elseif op == "r" then
    replace_selection(rng, cmd.arg)
    if rng.kind == "char" then
      D.set_cursor(D.on_char(rng.from))
    elseif rng.kind == "block" then
      D.set_cursor(D.on_char(P(rng.first, T.advance(D.line(rng.first), 0, rng.left))))
    else
      D.set_cursor(P(rng.first, 0))
    end
  elseif op == "p" or op == "P" then
    put_over(rng, cmd, op == "P")
  else
    Ops.case(rng, op, rng.from)
  end
end

-- shape describes the size of the area, so . can repeat a visual operator on the same
-- amount of text from the caret.
local function area_shape(rng)
  if rng.kind == "line" then
    return { mode = "V", lines = rng.last - rng.first + 1 }
  elseif rng.kind == "block" then
    return { mode = "b", lines = rng.last - rng.first + 1, width = rng.right and rng.right - rng.left or 0, eol = rng.right == nil }
  end
  local lines = rng.to.line - rng.from.line + 1
  local chars
  if lines <= 1 then
    chars = T.count(D.line(rng.from.line), rng.from.col, rng.to.col)
  else
    chars = T.count(D.line(rng.to.line), 0, rng.to.col)
  end
  return { mode = "v", lines = lines, chars = chars }
end

-- visual_op applies an operator to the area and leaves the visual mode.
local function visual_op(op, count, reg, arg, whole_lines)
  local V = S.vis
  local rng, lo = visual_range()
  if whole_lines and rng.kind ~= "line" then
    rng = { kind = "line", first = rng.first or rng.from.line, last = rng.last or D.max(V.anchor, V.head).line }
  end
  local cmd = { kind = "visual_op", op = op, reg = reg, arg = arg, rng = rng, times = count, count = count,
    shape = area_shape(rng) }
  leave_visual(true)
  -- the caret goes to the start of the area first, as vi's operators expect
  if rng.kind == "line" then
    D.set_cursor(P(rng.first, math.min(lo.col, T.last(D.line(rng.first)))), false)
  elseif rng.kind == "block" then
    D.set_cursor(D.on_char(P(rng.first, T.advance(D.line(rng.first), 0, rng.left))), false)
  else
    D.set_cursor(rng.from, false)
  end
  M.exec_visual_op(cmd)
  if repeatable(cmd) then
    S.last_change = cmd
  end
  if op == "y" then
    local pos
    if rng.kind == "line" then
      pos = P(rng.first, math.min(lo.col, T.last(D.line(rng.first))))
    elseif rng.kind == "block" then
      pos = P(rng.first, T.advance(D.line(rng.first), 0, rng.left))
    else
      pos = rng.from
    end
    D.set_cursor(D.on_char(pos))
  end
end

-- block_insert starts I or A on a block: the text typed on the first line goes to every
-- line of the block when the insert ends.
local function block_insert(append)
  local rng = visual_range()
  local to_eol = S.vis.to_eol
  -- what is typed is added to this command when the insert ends, and . repeats it
  local cmd = { kind = "block_insert", append = append, shape = area_shape(rng) }
  S.last_change = cmd
  leave_visual(true)
  local first = rng.first
  local s = D.line(first)
  local col
  if append then
    if to_eol then
      col = #s
    else
      local have = T.count(s, 0, #s)
      if have < rng.right then
        local pad = string.rep(" ", rng.right - have)
        D.replace(P(first, #s), P(first, #s), pad)
        D.set_cursor(P(first, #s + #pad))
        col = nil
      else
        col = T.advance(s, 0, rng.right)
      end
    end
  else
    col = T.advance(s, 0, rng.left)
  end
  if col then
    D.set_cursor(P(first, col))
  end
  enter_insert(cmd, 1, { block = { first = rng.first, last = rng.last, left = rng.left, right = rng.right, append = append, to_eol = to_eol } })
end

-- replay_block_insert is . after a block I or A: the text goes into a block of the same
-- size at the caret.
function M.replay_block_insert(cmd)
  local text = S.replay and S.replay.text
  if not text or text == "" or text:find("\n", 1, true) then
    return
  end
  local sh = cmd.shape
  local cur = D.cursor()
  local left = char_index(cur.line, cur.col)
  local right = (not sh.eol) and left + sh.width or nil
  local b = {
    first = cur.line, last = math.min(cur.line + sh.lines - 1, D.count() - 1), left = left, right = right,
    append = cmd.append, to_eol = sh.eol,
  }
  local s = D.line(b.first)
  local col
  if cmd.append and not sh.eol then
    local have = T.count(s, 0, #s)
    if have < right then
      D.replace(P(b.first, #s), P(b.first, #s), string.rep(" ", right - have))
      s = D.line(b.first)
    end
    col = T.advance(s, 0, right)
  elseif cmd.append then
    col = #s
  else
    col = T.advance(s, 0, left)
  end
  D.replace(P(b.first, col), P(b.first, col), text)
  M.finish_block_insert({ block = b }, text)
end

local function visual_command()
  S.pending = ""
  local k = getkey()
  local V = S.vis
  local count, reg
  while true do
    if k == '"' then
      reg = getchar(false)
      if not Reg.valid(reg) then
        cancel()
      end
      k = getkey()
    elseif k:find("^[1-9]$") or (k == "0" and count) then
      count, k = read_count(k, count)
    else
      break
    end
  end

  if k == "escape" or k == "ctrl+[" or k == "ctrl+c" then
    leave_visual()
    return
  end
  if k == "v" or k == "V" or k == "ctrl+v" then
    local mode = k == "v" and "v" or (k == "V" and "V" or "b")
    if V.mode == mode then
      leave_visual()
    else
      V.mode = mode
      apply_visual()
    end
    return
  end
  if k == "o" or k == "O" then
    swap_ends()
    apply_visual()
    return
  end
  if k == ":" then
    leave_visual() -- sets the marks '< and '>
    local text = prompt(":", "'<,'>", "ex")
    if text and text ~= "" then
      ex(text)
    end
    return
  end
  if k == "d" or k == "x" or k == "delete" then
    return visual_op("d", count, reg)
  elseif k == "X" or k == "D" then
    return visual_op("d", count, reg, nil, true)
  elseif k == "y" then
    return visual_op("y", count, reg)
  elseif k == "Y" then
    return visual_op("y", count, reg, nil, true)
  elseif k == "c" or k == "s" then
    return visual_op("c", count, reg)
  elseif k == "C" or k == "S" or k == "R" then
    return visual_op("c", count, reg, nil, true)
  elseif k == ">" or k == "<" then
    return visual_op(k, count or 1, reg)
  elseif k == "~" then
    return visual_op("g~", count, reg)
  elseif k == "u" then
    return visual_op("gu", count, reg)
  elseif k == "U" then
    return visual_op("gU", count, reg)
  elseif k == "J" then
    return visual_op("J", count, reg)
  elseif k == "p" or k == "P" then
    return visual_op(k, count, reg)
  elseif k == "r" then
    local ch = getchar(false)
    return visual_op("r", count, reg, ch)
  elseif (k == "I" or k == "A") and V.mode == "b" then
    return block_insert(k == "A")
  elseif k == "I" or k == "A" then
    -- outside a block I and A insert before the area and after it
    local lo, hi = D.min(V.anchor, V.head), D.max(V.anchor, V.head)
    leave_visual(true)
    if k == "I" then
      D.set_cursor(V.mode == "V" and P(lo.line, 0) or lo, false)
    else
      D.set_cursor(V.mode == "V" and P(hi.line, D.len(hi.line)) or D.after(hi), false)
    end
    enter_insert(nil, 1)
    return
  elseif k == "ctrl+a" or k == "ctrl+x" then
    local rng = visual_range()
    local first, last = rng.first or rng.from.line, rng.last or rng.to.line
    leave_visual(true)
    for l = first, last do
      pcall(Ops.increment, (k == "ctrl+a" and 1 or -1) * (count or 1), P(l, 0))
    end
    D.set_cursor(D.first_nonblank(first))
    return
  elseif k == "g" then
    local k2 = getkey()
    local kk = "g" .. k2
    if kk == "gu" then
      return visual_op("gu", count, reg)
    elseif kk == "gU" then
      return visual_op("gU", count, reg)
    elseif kk == "g~" then
      return visual_op("g~", count, reg)
    elseif kk == "g?" then
      return visual_op("g?", count, reg)
    elseif kk == "gJ" then
      return visual_op("gJ", count, reg)
    elseif kk == "gv" then
      local lv = S.last_visual
      if lv then
        local cur = { mode = V.mode, anchor = V.anchor, head = V.head, to_eol = V.to_eol }
        S.vis = { mode = lv.mode, anchor = D.clamp(lv.anchor), head = D.clamp(lv.head), to_eol = lv.to_eol }
        S.last_visual = cur
        apply_visual()
      end
      return
    end
    if NOT_PORTED[kk] then
      return not_ported(kk, count, reg, true)
    end
    local cmd = parse_motion(kk, count)
    if not cmd then
      if k2 == "escape" or k2 == "ctrl+[" or k2 == "ctrl+c" then
        cancel()
      end
      fail("not a vi command: " .. kk)
    end
    cmd.count = count
    visual_motion(cmd)
    apply_visual()
    return
  elseif k == "i" or k == "a" then
    local obj = parse_object(k == "a")
    obj.count = count
    visual_object(obj, obj.include)
    apply_visual()
    return
  elseif k == "z" then
    local k2 = getkey()
    if k2 == "z" or k2 == "." then
      gvim.command.run("recenter")
    end
    return
  elseif SCROLL[k] then
    D.set_cursor(V.head, false)
    scroll({ key = k, count = count })
    D.begin()
    V.head = D.cursor()
    V.want = nil
    apply_visual()
    return
  end

  if NOT_PORTED[k] then
    return not_ported(k, count, reg, true)
  end
  local mc = parse_motion(k, count)
  if not mc then
    fail("not a vi command: " .. display(k))
  end
  visual_motion(mc)
  apply_visual()
end

-- ---------- the reader ----------

local function main()
  while true do
    local ok, err = pcall(function()
      if S.mode == "visual" and S.vis then
        visual_command()
      else
        normal_command()
      end
    end)
    if not ok then
      if err ~= CANCEL then
        local msg = type(err) == "table" and (err.message or "error object") or tostring(err)
        gvim.log.error("vi_mode: %s", msg)
        gvim.notify("vi: " .. msg, "warn")
      end
      S.pending = ""
      if S.mode == "visual" and S.vis and D.buf() then
        local okv = pcall(apply_visual)
        if not okv then
          S.vis, S.mode = nil, "normal"
          M.hooks.set_mode("vi_normal")
        end
      end
    end
  end
end

-- ---------- what the editor does to vi: pastes and selections ----------

-- paste handles text pasted into the normal or the visual mode (the insert mode lets the editor
-- insert it). In the normal mode it goes in before the caret, as P puts it; in the visual mode
-- it replaces the area, as p does. A paste that ends in a line break is put as lines.
function M.paste(text)
  text = text:gsub("\r\n?", "\n")
  if text == "" then
    return
  end
  if busy or S.pending ~= "" then
    gvim.notify("the paste was ignored: a command is waiting for its keys", "warn")
    return
  end
  if not D.begin() then
    gvim.notify("vi: this file is too big to edit until its index is ready", "warn")
    return
  end
  local value = { text = text, kind = text:sub(-1) == "\n" and "line" or "char" }
  local last = S.last_change
  local ok, err = pcall(function()
    if S.mode == "visual" and S.vis then
      pasting = value
      reader = nil -- the reader waits inside visual_command: the next key starts over in the normal mode
      visual_op("P", 1, nil)
    else
      Ops.put(nil, 1, true, false, value)
      clamp_cursor()
    end
  end)
  pasting = nil
  S.last_change = last -- . does not repeat a paste
  M.hooks.status()
  if not ok and err ~= CANCEL then
    error(err, 0)
  end
end

-- expected_selection is the anchor and the caret the editor has while the visual area V is shown.
local function expected_selection(V)
  local a, h = V.anchor, V.head
  if V.mode == "V" then
    if a.line <= h.line then
      return P(a.line, 0), P(h.line, D.len(h.line))
    end
    return P(a.line, D.len(a.line)), P(h.line, 0)
  end
  if D.le(a, h) then
    return a, D.after(h)
  end
  return D.after(a), h
end

-- adopt_selection makes a selection that vi did not make (a mouse drag, a double click, shift and
-- an arrow key, Select All) the visual area, so that d, y, c and the rest act on it, and ends the
-- visual mode when a click took the selection away. It runs when the editor says the selection
-- changed, and only reads what the editor has now.
function M.adopt_selection()
  if busy or S.pending ~= "" or S.mode == "insert" or not D.begin() then
    return
  end
  local carets = D.buf():cursors()
  local c = type(carets) == "table" and carets[1]
  if not c then
    return
  end
  local pos = P(c.pos.line, c.pos.col)
  local anchor = c.anchor and P(c.anchor.line, c.anchor.col)
  local V = S.vis
  if S.mode == "visual" and V then
    if V.mode == "b" then
      return -- a block is shown with highlights, not as the selection
    end
    local ea, ec = expected_selection(V)
    if D.eq(anchor or pos, ea) and D.eq(pos, ec) then
      return -- vi's own selection
    end
    if not anchor or D.eq(anchor, pos) then
      reader = nil
      leave_visual(true)
      D.set_cursor(D.on_char(pos), false)
      M.hooks.status()
      return
    end
  elseif not anchor or D.eq(anchor, pos) then
    return
  end
  -- the editor's selection is [from, to); the head of the visual area is the last character in it
  local from, to = D.min(anchor, pos), D.max(anchor, pos)
  local last = D.dec(to)
  if D.le(anchor, pos) then
    S.vis = { mode = "v", anchor = from, head = last }
  else
    S.vis = { mode = "v", anchor = last, head = from }
  end
  S.mode = "visual"
  reader = nil -- it waits inside normal_command, and the next key belongs to visual_command
  M.hooks.set_mode("vi_visual")
  M.hooks.status()
end

-- reset forgets the command being read and the visual area, and goes back to normal mode.
-- With collapse the editor's selection of the visual area is dropped, the caret going to
-- the head (do not ask for it after the active buffer changed: the area is in another one).
function M.reset(collapse)
  if collapse and S.vis and D.begin() then
    pcall(function()
      D.set_cursor(D.on_char(S.vis.head), false)
    end)
  end
  reader = nil
  busy = false
  S.pending = ""
  if S.vis then
    pcall(show_block, nil)
  end
  S.vis = nil
  S.ins = nil
  S.replay = nil
  S.mode = "normal"
end

-- feed handles one key: a character ("d", "$", " ") or the name of a key ("escape",
-- "ctrl+r", "enter").
function M.feed(key)
  if busy then
    if not reader or coroutine.status(reader) == "dead" then
      -- the host stopped the command that was waiting (its time or memory budget ran out):
      -- the wait is over, but nothing was left to clear the flag
      busy = false
      reader = nil
      S.pending = ""
    elseif gvim.time.monotonic_ms() - busy_since < BUSY_LIMIT then
      return
    else
      -- the wait never ended (a lost completion): start over rather than drop keys for ever
      M.reset()
    end
  end
  if S.mode == "insert" then
    return
  end
  if not reader or coroutine.status(reader) == "dead" then
    reader = coroutine.create(main)
    local ok, err = coroutine.resume(reader)
    if not ok then
      reader = nil
      gvim.log.error("vi_mode: %s", tostring(err))
      return
    end
  end
  local ok, err = coroutine.resume(reader, key)
  if not ok then
    reader = nil
    S.pending = ""
    gvim.log.error("vi_mode: %s", tostring(err))
    gvim.notify("vi: " .. tostring(err), "warn")
  end
  if D.begin() and S.mode == "normal" then
    pcall(clamp_cursor)
  end
  M.hooks.status()
end

return M
