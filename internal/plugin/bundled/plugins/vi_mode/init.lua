-- Vi mode. A port of Fresh's vi_mode plugin: vi-style modal editing on top of the editor.
--
--   normal mode   counts, registers ("a), operators (d c y > < g~ gu gU g?), motions
--                 (hjkl w b e W B E ge 0 ^ $ gg G H M L f t F T ; , % { } n N * # / ? ' `),
--                 text objects (iw aw ip ap i( a( i[ i{ i< i" i' i` it ...), x X D C s S Y
--                 p P J ~ r u ctrl+r . ctrl+a ctrl+x, marks, scrolling, : commands
--   insert mode   typing goes to the editor; Esc returns to normal mode
--   visual mode   v, V and ctrl+v (blocks), with the same motions, text objects and
--                 operators, I and A on blocks, : with a range
--
-- The plugin is off unless plugins.vi_mode.enabled is true. Once loaded it starts in
-- normal mode (set plugins.vi_mode.settings.auto_start to false to start with vi off and
-- turn it on with "Vi Mode: Toggle"). Esc pressed three times quickly, or the panic chord
-- ctrl+^, clears every plugin input mode, vi's included; Toggle brings it back.
--
-- Not ported: macros (q, @: the editor has its own), the jump list (ctrl+o, ctrl+i),
-- replace mode (R), sentences, folds, and shell filters.
local gvim = require "gvim"
local S = require "vi.state"
local D = require "vi.doc"
local Reg = require "vi.reg"
local N = require "vi.normal"

local settings = gvim.settings.define {
  auto_start = {
    type = "boolean", default = true,
    description = "Start in vi normal mode when the editor starts. When false, turn vi on with Vi Mode: Toggle.",
  },
  arrow_keys = {
    type = "boolean", default = true,
    description = "The arrow, Home, End and Page keys move like h j k l 0 $ in vi's modes.",
  },
  ctrl_keys = {
    type = "boolean", default = true,
    description = "Bind vi's ctrl keys (ctrl+r redo, ctrl+d/u/f/b/e/y scrolling, ctrl+v block, ctrl+a/x number) in normal and visual mode.",
  },
  ignore_case = {
    type = "boolean", default = true,
    description = "Searches ignore case, unless smart_case sees an upper case letter in the pattern.",
  },
  smart_case = {
    type = "boolean", default = true,
    description = "With ignore_case, a pattern that has an upper case letter matches case.",
  },
  shift_width = {
    type = "integer", default = 0, minimum = 0, maximum = 16,
    description = "Columns that > and < move a line; 0 uses the tab size.",
  },
  clipboard_yank = {
    type = "boolean", default = true,
    description = "A yank (y) also sets the system clipboard.",
  },
  search_word_under_cursor = {
    type = "boolean", default = true,
    description = "* and # search for the word under the caret.",
  },
}
-- settings is a read-only view: copy the values into a table the :set command can change
for _, name in ipairs { "auto_start", "arrow_keys", "ctrl_keys", "ignore_case", "smart_case", "shift_width", "clipboard_yank", "search_word_under_cursor" } do
  S.settings[name] = settings[name]
end
Reg.opts.clipboard = settings.clipboard_yank

local item = gvim.ui.statusbar.item { id = "vi", text = "", align = "left", priority = 100 }
item:hide()

-- ---------- status row ----------

local function status()
  if not S.enabled or S.suspended then
    item:hide()
    return
  end
  local label
  if S.mode == "insert" then
    label = "-- INSERT --"
  elseif S.mode == "visual" and S.vis then
    label = "-- " .. N.VNAME[S.vis.mode] .. " --"
  else
    label = "-- NORMAL --"
  end
  if S.pending ~= "" and S.mode ~= "insert" then
    label = label .. " " .. S.pending
  end
  item:set(label)
  item:show()
end

-- ---------- modes ----------

local current_mode -- the vi mode the editor is in: "vi_normal", "vi_insert" or "vi_visual"

local function set_mode(name)
  if current_mode == name then
    return
  end
  current_mode = name
  gvim.mode.enter(name)
end

N.hooks.set_mode = set_mode
N.hooks.status = status

local function key(name)
  return function()
    N.feed(name)
  end
end

local function text_handler(ctx)
  local text = ctx.args and ctx.args.text
  if type(text) ~= "string" then
    return
  end
  local col = 0
  while col < #text do
    local b = text:byte(col + 1)
    local n = 1
    if b >= 0xF0 then
      n = 4
    elseif b >= 0xE0 then
      n = 3
    elseif b >= 0xC0 then
      n = 2
    end
    N.feed(text:sub(col + 1, col + n))
    col = col + n
  end
end

local function command_bindings()
  local b = {
    escape = key("escape"),
    ["ctrl+["] = key("escape"),
    enter = key("enter"),
    backspace = key("backspace"),
    tab = key("tab"),
    delete = key("delete"),
  }
  if settings.arrow_keys then
    for _, name in ipairs { "left", "right", "up", "down", "home", "end", "pageup", "pagedown" } do
      b[name] = key(name)
    end
  end
  if settings.ctrl_keys then
    for _, name in ipairs { "r", "f", "b", "d", "u", "e", "y", "v", "a", "x", "g" } do
      b["ctrl+" .. name] = key("ctrl+" .. name)
    end
  end
  return b
end

-- Keys the insert mode moves the caret with: the insert must not be repeated by . after
-- one of them.
local MOVES = {
  left = "move_left", right = "move_right", up = "move_up", down = "move_down",
  home = "smart_home", ["end"] = "move_line_end", pageup = "move_page_up", pagedown = "move_page_down",
}

local function insert_bindings()
  local function leave()
    N.leave_insert()
  end
  local b = { escape = leave, ["ctrl+["] = leave }
  for name, action in pairs(MOVES) do
    b[name] = function()
      N.insert_broken()
      gvim.command.run(action)
    end
  end
  return b
end

gvim.mode.define {
  name = "vi_normal", text = text_handler, paste = "swallow", fallthrough = true,
  bindings = command_bindings(),
}
gvim.mode.define {
  name = "vi_visual", text = text_handler, paste = "swallow", fallthrough = true,
  bindings = command_bindings(),
}
gvim.mode.define {
  name = "vi_insert", text = "pass", paste = "pass", fallthrough = true,
  bindings = insert_bindings(),
}

-- ---------- on and off ----------

local function enable(quiet)
  if S.enabled then
    return
  end
  S.enabled, S.suspended = true, false
  N.reset()
  S.mode = "normal"
  current_mode = nil
  set_mode("vi_normal")
  status()
  if not quiet then
    gvim.notify("vi mode on")
  end
end

local function disable(quiet)
  if not S.enabled then
    return
  end
  S.enabled = false
  N.reset(true)
  current_mode = nil
  gvim.mode.leave()
  status()
  if not quiet then
    gvim.notify("vi mode off")
  end
end

local function toggle()
  if S.enabled then
    disable()
  else
    enable()
  end
end

gvim.command.register {
  name = "toggle",
  title = "Vi Mode: Toggle",
  description = "Turn vi-style modal editing on or off",
  record = false,
  run = toggle,
}

gvim.plugins.export {
  toggle = toggle,
  enable = function() enable() end,
  disable = function() disable() end,
  is_enabled = function() return S.enabled end,
}

-- ---------- events ----------

local me = gvim.plugin.name
local displaced -- the plugin whose mode replaced ours

-- If the editor's input mode is cleared (the panic chord, three Esc) vi is off: that is
-- what the user asked for. If another plugin's mode replaces ours, vi waits and comes back
-- when that mode ends.
gvim.on("mode_changed", function(ev)
  if not S.enabled then
    return
  end
  if ev.mode then
    if ev.plugin ~= me and ev.previous_plugin == me then
      displaced = ev.plugin
      S.suspended = true
      status()
    end
    return
  end
  -- the editor's input mode ended
  if displaced and ev.previous_plugin == displaced then
    displaced = nil
    S.suspended = false
    N.reset()
    current_mode = nil
    set_mode("vi_normal")
    status()
    return
  end
  if S.suspended then
    return -- vi left the mode itself, for a buffer with a mode of its own
  end
  if ev.previous_plugin == me and gvim.mode.current() == nil then
    S.enabled = false
    N.reset(true)
    current_mode = nil
    status()
    gvim.notify("vi mode off: plugin input was cleared (run Vi Mode: Toggle to turn it on again)")
  end
end)

-- A buffer that has a mode of its own (a plugin's outline or list) takes its keys, so vi
-- steps aside while it is active.
gvim.on("buffer_activated", function(ev)
  if not S.enabled then
    return
  end
  local buf = ev.buf
  local info = buf and buf:info()
  local own = info and info.mode ~= nil and info.mode ~= ""
  if own and not S.suspended then
    S.suspended = true
    N.reset()
    current_mode = nil
    gvim.mode.leave()
    status()
    return
  end
  if not own and S.suspended and not displaced then
    S.suspended = false
    current_mode = nil
    S.mode = "normal"
    set_mode("vi_normal")
    status()
    return
  end
  if S.mode ~= "normal" or S.pending ~= "" then
    -- the pending command, the visual area or the insert belong to the buffer that was left
    N.reset()
    set_mode("vi_normal")
    status()
  end
end)

gvim.on("buffer_closed", function(ev)
  for name, m in pairs(S.marks) do
    if m.buf == ev.id then
      S.marks[name] = nil
    end
  end
end)

local function save_state()
  gvim.state.set("registers", Reg.snapshot())
  if S.last_search then
    gvim.state.set("search", S.last_search.pattern)
  end
end

gvim.on("shutdown", save_state)
gvim.plugin.on_unload(save_state)

-- ---------- start ----------

Reg.restore(gvim.state.get("registers"))
local saved = gvim.state.get("search")
if type(saved) == "string" and saved ~= "" then
  S.last_search = { pattern = saved, forward = true }
end

if settings.auto_start then
  enable(true)
end
