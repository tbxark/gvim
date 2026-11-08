-- Git explorer. A port of Fresh's git_explorer plugin.
--
-- Adds VS Code-style status badges (M/A/D/R/C/U/!) to the file explorer and, with the
-- color_names setting on, colours the names of changed entries. The status comes from
-- git status --porcelain -z in the workspace root's repository.
--
-- Differences from Fresh (documented in bundled.md): only the workspace root's repository is
-- decorated (no nested sub-repository discovery); there are no hover tooltips or arbitrary
-- icons (symbols only); a setting change applies on the next refresh; the plugin is off by
-- default to keep the default memory budget.
local gvim = require "gvim"
local git = gvim.git

local NAMESPACE = "git_explorer"

local COLORS = {
  added = "ui.file_status_added_fg",
  modified = "ui.file_status_modified_fg",
  deleted = "ui.file_status_deleted_fg",
  renamed = "ui.file_status_renamed_fg",
  untracked = "ui.file_status_untracked_fg",
  conflicted = "ui.file_status_conflicted_fg",
}

local PRIORITY = {
  conflicted = 90,
  deleted = 80,
  added = 60,
  modified = 50,
  renamed = 40,
  untracked = 30,
}

local settings = gvim.settings.define {
  color_names = {
    type = "boolean", default = false,
    description = "Color file explorer entry names by git status",
  },
}

local refresh_in_flight = false
local refresh_pending = false
local watcher = nil
local watched_dir = nil

local function status_to_decoration(x, y)
  if x == "?" and y == "?" then
    return { symbol = "U", color = COLORS.untracked, priority = PRIORITY.untracked }
  end
  local status, staged
  if x ~= " " and x ~= "?" then
    status, staged = x, true
  elseif y ~= " " then
    status, staged = y, false
  else
    return nil
  end
  if status == "A" then
    return { symbol = "A", color = COLORS.added, priority = PRIORITY.added }
  elseif status == "M" then
    return {
      symbol = "M",
      color = staged and COLORS.added or COLORS.modified,
      priority = PRIORITY.modified + (staged and 2 or 0),
    }
  elseif status == "D" then
    return { symbol = "D", color = COLORS.deleted, priority = PRIORITY.deleted }
  elseif status == "R" or status == "C" then
    return { symbol = status, color = COLORS.renamed, priority = PRIORITY.renamed }
  elseif status == "U" then
    return { symbol = "!", color = COLORS.conflicted, priority = PRIORITY.conflicted }
  end
  return nil
end

-- under_root reports whether p is root or inside it.
local function under_root(p, root)
  if p == root then
    return true
  end
  return p:sub(1, #root + 1) == root .. "/"
end

-- parse reads porcelain v1 -z output into decorations { path, symbol, color, priority }.
local function parse(root, output)
  local by_path, order = {}, {}
  local entries = gvim.text.split(output, "\0", true)
  local i = 1
  while i <= #entries do
    local entry = (entries[i]:gsub("\r$", ""))
    i = i + 1
    if #entry >= 3 then
      local x, y, path = entry:sub(1, 1), entry:sub(2, 2), entry:sub(4)
      if (x == "R" or x == "C") and i <= #entries then
        i = i + 1 -- the original path of a rename/copy follows as its own entry
      end
      local d = status_to_decoration(x, y)
      if d and path ~= "" then
        local abs = gvim.path.join(root, path)
        local old = by_path[abs]
        if not old then
          order[#order + 1] = abs
        end
        if not old or d.priority >= old.priority then
          by_path[abs] = { path = abs, symbol = d.symbol, color = d.color, priority = d.priority }
        end
      end
    end
  end
  local out = {}
  for _, path in ipairs(order) do
    out[#out + 1] = by_path[path]
  end
  return out
end

-- build_name_slots colours each changed path and its ancestor directories.
local function build_name_slots(root, decorations)
  local by_path, order = {}, {}
  for _, d in ipairs(decorations) do
    local function add(path)
      local old = by_path[path]
      if not old then
        order[#order + 1] = path
      end
      if not old or d.priority >= old.priority then
        by_path[path] = { path = path, name_color = d.color, priority = d.priority }
      end
    end
    add(d.path)
    local ancestor = d.path
    while true do
      local parent = gvim.path.dirname(ancestor)
      if parent == "" or parent == ancestor then
        break
      end
      if not under_root(parent, root) then
        break
      end
      add(parent)
      ancestor = parent
    end
  end
  local out = {}
  for _, path in ipairs(order) do
    out[#out + 1] = by_path[path]
  end
  return out
end

local function clear_all()
  gvim.explorer.clear_decorations(NAMESPACE)
  gvim.explorer.clear_slots(NAMESPACE)
end

local function watch_repo(head)
  local dir = gvim.path.dirname(head)
  if dir == "" or dir == watched_dir then
    return
  end
  if watcher then
    watcher:stop()
    watcher = nil
  end
  watched_dir = dir
  watcher = gvim.fs.watch(dir, function()
    refresh()
  end, { debounce_ms = 200 })
end

function refresh()
  if refresh_in_flight then
    refresh_pending = true
    return
  end
  refresh_in_flight = true
  local ok, err = pcall(function()
    if not git.available() then
      clear_all()
      return
    end
    local cwd = gvim.workspace.root()
    if cwd == "" or cwd == nil then
      clear_all()
      return
    end
    local repo = git.repo(cwd)
    if not repo then
      clear_all()
      return
    end
    watch_repo(repo.head)
    local r = git.run({ "status", "--porcelain", "-z" }, { cwd = repo.root })
    if not r or not r.ok then
      clear_all()
      return
    end
    local decorations = parse(repo.root, r.stdout)
    if #decorations == 0 then
      clear_all()
      return
    end
    gvim.explorer.set_decorations(NAMESPACE, decorations)
    if settings.color_names then
      gvim.explorer.set_slots(NAMESPACE, build_name_slots(repo.root, decorations))
    else
      gvim.explorer.clear_slots(NAMESPACE)
    end
  end)
  refresh_in_flight = false
  if not ok then
    clear_all()
    gvim.log.warn("git_explorer: %s", tostring(err))
  end
  if refresh_pending then
    refresh_pending = false
    refresh()
  end
end

gvim.on("ready", refresh)
gvim.on("after_file_open", refresh)
gvim.on("after_file_save", refresh)
gvim.on("after_file_revert", refresh)
gvim.on("focus_gained", refresh)
gvim.on("config_changed", refresh)

refresh()
