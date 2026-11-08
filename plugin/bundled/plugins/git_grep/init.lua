-- Git Grep: a port of Fresh's plugins/git_grep.ts (210 lines) over the shared gvim.finder
-- library.
--
-- Ported from Fresh: a live search over git-tracked files. The query runs 'git grep -n
-- --column -I [-i] -e <query>' in the repository of the active buffer (else the workspace's),
-- and the first 100 matches are listed as '<repo-relative path>:<line>' with a trimmed
-- source line; Enter opens the file at the match's line and column and says so. Case starts
-- insensitive (Fresh's effective default: editor.search.case_sensitive is false unless
-- configured, so it passes -i) and the choice is module-level, so it outlives one prompt, as
-- Fresh keeps it. git's basic regex is used (no -E, so 'foo(' is literal). git grep's exit
-- codes follow Fresh: 0 = matches, 1 = no matches (a normal search), >=2 = a real error.
-- The git call goes through the hardened gvim.git gateway (same argv/env safety as the other
-- git ports), with a 15 s timeout, a 2 MiB output cap and per-line/argument caps; paths are
-- repo-relative because a monorepo's repository root differs from the workspace root, and
-- results open joined onto that root. Not a repository and a missing git binary have their
-- own status lines.
--
-- Reduced adaptation vs Fresh: Fresh's prompt toolbar (setPromptToolbar + a Case toggle
-- widget + toggleOverlayToolbarWidget) has no gvim prompt surface, so the case state is
-- shown in the prompt's footer/hint line and flipped by the second Fresh palette command,
-- 'Git Grep: Toggle Case Sensitive' -- it refreshes an open prompt and otherwise applies to
-- the next search. Fresh binds no chord for either command, so neither does this port. Fresh
-- also disables the Finder's split preview because its floating overlay draws its own preview
-- band; gvim has no overlay preview band, and this port deliberately opens no preview tabs
-- either (a search hit is cheap to open with Enter). The search source has no kill handle
-- (gvim.git.run is one await, not a cancellable process): a superseded query's results are
-- discarded by the picker's generation guard, which Fresh's cancellation achieves by kill.
local gvim = require "gvim"
local finder = require "gvim.finder"
local git = gvim.git

local t = gvim.i18n.t

local MAX_RESULTS = 100
local MAX_QUERY = 256
local MAX_LINE = 4096
local MAX_OUTPUT = 2 * 1024 * 1024
local TIMEOUT_MS = 15000

-- Fresh's module-level case choice: insensitive unless toggled (Fresh's config preset
-- defaults to case-insensitive search; gvim plugins cannot read editor.search.case_sensitive,
-- so this starts from Fresh's effective default).
local case_sensitive = false

-- The open finder and the repository root it searches (nil while closed).
local prompt = nil
local root = nil
local last_query = ""

local function location()
  local buf = gvim.buf.current()
  if buf and not buf:is_virtual() then
    local path = buf:path()
    if path and path ~= "" then
      return path
    end
  end
  return gvim.workspace.root()
end

-- resolve_repo finds the repository that holds the active file, else the workspace's, or
-- tells the user why there is none.
local function resolve_repo()
  if not git.available() then
    gvim.notify(t("status.no_git"))
    return nil
  end
  local here = location()
  local repo = git.repo(here)
  if not repo and here ~= gvim.workspace.root() then
    repo = git.repo(gvim.workspace.root())
  end
  if not repo then
    gvim.notify(t("status.not_in_git"))
    return nil
  end
  return repo
end

local function case_state()
  return case_sensitive and t("case.on") or t("case.off")
end

local function footer_text()
  return t("footer.hint", { case = case_state() })
end

-- parse_matches reads 'path:line:column:content' lines, repo-relative paths included. Only
-- the first limit matches are parsed (Fresh's MAX_RESULTS), each line is capped, and a path
-- with colons in it still parses because the numeric fields anchor the match from the left.
local function parse_matches(stdout, repo_root, limit)
  local out = {}
  if type(stdout) ~= "string" or stdout == "" then
    return out
  end
  local lines = gvim.text.split(stdout, "\n", true)
  for _, raw in ipairs(lines) do
    if #out >= limit then
      break
    end
    local line = finder.clip(raw, MAX_LINE)
    local file, line_number, column, content = line:match("^(.-):(%d+):(%d+):(.*)$")
    if file and #file > 0 and #file <= 4096 then
      out[#out + 1] = {
        file = file,
        line = tonumber(line_number) or 1,
        column = tonumber(column) or 1,
        content = content or "",
        abs = gvim.path.join(repo_root, file),
      }
    end
  end
  return out
end

-- run_search is the finder's live source: one git grep per query (the picker debounces) and
-- the first matches, or an empty list after a status line for the error states.
local function run_search(query)
  last_query = query
  if query == "" then
    return {}
  end
  local args = { "grep", "-n", "--column", "-I" }
  if not case_sensitive then
    args[#args + 1] = "-i"
  end
  args[#args + 1] = "-e"
  args[#args + 1] = finder.clip(query, MAX_QUERY)
  local result, err = git.run(args, { cwd = root, timeout_ms = TIMEOUT_MS, max_output = MAX_OUTPUT })
  if not result then
    gvim.notify(t("status.error", { error = tostring(err) }), "warn")
    return {}
  end
  if result.truncated then
    gvim.notify(t("status.error", { error = "output too large" }), "warn")
    return {}
  end
  if result.code == 0 or result.code == 1 then
    return parse_matches(result.stdout, root, MAX_RESULTS)
  end
  local detail = tostring(result.stderr or ""):match("^[^\r\n]*") or ""
  gvim.notify(t("status.error", { error = ("exit %d: %s"):format(result.code, detail) }), "warn")
  return {}
end

local function jump(match)
  local buf, err = gvim.buf.open(match.abs, {
    pos = gvim.pos((match.line or 1) - 1, (match.column or 1) - 1),
    focus = true,
  })
  if not buf then
    gvim.notify(t("status.error", { error = tostring(err) }), "warn")
    return
  end
  gvim.notify(t("status.opened", { location = match.file .. ":" .. match.line }))
end

local function show_count(shown)
  if not prompt then
    return
  end
  if last_query == "" then
    prompt:set_status(t("status.type_to_search"))
  elseif shown == 0 then
    prompt:set_status(t("status.no_matches"))
  else
    prompt:set_status(t("status.found", { count = shown }))
  end
end

local function start_git_grep()
  -- Fresh has one prompt at a time: a second invocation closes the open one first.
  if prompt and prompt:is_open() then
    prompt:close()
    prompt = nil
  end
  local repo = resolve_repo()
  if not repo then
    return
  end
  root = repo.root
  last_query = ""
  prompt = finder.new {
    title = t("prompt.grep"),
    placeholder = t("prompt.placeholder"),
    footer = footer_text(),
    min_query = 1,
    max_results = MAX_RESULTS,
    search = run_search,
    format = function(match)
      local content = gvim.text.trim(match.content or "")
      local description = content
      if #content > 60 then
        description = (content:sub(1, 57):gsub("%s+$", "")) .. "..."
      end
      return {
        id = match.file .. ":" .. match.line .. ":" .. match.column,
        label = match.file .. ":" .. match.line,
        description = description,
      }
    end,
    on_select = jump,
    on_rows = show_count,
    on_close = function(reason)
      prompt = nil
      if reason == "cancelled" then
        gvim.log.debug("git_grep: prompt closed")
      end
    end,
  }
  prompt:open()
  prompt:set_status(t("status.type_to_search"))
end

-- Fresh's second command: flip the case mode (and refresh an open prompt). Fresh drives the
-- same flag from its prompt-toolbar toggle, which gvim has no surface for.
local function toggle_case()
  case_sensitive = not case_sensitive
  gvim.notify(t("status.case", { state = case_state() }))
  if prompt and prompt:is_open() then
    prompt:set_footer(footer_text())
    prompt:refresh()
  end
end

gvim.command.register {
  name = "git_grep",
  title = "%cmd.grep",
  description = "%cmd.grep_desc",
  run = start_git_grep,
}

gvim.command.register {
  name = "toggle_case",
  title = "%cmd.git_grep_toggle_case",
  description = "%cmd.git_grep_toggle_case_desc",
  run = toggle_case,
}

gvim.log.debug("git grep plugin loaded (shared gvim.finder)")
