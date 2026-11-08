-- gvim.git: read-only helpers over the git command line, for plugins.
--
--   gvim.git.available() -> boolean                 the git binary can be found
--   gvim.git.run(args, opts?) ⏳ -> Result | nil, err
--         args = {"rev-parse", "HEAD"} (without "git"); opts {cwd, stdin, timeout_ms, max_output, env}
--   gvim.git.repo(path) ⏳ -> {root=, head=} | nil | nil, err   the work tree of a file or directory, and its HEAD file
--   gvim.git.root(path) ⏳ -> root | nil | nil, err
--   gvim.git.branch(root?) ⏳ -> name, detached | nil | nil, err
--   gvim.git.show(path, opts?) ⏳ -> text | nil, err           opts {rev = "HEAD", for_buffer = Buffer}
--   gvim.git.ls_files(root?) ⏳ -> files, truncated | nil, err
--   gvim.git.blame(path, opts?) ⏳ -> entries | nil, err       opts {from, to (0-based, to exclusive), contents, rev}
--   gvim.git.commit(dir, hash) ⏳ -> lines | nil, err          dir: any directory of the work tree
--
-- Every call is `git -c core.fsmonitor=false -c core.pager=cat -c diff.external=
-- -c core.attributesFile=<null device> -c protocol.ext.allow=never <args>` with
-- --no-ext-diff --no-textconv where git takes them, and GIT_OPTIONAL_LOCKS=0
-- GIT_TERMINAL_PROMPT=0 GIT_CONFIG_NOSYSTEM=1 in the environment (AMENDMENTS C28): reading
-- a repository never runs a program the repository names. Without a git binary, `root` and
-- `branch` answer from the editor's own reader; `run`, `show`, `ls_files`, `blame` and
-- `commit` return "unsupported: git binary not found".
--
-- Errors are values: a missing file at a revision is "not_found: ...", a process that
-- cannot start is "exec: ...". Output that does not fit the output cap is "too_large: ...".
local gvim = require "gvim"

local M = {}

local NO_GIT = "unsupported: git binary not found"
local DEFAULT_TIMEOUT_MS = 15000
local MAX_OUTPUT = 8 * 1024 * 1024
local MAX_BLAME_OUTPUT = 4 * 1024 * 1024

-- Flags added after the subcommand name, for the commands that take them.
local EXTRA = {
  show = { "--no-ext-diff", "--no-textconv" },
  diff = { "--no-ext-diff", "--no-textconv" },
  log = { "--no-ext-diff", "--no-textconv" },
  blame = { "--no-textconv" },
}

local function null_device()
  return gvim.platform == "windows" and "NUL" or "/dev/null"
end

-- argv is the full command line for args = {subcommand, ...}. extra lists more "key=value"
-- settings for -c, for the calls that need them (see neutral_filters).
local function argv(args, extra)
  local out = {
    "git",
    "-c", "core.fsmonitor=false",
    "-c", "core.pager=cat",
    "-c", "diff.external=",
    "-c", "core.attributesFile=" .. null_device(),
    "-c", "protocol.ext.allow=never",
  }
  for _, kv in ipairs(extra or {}) do
    out[#out + 1] = "-c"
    out[#out + 1] = kv
  end
  out[#out + 1] = args[1]
  for _, a in ipairs(EXTRA[args[1]] or {}) do
    out[#out + 1] = a
  end
  for i = 2, #args do
    out[#out + 1] = args[i]
  end
  return out
end

local function chomp(s)
  return (s:gsub("\r?\n$", ""))
end

local function first_line(s)
  return (s:match("^[^\r\n]*")) or ""
end

function M.available()
  return gvim.has("git")
end

local function run(args, opts, extra)
  if type(args) ~= "table" or type(args[1]) ~= "string" or args[1] == "" then
    error("bad argument #1 to 'run' (command table expected)", 2)
  end
  if not M.available() then
    return nil, NO_GIT
  end
  opts = opts or {}
  local env = {}
  for k, v in pairs(opts.env or {}) do
    env[k] = v
  end
  env.GIT_OPTIONAL_LOCKS = "0"
  env.GIT_TERMINAL_PROMPT = "0"
  env.GIT_CONFIG_NOSYSTEM = "1"
  return gvim.proc.run(argv(args, extra), {
    cwd = opts.cwd,
    env = env,
    stdin = opts.stdin,
    timeout_ms = opts.timeout_ms or DEFAULT_TIMEOUT_MS,
    max_output = opts.max_output or MAX_OUTPUT,
  })
end

function M.run(args, opts)
  return run(args, opts)
end

local MAX_FILTERS = 32

-- neutral_filters returns the -c settings that switch off every filter driver the
-- repository configures (clean, smudge, process), or nil, err. Listing the keys runs no
-- program. git exits 1 when there is no match.
local function neutral_filters(cwd)
  local r, err = run({ "config", "-z", "--name-only", "--get-regexp", "^filter\\..*\\.(clean|smudge|process|required)$" },
    { cwd = cwd })
  if not r then
    return nil, err
  end
  if r.truncated or (not r.ok and r.code ~= 1) then
    return nil, "unsupported: cannot list the filters of the repository"
  end
  local seen, extra, n = {}, {}, 0
  for _, key in ipairs(gvim.text.split(r.stdout, "\0", true)) do
    local name = key:match("^[Ff][Ii][Ll][Tt][Ee][Rr]%.(.+)%.%a+$")
    if name and not seen[name] then
      -- `-c` splits at the first "=": a name with one cannot be switched off
      if name:find("=", 1, true) or n >= MAX_FILTERS then
        return nil, "unsupported: the repository defines filters that cannot be switched off"
      end
      seen[name] = true
      n = n + 1
      for _, var in ipairs { "clean", "smudge", "process" } do
        extra[#extra + 1] = "filter." .. name .. "." .. var .. "="
      end
      extra[#extra + 1] = "filter." .. name .. ".required=false"
    end
  end
  return extra
end

-- dir_of is path itself when it is a directory, else its parent.
local function dir_of(path)
  local st = gvim.fs.stat(path)
  if st and st.kind == "dir" then
    return path
  end
  return gvim.path.dirname(path)
end

function M.repo(path)
  local dir = dir_of(path)
  if M.available() then
    local r, err = M.run({ "rev-parse", "--show-toplevel", "--git-path", "HEAD" }, { cwd = dir })
    if not r then
      return nil, err
    end
    if not r.ok then
      return nil
    end
    local lines = gvim.text.split(r.stdout, "\n", true)
    local root, head = chomp(lines[1] or ""), chomp(lines[2] or "")
    if root == "" then
      return nil
    end
    if head ~= "" and not gvim.path.is_absolute(head) then
      head = gvim.path.join(dir, head)
    end
    return { root = root, head = head ~= "" and head or nil }
  end
  local root, err = gvim.workspace.git_root(dir)
  if not root then
    return nil, err
  end
  return { root = root, head = gvim.path.join(root, ".git", "HEAD") }
end

function M.root(path)
  local repo, err = M.repo(path)
  if not repo then
    return nil, err
  end
  return repo.root
end

function M.branch(root)
  root = root or gvim.workspace.root()
  if not M.available() then
    return gvim.workspace.git_branch(root)
  end
  local r, err = M.run({ "symbolic-ref", "--short", "-q", "HEAD" }, { cwd = root })
  if not r then
    return nil, err
  end
  if r.ok then
    local name = chomp(r.stdout)
    if name ~= "" then
      return name
    end
  end
  if r.code == 1 then -- HEAD is not a branch: the first seven digits of the commit
    local d = M.run({ "rev-parse", "--short=7", "HEAD" }, { cwd = root })
    if d and d.ok and chomp(d.stdout) ~= "" then
      return chomp(d.stdout), true
    end
  end
  return nil
end

-- A revision is passed to git as part of a path argument ("HEAD:./file") or as an argument
-- of its own; it must not look like an option.
local function check_rev(rev)
  if type(rev) ~= "string" or rev == "" or rev:sub(1, 1) == "-" then
    return "invalid: bad revision"
  end
  for _, c in ipairs { "\0", "\r", "\n" } do
    if rev:find(c, 1, true) then
      return "invalid: bad revision"
    end
  end
end

function M.show(path, opts)
  opts = opts or {}
  local rev = opts.rev or "HEAD"
  local bad = check_rev(rev)
  if bad then
    return nil, bad
  end
  if not M.available() then
    return nil, NO_GIT
  end
  -- "<rev>:./<name>" names the blob relative to the directory git runs in, so the path
  -- needs no translation to a repository-relative one (symlinked temp dirs, sub-projects).
  local r, err = M.run({ "show", rev .. ":./" .. gvim.path.basename(path) }, { cwd = gvim.path.dirname(path) })
  if not r then
    return nil, err
  end
  if not r.ok then
    return nil, "not_found: " .. first_line(r.stderr)
  end
  if r.truncated then
    return nil, "too_large: the file is larger than " .. MAX_OUTPUT .. " bytes"
  end
  local buf = opts.for_buffer
  if buf then
    local info = buf:info()
    if not info then
      return nil, "closed: the buffer was closed"
    end
    return gvim.text.decode(r.stdout, info.encoding or "utf-8", { normalize = true })
  end
  return r.stdout
end

function M.ls_files(root)
  root = root or gvim.workspace.root()
  local r, err = M.run({ "ls-files", "-z", "--full-name" }, { cwd = root })
  if not r then
    return nil, err
  end
  if not r.ok then
    return nil, "not_found: " .. first_line(r.stderr)
  end
  local split_ok, files = pcall(gvim.text.split, r.stdout, "\0", true)
  if not split_ok then
    return nil, "too_large: too many files to list"
  end
  if files[#files] == "" then
    files[#files] = nil
  end
  if r.truncated then
    files[#files] = nil -- the last name may be cut short
  end
  return files, r.truncated or false
end

-- blame_entries parses `git blame --porcelain`. The first time a commit shows up its
-- header lines follow; later groups of the same commit carry no header. Every group ends
-- with the source line, which starts with a tab. It yields to the scheduler now and then
-- so a long output does not use up the time slice of the task.
local function blame_entries(out)
  local commits, entries = {}, {}
  local cur, final
  local pos, n, count = 1, #out, 0
  while pos <= n do
    local e = out:find("\n", pos, true) or (n + 1)
    local line = out:sub(pos, e - 1)
    pos = e + 1
    if line:byte(1) == 9 then
      entries[#entries + 1] = {
        line = final - 1,
        commit = cur.commit,
        author = cur.author,
        time = cur.time,
        summary = cur.summary,
        uncommitted = cur.uncommitted,
      }
      count = count + 1
      if count % 1000 == 0 then
        gvim.timer.sleep(0)
      end
    else
      local sha, fin = line:match("^(%x+) %d+ (%d+)")
      if sha and #sha >= 40 then
        cur = commits[sha]
        if not cur then
          cur = { commit = sha, uncommitted = sha:find("^0+$") ~= nil }
          commits[sha] = cur
        end
        final = tonumber(fin)
      elseif cur then
        local key, val = line:match("^(%S+) ?(.*)$")
        if key == "author" then
          cur.author = val
        elseif key == "author-time" then
          cur.time = tonumber(val)
        elseif key == "summary" then
          cur.summary = val
        end
      end
    end
  end
  return entries
end

function M.blame(path, opts)
  opts = opts or {}
  local args = { "blame", "--porcelain" }
  if opts.from or opts.to then
    local from = (opts.from or 0) + 1
    local to = opts.to or math.huge
    if to == math.huge then
      args[#args + 1] = "-L" .. from .. ","
    else
      args[#args + 1] = "-L" .. from .. "," .. math.max(to, from)
    end
  end
  if opts.contents then
    args[#args + 1] = "--contents"
    args[#args + 1] = "-"
  end
  if opts.rev then
    local bad = check_rev(opts.rev)
    if bad then
      return nil, bad
    end
    args[#args + 1] = opts.rev
  end
  args[#args + 1] = "--"
  args[#args + 1] = gvim.path.basename(path)
  if not M.available() then
    return nil, NO_GIT
  end
  local cwd = gvim.path.dirname(path)
  local filters, ferr = neutral_filters(cwd)
  if not filters then
    return nil, ferr
  end
  local r, err = run(args, {
    cwd = cwd,
    stdin = opts.contents,
    max_output = MAX_BLAME_OUTPUT,
  }, filters)
  if not r then
    return nil, err
  end
  if not r.ok then
    return nil, "not_found: " .. first_line(r.stderr)
  end
  if r.truncated then
    return nil, "too_large: the blame output is larger than " .. MAX_BLAME_OUTPUT .. " bytes"
  end
  return blame_entries(r.stdout)
end

-- commit returns the description of a commit: header, message and the files it touched.
function M.commit(dir, hash)
  if type(hash) ~= "string" or not hash:find("^%x+$") or #hash < 4 or #hash > 64 then
    return nil, "invalid: bad commit id"
  end
  local r, err = M.run({ "show", "--stat", "--format=fuller", hash }, { cwd = dir, max_output = 1024 * 1024 })
  if not r then
    return nil, err
  end
  if not r.ok then
    return nil, "not_found: " .. first_line(r.stderr)
  end
  return gvim.text.split(chomp(r.stdout), "\n", true)
end

return M
