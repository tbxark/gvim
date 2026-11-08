package document

// Values of Options.VirtualSpace, as the config key editor.virtual_space writes them.
const (
	VirtualSpaceOff   = "off"
	VirtualSpaceBlock = "block"
	VirtualSpaceOn    = "on"
)

// LanguageSettings are the effective editing and display settings of one document:
// the global config options with the override of the document's language applied
// (config.Config.EditorFor). The app builds them; the editor does not read config.
//
// The editor consumes TabSize, UseTabs, AutoIndent, AutoClose, AutoSurround and
// CommentPrefix; the other fields are not consumed yet. SettingsFor says which layer
// wins when a document also has a per-buffer override in ViewSettings.
type LanguageSettings struct {
	// Language is the language id the settings were resolved for, such as "go" or
	// "text". It is lowercase and may be empty.
	Language string

	TabSize      int  // columns per tab stop
	UseTabs      bool // Tab inserts a tab character
	AutoIndent   bool // insert_newline indents the new line
	AutoClose    bool // typing an opener inserts its closer
	AutoSurround bool // typing an opener over a selection wraps the selection

	LineWrap   bool
	WrapColumn int // text column at which lines wrap; 0 means the viewport width
	PageView   bool
	PageWidth  int // page view width; 0 means the viewport width

	// WhitespaceTabs* say whether tab marks show at the start, inside and at the end of
	// a line once whitespace display is on. They are all false for a language that sets
	// show_whitespace_tabs to false, such as Go.
	WhitespaceTabsLeading  bool
	WhitespaceTabsInner    bool
	WhitespaceTabsTrailing bool

	// IndentationGuide is false when indentation guides are suppressed for this
	// document whatever the global mode is. Plain text defaults to false.
	IndentationGuide bool

	CommentPrefix  string // line comment prefix; empty means the editor's own table
	WordCharacters string // extra word characters; empty means letters, digits and "_"

	FormatOnSave bool
	Formatter    *FormatterSpec
	OnSave       []OnSaveAction
	Indent       *IndentRules
}

// FormatterSpec is languages.<id>.formatter.
type FormatterSpec struct {
	Command   string
	Args      []string
	Stdin     bool
	TimeoutMS int
}

// OnSaveAction is one entry of languages.<id>.on_save.
type OnSaveAction struct {
	Command    string
	Args       []string
	WorkingDir string
	Stdin      bool
	TimeoutMS  int
	Enabled    bool
}

// IndentRules is languages.<id>.indent: regular expressions for the indentation of the
// next line. An empty pattern is unset.
type IndentRules struct {
	IncreaseIndentPattern string
	DecreaseIndentPattern string
	IndentNextLinePattern string
	DedentNextLinePattern string
	SelfClosePattern      string
}

// SettingsResolver returns the settings for a document with the given language name
// (Document.Language, which may be empty) and file path (which may be empty).
type SettingsResolver func(language, path string) LanguageSettings

// Settings are the editor options one document reads while it edits and saves.
// The editor builds them from its Options and refreshes a document's settings
// when the options or the language settings change, so the document side never
// reads the session state. See editor.Options.
type Settings struct {
	// TrimTrailingWhitespace strips trailing spaces and tabs from every line on
	// save. The buffer is changed to match, as one undo step.
	TrimTrailingWhitespace bool
	// EnsureFinalNewline ends the saved text with a line break, and the buffer
	// gets it too. An empty text stays empty.
	EnsureFinalNewline bool
	// UseTabs makes Tab insert a literal tab; TabSize is the columns per tab
	// stop and the width of one indent unit in spaces.
	UseTabs    bool
	TabSize    int
	AutoIndent bool
	// AutoClose and AutoSurround are the typing assists; they can be switched
	// off per language, and the editor resolves which wins.
	AutoClose    bool
	AutoSurround bool
	// DefaultLineEnding is the line ending for a document with no file to read
	// it from: "LF" or "CRLF".
	DefaultLineEnding string
	// AutoReadOnly opens files that are not writable, or that sit in a library
	// folder, as read-only.
	AutoReadOnly bool
	// SnapshotInterval is the edits between undo snapshots.
	SnapshotInterval int
	// LargeFileThresholdBytes is the file size from which a file opens in large
	// file mode; LargeFileMode is the switch that enables the mode.
	LargeFileThresholdBytes int
	EstimatedLineLength     int
	LargeFileMode           bool
}
