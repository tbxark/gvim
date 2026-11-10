-- Git blame, reduced. Fresh's git_blame is a magit-style panel that steps back through
-- history; this port keeps the part that is useful while editing:
--
--   Git: Toggle Blame       the author, age and summary of the commit that last changed the
--                           cursor line, shown after the line (off until you ask for it)
--   Git: Show Commit        the commit behind the cursor line in a popup
--   Git: Copy Commit Hash   the hash of that commit to the clipboard
--   Git: Blame Previous Revision  step the inline blame to the parent of the blamed commit
--   Git: Blame HEAD         return the inline blame to the working-tree revision
--
-- Only a window of 40 lines around the cursor is blamed at a time (`git blame -L`), so the
-- cost does not grow with the file. A buffer with unsaved changes is blamed by its text
-- (`--contents -`), so the answer matches what is on screen; the answers are kept per
-- (file, buffer version) for 30 seconds. A file that is not tracked shows nothing.
local gvim = require "gvim"
local git = gvim.git
local t = gvim.i18n.t

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
local view_rev = {} -- buffer id -> revision currently being blamed (nil means HEAD)
local view_path = {} -- buffer id -> path that revision belongs to
-- Every asynchronous git yield carries this generation. A state change (toggle, revision
-- switch, buffer close) invalidates older completions before they can paint/set state.
local generation = 0

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

-- current_revision returns the parent-revision override only while the buffer still has the
-- path that revision belongs to. Save As changes a buffer's path in place; keeping the old
-- parent revision would blame unrelated history, so it is invalidated (and bumping the
-- generation makes any in-flight result stale).
local function current_revision(b, info)
  local stored_path = view_path[b.id]
  local path = info.path or ""
  if stored_path == nil then
    view_rev[b.id] = nil
    return nil
  end
  if stored_path ~= path then
    view_rev[b.id] = nil
    view_path[b.id] = nil
    generation = generation + 1
    return nil
  end
  return view_rev[b.id]
end

-- entry_for returns the blame entry of a line of buffer b, asking git when the cache does
-- not have it.
local function entry_for(b, line, rev_override)
  local info = b:info()
  if not info or (info.path or "") == "" or info.virtual or info.large then
    return nil, "unsupported: this buffer has no file"
  end
  local rev = rev_override or current_revision(b, info)
  if rev and info.modified then
    return nil, "unsupported: save the file before stepping through history"
  end
  local key = info.path .. "@" .. info.version .. "@" .. (rev or "HEAD")
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
  local opts = { from = from, to = math.min(from + WINDOW, n), rev = rev }
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

-- snapshot captures everything an async blame result is allowed to depend on.
local function snapshot(b)
  local info = b and b:info()
  if not info then
    return nil
  end
  local cursor = b:cursor()
  return {
    buf = b,
    id = b.id,
    path = info.path or "",
    version = b:version(),
    line = cursor.line,
    modified = info.modified,
    rev = current_revision(b, info),
    generation = generation,
  }
end

-- still_holds re-reads the live state after a yield. Any changed buffer, path, cursor,
-- revision, enable state or generation makes the captured task stale.
local function still_holds(s, require_enabled)
  if not s or generation ~= s.generation then
    return false
  end
  if require_enabled and not enabled then
    return false
  end
  local current = gvim.buf.current()
  if not current or current.id ~= s.id or not s.buf:valid() then
    return false
  end
  local info = s.buf:info()
  if not info or (info.path or "") ~= s.path or s.buf:version() ~= s.version then
    return false
  end
  if s.buf:cursor().line ~= s.line then
    return false
  end
  return (view_rev[s.id] or "HEAD") == (s.rev or "HEAD")
end

local function update()
  if not enabled then
    return
  end
  local b = gvim.buf.current()
  if not b then
    return
  end
  local s = snapshot(b)
  if not s then
    return
  end
  local e, err = entry_for(b, s.line, s.rev)
  if not e and err then
    gvim.log.debug("git_blame: %s", tostring(err))
  end
  if not still_holds(s, true) then
    return -- a newer update/revision/toggle owns the state
  end
  if not e then
    ns:clear(b)
    return
  end
  ns:set(b, { { kind = "eol", line = s.line, text = label(e), style = { fg = "dim", italic = true }, priority = 0 } })
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
  generation = generation + 1
  view_rev, view_path = {}, {}
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
  generation = generation + 1
  for _, s in ipairs(subs) do
    s:off()
  end
  subs, cache, view_rev, view_path = {}, {}, {}, {}
  ns:clear()
end

-- current_entry is the blame entry under the caret and its captured snapshot, or nil after
-- telling the user why not. Callers use the snapshot path (not a later b:path()) so a Save As
-- or tab/cursor change while git ran cannot retarget the popup/clipboard operation.
local function current_entry()
  local b = gvim.buf.current()
  if not b then
    gvim.notify("No file open to blame")
    return
  end
  local s = snapshot(b)
  if not s then
    gvim.notify("No blame information available (not a git file or error)")
    return
  end
  local e, err = entry_for(b, s.line, s.rev)
  if not still_holds(s, false) then
    return
  end
  if not e then
    gvim.log.debug("git_blame: %s", tostring(err))
    gvim.notify("No blame information available (not a git file or error)")
    return
  end
  if e.uncommitted then
    gvim.notify("This line is not yet committed")
    return
  end
  return e, s
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
    local e, s = current_entry()
    if not e then
      return
    end
    local lines, err = git.commit(gvim.path.dirname(s.path), e.commit)
    if not still_holds(s, false) then
      return
    end
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
    gvim.notify(t("status.hash_copied", { short = e.commit:sub(1, 7), full = e.commit }))
  end,
}

-- parent_revision returns the first parent of commit, using the same neutralized git
-- runner as the rest of the plugin. A root commit or an unreadable result is an error.
local function parent_revision(b, commit)
  if type(commit) ~= "string" or not commit:match("^[0-9a-fA-F]+$") then
    return nil, "invalid commit"
  end
  local r, err = git.run({ "rev-parse", "--verify", commit .. "^" }, {
    cwd = gvim.path.dirname(b:path()),
    timeout_ms = 15000,
  })
  if not r then
    return nil, err
  end
  if not r.ok then
    return nil, "no parent"
  end
  local parent = gvim.text.trim(r.stdout or "")
  if parent == "" or not parent:match("^[0-9a-fA-F]+$") then
    return nil, "no parent"
  end
  return parent
end

gvim.command.register {
  name = "blame_previous",
  title = "Git: Blame Previous Revision",
  description = "Show the inline blame at the parent revision of the cursor line's commit",
  run = function()
    if not enabled then
      gvim.notify("Git blame is off")
      return
    end
    local b = gvim.buf.current()
    if not b then
      gvim.notify(t("status.no_file"))
      return
    end
    local s = snapshot(b)
    if not s then
      gvim.notify(t("status.no_blame_info"))
      return
    end
    if s.modified then
      gvim.notify("Save the file before stepping through blame history")
      return
    end
    local e, err = entry_for(b, s.line)
    if not still_holds(s, true) then
      return -- cursor/tab/state changed while git ran: never step the new line with the old entry
    end
    if not e then
      gvim.log.debug("git_blame: %s", tostring(err))
      gvim.notify(t("status.no_blame_info"))
      return
    end
    if e.uncommitted then
      gvim.notify(t("status.not_committed"))
      return
    end
    local commit = e.commit
    if type(commit) ~= "string" or not commit:match("^[0-9a-fA-F]+$") then
      gvim.notify("No blame information available (not a git file or error)")
      return
    end
    local parent, parent_err = parent_revision(b, commit)
    if not still_holds(s, true) then
      return
    end
    if not parent then
      gvim.notify("Git: no parent revision for " .. commit:sub(1, 7) .. " (" .. tostring(parent_err) .. ")", "warn")
      return
    end
    -- Resolve the parent line before switching; a root/merge or a parent file without this
    -- line must not report a successful step or change the next view.
    local parent_entry, blame_err = entry_for(b, s.line, parent)
    if not still_holds(s, true) then
      return
    end
    if not parent_entry then
      gvim.notify("Git: no blame at parent " .. parent:sub(1, 7) .. " (" .. tostring(blame_err) .. ")", "warn")
      return
    end
    generation = generation + 1
    view_rev[b.id] = parent
    view_path[b.id] = s.path
    refresh()
    gvim.notify("Blame at parent " .. parent:sub(1, 7))
  end,
}

gvim.command.register {
  name = "blame_head",
  title = "Git: Blame HEAD",
  description = "Return the inline blame to the working-tree revision",
  run = function()
    if not enabled then
      gvim.notify("Git blame is off")
      return
    end
    local b = gvim.buf.current()
    if not b then
      gvim.notify("No file open to blame")
      return
    end
    generation = generation + 1
    view_rev[b.id] = nil
    view_path[b.id] = nil
    refresh()
    gvim.notify("Blame at HEAD")
  end,
}
