package agentdiscover

import (
	"strconv"
	"strings"
)

// The tmux format strings the scanner asks for. tmux separates fields with the
// unit separator (0x1f) and escapes control bytes in its format output as octal
// (the separator becomes the four characters backslash 0 3 7) and doubles
// backslashes, so the output cannot be split and then unescaped per field:
// splitEscapedFields does both in one pass. These are Fresh's formats
// (plugins/lib/tmux_format.ts).
const (
	sepByte = 31 // the unit separator
	// backslashByte is the escape byte tmux doubles and uses before octal codes.
	backslashByte = 92
)

var (
	sep        = string(rune(sepByte))
	sessionFmt = "#{session_id}" + sep + "#{session_name}" + sep + "#{session_created}" + sep +
		"#{session_attached}" + sep + "#{session_path}"
	paneFmt = "#{session_id}" + sep + "#{window_index}" + sep + "#{window_name}" + sep + "#{pane_id}" + sep +
		"#{pane_pid}" + sep + "#{pane_current_path}" + sep + "#{pane_current_command}" + sep + "#{pane_title}"
)

// agentCommands are the foreground command names Fresh reports as probable
// agents: a name match, explicitly not an identity. Config agents extend the set.
var agentCommands = map[string]bool{
	"claude": true, "codex": true, "gemini": true, "goose": true, "aider": true,
	"cursor-agent": true, "copilot": true, "qwen": true, "droid": true,
	"opencode": true, "amp": true,
}

// splitEscapedFields splits one line of tmux format output at the separator and
// undoes tmux's escaping: a backslash and three octal digits become the byte, a
// doubled backslash becomes one, and an escape tmux would never produce passes
// through as written. A raw separator from newer tmux versions also splits.
func splitEscapedFields(line string) []string {
	fields := make([]string, 0, 8)
	var current strings.Builder
	for i := 0; i < len(line); {
		switch {
		case line[i] == sepByte:
			fields = append(fields, current.String())
			current.Reset()
			i++
		case line[i] != backslashByte:
			current.WriteByte(line[i])
			i++
		case i+1 < len(line) && line[i+1] == backslashByte:
			current.WriteByte(backslashByte)
			i += 2
		case i+3 < len(line) && isOctal3(line[i+1:i+4]):
			value := decodeOctal(line[i+1 : i+4])
			if value == sepByte {
				fields = append(fields, current.String())
				current.Reset()
			} else {
				current.WriteByte(value)
			}
			i += 4
		default:
			current.WriteByte(line[i])
			i++
		}
	}
	fields = append(fields, current.String())
	return fields
}

// isOctal3 reports whether s is exactly three octal digits.
func isOctal3(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '7' {
			return false
		}
	}
	return true
}

// decodeOctal decodes three octal digits to one byte; 0 when they are not.
func decodeOctal(s string) byte {
	n, err := strconv.ParseUint(s, 8, 8)
	if err != nil {
		return 0
	}
	return byte(n)
}

// sessionRow is one parsed list-sessions line.
type sessionRow struct {
	ID       string
	Name     string
	Created  int64
	Attached bool
	Path     string
}

// parseSessions parses list-sessions output. A line with no id is skipped: a
// malformed line must not fail the whole scan.
func parseSessions(output string) []sessionRow {
	out := make([]sessionRow, 0, 8)
	forEachLine(output, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		f := splitEscapedFields(line)
		if len(f) < 1 || f[0] == "" {
			return
		}
		row := sessionRow{ID: f[0]}
		if len(f) > 1 {
			row.Name = f[1]
		}
		if len(f) > 2 {
			row.Created, _ = strconv.ParseInt(strings.TrimSpace(f[2]), 10, 64)
		}
		if len(f) > 3 {
			row.Attached = strings.TrimSpace(f[3]) == "1"
		}
		if len(f) > 4 {
			row.Path = f[4]
		}
		out = append(out, row)
	})
	return out
}

// paneRow is one parsed list-panes line: the fields the scanner uses.
type paneRow struct {
	SessionID string
	Path      string
	Command   string
}

// parsePanes parses list-panes output. Fields the scanner does not use
// (window, pane, pid, title) are ignored.
func parsePanes(output string) []paneRow {
	out := make([]paneRow, 0, 8)
	forEachLine(output, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		f := splitEscapedFields(line)
		if len(f) < 1 || f[0] == "" {
			return
		}
		row := paneRow{SessionID: f[0]}
		// pane_current_path is field 5, pane_current_command field 6.
		if len(f) > 5 {
			row.Path = f[5]
		}
		if len(f) > 6 {
			row.Command = f[6]
		}
		out = append(out, row)
	})
	return out
}

// forEachLine calls fn for each line of text, without allocating the split.
func forEachLine(text string, fn func(string)) {
	for len(text) > 0 {
		i := strings.IndexByte(text, 10)
		if i < 0 {
			fn(text)
			return
		}
		fn(text[:i])
		text = text[i+1:]
	}
}

// baseName returns the last element of a path, the program name of an argv or a
// pane command.
func baseName(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
