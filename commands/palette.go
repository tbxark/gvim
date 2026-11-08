// Package commands is the command registry behind the command palette: the
// entries, the query modes, the availability rules and the filtering and
// ranking for a query, following Fresh's command registry
// (crates/fresh-editor/src/input/command_registry.rs).
//
// The builtin command table (commands_gen.go) is generated from the Fresh
// sources by gen/generate.py. Each entry's Keybinding comes from a keymap.Map,
// so rebinding a key updates the palette too.
//
// The query has four modes, chosen by its first character: no prefix searches
// files; ">" runs commands; ":" jumps to a line; "#" picks a buffer. Nothing is
// forced: a query without ">" is a file query. Search ranks only command mode.
// File search is finder's; line and buffer modes are the editor's, and
// Search returns nil for them.
package commands

import (
	"cmp"
	"slices"
	"strings"

	"github.com/tbxark/gvim/fuzzy"
	"github.com/tbxark/gvim/keymap"
)

// SourceBuiltin is the Source of every command in the builtin table.
const SourceBuiltin = "builtin"

// normalContext is the KeyContext name used for the editor's normal mode.
const normalContext = "Normal"

// Entry is one row of the palette.
type Entry struct {
	Name        string // label shown in the list, for example "Add Cursor Above"
	Keybinding  string // formatted shortcut, for example "⌃⌥↑"; empty when unbound
	Description string
	Action      string // Fresh action name, for example "add_cursor_above"
	Source      string // SourceBuiltin for the builtin table
	// Disabled is set by SearchIn: the command is listed but cannot run in the
	// current state. Disabled entries sort after enabled ones.
	Disabled bool
	// When holds the Holds-style predicates of a plugin command ("has_buffer",
	// "!read_only", "language:go"). The ui evaluates them; available does not read them.
	When []string

	contexts       []string // KeyContext names the command is scoped to; empty means any
	customContexts []string // custom context keys that must be active to show it
}

// builtinCommand is one generated row of commands_gen.go.
type builtinCommand struct {
	Name           string
	Description    string
	Action         string
	Contexts       []string
	CustomContexts []string
}

// Commands returns the builtin command entries in palette order. Keybinding is
// the formatted primary shortcut of the entry's action in km, found with
// keymap.Map.Primary as the menu finds its shortcuts. Only a single-chord shortcut
// that the editor runs counts: a chord that an override took away, a global chord
// that a normal binding takes, and a multi-key sequence are not shown. A nil km
// means keymap.Default().
//
// Bindings scoped to other contexts are not shown: the palette runs in the
// normal editor context, so a key that only works in, say, a composite buffer
// ("q" for Close Buffer) does not appear in its column. An action whose primary
// binding is in another context has an empty Keybinding.
func Commands(km *keymap.Map) []Entry {
	if km == nil {
		km = keymap.Default()
	}
	out := make([]Entry, 0, len(builtinCommands)+len(compareCommands)+len(markdownCommands)+len(pluginCommands))
	for _, c := range slices.Concat(builtinCommands, compareCommands, markdownCommands, pluginCommands) {
		if r, ok := gvimRenames[c.Action]; ok {
			c.Name, c.Description = cmp.Or(r.Name, c.Name), r.Description
		}
		e := Entry{
			Name:           c.Name,
			Description:    c.Description,
			Action:         c.Action,
			Source:         SourceBuiltin,
			contexts:       c.Contexts,
			customContexts: c.CustomContexts,
		}
		if b, ok := km.Primary(c.Action); ok && (b.When == keymap.ContextNormal || b.When == keymap.ContextGlobal) {
			e.Keybinding = FormatChord(b.Chord)
		}
		out = append(out, e)
	}
	return out
}

// Mode is the kind of search the palette runs for a query.
type Mode int

// The palette modes. ModeFile is the default when the query has no prefix.
const (
	ModeFile Mode = iota
	ModeCommand
	ModeLine
	ModeBuffer
)

// String returns a short name for the mode, for logs and tests.
func (m Mode) String() string {
	switch m {
	case ModeCommand:
		return "command"
	case ModeLine:
		return "line"
	case ModeBuffer:
		return "buffer"
	}
	return "file"
}

// ParseQuery splits a raw palette query into its mode and the text to search
// for. The prefix character is removed; spaces in the text are kept.
func ParseQuery(query string) (Mode, string) {
	switch {
	case strings.HasPrefix(query, ">"):
		return ModeCommand, query[1:]
	case strings.HasPrefix(query, ":"):
		return ModeLine, query[1:]
	case strings.HasPrefix(query, "#"):
		return ModeBuffer, query[1:]
	}
	return ModeFile, query
}

// State is the editor state the availability rules read. It mirrors the
// BufferCapabilities and flags the legacy palette consults. The zero value
// describes no buffer at all.
type State struct {
	TextBuffer      bool // the active buffer holds the user's text (not a terminal or panel)
	Editable        bool // the active buffer accepts edits
	Modified        bool // the active buffer has unsaved changes
	HasPath         bool // the active buffer is backed by a file
	AnyModified     bool // some buffer has unsaved changes
	HasLSPConfig    bool // the language has an LSP server configured
	SelectionActive bool // a selection is active
}

// DefaultState describes a clean, file-backed text buffer with no LSP config and
// no selection. It is the state of the palette parity scenarios.
func DefaultState() State {
	return State{TextBuffer: true, Editable: true, HasPath: true}
}

// Search returns the entries that match a raw query, best match first, for the
// DefaultState. It handles only ModeCommand and returns nil for other modes.
func Search(entries []Entry, query string) []Entry {
	return SearchIn(entries, query, DefaultState())
}

// SearchIn is Search for an explicit editor state. It hides commands whose
// custom contexts are not active, marks the rest Disabled from st, and ranks
// them the way the legacy palette does.
func SearchIn(entries []Entry, query string, st State) []Entry {
	mode, text := ParseQuery(query)
	if mode != ModeCommand {
		return nil
	}
	visible := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if len(e.customContexts) > 0 {
			continue // custom contexts are never active in this port
		}
		e.Disabled = !available(e, st)
		visible = append(visible, e)
	}
	return Filter(visible, text)
}

// available reports whether a command can run in the normal editor context with
// the given state. It follows is_available and the buffer checks in
// command_registry.rs.
func available(e Entry, st State) bool {
	if len(e.contexts) > 0 && !slices.Contains(e.contexts, normalContext) {
		return false
	}
	switch e.Action {
	case "save":
		return st.TextBuffer && st.Modified
	case "save_as":
		return st.TextBuffer
	case "save_all":
		return st.AnyModified
	case "revert", "reload_with_encoding":
		return st.TextBuffer && st.HasPath
	case "lsp_restart", "lsp_toggle_for_buffer":
		return st.HasLSPConfig
	case "find_in_selection":
		return st.SelectionActive
	}
	if editingActions[e.Action] {
		return st.Editable
	}
	return true
}

// editingActions are the actions that change buffer text; they need an
// editable buffer. The set is Fresh's is_buffer_mutating_action.
var editingActions = map[string]bool{
	"undo": true, "redo": true, "cut": true, "paste": true,
	"delete_line": true, "delete_word_backward": true, "delete_word_forward": true,
	"delete_to_line_end": true, "transpose_chars": true, "to_upper_case": true,
	"to_lower_case": true, "sort_lines": true, "open_line": true,
	"duplicate_line": true, "duplicate_line_above": true, "toggle_comment": true,
	"dedent_selection": true, "replace": true, "query_replace": true,
	"format_buffer": true, "trim_trailing_whitespace": true,
	"ensure_final_newline": true, "lsp_rename": true, "shell_command_replace": true,
}

// Filter ranks entries against text, the query without its mode prefix.
//
// Each entry matches on its name first; if the name does not match and text is
// not empty, it matches on its description. Results are sorted by, in order:
// enabled before disabled; name matches before description matches; an exact
// name match (case-insensitive, trimmed text) before other name matches; higher
// match score first; then name in byte order. With an empty text every entry
// matches, sorted only by disabled state and name.
func Filter(entries []Entry, text string) []Entry {
	type candidate struct {
		e     Entry
		kind  int // 0: name match, 1: description match
		exact int // 0: exact name match, 1: otherwise
		score int
	}
	trimmed := strings.TrimSpace(text)
	pat := fuzzy.NewPattern(text)
	var hits []candidate
	for _, e := range entries {
		if score, _, ok := pat.Match(e.Name); ok {
			exact := 1
			if trimmed != "" && asciiEqualFold(e.Name, trimmed) {
				exact = 0
			}
			hits = append(hits, candidate{e: e, kind: 0, exact: exact, score: score})
			continue
		}
		if text == "" {
			continue
		}
		if score, _, ok := pat.Match(e.Description); ok {
			hits = append(hits, candidate{e: e, kind: 1, exact: 1, score: score})
		}
	}
	hasQuery := text != ""
	slices.SortStableFunc(hits, func(a, b candidate) int {
		if c := cmp.Compare(boolInt(a.e.Disabled), boolInt(b.e.Disabled)); c != 0 {
			return c
		}
		if hasQuery {
			if c := cmp.Compare(a.kind, b.kind); c != 0 {
				return c
			}
			if c := cmp.Compare(a.exact, b.exact); c != 0 {
				return c
			}
			if c := cmp.Compare(b.score, a.score); c != 0 {
				return c
			}
		}
		return cmp.Compare(a.e.Name, b.e.Name)
	})
	out := make([]Entry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// asciiEqualFold compares two strings with ASCII case folding only, matching
// Rust's eq_ignore_ascii_case.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// FormatChord renders a canonical chord, or a multi-key sequence, as plain text:
// "Ctrl+Alt+Up", "Ctrl+X Ctrl+S". It is keymap.FormatChord, the one label style of the
// menus, the palette, the welcome screen, the status row and the help.
func FormatChord(chord string) string { return keymap.FormatChord(chord) }
