-- Git find file. A port of Fresh's git_find_file plugin: pick one of the files git tracks
-- (fuzzy search over the paths relative to the repository) and open it.
--
-- The repository is the one that holds the active file, else the workspace's. Opening
-- paths are joined to the repository root, so this works from a sub-project too. The list
-- is read again every time the picker opens ("Git Find File: Reload" does the same and
-- says so); a repository with more than 50 000 tracked files shows the first 50 000.
local gvim = require "gvim"
local git = gvim.git
local t = gvim.i18n.t

local MAX_FILES = 50000
local MAX_NAME = 4000 -- the picker takes names up to 4096 bytes

local function location()
  local b = gvim.buf.current()
  local path = b and b:path() or ""
  if path ~= "" and not b:is_virtual() then
    return path
  end
  return gvim.workspace.root()
end

-- load_files returns the root of the repository and the tracked files, or nil after telling
-- the user why not.
local function load_files()
  if not git.available() then
    gvim.notify("Git Find File: git is not installed")
    return
  end
  local here = location()
  local repo = git.repo(here)
  if not repo and here ~= gvim.workspace.root() then
    repo = git.repo(gvim.workspace.root())
  end
  if not repo then
    gvim.notify("Git Find File: not inside a git repository")
    return
  end
  local files, truncated = git.ls_files(repo.root)
  if not files then
    gvim.notify("Git Find File: cannot list the files (" .. tostring(truncated) .. ")", "warn")
    return
  end
  -- A big repository is a long loop: give the scheduler a turn now and then, so no slice
  -- of this task runs past its time budget.
  local out = {}
  for i, rel in ipairs(files) do
    if #rel <= MAX_NAME then
      out[#out + 1] = rel
      if #out >= MAX_FILES then
        truncated = true
        break
      end
    end
    if i % 5000 == 0 then
      gvim.timer.sleep(0)
    end
  end
  if #out == 0 then
    gvim.notify(t("status.no_files"))
    return
  end
  if truncated then
    gvim.notify(t("status.showing_first", { shown = tostring(#out) }))
  end
  return repo.root, out
end

local function find_file()
  local root, files = load_files()
  if not root then
    return
  end
  local rel = gvim.ui.select(files, { title = "Find file", placeholder = "type to filter" })
  if not rel then
    return
  end
  local path = gvim.path.join(root, rel)
  local b, err = gvim.buf.open(path)
  if not b then
    gvim.notify("Cannot open " .. rel .. ": " .. tostring(err), "warn")
  end
end

gvim.command.register {
  name = "find",
  title = t("cmd.find"),
  description = t("cmd.find_desc"),
  run = find_file,
}

gvim.command.register {
  name = "reload",
  title = t("cmd.reload"),
  description = t("cmd.reload_desc"),
  run = function()
    gvim.notify(t("status.loading"))
    find_file()
  end,
}
