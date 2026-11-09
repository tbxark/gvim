-- Git status bar item. A port of Fresh's git_statusbar plugin: the branch of the
-- repository that holds the active file (or the workspace), shown on the right of the
-- status row. A detached HEAD shows the first seven digits of the commit. Outside a
-- repository the item is hidden.
--
-- The branch is read when a file opens, when a tab is activated or a file is saved, and
-- when the repository's HEAD file changes (a `git checkout` in another terminal): the plugin
-- watches the directory that holds HEAD, so git is not run on any other occasion.
local gvim = require "gvim"
local git = gvim.git

local item = gvim.ui.statusbar.item { id = "branch", text = "", align = "right", priority = 20 }
item:hide()

local watched   -- the path of the HEAD file being watched
local watcher
local looked_at     -- the place the item was last computed for
local running, again, forced = false, false, false

local function stop_watch()
  if watcher then
    watcher:stop()
  end
  watcher, watched, looked_at = nil, nil, nil
end

local refresh

-- watch_head makes a change of the HEAD file refresh the item. The directory is watched,
-- not the file: git replaces HEAD by renaming a lock file over it, which a watch on the
-- file itself would not survive.
local function watch_head(head)
  if head == watched then
    return
  end
  stop_watch()
  if not head then
    return
  end
  local w = gvim.fs.watch(gvim.path.dirname(head), function(ev)
    if ev.kind ~= "error" and gvim.path.basename(ev.path or "") == "HEAD" then
      refresh(true)
    end
  end, { debounce_ms = 200 })
  if w then
    watcher, watched = w, head
  end
end

-- location is the directory to look for a repository in: the active file's own, else the
-- workspace.
local function location()
  local b = gvim.buf.current()
  local path = b and b:path() or ""
  if path ~= "" and not b:is_virtual() then
    return gvim.path.dirname(path)
  end
  return gvim.workspace.root()
end

local function update(force)
  local here = location()
  -- While the watch on HEAD works, a change of branch reaches us as an event, so git is
  -- asked again only when the active file lives somewhere else.
  if not force and watcher and here == looked_at then
    return
  end
  local repo = git.repo(here)
  if not repo and here ~= gvim.workspace.root() then
    repo = git.repo(gvim.workspace.root())
  end
  if not repo then
    stop_watch()
    item:hide()
    return
  end
  watch_head(repo.head)
  looked_at = here
  local name = git.branch(repo.root)
  if name then
    item:set(name)
    item:show()
  else
    item:hide()
  end
end

-- refresh runs update; a request that comes while it runs makes it go round once more.
function refresh(force)
  forced = forced or force == true
  if running then
    again = true
    return
  end
  running = true
  repeat
    again = false
    local f = forced
    forced = false
    local ok, err = pcall(update, f)
    if not ok then
      gvim.log.warn("git_statusbar: %s", tostring(err))
    end
  until not again
  running = false
end

for _, name in ipairs { "buffer_activated", "after_file_open", "after_file_save", "window_focused" } do
  gvim.on(name, function()
    refresh(name == "after_file_save" or name == "window_focused")
  end)
end

-- Fill the item at load, before any event: a gvim started on a directory has no file open.
gvim.async.spawn(refresh)

gvim.plugin.on_unload(stop_watch)
