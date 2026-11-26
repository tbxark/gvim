-- Installed by `gvim rex install`; `gvim rex uninstall` removes it.
--
-- gvim in a Rex pane: open a file or directory, open Review Diff Mode, or
-- compare two files in a floating window over the session.
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

local function default_path()
  local parts = {}
  local env_path = os.getenv("PATH")
  if type(env_path) == "string" and env_path ~= "" then
    parts[#parts + 1] = env_path
  end
  local home = os.getenv("HOME")
  if type(home) == "string" and home ~= "" then
    parts[#parts + 1] = home .. "/go/bin"
    parts[#parts + 1] = home .. "/.local/bin"
  end
  for _, dir in ipairs({
    "/opt/homebrew/bin",
    "/opt/homebrew/sbin",
    "/usr/local/bin",
    "/usr/bin",
    "/bin",
  }) do
    parts[#parts + 1] = dir
  end
  return table.concat(parts, ":")
end

-- decode_file_url turns an OSC 7 style file:// URL into a path.
local function decode_file_url(url, windows)
  if type(url) ~= "string" then return nil end
  local path = url:match("^file://[^/]*(/.*)$")
  if path == nil then return nil end
  path = path:gsub("%%(%x%x)", function(hex)
    return string.char(tonumber(hex, 16))
  end)
  if windows and path:match("^/%a:/") then return path:sub(2) end
  return path
end

function M.setup(opts)
  opts = opts or {}
  local bin = opts.bin or { "gvim" }
  if type(bin) == "string" then bin = { bin } end
  local path = opts.path or default_path()

  -- Remote panes resolve gvim on their own server. On Unix, add the usual
  -- install directories to the server's PATH before launching it.
  local function command(ctx, args, windows)
    local out = {}
    if windows then
      local executable = ctx.server ~= nil and { "gvim.exe" } or bin
      for _, part in ipairs(executable) do out[#out + 1] = part end
    elseif ctx.server ~= nil then
      out = { "/bin/sh", "-c",
        'PATH="$PATH:$HOME/go/bin:$HOME/.local/bin"; exec gvim "$@"', "gvim" }
    else
      out = { "/usr/bin/env", "PATH=" .. path }
      for _, part in ipairs(bin) do out[#out + 1] = part end
    end
    for _, part in ipairs(args) do out[#out + 1] = part end
    return out
  end

  -- Where the program in front works, which is right for an editor too; or
  -- else what the shell last reported.
  local function cwd(ctx, windows)
    if ctx.block_id == nil then return nil end
    local info = rex.block.call(TERMINAL, "process", { block_id = ctx.block_id })
    if type(info) == "table" and type(info.foreground) == "table"
      and type(info.foreground.cwd) == "string" and info.foreground.cwd ~= "" then
      return info.foreground.cwd
    end
    local pwd = rex.block.call(TERMINAL, "pwd", { block_id = ctx.block_id })
    if type(pwd) ~= "table" or type(pwd.pwd) ~= "string" then return nil end
    local path = decode_file_url(pwd.pwd, windows)
    if path ~= nil then return path end
    if pwd.pwd:sub(1, 1) == "/" then return pwd.pwd end
    if windows and (pwd.pwd:match("^%a:[/\\]") or pwd.pwd:sub(1, 2) == "\\\\") then
      return pwd.pwd
    end
    return nil
  end

  local function open(ctx, label, args)
    -- Rex routes this call to the server that owns the target pane.
    local version = rex.call("server.version")
    local windows = type(version) == "table" and type(version.platform) == "string"
      and version.platform:match("^windows_") ~= nil
    local options = { shell = "none", command = command(ctx, args, windows) }
    local dir = cwd(ctx, windows)
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
      open(ctx, target:match("([^/\\]+)$") or "gvim", { "--", target })
    end,
  }

  rex.action{
    name = "gvim_review",
    title = "Gvim: Review Diff",
    description = "Open gvim in Review Diff Mode in a floating window over this session,"
      .. " in the pane's directory.",
    category = "Gvim",
    keywords = "gvim review diff git changes",
    run = function(ctx)
      open(ctx, "review", { "--review", "." })
    end,
  }

  rex.action{
    name = "gvim_diff",
    title = "Gvim: Compare Files…",
    description = "Compare two files side by side in gvim, in this pane's directory.",
    category = "Gvim",
    keywords = "gvim diff compare review",
    args = { left = "string", right = "string" },
    run = function(ctx, args)
      open(ctx, "diff", { "--diff", "--", args.left, args.right })
    end,
  }

  for short, key in pairs(opts.keys or {}) do
    rex.bind(key, "gvim_" .. short)
  end
end

return M
