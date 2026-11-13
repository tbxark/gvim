-- Path completion: a port of Fresh's plugins/path_complete.ts (168 lines).
--
-- Fresh's plugin is purely passive: it answers the editor's prompt_changed event for the
-- built-in "open-file" and "save-file-as" prompts and supplies directory suggestions. gvim
-- has no prompt_changed event, and gvim.ui.set_prompt_suggestions only fills a prompt the
-- calling plugin opened (the editor's Open File / Switch Project are the native file-browser
-- overlay and Save As is the Save-As dialog, neither of which a plugin can attach to). The
-- port therefore keeps Fresh's completion engine exactly -- path parsing, hidden-file rule,
-- case-insensitive prefix match, directories first then alphabetical, the 100-row cap and
-- the "new file" row -- and exposes it on the one prompt surface gvim has:
--
--   * gvim.plugins.export{ complete = function(input) -> suggestions } for other plugins
--   * one command, "Path Completion: Complete Path", that opens gvim.ui.input with the
--     suggestions on every edit: Enter on a directory descends into it, Enter on a file
--     opens it, Esc cancels (the suggestions vanish with the prompt)
--
-- Reductions (documented in docs/plugins/bundled.md): Fresh registers no command and no
-- binding; this port adds one command (no default key binding) because the built-in prompts
-- are not plugin-visible, and it does not hook the native Open File/Save As browser. A host
-- seam (a prompt_changed-style event plus suggestion support in the browser/dialog inputs)
-- would let a future version drop the command; see docs/plugins/porting.md.
local gvim = require "gvim"

local t = gvim.i18n.t

-- Fresh caps the result list at 100 rows; the scan is bounded too (the host lists at most
-- 100 000 entries, which is more than a prompt edit should parse in Lua).
local MAX_SUGGESTIONS = 100
local MAX_SCAN = 20000
local MAX_INPUT = 4096

-- parse_path mirrors Fresh's parsePath: the directory to list and the prefix pattern.
local function parse_path(input)
  if input == "" then
    return ".", ""
  end
  local last_slash = input:match("^.*()/")
  if not last_slash then
    return ".", input
  end
  if last_slash == 1 then
    return "/", input:sub(2)
  end
  return input:sub(1, last_slash - 1), input:sub(last_slash + 1)
end

-- absolute resolves a possibly relative path against the editor's working directory.
local function absolute(path)
  if path == "" then
    return gvim.dirs.cwd()
  end
  if gvim.path.is_absolute(path) then
    return path
  end
  return gvim.path.join(gvim.dirs.cwd(), path)
end

-- join_display is the path a suggestion's value carries: the input directory as written
-- (".", "/", "src" stay as they are), never the resolved working directory.
local function join_display(dir, name)
  if dir == "." then
    return name
  end
  if dir == "/" then
    return "/" .. name
  end
  return dir .. "/" .. name
end

local function lower(text)
  return gvim.text.lower(text)
end

-- list_dir returns the entries of dir (never nil) and whether the directory was readable.
-- The hidden flag follows Fresh's rule: dotfiles are listed only while the pattern itself
-- starts with a dot (the host filters hidden entries, so ask for them only then).
local function list_dir(dir, hidden)
  local path = absolute(dir)
  local entries, err = gvim.fs.list(path, { hidden = hidden })
  if not entries then
    gvim.log.debug("path_complete: list %s: %s", path, tostring(err))
    return {}, false
  end
  return entries, true
end

-- generate returns Fresh's suggestions for the current prompt text, bounded to the cap.
local function generate(input)
  input = gvim.text.trim(tostring(input or ""))
  if #input > MAX_INPUT then
    input = input:sub(1, MAX_INPUT)
  end
  local dir, pattern = parse_path(input)
  local show_hidden = pattern:sub(1, 1) == "."
  local entries, readable = list_dir(dir, show_hidden)
  local pattern_lower = lower(pattern)
  local matched = {}
  local scanned = 0
  for _, entry in ipairs(entries) do
    scanned = scanned + 1
    if scanned > MAX_SCAN then
      break
    end
    local name = tostring(entry.name or "")
    if name ~= "" and (show_hidden or name:sub(1, 1) ~= ".") and lower(name):sub(1, #pattern_lower) == pattern_lower then
      matched[#matched + 1] = {
        name = name,
        -- A symlink to a directory completes as a directory.
        is_dir = entry.kind == "dir" or (entry.kind == "symlink" and entry.target_kind == "dir"),
      }
    end
  end
  table.sort(matched, function(a, b)
    if a.is_dir ~= b.is_dir then
      return a.is_dir
    end
    return lower(a.name) < lower(b.name)
  end)
  local suggestions = {}
  for i = 1, math.min(#matched, MAX_SUGGESTIONS) do
    local item = matched[i]
    local full = join_display(dir, item.name)
    if item.is_dir then
      suggestions[#suggestions + 1] = {
        id = full .. "/",
        text = item.name .. "/",
        description = t("suggestion.directory"),
        value = full .. "/",
      }
    else
      suggestions[#suggestions + 1] = {
        id = full,
        text = item.name,
        value = full,
      }
    end
  end
  -- The "new file" row: Fresh adds it when the typed path does not exist, so a path that
  -- the directory listing cannot show is still offered as a value.
  if readable and pattern ~= "" and input ~= "" then
    local target = absolute(input)
    if not gvim.fs.exists(target) and #suggestions < MAX_SUGGESTIONS then
      suggestions[#suggestions + 1] = {
        id = "new:" .. input,
        text = t("suggestion.new_file", { filename = input }),
        description = t("suggestion.new_file_desc"),
        value = input,
      }
    end
  end
  return suggestions
end

-- The exported engine: other plugins open a prompt and call complete() from on_change.
gvim.plugins.export { complete = generate }

-- The prompt loop: a directory answer descends, a file answer opens, Esc cancels.
local function run_prompt(start)
  local input = start or ""
  while true do
    local answer = gvim.ui.input {
      prompt = t("prompt.path"),
      default = input,
      on_change = function(text)
        gvim.ui.set_prompt_suggestions(generate(text))
      end,
    }
    if answer == nil then
      gvim.notify(t("status.cancelled"))
      return
    end
    answer = gvim.text.trim(answer)
    if answer == "" then
      return
    end
    local target = absolute(answer)
    local stat = gvim.fs.stat(target)
    if stat and stat.kind == "dir" then
      input = answer
      if input:sub(-1) ~= "/" then
        input = input .. "/"
      end
    else
      local buf, err = gvim.buf.open(answer, { focus = true })
      if not buf then
        gvim.notify(t("status.not_found", { path = answer }), "warn")
        gvim.log.warn("path_complete: open %s: %s", answer, tostring(err))
        return
      end
      gvim.notify(t("status.opened", { path = answer }))
      return
    end
  end
end

gvim.command.register {
  name = "complete_path",
  title = "%cmd.complete_path",
  description = "%cmd.complete_path_desc",
  run = function()
    run_prompt("")
  end,
}

gvim.log.debug("path completion plugin loaded")
