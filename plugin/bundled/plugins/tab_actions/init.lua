-- Tab actions. A port of Fresh's tab_actions plugin: close the other tabs, the tabs to the
-- right or to the left of the active one, or all of them, and move the active tab one place
-- left or right. A tab with unsaved changes is never closed, and no dialog asks about it:
-- it stays, and the status message says how many were kept.
local gvim = require "gvim"
local t = gvim.i18n.t

local function plural(n)
  return n == 1 and "1 tab" or (n .. " tabs")
end

-- close_where closes the tabs for which want(position, active_position) is true, from the
-- last to the first. The active tab stays the active tab unless it is one of them.
local function close_where(want)
  local tabs = gvim.buf.list()
  local active
  for i, b in ipairs(tabs) do
    if b == gvim.buf.current() then
      active = i
    end
  end
  if not active then
    return
  end
  local keep = tabs[active]
  local closed, kept = 0, 0
  for i = #tabs, 1, -1 do
    if want(i, active) then
      local ok, err = tabs[i]:close()
      if ok then
        closed = closed + 1
      elseif gvim.err_code(err) == "conflict" then
        kept = kept + 1
      else
        gvim.log.warn("cannot close %s: %s", tabs[i]:name(), tostring(err))
      end
    end
  end
  if keep:valid() then
    keep:show()
  end
  local msg = "Closed " .. plural(closed)
  if kept > 0 then
    msg = msg .. "; kept " .. plural(kept) .. " with unsaved changes"
  end
  gvim.notify(msg)
end

local function move_active(delta)
  local b = gvim.buf.current()
  if b then
    b:move_tab(delta)
  end
end

local commands = {
  {
    name = "close_others",
    title = t("cmd.close_others"),
    description = t("cmd.close_others_desc"),
    run = function()
      close_where(function(i, active) return i ~= active end)
    end,
  },
  {
    name = "close_all",
    title = t("cmd.close_all"),
    description = t("cmd.close_all_desc"),
    run = function()
      close_where(function() return true end)
    end,
  },
  {
    name = "close_to_right",
    title = t("cmd.close_to_right"),
    description = t("cmd.close_to_right_desc"),
    run = function()
      close_where(function(i, active) return i > active end)
    end,
  },
  {
    name = "close_to_left",
    title = t("cmd.close_to_left"),
    description = t("cmd.close_to_left_desc"),
    run = function()
      close_where(function(i, active) return i < active end)
    end,
  },
  {
    name = "move_to_left",
    title = t("cmd.move_to_left"),
    description = t("cmd.move_to_left_desc"),
    run = function() move_active(-1) end,
  },
  {
    name = "move_to_right",
    title = t("cmd.move_to_right"),
    description = t("cmd.move_to_right_desc"),
    run = function() move_active(1) end,
  },
}

for _, c in ipairs(commands) do
  gvim.command.register(c)
end
