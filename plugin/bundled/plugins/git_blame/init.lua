-- Git blame, reduced. Fresh's git_blame is a magit-style panel that steps back through
-- history; this port keeps the part that is useful while editing:
--
--   Git: Toggle Blame       the author, age and summary of the commit that last changed the
--                           cursor line, shown after the line (off until you ask for it)
--   Git: Show Commit        the commit behind the cursor line in a popup
--   Git: Copy Commit Hash   the hash of that commit to the clipboard
--
-- Only a window of 40 lines around the cursor is blamed at a time (`git blame -L`), so the
-- cost does not grow with the file. A buffer with unsaved changes is blamed by its text
-- (`--contents -`), so the answer matches what is on screen; the answers are kept per
-- (file, buffer version) for 30 seconds. A file that is not tracked shows nothing.
local gvim = require "gvim"
local git = gvim.git

local settings = gvim.settings.define {
  debounce_ms = {
    type = "integer", default = 150, minimum = 50, maximum = 2000, project = true,
    description = "Pause after the cursor moves before the blame text is updated, in milliseconds",
  },
  summary_width = {
    type = "integer", default = 60, minimum = 10, maximum = 200, project = true,
    description = "Display cells of the commit summary shown after the line",
  },
}

local ns = gvim.decor.namespace("blame")

local WINDOW = 40
local TTL_MS = 30000
local MAX_CACHED = 2000
local MAX_CONTENTS = 4 * 1024 * 1024
local POPUP_LINES = 200

local enabled = false
local subs = {}
local cache = {} -- buffer id -> { key, at, lines = { [line] = entry }, n }

local function age(t)
  local d = math.floor(gvim.time.now_ms() / 1000) - (t or 0)
  local units = {
    { 31536000, "year" }, { 2592000, "month" }, { 604800, "week" },
    { 86400, "day" }, { 3600, "hour" }, { 60, "minute" },
  }
  for _, u in ipairs(units) do
    if d >= u[1] then
      local n = math.floor(d / u[1])
      return n .. " " .. u[2] .. (n == 1 and "" or "s") .. " ago"
    end
  end
  return "just now"
end

local function label(e)
  if e.uncommitted then
    return "  Not committed yet"
  end
  return string.format("  %s, %s • %s", e.author or "?", age(e.time),
    gvim.text.truncate(e.summary or "", settings.summary_width, "…"))
end

-- entry_for returns the blame entry of a line of buffer b, asking git when the cache does
-- not have it.
local function entry_for(b, line)
  local info = b:info()
  if not info or (info.path or "") == "" or info.virtual or info.large then
    return nil, "unsupported: this buffer has no file"
  end
  local key = info.path .. "@" .. info.version
  local now = gvim.time.monotonic_ms()
  local c = cache[b.id]
  if not c or c.key ~= key or now - c.at > TTL_MS then
    c = { key = key, at = now, lines = {}, n = 0 }
    cache[b.id] = c
  end
  if c.lines[line] then
    return c.lines[line]
  end
  -- git counts the lines of the file; the editor shows one more, empty, after a final newline.
  local n = info.line_count
  if not n then
    return nil, "unsupported: the size of the buffer is not known"
  end
  if n > 0 and b:line(n - 1) == "" then
    n = n - 1
  end
  if line >= n then
    return nil, "not_found: the caret is below the last line"
  end
  local from = math.max(0, line - math.floor(WINDOW / 2))
  local opts = { from = from, to = math.min(from + WINDOW, n) }
  if info.modified then
    -- the editor reports "UTF-8" or "ASCII" for text that git compares byte for byte
    local enc = tostring(info.encoding):lower()
    if enc ~= "utf-8" and enc ~= "ascii" then
      return nil, "unsupported: unsaved text in " .. tostring(info.encoding)
    end
    if (b:byte_length() or math.huge) > MAX_CONTENTS then
      return nil, "too_large: the buffer is too large to blame"
    end
    local text = b:text()
    if not text then
      return nil, "closed: the buffer was closed"
    end
    if tostring(info.line_ending):lower() == "crlf" then
      text = text:gsub("\n", "\r\n")
    end
    opts.contents = text
  end
  local entries, err = git.blame(info.path, opts)
  if not entries then
    return nil, err
  end
  if not b:valid() or b:version() ~= info.version then
    return nil, "stale: the buffer changed"
  end
  if c.n + #entries > MAX_CACHED then
    c.lines, c.n = {}, 0
  end
  for _, e in ipairs(entries) do
    c.lines[e.line] = e
    c.n = c.n + 1
  end
  return c.lines[line]
end

local running, again = false, false

local function update()
  local b = gvim.buf.current()
  if not b then
    return
  end
  local line = b:cursor().line
  local e, err = entry_for(b, line)
  if not e and err then
    gvim.log.debug("git_blame: %s", tostring(err))
  end
  if not enabled or not b:valid() or b:cursor().line ~= line then
    return -- turned off, or the caret moved on while git ran: a newer update follows
  end
  if not e then
    ns:clear(b)
    return
  end
  ns:set(b, { { kind = "eol", line = line, text = label(e), style = { fg = "dim", italic = true }, priority = 0 } })
end

-- refresh runs update; a request that arrives meanwhile makes it run once more.
local function refresh()
  if running then
    again = true
    return
  end
  running = true
  repeat
    again = false
    local ok, err = pcall(update)
    if not ok then
      gvim.log.warn("git_blame: %s", tostring(err))
    end
  until not again or not enabled
  running = false
end

local function enable()
  enabled = true
  subs = {
    gvim.on("cursor_moved", refresh, { debounce = settings.debounce_ms }),
    gvim.on("buffer_changed", refresh, { debounce = 500 }),
    gvim.on("buffer_activated", refresh),
    gvim.on("after_file_save", refresh),
  }
  refresh()
end

local function disable()
  enabled = false
  for _, s in ipairs(subs) do
    s:off()
  end
  subs, cache = {}, {}
  ns:clear()
end

-- current_entry is the blame entry under the caret, or nil after telling the user why not.
local function current_entry()
  local b = gvim.buf.current()
  if not b then
    gvim.notify("No file open to blame")
    return
  end
  local e, err = entry_for(b, b:cursor().line)
  if not e then
    gvim.log.debug("git_blame: %s", tostring(err))
    gvim.notify("No blame information available (not a git file or error)")
    return
  end
  if e.uncommitted then
    gvim.notify("This line is not yet committed")
    return
  end
  return e, b
end

gvim.command.register {
  name = "toggle",
  title = "Git: Toggle Blame",
  description = "Show or hide the blame of the cursor line after the line",
  run = function()
    if enabled then
      disable()
      gvim.notify("Git blame off")
    else
      enable()
      gvim.notify("Git blame on")
    end
  end,
}

gvim.command.register {
  name = "show_commit",
  title = "Git: Show Commit",
  description = "Show the commit that last changed the cursor line",
  run = function()
    local e, b = current_entry()
    if not e then
      return
    end
    local lines, err = git.commit(gvim.path.dirname(b:path()), e.commit)
    if not lines then
      gvim.notify("Git: cannot show " .. e.commit:sub(1, 7) .. ": " .. tostring(err), "warn")
      return
    end
    if #lines > POPUP_LINES then
      local head = {}
      for i = 1, POPUP_LINES do
        head[i] = lines[i]
      end
      lines = head
    end
    gvim.ui.popup { title = e.commit:sub(1, 7), lines = lines, anchor = "center", width = 100 }
  end,
}

gvim.command.register {
  name = "copy_hash",
  title = "Git: Copy Commit Hash",
  description = "Copy the hash of the commit that last changed the cursor line",
  run = function()
    local e = current_entry()
    if not e then
      return
    end
    gvim.clipboard.set(e.commit)
    gvim.notify(string.format("Copied: %s (%s)", e.commit:sub(1, 7), e.commit))
  end,
}
