-- Markdown source-mode smart list editing: a port of Fresh's plugins/markdown_source.ts
-- (374 lines, mostly the Enter/Tab/Shift+Tab rules and their parse helpers).
--
-- gvim's editor already implements Fresh's markdown_source rules natively for Markdown
-- buffers (editor/markdown.go is called from the insert_newline, insert_tab and
-- dedent_selection actions). This bundled plugin is the explicit Lua port for plugin
-- parity, and it is opt-in: enabling it installs normal-context hidden commands for
-- Enter/Tab/Shift+Tab that perform the rules in Lua when the active buffer is Markdown,
-- and fall back to the editor's own actions everywhere else. It deliberately does not
-- define Fresh's "markdown-source" input mode: gvim's plugin API has no source/compose
-- view query and a buffer-local mode would add a status label and an Esc-leave hint to
-- every Markdown tab. The fallbacks are the editor actions, so a non-Markdown buffer,
-- a selection, extra cursors or an active input mode (vi) keep exactly the old
-- behaviour instead of being intercepted by the plugin.
local gvim = require "gvim"

local t = gvim.i18n.t

local TAB_SIZE = 4

local function trim(s)
  return (s:match("^%s*(.-)%s*$")) or ""
end

local function leading_whitespace(s)
  return s:match("^[ \t]*") or ""
end

local function active_buffer()
  local buf = gvim.buf.current()
  if not buf then
    gvim.status(t("status.no_active_buffer"))
    return nil
  end
  return buf
end

local function is_markdown_path(path)
  if not path or path == "" then
    return false
  end
  local lower = string.lower(path)
  return lower:sub(-3) == ".md" or lower:sub(-9) == ".markdown" or lower:sub(-4) == ".mdx"
end

local function is_markdown(buf)
  local language = buf:language()
  if language == "markdown" or language == "multimarkdown" then
    return true
  end
  return is_markdown_path(buf:path())
end

-- handled_by_others reports the paths that must not go through the list rules: any
-- editor-wide/buffer input mode (Fresh steps aside for its input mode), a selection, or
-- more than one caret. Those fall back to the built-in editor actions below.
local function handled_by_others(buf)
  if gvim.mode.current() ~= nil then
    return true
  end
  if buf:selection() ~= nil then
    return true
  end
  local cursors = buf:cursors()
  return cursors ~= nil and #cursors > 1
end

local function run_action(action, args)
  local ok, err = gvim.command.run(action, args)
  if not ok then
    gvim.status(t("status.action_failed", { action = action, error = tostring(err) }))
    return false
  end
  return true
end

local function report_edit_error(err)
  gvim.status(t("status.edit_failed", { error = tostring(err) }))
end

-- parse_marker parses the caret's line prefix the way Fresh's parseListMarker does:
-- leading blanks, then a checkbox ("- [x] "), ordered ("1. ") or unordered ("- ")
-- marker. A line without a marker returns nil.
local function parse_marker(prefix)
  local indent = leading_whitespace(prefix)
  local rest = prefix:sub(#indent + 1)
  local bullet, check, content = rest:match("^([%*%-%+]) %[([ xX])%] (.*)$")
  if bullet then
    return { kind = "checkbox", indent = indent, bullet = bullet, checked = check ~= " ", content = content }
  end
  local number, ordered_content = rest:match("^(%d+)%. (.*)$")
  if number then
    return { kind = "ordered", indent = indent, number = tonumber(number), content = ordered_content }
  end
  bullet, content = rest:match("^([%*%-%+]) (.*)$")
  if bullet then
    return { kind = "unordered", indent = indent, bullet = bullet, content = content }
  end
  return nil
end

local function marker_blank(marker, after)
  return trim(marker.content) == "" and trim(after) == ""
end

local function next_marker(marker)
  if marker.kind == "ordered" then
    return marker.indent .. tostring((marker.number or 0) + 1) .. ". "
  end
  if marker.kind == "checkbox" then
    return marker.indent .. marker.bullet .. " [ ] "
  end
  return marker.indent .. marker.bullet .. " "
end

local function bullet_text(marker, bullet)
  if marker.kind ~= "checkbox" then
    return bullet .. " "
  end
  return bullet .. " [" .. (marker.checked and "x" or " ") .. "] "
end

local function cycle_bullet(bullet, forward)
  if forward then
    if bullet == "*" then
      return "-"
    elseif bullet == "-" then
      return "+"
    elseif bullet == "+" then
      return "*"
    end
    return "-"
  end
  if bullet == "*" then
    return "+"
  elseif bullet == "+" then
    return "-"
  elseif bullet == "-" then
    return "*"
  end
  return "-"
end

-- line_parts returns the current line, the text before the caret and the text after it.
-- Columns are byte columns, like gvim's plugin API.
local function line_parts(buf)
  local pos = buf:cursor()
  local line = buf:line(pos.line) or ""
  if pos.col < 0 then
    pos.col = 0
  elseif pos.col > #line then
    pos.col = #line
  end
  return pos, line, line:sub(1, pos.col), line:sub(pos.col + 1)
end

local function insert_text(buf, pos, text, cursor_line, cursor_col)
  local ok, err = buf:insert(pos, text)
  if not ok then
    report_edit_error(err)
    return
  end
  if cursor_line then
    buf:set_cursor(gvim.pos(cursor_line, cursor_col or 0), { reveal = true })
  end
end

local function replace_line(buf, line, text)
  local old = buf:line(line) or ""
  local ok, err = buf:replace(gvim.range(line, 0, line, #old), text)
  if not ok then
    report_edit_error(err)
    return false
  end
  buf:set_cursor(gvim.pos(line, #text), { reveal = true })
  return true
end

-- Enter: continue the list with the next marker (same indent/bullet, number+1,
-- unchecked checkbox); an empty list item drops its marker instead. A non-list line
-- copies its leading blanks. Everything that is not a Markdown source edit goes to the
-- editor's insert_newline action, which applies the same rules itself for Markdown.
local function md_enter()
  local buf = active_buffer()
  if not buf then
    return
  end
  if not is_markdown(buf) or handled_by_others(buf) then
    run_action("insert_newline")
    return
  end
  local pos, line, before, after = line_parts(buf)
  local marker = parse_marker(before)
  if marker then
    if marker_blank(marker, after) then
      local ok, err = buf:delete(gvim.range(pos.line, 0, pos.line, #line))
      if not ok then
        report_edit_error(err)
        return
      end
      buf:set_cursor(gvim.pos(pos.line, 0), { reveal = true })
      return
    end
    local text = "\n" .. next_marker(marker)
    insert_text(buf, pos, text, pos.line + 1, #text - 1)
    return
  end
  local indent = leading_whitespace(before)
  insert_text(buf, pos, "\n" .. indent, pos.line + 1, #indent)
end

-- Tab: a blank unordered/checkbox item indents four spaces and cycles its bullet
-- forward; any other line of a Markdown buffer takes four spaces at the caret. Everything
-- that is not a Markdown source edit goes to the editor's insert_tab action.
local function md_tab()
  local buf = active_buffer()
  if not buf then
    return
  end
  if not is_markdown(buf) or handled_by_others(buf) then
    run_action("insert_tab")
    return
  end
  local pos, _, before, after = line_parts(buf)
  local marker = parse_marker(before)
  if marker and marker.kind ~= "ordered" and marker_blank(marker, after) then
    local text = marker.indent .. string.rep(" ", TAB_SIZE) .. bullet_text(marker, cycle_bullet(marker.bullet, true))
    replace_line(buf, pos.line, text)
    return
  end
  insert_text(buf, pos, string.rep(" ", TAB_SIZE), pos.line, pos.col + TAB_SIZE)
end

-- Shift+Tab: a blank unordered/checkbox item with at least four blanks of indent loses
-- four and cycles its bullet in reverse; everything else goes to the editor's
-- dedent_selection action.
local function md_shift_tab()
  local buf = active_buffer()
  if not buf then
    return
  end
  if not is_markdown(buf) or handled_by_others(buf) then
    run_action("dedent_selection")
    return
  end
  local pos, _, before, after = line_parts(buf)
  local marker = parse_marker(before)
  if marker and marker.kind ~= "ordered" and marker_blank(marker, after) and #marker.indent >= TAB_SIZE then
    local text = marker.indent:sub(TAB_SIZE + 1) .. bullet_text(marker, cycle_bullet(marker.bullet, false))
    replace_line(buf, pos.line, text)
    return
  end
  run_action("dedent_selection")
end

-- Hidden commands own the key bindings, so the plugin only adds the three normal-context
-- chords (a user keybinding still wins) and the palette stays free of demo rows.
gvim.command.register({
  name = "enter",
  title = "Markdown Source: Enter",
  hidden = true,
  keys = { "enter" },
  run = function() md_enter() end,
})

gvim.command.register({
  name = "tab",
  title = "Markdown Source: Tab",
  hidden = true,
  keys = { "tab" },
  run = function() md_tab() end,
})

gvim.command.register({
  name = "shift_tab",
  title = "Markdown Source: Shift+Tab",
  hidden = true,
  keys = { "shift+tab" },
  run = function() md_shift_tab() end,
})

gvim.log.debug("markdown_source: Plugin loaded")
