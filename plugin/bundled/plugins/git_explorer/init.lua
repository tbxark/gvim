-- Git explorer. A port of Fresh's git_explorer plugin.
--
-- Adds VS Code-style status badges (M/A/D/R/C/U/!) to the file explorer and, with the
-- color_names setting on, colours the names of changed entries. The status comes from
-- git status --porcelain -z in the workspace root's repository.
--
-- Nested independent repositories below the workspace root are decorated too (Fresh's
-- #2592 monorepo behavior): a bounded breadth-first directory scan (one gvim.fs.list per
-- candidate directory, never one exists call per entry) finds a directory whose .git
-- entry is present and runs git status from that directory's own root. The scan is capped
-- at 128 directory listings, depth 3, 32 repos and a 1.5 s wall-clock/status budget; a
-- partial scan decorates what it found and shows a one-time warning. The root repo's .git
-- directory and every discovered regular nested repo's .git directory are watched (at most the
-- 32-repo discovery cap plus one), so commits/checkouts in another terminal refresh the badges
-- for the whole monorepo; linked-worktree .git files are skipped because their HEAD lives
-- elsewhere. Differences from Fresh (documented in bundled.md): no hover tooltips or arbitrary
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
-- One HEAD-directory watcher per root/nested repository, bounded by the discovery repo cap.
local watchers = {}


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

-- Discovery bounds for nested repositories. Fresh walks the monorepo recursively
-- (MONOREPO_MAX_DEPTH = 3); gvim uses one bounded directory listing per candidate
-- directory (never a per-entry exists round-trip) and works through the same plugin host
-- as the editor, so a remote authority sees at most DISCOVERY_MAX_DIRS ReadDir calls. The
-- time budget and the directory/repo caps stop a slow or huge tree; the result is then
-- partial and a one-time warning says so. Only root-repository HEAD is watched; nested
-- repositories are refreshed by the ordinary file/focus/config events.
local MONOREPO_MAX_DEPTH = 3
local DISCOVERY_MAX_DIRS = 128
local DISCOVERY_MAX_REPOS = 32
local DISCOVERY_BUDGET_MS = 1500
local MAX_WATCHERS = DISCOVERY_MAX_REPOS + 1 -- every discovered repo plus the root

local unloaded = false
local refresh_generation = 0
local discovery_notice_root = nil

local function notify_discovery_partial(root)
  if discovery_notice_root == root then
    return
  end
  discovery_notice_root = root
  gvim.notify(
    "git_explorer: nested repository discovery stopped at its directory/time bound; only the repositories found so far are decorated",
    "warn"
  )
end

-- discover_subrepos lists directories breadth-first below root under a hard directory
-- budget and a wall-clock budget. A directory is an independent repository when its
-- listing contains .git (a directory for a normal repo, a file for a worktree/linked
-- checkout). Hidden and node_modules directories are skipped; .git contents are never
-- listed. It returns repos, complete; complete is false when a bound stopped the scan.
local function discover_subrepos(root, generation)
  local repos = {}
  local started = gvim.time.monotonic_ms()
  local reads = 0
  local complete = true
  local queue = { { path = root, depth = 0 } }
  local head = 1
  while head <= #queue do
    if unloaded or generation ~= refresh_generation then
      return repos, false
    end
    if #repos >= DISCOVERY_MAX_REPOS or reads >= DISCOVERY_MAX_DIRS or
      gvim.time.monotonic_ms() - started >= DISCOVERY_BUDGET_MS then
      complete = false
      break
    end
    local item = queue[head]
    head = head + 1
    reads = reads + 1
    local entries = gvim.fs.list(item.path, { hidden = true })
    if entries then
      local has_git = false
      for _, entry in ipairs(entries) do
        if entry.name == ".git" then
          has_git = true
        end
      end
      if item.depth > 0 and has_git then
        repos[#repos + 1] = item.path
        if #repos >= DISCOVERY_MAX_REPOS then
          complete = false
          break
        end
      else
        for _, entry in ipairs(entries) do
          if entry.kind == "dir" and entry.name ~= ".git" and entry.name ~= "node_modules" and
            entry.name:sub(1, 1) ~= "." and item.depth < MONOREPO_MAX_DEPTH then
            queue[#queue + 1] = { path = gvim.path.join(item.path, entry.name), depth = item.depth + 1 }
          end
        end
      end
    end
  end
  if head <= #queue then
    complete = false
  end
  return repos, complete
end

-- add_group reads one repository's porcelain v1 -z status and keeps its root with the
-- decorations, so color slots can be built per-repo (a sibling path sharing a string
-- prefix is not mistaken for the same repository).
local function add_group(groups, seen, root)
  if root == "" or root == nil or seen[root] then
    return
  end
  seen[root] = true
  local r = git.run({ "status", "--porcelain", "-z" }, { cwd = root })
  if not r or not r.ok then
    return
  end
  local decorations = parse(root, r.stdout)
  if #decorations > 0 then
    groups[#groups + 1] = { root = root, decorations = decorations }
  end
end

-- merge_decorations combines per-repo badges by absolute path, keeping the highest
-- priority (the same rule Fresh applies inside one repo), and returns a stable order.
local function merge_decorations(groups)
  local by_path, order = {}, {}
  for _, group in ipairs(groups) do
    for _, d in ipairs(group.decorations) do
      local old = by_path[d.path]
      if not old then
        order[#order + 1] = d.path
      end
      if not old or d.priority >= old.priority then
        by_path[d.path] = d
      end
    end
  end
  local out = {}
  for _, path in ipairs(order) do
    out[#out + 1] = by_path[path]
  end
  return out
end

-- merge_slots combines the name-color slots of every repo by absolute path.
local function merge_slots(groups)
  local by_path, order = {}, {}
  for _, group in ipairs(groups) do
    for _, s in ipairs(build_name_slots(group.root, group.decorations)) do
      local old = by_path[s.path]
      if not old then
        order[#order + 1] = s.path
      end
      if not old or s.priority >= old.priority then
        by_path[s.path] = s
      end
    end
  end
  local out = {}
  for _, path in ipairs(order) do
    out[#out + 1] = by_path[path]
  end
  return out
end

local function watcher_count()
  local n = 0
  for _ in pairs(watchers) do
    n = n + 1
  end
  return n
end

-- watch_repo adds want[dir] and starts a debounced watcher when the repo is new and the
-- watcher cap has room. Reconciliation after a refresh stops watchers for repos that vanished.
local function watch_repo(head, want)
  if type(head) ~= "string" then
    return
  end
  local dir = gvim.path.dirname(head)
  if dir == "" then
    return
  end
  want[dir] = true
  if watchers[dir] or watcher_count() >= MAX_WATCHERS then
    return
  end
  local w = gvim.fs.watch(dir, function()
    if not unloaded then
      refresh()
    end
  end, { debounce_ms = 200 })
  if w then
    watchers[dir] = w
  end
end

-- watch_nested_repo watches the nested repo's .git directory directly. Discovery already
-- proved a .git entry exists; a regular repository has a .git directory whose HEAD moves on
-- commit/checkout. Linked-worktree .git files are skipped here (their HEAD lives elsewhere),
-- which is documented as a residual.
local function watch_nested_repo(sub, want)
  local gitdir = gvim.path.join(sub, ".git")
  local st = gvim.fs.stat(gitdir)
  if st and st.kind == "dir" then
    watch_repo(gvim.path.join(gitdir, "HEAD"), want)
  end
end

local function prune_watchers(want)
  for dir, w in pairs(watchers) do
    if not want[dir] then
      w:stop()
      watchers[dir] = nil
    end
  end
end

local function stop_watchers()
  for _, w in pairs(watchers) do
    w:stop()
  end
  watchers = {}
end

function refresh()
  if refresh_in_flight then
    refresh_pending = true
    return
  end
  refresh_in_flight = true
  refresh_generation = refresh_generation + 1
  local generation = refresh_generation
  local ok, err = pcall(function()
    local want = {}
    if not git.available() then
      prune_watchers(want)
      clear_all()
      return
    end
    local cwd = gvim.workspace.root()
    if cwd == "" or cwd == nil then
      prune_watchers(want)
      clear_all()
      return
    end
    local groups, seen = {}, {}
    local started = gvim.time.monotonic_ms()
    local root_repo = git.repo(cwd)
    if root_repo then
      watch_repo(root_repo.head, want)
      add_group(groups, seen, root_repo.root)
    end
    local subrepos, complete = discover_subrepos(cwd, generation)
    if unloaded then
      return
    end
    for _, sub in ipairs(subrepos) do
      if unloaded or generation ~= refresh_generation then
        return
      end
      if gvim.time.monotonic_ms() - started >= DISCOVERY_BUDGET_MS then
        complete = false
        break
      end
      watch_nested_repo(sub, want)
      add_group(groups, seen, sub)
    end
    prune_watchers(want)
    if not complete then
      notify_discovery_partial(cwd)
    end
    if #groups == 0 then
      clear_all()
      return
    end
    local decorations = merge_decorations(groups)
    if #decorations == 0 then
      clear_all()
      return
    end
    gvim.explorer.set_decorations(NAMESPACE, decorations)
    if settings.color_names then
      gvim.explorer.set_slots(NAMESPACE, merge_slots(groups))
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

-- Stop the watcher and make any yielded discovery/status step stop applying its result
-- once the plugin is unloaded.
gvim.plugin.on_unload(function()
  unloaded = true
  refresh_generation = refresh_generation + 1
  stop_watchers()
  clear_all()
end)
