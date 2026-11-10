-- Live Agent Sessions: the reduced Lua port of Fresh's live_sessions plugin.
--
-- Fresh keeps the orchestrator dock's external Claude/Codex group current: it polls the
-- vendors' local/cloud listings while the dock is open and hands rows to the orchestrator.
-- gvim has no orchestrator dock, no cloud sign-in reader and no process-attach/agent-resume
-- surface. The real subset available here is gvim's native read-only agent discovery: tmux
-- sessions and local Claude Code/Codex transcript history, exposed through
-- gvim.agent_sessions.discover (the app's bounded, trust-gated scan).
--
-- This plugin shows those rows in a picker while it is open, polls every POLL_MS, and
-- refreshes on a timer or the refresh command. Enter opens a transcript row's file; a live
-- tmux row is informational and says that process attach is not ported. A picker can be
-- closed while a scan is in flight: the generation guard discards that answer and the timer
-- is cancelled, so a late result cannot reopen or overwrite a closed list. If the user reopens
-- before that scan returns, the reopened picker's pending refresh is consumed by the stale
-- completion, which starts a fresh scan immediately instead of waiting for the poll tick.
local gvim = require "gvim"

local t = gvim.i18n.t

local POLL_MS = 60000
local MAX_ROWS = 200
local MAX_PROBLEM = 200

local state = {
  picker = nil,
  timer = nil,
  generation = 0,
  running = false,
  pending = false,
}

local refresh

local function stop_poll()
  if state.timer then
    state.timer:cancel()
  end
  state.timer = nil
end

-- close_state invalidates every in-flight scan and drops the picker/timer. It is safe to
-- call twice (Esc, a selection, unload).
local function close_state()
  state.generation = state.generation + 1
  stop_poll()
  state.picker = nil
  state.pending = false
end

local function clip(s, limit)
  s = tostring(s or ""):gsub("[\r\n]+", " ")
  if #s <= limit then
    return s
  end
  local cut = s:sub(1, limit)
  -- The source strings are UTF-8. Drop a partial trailing sequence; removing a whole
  -- trailing multibyte character is acceptable for a display clip.
  while #cut > 0 and cut:byte(-1) >= 128 do
    cut = cut:sub(1, #cut - 1)
  end
  return cut .. "…"
end

local function tool_name(tool)
  if tool == "codex-cli" then
    return t("tool.codex")
  end
  if tool == "claude-code" then
    return t("tool.claude")
  end
  return t("tool.tmux")
end

local function row_text(session)
  if session.kind == "tmux" then
    local text = t("row.tmux", { title = clip(session.title, 120) })
    if session.attached then
      text = text .. t("row.attached")
    end
    if type(session.agents) == "table" and #session.agents > 0 then
      text = text .. " · " .. table.concat(session.agents, ",")
    end
    return text
  end
  return t("row.history", { tool = tool_name(session.tool), title = clip(session.title, 120) })
end

local function row_detail(session)
  if session.kind == "tmux" then
    local dir = clip(session.dir or "", 200)
    if session.socket_only then
      return dir ~= "" and (dir .. " · server not queried") or "server not queried"
    end
    return dir ~= "" and dir or ""
  end
  local parts = {}
  if session.agent and session.agent ~= "" then
    parts[#parts + 1] = clip(session.agent, 40)
  end
  if session.dir and session.dir ~= "" then
    parts[#parts + 1] = clip(session.dir, 200)
  end
  if not session.exact then
    parts[#parts + 1] = "resume is inexact"
  end
  return table.concat(parts, " · ")
end

local function set_status(text)
  if state.picker then
    state.picker:set_status(text)
  end
end

local function show_empty(key)
  if not state.picker then
    return
  end
  state.picker:set_items { { text = t(key), disabled = true } }
  set_status(t(key))
end

local function apply(report)
  if not state.picker then
    return
  end
  if type(report) ~= "table" or report.supported ~= true then
    show_empty("row.unsupported")
    return
  end
  local sessions = type(report.sessions) == "table" and report.sessions or {}
  local items = {}
  for i = 1, math.min(#sessions, MAX_ROWS) do
    local session = sessions[i]
    if type(session) == "table" then
      items[#items + 1] = {
        text = row_text(session),
        detail = row_detail(session),
        session = session,
      }
    end
  end
  if #items == 0 then
    show_empty(report.installed and "row.none" or "row.no_store")
    return
  end
  state.picker:set_items(items)
  local problems = type(report.problems) == "table" and report.problems or {}
  if #problems > 0 then
    set_status(t("status.count_problems", {
      count = #items,
      problems = clip(problems[1], MAX_PROBLEM),
    }))
  else
    set_status(t("status.count", { count = #items }))
  end
end

-- refresh is a task: the discovery await runs off the editor loop. A close during the
-- await bumps generation, so its result is discarded. A refresh requested while one is in
-- flight is coalesced into one more round.
refresh = function()
  if not state.picker then
    return
  end
  if state.running then
    state.pending = true
    return
  end
  state.running = true
  local generation = state.generation
  local report, err = gvim.agent_sessions.discover()
  state.running = false
  if generation ~= state.generation or not state.picker then
    -- The picker was closed and possibly reopened while this scan was in flight. The
    -- reopened picker's refresh set pending; consume it now that running is false so the
    -- new picker starts a fresh scan instead of sitting in Scanning until the poll tick.
    if state.pending and state.picker then
      state.pending = false
      refresh()
    end
    return
  end
  if not report then
    gvim.log.debug("live_sessions: %s", tostring(err))
    show_empty("row.unsupported")
    return
  end
  apply(report)
  if state.pending and state.picker then
    state.pending = false
    refresh()
  end
end

local function open_session(session)
  if session.kind == "tmux" then
    gvim.notify(t("status.tmux_no_attach", {
      name = clip(session.title, 120),
      dir = session.dir ~= "" and clip(session.dir, 200) or "(not reported)",
    }))
    return
  end
  local path = session.path or ""
  if path == "" then
    gvim.notify(t("status.no_location"))
    return
  end
  local buf, err = gvim.buf.open(path, { pos = gvim.pos(0, 0), focus = true })
  if not buf then
    gvim.notify(t("status.open_failed", { path = path, error = tostring(err) }), "warn")
    return
  end
  gvim.notify(t("status.opened", { path = path }))
end

local function on_select(item)
  local session = item and item.session
  close_state()
  if type(session) ~= "table" then
    return
  end
  open_session(session)
end

local function show()
  if state.picker then
    refresh()
    return
  end
  state.generation = state.generation + 1
  local picker = gvim.ui.picker {
    title = t("cmd.show"),
    placeholder = t("placeholder"),
    footer = t("footer"),
    filter = "fuzzy",
    items = { { text = t("status.scanning"), disabled = true } },
    on_select = on_select,
    on_close = close_state,
  }
  state.picker = picker
  picker:open()
  picker:set_status(t("status.scanning"))
  state.timer = gvim.timer.every(POLL_MS, function()
    refresh()
  end)
  refresh()
end

gvim.command.register {
  name = "show",
  title = "%cmd.show",
  description = "%cmd.show_desc",
  run = show,
}

gvim.command.register {
  name = "refresh",
  title = "%cmd.refresh",
  description = "%cmd.refresh_desc",
  run = function()
    if state.picker then
      refresh()
    else
      show()
    end
  end,
}

gvim.plugin.on_unload(close_state)

gvim.log.debug("live sessions plugin loaded")
