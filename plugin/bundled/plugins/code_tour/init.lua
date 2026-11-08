-- Code Tour. A port of Fresh's code-tour plugin.
--
-- Loads a JSON tour (Fresh's .fresh-tour.json schema or a VS Code CodeTour
-- .tour file), shows its steps in a floating widget panel, and walks them:
-- each step opens the file at its line range and highlights it.
--
-- Reduction vs Fresh (docs/plugins/bundled.md):
--   * no Utility Dock: the step panel is a floating widget card (title, step
--     list, prose, location, Prev/Next/Jump/Exit buttons, hint bar).
--   * prose is styled plain text, not rendered Markdown; headings/lists stay
--     text and code blocks are one muted block (the host has no markdown widget
--     renderer yet).
--   * step highlights use local line anchors recomputed on every jump: gvim has
--     no line-targets plugin API yet. The ranges are anchored decorations, so a
--     later edit moves them until the next step.
--   * no default keybindings: Fresh's n/space/p/backspace/g/r/q/Esc keys are
--     binding in its dock-buffer mode; here the panel widgets own the keyboard
--     and the same actions are palette commands and buttons.
--   * one tour at a time (Fresh keeps several dock tabs); the last active tour
--     and step are restored from gvim.state on the next load.
local gvim = require "gvim"

local t = gvim.i18n.t

local MAX_MANIFEST_BYTES = 1024 * 1024
local MAX_STEPS = 200
local MAX_PROSE_LINES = 12
local MAX_PROSE_WIDTH = 56
local PROSE_BYTES = 8 * 1024
local MAX_DISCOVERED = 50
local HIGHLIGHT_BG = "editor.selection_bg"

local ns = gvim.decor.namespace("code_tour")

local state = {
  tour = nil,      -- { path, title, manifest, step, panel }
  panel = nil,     -- mounted widget panel id
  buf = nil,       -- the buffer the current step highlighted
  restoring = false,
}

local function notice(msg)
  gvim.notify(msg)
end

local function active()
  return state.tour ~= nil
end

local function current_step()
  if not state.tour then
    return nil
  end
  return state.tour.manifest.steps[state.tour.step]
end

local function save_state()
  if not state.tour then
    gvim.state.set("code_tour.active", nil)
    return
  end
  gvim.state.set("code_tour.active", {
    path = state.tour.path,
    step = state.tour.step,
  })
end

local function clear_highlight()
  if state.buf and state.buf:valid() then
    ns:clear(state.buf)
  end
  state.buf = nil
end

-- Paths and formats ---------------------------------------------------------

local function workspace_root()
  local root = gvim.workspace.root() or ""
  return root
end

local function absolute(rel)
  if not rel or rel == "" then
    return nil
  end
  if gvim.path.is_absolute(rel) then
    return rel
  end
  local root = workspace_root()
  if root == "" then
    return nil
  end
  return gvim.path.join(root, rel)
end

local function relative(path)
  local root = workspace_root()
  if root == "" then
    return path
  end
  local rel = gvim.path.relative(root, path)
  if rel and rel ~= "" then
    return rel
  end
  return path
end

-- parse_manifest converts either format to one shape:
-- { title, description, steps = { {title, file, from, to, explanation}, ... } }
-- where from/to are 1-based inclusive line numbers (from may be nil for a
-- content-only step).
local function parse_manifest(text, path)
  local data, err = json.decode(text)
  if data == nil then
    return nil, t("error.parse_failed", { path = path, error = tostring(err) })
  end
  if type(data) ~= "table" or type(data.steps) ~= "table" then
    return nil, t("error.parse_failed", { path = path, error = "a tour needs a steps array" })
  end
  local count = 0
  for _ in pairs(data.steps) do
    count = count + 1
  end
  if count == 0 then
    return nil, t("error.no_steps")
  end
  if count > MAX_STEPS then
    return nil, t("error.parse_failed", { path = path, error = "too many steps (max " .. MAX_STEPS .. ")" })
  end
  local ours = data.schema_version ~= nil
  if ours and data.schema_version ~= "1.0" then
    return nil, t("error.bad_version", { version = tostring(data.schema_version) })
  end
  local manifest = {
    title = (type(data.title) == "string" and data.title ~= "") and data.title or gvim.path.basename(path),
    description = type(data.description) == "string" and data.description or "",
    steps = {},
  }
  for i = 1, count do
    local raw = data.steps[i]
    if type(raw) ~= "table" then
      return nil, t("error.parse_failed", { path = path, error = "step " .. i .. " is not an object" })
    end
    local step = { title = "", file = nil, from = nil, to = nil, explanation = "" }
    if ours then
      step.title = type(raw.title) == "string" and raw.title or ""
      step.file = type(raw.file_path) == "string" and raw.file_path or nil
      step.explanation = type(raw.explanation) == "string" and raw.explanation or ""
      if type(raw.lines) == "table" and #raw.lines >= 2 then
        step.from = tonumber(raw.lines[1]) or 1
        step.to = tonumber(raw.lines[2]) or step.from
        if step.to < step.from then
          step.from, step.to = step.to, step.from
        end
      end
    else
      step.title = type(raw.title) == "string" and raw.title or ""
      step.file = type(raw.file) == "string" and raw.file or (type(raw.uri) == "string" and raw.uri or nil)
      step.explanation = type(raw.description) == "string" and raw.description or ""
      local line = tonumber(raw.line)
      local sel = type(raw.selection) == "table" and raw.selection or nil
      if sel then
        local function sel_line(side)
          local part = type(sel[side]) == "table" and sel[side] or nil
          return part and tonumber(part.line) or nil
        end
        local a, b = sel_line("start"), sel_line("end")
        if a and b then
          step.from, step.to = math.min(a, b) + 1, math.max(a, b) + 1
        elseif a then
          step.from, step.to = a + 1, a + 1
        end
      end
      if not step.from and line then
        step.from, step.to = line + 1, line + 1
      end
    end
    if step.from then
      step.from = math.max(step.from, 1)
      step.to = math.max(step.to or step.from, step.from)
    end
    if step.title == "" then
      step.title = t("step.untitled", { n = i })
    end
    if #step.explanation > PROSE_BYTES then
      step.explanation = gvim.text.truncate(step.explanation, PROSE_BYTES, "…")
    end
    manifest.steps[#manifest.steps + 1] = step
  end
  return manifest
end

local function read_manifest(path)
  local text, err = gvim.fs.read(path, { max = MAX_MANIFEST_BYTES })
  if not text then
    return nil, t("error.read_failed", { path = path })
  end
  return parse_manifest(text, path)
end

-- Panel ---------------------------------------------------------------------

-- wrap_prose breaks the explanation into plain rows at the panel width.
local function wrap_prose(text)
  local out = {}
  if not text or text == "" then
    return { t("panel.no_explanation") }
  end
  for raw_line in (text .. "\n"):gmatch("(.-)\n") do
    if raw_line == "" then
      out[#out + 1] = ""
    else
      local line = raw_line
      while #line > 0 and #out < MAX_PROSE_LINES do
        local cut = gvim.text.truncate(line, MAX_PROSE_WIDTH, "")
        if cut == "" then
          cut = line:sub(1, MAX_PROSE_WIDTH)
        end
        out[#out + 1] = cut
        line = line:sub(#cut + 1)
      end
    end
    if #out >= MAX_PROSE_LINES then
      break
    end
  end
  return out
end

local function location_text(step)
  if not step or not step.file then
    return t("panel.content_step")
  end
  if not step.from then
    return t("panel.content_step")
  end
  local abs = absolute(step.file)
  if not abs or not gvim.fs.exists(abs) then
    return t("panel.location_missing")
  end
  return t("panel.location", { file = step.file, from = step.from, to = step.to or step.from })
end

local function panel_spec()
  local tour = state.tour
  local step = current_step()
  local items = {}
  for i, s in ipairs(tour.manifest.steps) do
    local detail = s.file or ""
    if s.from then
      detail = detail .. ":" .. s.from
    end
    items[#items + 1] = { text = s.title, detail = detail }
  end
  local prose = wrap_prose(step and step.explanation or "")
  local children = {
    { kind = "label", text = tour.manifest.title, style = { fg = "syntax.keyword", bold = true } },
    { kind = "divider" },
    {
      kind = "label",
      text = t("panel.step_counter", { step = tour.step, total = #tour.manifest.steps })
        .. "  " .. location_text(step),
      style = { fg = "syntax.comment" },
    },
    { kind = "list", key = "steps", items = items, selected_index = tour.step, visible_rows = 6 },
    { kind = "divider" },
    {
      kind = "text", key = "prose", text = table.concat(prose, "\n"),
      rows = math.max(#prose, 1), read_only = true, field_width = MAX_PROSE_WIDTH,
      full_width = true,
    },
    {
      kind = "row",
      children = {
        { kind = "button", key = "prev", label = t("btn.prev"), disabled = tour.step <= 1 },
        { kind = "button", key = "next", label = tour.step >= #tour.manifest.steps and t("btn.finish") or t("btn.next") },
        { kind = "button", key = "jump", label = t("btn.jump") },
        { kind = "spacer", flex = true },
        { kind = "button", key = "exit", label = t("btn.exit") },
      },
    },
    {
      kind = "hintBar",
      runs = {
        { text = t("hint.next") }, { text = t("hint.prev") },
        { text = t("hint.jump") }, { text = t("hint.exit") },
      },
    },
  }
  return { root = { kind = "col", children = children }, focus_key = "steps" }
end

local function render_panel()
  if not state.panel then
    return
  end
  local ok, err = gvim.ui.update_widget_panel(state.panel, panel_spec())
  if not ok then
    gvim.log.debug("code_tour: panel update: " .. tostring(err))
  end
end

local function mount_panel()
  if state.panel then
    render_panel()
    return
  end
  local id, err = gvim.ui.mount_widget_panel(panel_spec(), {
    title = t("panel.tab", { title = state.tour and state.tour.manifest.title or "Tour" }),
    focus = true,
    width = 60,
    height = 45,
    placement = "float",
  })
  if not id then
    notice(t("error.open_failed", { error = tostring(err) }))
    return
  end
  state.panel = id
end

local function unmount_panel()
  if state.panel then
    gvim.ui.unmount_widget_panel(state.panel)
    state.panel = nil
  end
end

-- Navigation ----------------------------------------------------------------

local goToStep

local function highlight_step(step)
  clear_highlight()
  if not step or not step.file or not step.from then
    return
  end
  local abs = absolute(step.file)
  if not abs or not gvim.fs.exists(abs) then
    notice(t("panel.file_missing", { file = step.file }))
    return
  end
  local buf, err = gvim.buf.open(abs, { pos = gvim.pos(step.from - 1, 0), focus = true })
  if not buf then
    notice(t("error.read_failed", { path = step.file }) .. ": " .. tostring(err))
    return
  end
  state.buf = buf
  local items = {}
  for line = step.from - 1, (step.to or step.from) - 1 do
    items[#items + 1] = {
      kind = "highlight",
      range = gvim.range(line, 0, line, 1073741824),
      style = { bg = HIGHLIGHT_BG },
      priority = 5,
    }
  end
  ns:set(buf, items)
  notice(t("status.jumped", { file = step.file, line = step.from }))
end

goToStep = function(index, jump)
  local tour = state.tour
  if not tour then
    return
  end
  local total = #tour.manifest.steps
  if index < 1 then
    notice(t("status.at_first"))
    return
  end
  if index > total then
    notice(t("status.at_last"))
    return
  end
  tour.step = index
  local step = tour.manifest.steps[index]
  save_state()
  render_panel()
  if jump ~= false then
    highlight_step(step)
  end
  notice(t("status.current_step", { step = index, total = total }))
end

local function exit_tour(quiet)
  if not active() then
    return
  end
  local title = state.tour.manifest.title
  clear_highlight()
  unmount_panel()
  state.tour = nil
  gvim.context.set("tour_active", false)
  save_state()
  if not quiet then
    notice(t("status.ended", { title = title }))
  end
end

local function start_tour(path, manifest, step)
  exit_tour(true)
  state.tour = {
    path = path,
    manifest = manifest,
    step = math.max(1, math.min(step or 1, #manifest.steps)),
  }
  gvim.context.set("tour_active", true)
  save_state()
  mount_panel()
  render_panel()
  -- Opening a tour shows its first step; a restored tour only rebuilds the
  -- panel and leaves the editor where it was.
  goToStep(state.tour.step, not state.restoring)
end

local function load_tour(path, step)
  if not path or path == "" then
    return
  end
  local manifest, err = read_manifest(path)
  if not manifest then
    notice(err)
    return
  end
  start_tour(path, manifest, step)
end

-- Discovery and create ------------------------------------------------------

local function discover_tours()
  local root = workspace_root()
  if root == "" then
    return {}
  end
  local entries = gvim.fs.walk(root, {
    hidden = true,
    max_depth = 5,
    max_entries = 20000,
    skip_dirs = { ".git", "node_modules" },
  })
  local out, seen = {}, {}
  local found = entries and entries.entries or {}
  for _, entry in ipairs(found) do
    if entry.kind == "file" then
      local name = gvim.path.basename(entry.path)
      local is_tour = name:sub(-5) == ".tour"
        or name:sub(-16) == ".fresh-tour.json"
        or (name:sub(-5) == ".json" and (entry.rel:find("%.tours/") ~= nil or entry.rel:find("/%.tours/") ~= nil))
      if is_tour and not seen[entry.path] then
        seen[entry.path] = true
        out[#out + 1] = entry.path
        if #out >= MAX_DISCOVERED then
          break
        end
      end
    end
  end
  table.sort(out)
  local items = {}
  for _, path in ipairs(out) do
    items[#items + 1] = { text = gvim.path.basename(path), detail = relative(path) }
  end
  return items
end

local function slug(title)
  local s = gvim.text.lower(title)
  s = s:gsub("[^a-z0-9]+", "-"):gsub("^-+", ""):gsub("-+$", "")
  if s == "" then
    return "tour"
  end
  return s
end

local function create_tour()
  local buf = gvim.buf.current()
  local info = buf and buf:info() or nil
  if not buf or not info or info.virtual or not info.path or info.path == "" then
    notice(t("error.no_file"))
    return
  end
  local root = workspace_root()
  if root == "" then
    notice(t("error.no_workspace"))
    return
  end
  local title = gvim.ui.input { prompt = t("prompt.title"), default = "Code Tour", select_all = true }
  if title == nil then
    return
  end
  title = gvim.text.trim(title)
  if title == "" then
    title = "Code Tour"
  end
  local rel = relative(info.path)
  local cursor = buf:cursor()
  local default_path = gvim.path.join(root, ".tours", slug(title) .. ".tour")
  local answer = gvim.ui.input { prompt = t("prompt.create_path"), default = default_path, select_all = true }
  if answer == nil then
    return
  end
  answer = gvim.text.trim(answer)
  if answer == "" then
    return
  end
  local path = absolute(answer)
  if not path then
    return
  end
  local manifest = {
    title = title,
    description = "",
    ref = "",
    steps = {
      {
        title = title,
        file = rel,
        line = cursor.line,
        description = "Describe what this step shows.",
      },
    },
  }
  local encoded = json.encode(manifest)
  if not encoded then
    notice(t("error.write_failed", { error = "could not encode the manifest" }))
    return
  end
  local ok, err = gvim.fs.write(path, encoded, { atomic = true, parents = true })
  if not ok then
    notice(t("error.write_failed", { error = tostring(err) }))
    return
  end
  notice(t("status.created", { path = path }))
  load_tour(path, 1)
end

-- Commands ------------------------------------------------------------------

local function with_tour(fn)
  return function()
    if not active() then
      notice(t("error.no_tour"))
      return
    end
    fn()
  end
end

gvim.command.register {
  name = "load",
  title = "%cmd.tour_load",
  description = "%cmd.tour_load_desc",
  run = function()
    local path = gvim.ui.input { prompt = t("prompt.path"), select_all = false }
    if path == nil then
      return
    end
    path = gvim.text.trim(path)
    if path == "" then
      return
    end
    load_tour(absolute(path), 1)
  end,
}

gvim.command.register {
  name = "discover",
  title = "%cmd.tour_discover",
  description = "%cmd.tour_discover_desc",
  run = function()
    local items = discover_tours()
    if #items == 0 then
      notice(t("error.read_failed", { path = workspace_root() }))
      return
    end
    local item = gvim.ui.select(items, { title = t("cmd.tour_discover"), filter = "fuzzy" })
    if item and item.detail then
      load_tour(absolute(item.detail), 1)
    end
  end,
}

gvim.command.register {
  name = "next",
  title = "%cmd.tour_next",
  description = "%cmd.tour_next_desc",
  run = with_tour(function()
    goToStep(state.tour.step + 1)
  end),
}

gvim.command.register {
  name = "prev",
  title = "%cmd.tour_prev",
  description = "%cmd.tour_prev_desc",
  run = with_tour(function()
    goToStep(state.tour.step - 1)
  end),
}

gvim.command.register {
  name = "exit",
  title = "%cmd.tour_exit",
  description = "%cmd.tour_exit_desc",
  run = function()
    exit_tour(false)
  end,
}

gvim.command.register {
  name = "create",
  title = "%cmd.tour_create",
  description = "%cmd.tour_create_desc",
  run = create_tour,
}

-- Widget events and lifecycle ------------------------------------------------

gvim.on("widget_event", function(ev)
  if not state.panel or ev.panel ~= state.panel then
    return
  end
  if ev.event == "activate" then
    if ev.widget == "prev" then
      goToStep(state.tour.step - 1)
    elseif ev.widget == "next" then
      if state.tour.step >= #state.tour.manifest.steps then
        exit_tour(false)
      else
        goToStep(state.tour.step + 1)
      end
    elseif ev.widget == "jump" then
      highlight_step(current_step())
    elseif ev.widget == "exit" then
      exit_tour(false)
    end
  elseif ev.event == "select" and ev.widget == "steps" then
    goToStep(ev.index or state.tour.step)
  elseif ev.event == "close" or ev.event == "cancel" then
    exit_tour(false)
  end
end)

local function restore_tour()
  if active() or state.restoring then
    return
  end
  gvim.context.set("tour_active", false)
  local saved = gvim.state.get("code_tour.active", nil)
  if type(saved) ~= "table" then
    return
  end
  local path = absolute(saved.path)
  if not path or not gvim.fs.exists(path) then
    gvim.state.set("code_tour.active", nil)
    return
  end
  local manifest = read_manifest(path)
  if not manifest then
    gvim.state.set("code_tour.active", nil)
    return
  end
  state.restoring = true
  start_tour(path, manifest, tonumber(saved.step) or 1)
  state.restoring = false
end

gvim.on("ready", function()
  restore_tour()
end)

gvim.on("plugins_loaded", function()
  restore_tour()
end)

gvim.plugin.on_unload(function()
  unmount_panel()
  clear_highlight()
  gvim.context.set("tour_active", false)
end)

gvim.log.debug("code_tour plugin loaded")
