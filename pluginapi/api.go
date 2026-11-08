// Package pluginapi is the contract between the plugin host in internal/tui and
// internal/app (host side) and the plugin package (Lua runtime). It holds plain
// types, the Host and Runtime interfaces, the error codes and Unsupported, a Host
// that refuses everything. It imports the standard library only, so both sides
// and the host packages can depend on it without a cycle.
package pluginapi

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------- ids, positions ----------

// BufferID names one document for the life of the process. 0 means none. Ids are
// never reused.
type BufferID uint64

// WinID names one split. 0 means none.
type WinID uint64

// Token is the handle of one async wait.
type Token uint64

// Pos is a 0-based line and a 0-based UTF-8 BYTE column.
type Pos struct{ Line, Col int }

// Range is half-open: From inclusive, To exclusive.
type Range struct{ From, To Pos }

// Edit replaces Range with Text.
type Edit struct {
	Range Range
	Text  string
}

// ---------- versions ----------

// Version identifies one content state of one document. Compare for EQUALITY only.
// Gen is a per-document generation that changes whenever the document's Buffer
// object is replaced (revert, reload, encoding change, session restore); Gen starts
// at 1, so the zero Version never names a real state. Ver is Buffer.Version(), which
// is NOT monotonic: Undo returns an earlier value together with the earlier content,
// so equal Versions mean equal text.
type Version struct{ Gen, Ver uint64 }

// IsZero reports whether v is the zero Version, which names no real state.
func (v Version) IsZero() bool { return v == Version{} }

// String returns "<gen>.<ver>" in decimal. This is the token Lua sees.
func (v Version) String() string {
	return strconv.FormatUint(v.Gen, 10) + "." + strconv.FormatUint(v.Ver, 10)
}

// ParseVersion is the inverse of Version.String. It accepts exactly two canonical
// decimal numbers (no sign, no spaces, no leading zeros) joined by one dot.
func ParseVersion(s string) (Version, bool) {
	g, v, ok := strings.Cut(s, ".")
	if !ok {
		return Version{}, false
	}
	gen, ok := parseCanonical(g)
	if !ok {
		return Version{}, false
	}
	ver, ok := parseCanonical(v)
	if !ok {
		return Version{}, false
	}
	return Version{Gen: gen, Ver: ver}, true
}

// parseCanonical parses a decimal uint64 that String could have produced.
func parseCanonical(s string) (uint64, bool) {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

// ---------- errors ----------

// Code is the class of an expected failure.
type Code string

// The error codes. The set is fixed: Lua sees them as the prefix of "<code>: <msg>".
const (
	CodeReadOnly    Code = "read_only"
	CodeClosed      Code = "closed"    // buffer/window/handle no longer exists
	CodeTooLarge    Code = "too_large" // large-file mode or size cap
	CodeInvalid     Code = "invalid"
	CodeNotFound    Code = "not_found"
	CodeBusy        Code = "busy"
	CodeTimeout     Code = "timeout"
	CodeCancelled   Code = "cancelled"
	CodeIO          Code = "io"
	CodeExec        Code = "exec"
	CodeUnsupported Code = "unsupported"
	CodeConflict    Code = "conflict" // e.g. unsaved changes
	CodeLimit       Code = "limit"
	CodeStale       Code = "stale" // version check failed
)

// Error is an expected failure. The runtime returns it to Lua as `nil, "<code>: <msg>"`.
type Error struct {
	Code Code
	Msg  string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Msg }

// Errf returns an *Error with the code and the formatted message.
func Errf(code Code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// CodeOf returns the code of err; any error that is not an *Error has code CodeIO.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeIO
}

// ---------- data ----------

// BufferInfo describes one document.
type BufferInfo struct {
	ID                                 BufferID
	Path, Name, Language               string // Path is "" for untitled and virtual buffers
	Encoding, LineEnding, Mode         string
	Modified, ReadOnly, Virtual, Large bool
	Preview, Active                    bool
	LineCount                          int     // -1 while the line index of a large file is incomplete
	Version                            Version // content state; compare for equality only
}

// Caret is one cursor and its selection anchor.
type Caret struct {
	Pos    Pos
	Anchor *Pos // nil when there is no selection
}

// CursorMode says where the carets go after an edit.
type CursorMode uint8

// The cursor modes.
const (
	CursorMap  CursorMode = iota // carets map through the edits (default)
	CursorKeep                   // carets keep their positions, clamped
	CursorEnd                    // primary caret goes to the end of the last edit
)

// EditOptions tune BufferEditor.Edit.
type EditOptions struct {
	Cursor CursorMode
	// Expect is optional: nil means no check. A non-nil value that differs from the
	// document's Version fails with CodeStale.
	Expect *Version
}

// SearchQuery is a text search in one buffer.
type SearchQuery struct {
	Text                            string
	Regex, CaseSensitive, WholeWord bool
	Max                             int    // <= 0: 1000
	Within                          *Range // nil: whole buffer
}

// SearchResult is what BufferEditor.Search found.
type SearchResult struct {
	Ranges    []Range
	Truncated bool // Max reached (the scan stopped early)
}

// FindLinesQuery scans whole lines (a pattern never matches across lines) and
// returns the 0-based numbers of matching lines. It is time-boxed so a plugin can
// chunk a big file.
type FindLinesQuery struct {
	Pattern              string
	Regex, CaseSensitive bool
	From, To             int           // line range [From, To); To <= 0: to the end
	Max                  int           // <= 0: 10000
	Budget               time.Duration // <= 0: 20 ms; clamped to 100 ms
}

// FindLinesResult is the answer to a FindLinesQuery.
type FindLinesResult struct {
	Lines []int
	Next  int // first line NOT scanned; -1 when the range was fully scanned
}

// Hunk is one run of changed lines, 0-based starts (diff.Hunk shape).
type Hunk struct {
	Kind                                   string // "insert", "delete", "change"
	OldStart, OldCount, NewStart, NewCount int
}

// ChangeRec is one buffer edit in byte columns. Range is expressed in the
// coordinates of the document IMMEDIATELY BEFORE this record; records are applied in
// order (record k+1 is relative to the text after record k). A Replace is two
// records: a delete, then an insert at its From. Changes.Edits therefore transforms
// an old mirror of the text into the new text when applied first to last.
type ChangeRec struct {
	Range   Range  // replaced range; From == To for a pure insert
	Text    string // inserted text
	Removed string // removed text, "" when larger than 64 KiB or in large mode
}

// Changes is what a buffer did since the consumer's last TakeChanges.
type Changes struct {
	Version  Version
	Edits    []ChangeRec
	Undo     bool // any edit came from undo/redo
	Overflow bool // too many or too large records, or a coarse (large-mode, buffer replaced) edit: Edits is nil
}

// Color is a theme key or a literal RGB value.
type Color struct {
	Key     string // theme key "section.key" or a semantic name; resolved at paint time
	R, G, B uint8
	Set     bool
}

// Style is a decoration style.
type Style struct {
	FG, BG                                           Color
	Bold, Italic, Underline, Strike, Reverse, Extend bool // Extend: background runs to the end of the row
}

// DecorKind is the kind of one decoration.
type DecorKind uint8

// The decoration kinds.
const (
	DecorHighlight DecorKind = iota // Range, Style
	DecorLine                       // Line, Style (whole row)
	DecorEOL                        // Line, Text, Style (virtual text after the last cell)
	DecorGutter                     // Line, Text (1 cell), Style (replaces the change-bar cell)
	DecorScroll                     // Line or Range (lines), Color (scrollbar marker)
	DecorReplace                    // Pos, Text, Style (draws over the full width of the cell at Pos)
)

// Decoration is one item of a decoration namespace. Columns are byte columns.
type Decoration struct {
	Kind     DecorKind
	Range    Range
	Line     int
	Pos      Pos
	Text     string
	Style    Style
	Color    Color
	Priority int
}

// Key is one decoded key press.
type Key struct {
	Key                     string // canonical chord name of the key alone, e.g. "a", "enter", "f5"
	Text                    string // printable text or ""
	Ctrl, Alt, Shift, Super bool
}

// ---------- commands, keys, modes, status ----------

// CommandSource says what started a command.
type CommandSource string

// The command sources.
const (
	SrcPalette CommandSource = "palette"
	SrcKey     CommandSource = "key"
	SrcMenu    CommandSource = "menu"
	SrcAPI     CommandSource = "api"
	SrcMode    CommandSource = "mode"
)

// CommandInfo is what the host knows about a plugin command. The function stays in
// the runtime.
type CommandInfo struct {
	ID, Title, Description, Plugin string
	When                           []string
	Hidden, Record                 bool
}

// Binding binds a chord to an action.
type Binding struct {
	Chord  string // keymap canonical spelling; space-separated chords form a sequence
	Action string
	Plugin string
	Args   map[string]any
	When   []string
}

// TextPolicy says what an input mode does with printable keys it does not bind, and
// (as ModeInfo.Paste) with pasted text.
type TextPolicy uint8

// The text policies. The zero value is TextPass.
const (
	TextPass    TextPolicy = iota // unbound printable keys are typed; paste inserts as usual
	TextSwallow                   // unbound printable keys, or a paste, are dropped
	TextHandler                   // unbound printable keys, or a paste, call TextAction with args{text=} (paste adds paste=true)
)

// ModeInfo describes an input mode a plugin defined.
type ModeInfo struct {
	Name, Plugin string
	Bindings     map[string]string // canonical chord -> action id (functions are registered as hidden commands)
	Args         map[string]map[string]any
	Text         TextPolicy
	Paste        TextPolicy // zero (TextPass): paste inserts as usual
	TextAction   string
	Fallthrough  bool   // unbound non-text keys go to the base keymap
	Status       string // shown as a status item while the mode is active, e.g. "-- NORMAL --"
}

// StatusItem is one plugin item of the status row.
type StatusItem struct {
	ID, Plugin, Text string
	Right            bool
	Priority         int
	Clickable        bool
}

// MenuItem is one plugin-contributed row of a menu bar dropdown. Menu matches a menu's
// stable id ("View", "File", ...) first and its label second. Action is the action or plugin
// command the row runs; Checkbox names a context key whose boolean state draws the row's
// checkmark; When gates the row; After and Before name a neighbouring row (its action or its
// label) to position this one; a neighbour that matches nothing appends the row.
type MenuItem struct {
	Menu, Label, Action string
	Checkbox, When      string
	After, Before       string
}

// ---------- ui specs ----------

// Level is the severity of a notice.
type Level string

// The levels.
const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// InputSpec describes a one-line prompt.
type InputSpec struct {
	Prompt, Default string
	SelectAll       bool
	// History holds earlier answers, oldest first; Up and Down in the prompt walk them.
	History []string
	// Validate and OnChange are closures made by the runtime. The ui calls them on the UI goroutine
	// from Update: Validate runs Lua synchronously (25 ms, no yield) and a non-nil error keeps the
	// prompt open (prompt.LineOptions.Validate); OnChange only enqueues a callback task, which the
	// ui then Drains. Either may be nil.
	Validate func(text string) error
	OnChange func(text string)
}

// ConfirmSpec describes a yes/no prompt.
type ConfirmSpec struct {
	Message, Yes, No string
	Default          bool
}

// PickItem is one row of a picker or choice popup.
type PickItem struct {
	ID                       int // index into the plugin's item table; the runtime owns the data
	Text, Detail, Keybinding string
	Disabled                 bool
}

// PickerSpec describes a picker.
type PickerSpec struct {
	Title, Footer, Placeholder, InitialQuery string
	Items                                    []PickItem
	Fuzzy                                    bool // false: items are shown as given
	Live                                     bool // query changes are reported through Resolve (Completion.Kind "query")
	Preview                                  bool // item highlights are reported (Kind "move"; the value is -1 when no row is shown)
}

// PickerUpdate changes an open picker; nil fields stay as they are.
type PickerUpdate struct {
	Title, Footer, Status *string
	Items                 *[]PickItem
	Selected              *int
}

// ActionPopupAction is one button of an action popup.
type ActionPopupAction struct {
	ID, Label string
}

// ActionPopupSpec describes a message with action buttons.
type ActionPopupSpec struct {
	Title, Message string
	Actions        []ActionPopupAction
}

// PromptSuggestion is one row of a prompt's suggestion list.
type PromptSuggestion struct {
	ID, Text, Description, Value string
	Disabled                     bool
}

// PopupSpec describes a popup.
type PopupSpec struct {
	Title  string
	Lines  []string
	Items  []PickItem // non-empty: a choice popup
	Anchor string     // "cursor" | "center"
	Width  int
}

// ThemeInfo names the active theme.
type ThemeInfo struct {
	Name string
	Dark bool
}

// ThemeFileInfo is one theme the editor offers, for the theme-file APIs.
type ThemeFileInfo struct {
	Name    string
	Ref     string
	Builtin bool
}

// ThemeKeyInfo is one color key of the theme model, for the theme editor's schema.
type ThemeKeyInfo struct {
	Name     string // "section.key"
	Kind     string // "required" | "fallback" | "optional"
	Fallback string
}

// ThemeSchema is the theme model as a plugin sees it: the key table, the attribute-only
// keys and the named colors. It is the gvim equivalent of Fresh's JSON theme schema.
type ThemeSchema struct {
	Keys         []ThemeKeyInfo
	ModifierOnly []string
	NamedColors  []string
}

// DiagnosticInfo is one LSP diagnostic, with byte columns.
type DiagnosticInfo struct {
	Range    Range
	Severity string // "error" | "warning" | "info" | "hint"
	Message  string
	Source   string
}

// FileDiagnostics is the diagnostics of one open document, for AllDiagnostics.
type FileDiagnostics struct {
	Path        string
	Diagnostics []DiagnosticInfo
}

// ---------- windows, virtual buffers (stages S17, S18) ----------

// WindowInfo describes one split.
type WindowInfo struct {
	ID                  WinID
	Buffer              BufferID
	X, Y, Width, Height int
	TopLine, LeftCol    int
	Active              bool
}

// VisibleRow is one painted row of a window.
type VisibleRow struct {
	Line           int  // buffer line
	ColFrom, ColTo int  // byte columns of that line drawn in this row, [ColFrom, ColTo)
	Wrapped        bool // continuation row of a soft-wrapped line
}

// SplitOptions describe a new split.
type SplitOptions struct {
	Dir    string // "right" | "below"
	Buffer BufferID
	Ratio  float64 // 0 < r < 1, size of the new window
	Before bool
	Focus  bool
}

// CreateBuffer describes a virtual buffer. Plugin names the plugin that makes it: the ui
// closes the virtual buffers of a plugin when the plugin is unloaded (OwnerGone).
type CreateBuffer struct {
	Plugin                     string
	Name, Text, Language, Mode string
	ReadOnly, Focus            bool
}

// Segment is a styled run inside a ContentEntry.
type Segment struct {
	Text  string
	Style Style
}

// ContentEntry is one entry of a virtual buffer's content.
type ContentEntry struct {
	Text     string // concatenated verbatim, entries carry their own "\n"
	Style    Style
	Segments []Segment
	Props    map[string]any
}

// OpenRequest asks the host to open a file.
type OpenRequest struct {
	Path                       string
	Pos                        *Pos
	Focus, Preview, Background bool
}

// Step is one action of Do/DoAll.
type Step struct {
	Action string
	Args   map[string]any
	Count  int // <= 0: 1
}
