-- Registers. A register holds text and how it was taken: "char" (a stretch of characters),
-- "line" (whole lines; the text ends with "\n") or "block" (a column of text; the lines are
-- joined with "\n" and the text has no final "\n"). The unnamed register is the one most
-- recently written or read. Register 0 holds the last yank, 1 to 9 the last deleted lines
-- (newest first), "-" the last small delete, a-z the registers you name, and A-Z appends to
-- them. "_" is the black hole. "+" and "*" are the system clipboard, which is written (but
-- cannot be read back by a plugin).
local gvim = require "gvim"

local M = {}

local regs = {}
local unnamed = nil

-- opts.clipboard: write the system clipboard on a yank (the setting clipboard_yank).
M.opts = { clipboard = true }

local function copy(v) return { text = v.text, kind = v.kind } end

local function valid(name)
  return name == nil or name:find("^[%a0-9\"%-_+*]$") ~= nil
end
M.valid = valid

-- is_clip reports whether the name is the system clipboard.
function M.is_clip(name) return name == "+" or name == "*" end

-- set stores a value written by an operator: op is "y" for a yank, "d" for a delete or a
-- change. name is the register the user asked for, or nil.
function M.set(name, text, kind, op)
  if name == "_" then
    return
  end
  local v = { text = text, kind = kind }
  if name and name:find("^%u$") then
    local old = regs[name:lower()]
    if old then
      if old.kind == "line" or kind == "line" then
        local a = old.text
        if old.kind ~= "line" then
          a = a .. "\n"
        end
        v = { text = a .. text, kind = "line" }
      else
        v = { text = old.text .. text, kind = old.kind }
      end
    end
    regs[name:lower()] = v
  elseif name and name:find("^[%l0-9\"]$") then
    if name ~= '"' then
      regs[name] = v
    end
  end
  if name == nil or name == '"' or name:find("^%u$") then
    if op == "y" then
      regs["0"] = copy(v)
    elseif kind == "line" or text:find("\n", 1, true) then
      for i = 9, 2, -1 do
        regs[tostring(i)] = regs[tostring(i - 1)]
      end
      regs["1"] = copy(v)
    else
      regs["-"] = copy(v)
    end
  end
  unnamed = v
  if M.is_clip(name) or (op == "y" and M.opts.clipboard) then
    gvim.clipboard.set(v.text)
  end
end

-- get returns the value of a register ({text=, kind=}), or nil when it is empty. nil asks
-- for the unnamed register.
function M.get(name)
  if name == nil or name == '"' then
    return unnamed
  end
  if name == "_" then
    return nil
  end
  return regs[name:lower()]
end

-- snapshot and restore let the plugin keep the named registers between sessions.
function M.snapshot()
  local out = {}
  for name, v in pairs(regs) do
    if name:find("^%l$") and #v.text <= 65536 then
      out[name] = { text = v.text, kind = v.kind }
    end
  end
  return out
end

function M.restore(saved)
  if type(saved) ~= "table" then
    return
  end
  for name, v in pairs(saved) do
    if type(name) == "string" and name:find("^%l$") and type(v) == "table" and type(v.text) == "string"
        and (v.kind == "char" or v.kind == "line" or v.kind == "block") then
      regs[name] = { text = v.text, kind = v.kind }
    end
  end
end

-- list returns {name, value} rows for :registers, in a fixed order.
function M.list()
  local out = {}
  local function add(name, v)
    if v then
      out[#out + 1] = { name = name, value = v }
    end
  end
  add('"', unnamed)
  for _, n in ipairs { "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "-" } do
    add(n, regs[n])
  end
  for c = 97, 122 do
    add(string.char(c), regs[string.char(c)])
  end
  return out
end

return M
