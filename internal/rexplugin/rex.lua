-- Installed by `gvim rex install`; `gvim rex uninstall` removes it.
--
-- gvim in a Rex pane: open a file, a directory, or a two-file diff in a
-- floating window over the session.
--
-- `gvim rex install` puts this file under Rex's config directory and adds the
-- managed block that loads it to init.lua. A copy loaded by hand takes the same
-- options:
--
--   dofile("/path/to/init.lua").setup {
--     bin = { "/Users/you/go/bin/gvim" },
--   }
--
-- The actions read the working directory of the calling pane the way a shell
-- there would, and open gvim in a floating layer over the active window.

local M = {}

local SHELL = "com.superlogical.terminal.shell"
local TERMINAL = "com.superlogical.terminal"

local DEFAULT_PATH = table.concat({
  (os.getenv("HOME") or "") .. "/go/bin",
  (os.getenv("HOME") or "") .. "/.local/bin",
  "/opt/homebrew/bin",
  "/opt/homebrew/sbin",
  "/usr/local/bin",
  "/usr/bin",
  "/bin",
}, ":")

-- decode_file_url turns an OSC 7 style file:// URL into a path.
local function decode_file_url(url)
  if type(url) ~= "string" then return nil end
  local path = url:match("^file://[^/]*(/.*)$")
  if path == nil then return nil end
  return (path:gsub("%%(%x%x)", function(hex)
    return string.char(tonumber(hex, 16))
  end))
end

function M.setup(opts)
  opts = opts or {}
  local bin = opts.bin or { "gvim" }
  if type(bin) == "string" then bin = { bin } end
  local path = opts.path or DEFAULT_PATH

  -- The argv of gvim with ARGS appended.
  local function argv(args)
    local out = {}
    for _, part in ipairs(bin) do out[#out + 1] = part end
    for _, part in ipairs(args) do out[#out + 1] = part end
    return out
  end

  -- The command for a pane. A local pane gets an explicit PATH because
  -- shell = "none" inherits the Rex server's minimal one; a remote pane
  -- keeps the host's own environment.
  local function command(ctx, args)
    if ctx.server ~= nil then return argv(args) end
    local out = { "/usr/bin/env", "PATH=" .. path }
    for _, part in ipairs(argv(args)) do out[#out + 1] = part end
    return out
  end

  -- Where the program in front works, which is right for an editor too; or
  -- else what the shell last reported.
  local function cwd(ctx)
    if ctx.block_id == nil then return nil end
    local info = rex.block.call(TERMINAL, "process", { block_id = ctx.block_id })
    if type(info) == "table" and type(info.foreground) == "table"
      and type(info.foreground.cwd) == "string" and info.foreground.cwd ~= "" then
      return info.foreground.cwd
    end
    local pwd = rex.block.call(TERMINAL, "pwd", { block_id = ctx.block_id })
    if type(pwd) ~= "table" or type(pwd.pwd) ~= "string" then return nil end
    local path = decode_file_url(pwd.pwd)
    if path ~= nil then return path end
    if pwd.pwd:sub(1, 1) == "/" then return pwd.pwd end
    return nil
  end

  local function open(ctx, label, args)
    local options = { shell = "none", command = command(ctx, args) }
    local dir = cwd(ctx)
    if dir ~= nil and dir ~= "" then options.cwd = dir end
    -- A floating layer keeps the editor wide; the file explorer and the
    -- contents panel take too much of a half-width split. Fed up with the
    -- overlay, `session.move_block` moves it into a split of its own.
    local _, err = rex.session.new_layer{
      bounds = { 0.10, 0.10, 0.80, 0.80 },
      focus = true,
      layout = rex.layout.block{ flavor = SHELL, label = label, options = options },
    }
    if err ~= nil then error("gvim: couldn't open a window: " .. tostring(err), 0) end
  end

  rex.action{
    name = "gvim_open",
    title = "Gvim: Open File…",
    description = "Open gvim in a floating window over this session, in the pane's"
      .. " directory, or on a file. FILE:LINE[:COL] puts the cursor on that line.",
    category = "Gvim",
    keywords = "gvim editor open file edit",
    args = { target = "string?" },
    run = function(ctx, args)
      local target = args.target or ""
      if target == "" then
        open(ctx, "gvim", { "." })
        return
      end
      open(ctx, target:match("([^/\\]+)$") or "gvim", { target })
    end,
  }

  rex.action{
    name = "gvim_diff",
    title = "Gvim: Review Diff",
    description = "Compare two files side by side in gvim, in this pane's directory.",
    category = "Gvim",
    keywords = "gvim diff compare review",
    args = { left = "string", right = "string" },
    run = function(ctx, args)
      open(ctx, "diff", { "--diff", args.left, args.right })
    end,
  }

  for short, key in pairs(opts.keys or {}) do
    rex.bind(key, "gvim_" .. short)
  end
end

return M
