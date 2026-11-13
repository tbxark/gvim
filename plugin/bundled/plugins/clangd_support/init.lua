-- Clangd helper: a port of Fresh's plugins/clangd_support.ts (320 lines).
--
-- Fresh's plugin has three user-visible pieces: Switch Source/Header (asks clangd's
-- non-standard textDocument/switchSourceHeader request), a "Clangd project setup" panel
-- that reports whether compile_commands.json/.clangd exist and which build system the
-- project uses, and Open Project Config. gvim's read-only LSP request allowlist has no
-- server-specific method, so the switch command cannot send Fresh's clangd request. This
-- port replaces it with a bounded same-named-file lookup over the usual source/header
-- extensions and directories (documented reduction), and keeps the project setup view
-- (as a read-only virtual buffer instead of a floating panel) and Open Project Config.
-- Fresh's lsp/custom_notification file-status line is not ported: gvim's plugin API does
-- not expose language-server custom notifications.
local gvim = require "gvim"

local t = gvim.i18n.t

local SOURCE_EXTS = { "c", "cc", "cpp", "cxx" }
local HEADER_EXTS = { "h", "hh", "hpp", "hxx", "hp" }

local source_set, header_set = {}, {}
for _, ext in ipairs(SOURCE_EXTS) do
  source_set[ext] = true
end
for _, ext in ipairs(HEADER_EXTS) do
  header_set[ext] = true
end

local project_buf = nil

local function dir_name(path)
  return gvim.path.dirname(path)
end

local function basename(path)
  return (path:match("([^/\\]+)$")) or path
end

local function extension(path)
  local ext = path:match("%.([^./\\]+)$")
  if not ext then
    return nil, nil
  end
  local base = basename(path)
  return string.lower(ext), base:sub(1, #base - #ext - 1)
end

local function active_path()
  local buf = gvim.buf.current()
  if not buf then
    return nil
  end
  return buf:path()
end

-- candidates_for returns the counterpart extensions for path's extension, or nil when the
-- file is not one of the C-family source/header types Fresh handles.
local function candidates_for(path)
  local ext, stem = extension(path)
  if not ext then
    return nil, nil
  end
  if source_set[ext] then
    return stem, HEADER_EXTS
  end
  if header_set[ext] then
    return stem, SOURCE_EXTS
  end
  return nil, nil
end

-- candidate_dirs lists, in priority order, the directories the lookup tries: the buffer's
-- directory first, then the workspace root, then the conventional include/src directories
-- next to the buffer and under the root. Fresh's clangd request resolves includes from
-- compile_commands.json; the heuristic is deliberately local and bounded (documented
-- reduction), and never leaves the workspace.
local function candidate_dirs(path)
  local dirs, seen = {}, {}
  local function add(dir)
    if dir and dir ~= "" and not seen[dir] then
      seen[dir] = true
      dirs[#dirs + 1] = dir
    end
  end
  local buffer_dir = dir_name(path)
  add(buffer_dir)
  local root = gvim.workspace.root()
  if root and root ~= "" then
    add(root)
    add(gvim.path.join(root, "include"))
    add(gvim.path.join(root, "src"))
  end
  -- The parent of the buffer's directory only counts while it is still inside the workspace.
  local parent = buffer_dir and buffer_dir ~= "" and dir_name(buffer_dir) or ""
  if parent ~= "" and root and root ~= "" and (parent == root or parent:sub(1, #root + 1) == root .. "/") then
    add(gvim.path.join(parent, "include"))
    add(gvim.path.join(parent, "src"))
  end
  return dirs
end

local function counterpart_files(path)
  local stem, exts = candidates_for(path)
  if not stem then
    return nil
  end
  local found, seen = {}, {}
  for _, dir in ipairs(candidate_dirs(path)) do
    for _, ext in ipairs(exts) do
      local candidate = gvim.path.join(dir, stem .. "." .. ext)
      if not seen[candidate] and gvim.fs.exists(candidate) then
        seen[candidate] = true
        found[#found + 1] = candidate
      end
    end
  end
  return found
end

local function open_file(path)
  local ok, err = gvim.buf.open(path, { focus = true })
  if not ok then
    gvim.status(t("status.switch_failed", { error = tostring(err) }))
    return
  end
  gvim.status(t("status.opened_file", { path = path }))
end

local function switch_source_header()
  local path = active_path()
  if not path then
    gvim.status(t("status.no_active_file"))
    return
  end
  if not candidates_for(path) then
    gvim.status(t("status.unsupported_file_type"))
    return
  end
  local found = counterpart_files(path)
  if not found or #found == 0 then
    gvim.status(t("status.no_matching_file"))
    return
  end
  if #found == 1 then
    open_file(found[1])
    return
  end
  local choice = gvim.ui.select(found, {
    title = t("picker.title"),
    footer = t("picker.footer"),
  })
  if not choice then
    gvim.status(t("status.cancelled"))
    return
  end
  open_file(choice)
end

local function project_root()
  local root = gvim.workspace.root()
  if root and root ~= "" then
    return root
  end
  local path = active_path()
  if path then
    return dir_name(path)
  end
  return gvim.dirs.cwd()
end

local function analysis_dirs(root)
  local dirs, seen = {}, {}
  local function add(dir)
    if dir and dir ~= "" and not seen[dir] then
      seen[dir] = true
      dirs[#dirs + 1] = dir
    end
  end
  add(root)
  add(gvim.dirs.cwd())
  local path = active_path()
  if path then
    add(dir_name(path))
  end
  return dirs
end

local function first_existing(dirs, relative)
  for _, dir in ipairs(dirs) do
    local candidate = gvim.path.join(dir, relative)
    if gvim.fs.exists(candidate) then
      return candidate
    end
  end
  return nil
end

local COMPILE_CANDIDATES = {
  "compile_commands.json",
  "build/compile_commands.json",
  "out/compile_commands.json",
  "cmake-build-debug/compile_commands.json",
}
local CONFIG_CANDIDATES = { ".clangd", ".clangd/compile_flags.txt" }

local function exists_in(dir, name)
  return gvim.fs.exists(gvim.path.join(dir, name))
end

local function project_analysis()
  local root = project_root()
  local dirs = analysis_dirs(root)
  local lines = {}
  local function add(line)
    lines[#lines + 1] = line
  end

  add(root and t("panel.root", { path = root }) or t("panel.root_unknown"))
  add("")

  local compile_path = nil
  for _, relative in ipairs(COMPILE_CANDIDATES) do
    compile_path = first_existing(dirs, relative)
    if compile_path then
      break
    end
  end
  if compile_path then
    add(t("panel.compile_ready", { path = compile_path }))
  else
    add(t("panel.compile_missing"))
    add("  " .. t("panel.compile_tip"))
  end

  local clangd_path = nil
  for _, relative in ipairs(CONFIG_CANDIDATES) do
    clangd_path = first_existing(dirs, relative)
    if clangd_path then
      break
    end
  end
  if clangd_path then
    add(t("panel.config_present", { path = clangd_path }))
  else
    add(t("panel.config_missing"))
    add("  " .. t("panel.config_tip"))
  end

  local hints = {}
  if root then
    if exists_in(root, "CMakeLists.txt") then
      hints[#hints + 1] = t("panel.hint_cmake")
    end
    if exists_in(root, "WORKSPACE") or exists_in(root, "WORKSPACE.bazel") or exists_in(root, "BUILD.bazel") then
      hints[#hints + 1] = t("panel.hint_bazel")
    end
    if exists_in(root, "Makefile") then
      hints[#hints + 1] = t("panel.hint_make")
    end
  end
  if #hints > 0 then
    add("")
    add(t("panel.build_hints"))
    for _, hint in ipairs(hints) do
      add("  - " .. hint)
    end
  end

  add("")
  add(t("panel.general_tips"))
  add("  * " .. t("panel.tip_compile"))
  add("  * " .. t("panel.tip_config"))
  return lines, root
end

local function entries_for(lines)
  local entries = {}
  for _, line in ipairs(lines) do
    entries[#entries + 1] = { text = line .. "\n" }
  end
  return entries
end

local function show_project_setup()
  local lines = project_analysis()
  local entries = entries_for(lines)
  if project_buf and project_buf:valid() then
    local ok, err = project_buf:set_content(entries)
    if not ok then
      gvim.status(t("status.panel_failed", { error = tostring(err) }))
      return
    end
    project_buf:show()
    return
  end
  local buf, err = gvim.buf.create {
    name = t("panel.name"),
    read_only = true,
    focus = true,
  }
  if not buf then
    gvim.status(t("status.panel_failed", { error = tostring(err) }))
    return
  end
  project_buf = buf
  local ok, serr = buf:set_content(entries)
  if not ok then
    gvim.status(t("status.panel_failed", { error = tostring(serr) }))
  end
end

local function open_project_config()
  local dirs, seen = {}, {}
  local function add(dir)
    if dir and dir ~= "" and not seen[dir] then
      seen[dir] = true
      dirs[#dirs + 1] = dir
    end
  end
  local path = active_path()
  if path then
    add(dir_name(path))
  end
  add(gvim.workspace.root())
  for _, dir in ipairs(dirs) do
    local config = gvim.path.join(dir, ".clangd")
    if gvim.fs.exists(config) then
      local ok, err = gvim.buf.open(config, { focus = true })
      if not ok then
        gvim.status(t("status.open_failed", { error = tostring(err) }))
        return
      end
      gvim.status(t("status.config_opened", { path = config }))
      return
    end
  end
  gvim.status(t("status.config_not_found"))
end

gvim.command.register({
  name = "switch_source_header",
  title = t("cmd.switch_source_header"),
  description = t("cmd.switch_source_header_desc"),
  run = function()
    switch_source_header()
  end,
})

gvim.command.register({
  name = "project_setup",
  title = t("cmd.project_setup"),
  description = t("cmd.project_setup_desc"),
  run = function()
    show_project_setup()
  end,
})

gvim.command.register({
  name = "open_project_config",
  title = t("cmd.open_project_config"),
  description = t("cmd.open_project_config_desc"),
  run = function()
    open_project_config()
  end,
})

gvim.log.debug("clangd_support: Plugin loaded")
