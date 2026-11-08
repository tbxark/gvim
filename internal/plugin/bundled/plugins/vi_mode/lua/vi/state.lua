-- The state the modules of vi_mode share. It is plain data; nothing here has behaviour.
local S = {
  enabled = false, -- vi is on: one of its modes is the editor's input mode
  mode = "normal", -- "normal", "insert" or "visual"
  suspended = false, -- vi stepped aside (a buffer with a mode of its own, another plugin's mode)

  -- settings (replaced by init.lua from gvim.settings)
  settings = {},

  -- what the next command remembers
  last_find = nil, -- {ch=, forward=, till=} for ; and ,
  last_search = nil, -- {pattern=, forward=, nosmart=} for n and N
  hl = nil, -- {pattern=, nosmart=} while the matches of a search are highlighted
  last_change = nil, -- the command . repeats
  sticky_eol = nil, -- where $ left the caret, while j and k keep to the end of the line
  prev_pos = nil, -- the position before the last jump, for '' and ``
  marks = {}, -- name -> {buf=, line=, col=}
  visual_marks = nil, -- {from=, to=} of the last visual area, for '< and '>
  last_visual = nil, -- {mode=, anchor=, head=} for gv

  -- the visual area, while the mode is "visual": mode "v", "V" or "b"
  vis = nil,

  -- a command being read: the keys shown in the status row
  pending = "",

  -- what was typed at the : and / prompts, oldest first (Up and Down in the prompt walk it)
  history = { ex = {}, search = {} },

  -- the insert in progress
  ins = nil,

  -- set while . replays a change
  replay = nil,
}

return S
