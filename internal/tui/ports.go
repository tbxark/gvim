package tui

import (
	"context"
	"time"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// The interfaces in this file are the ones ui needs, written where ui uses
// them. Concrete domain types satisfy them through the adapters in
// internal/app, and test fakes implement them directly. ui does not import
// the concrete finder, fsops, lsp or markdown packages.

// EditorState is what the root model needs from the editor. The app adapter
// wraps *editor.Editor and maps editor.Tab to TabInfo.
//
// Do runs a named action and may change the text. The other methods move the
// caret or the viewport and never change the text, so they do not go through
// Do. None of them scrolls the viewport to the caret, except through the
// editor's own rules.
type EditorState interface {
	SetViewport(rows, cols int)
	Do(a editor.Action, args map[string]any) editor.Result
	Active() Document // the active document, or nil when no tab is open (the welcome screen shows)
	Tabs() []TabInfo
	Status() string
	SetCursor(p buffer.Pos, extend bool)
	ExtendSelection(p buffer.Pos, u editor.Unit)
	ScrollBy(rows int)
	ScrollTo(top int)
}

// VirtualSpaceEditor is the optional part of EditorState that puts the caret past
// the end of a line and reports whether the setting lets it. The app adapter
// implements it; an editor without it keeps every caret on the text.
type VirtualSpaceEditor interface {
	// VirtualSpaceEnabled reports whether the active document may put its caret
	// past the end of a line (editor.virtual_space on, the per-buffer switch, and
	// line wrap off).
	VirtualSpaceEnabled() bool
	// SetVirtualColumns parks the caret n display columns past the end of its
	// line. It is 0 when the editor's mode does not allow the position.
	SetVirtualColumns(n int)
}

// PluginEditor is the port of the editor that plugins use: documents by id, edits in API
// coordinates, tabs, change tracking. The app builds it over the editor and passes it as
// RootPorts.PluginEditor. It cannot be an optional method set of EditorState: both have a
// SetCursor, with different parameters, so one type cannot implement both. Without it a
// plugin's buffer calls answer unsupported.
type PluginEditor = pluginapi.BufferEditor

// ShellRunner is the optional view of the editor that lets the root run a shell
// command off the update loop. shell_command and shell_command_replace go through it
// when the editor has it: BeginShell prepares the job, a tea.Cmd runs it with
// editor.RunShellJob, and EndShell applies the output when the result comes back. An
// editor without it runs the command in Do, which blocks until the command ends. The
// app adapter implements it.
type ShellRunner interface {
	BeginShell(a editor.Action, args map[string]any) (editor.ShellJob, error)
	EndShell(job editor.ShellJob, out string, runErr error) editor.Result
}

// formatterEditor is the formatter surface of the editor, an optional port that the root
// asserts on EditorState, as it does fileEditor. The app adapter implements it. HasFormatter
// reports whether the active buffer has a formatter, and it answers the Format Buffer row's
// when-clause. An editor without it has no formatter, so the row stays disabled.
type formatterEditor interface {
	HasFormatter() bool
}

// rootEditor is the editor's workspace root, an optional port that the root asserts on
// EditorState. The root calls SetRoot on its update loop when a project opens, so the editor
// measures relative paths from the folder that is open now. The app adapter implements it.
type rootEditor interface {
	SetRoot(root string)
}

// Document is the view of the active buffer that the root and the text pane
// read. It is the pane's view (editorpane.Document) plus the name and the first
// visible line. The app adapter passes the editor's document through.
type Document interface {
	editorpane.Document
	Name() string
	Top() int // the first visible line of the viewport
	// Encoding is the encoding label of the saved text, "ASCII" or "UTF-8".
	Encoding() string
	// LineEnding is "LF" or "CRLF", the ending of the saved text.
	LineEnding() string
}

// LargeDoc is the optional view of a Document in large file mode, as *editor.Document
// has it and the app adapter forwards it. The root asserts it on the active document.
// Large reports whether the document is in that mode, and Index how much of its line
// index exists. A document without it is not large.
type LargeDoc interface {
	Large() bool
	Index() buffer.IndexInfo
}

// ExplorerTree is the subset of *explorer.Tree used by the explorer panel. The
// root passes a nil ExplorerTree when the explorer is hidden.
type ExplorerTree interface {
	Root() string
	Rows() []explorer.Row
	Selected() (explorer.Row, bool)
	Move(delta int)
	Toggle(path string) error
	Reveal(path string) error
	Reload() error
	ScrollBy(n int)
	ScrollOffset() int
	ShowHidden() bool
	SetShowHidden(v bool)
	ShowIgnored() bool
	SetShowIgnored(v bool)
}

// FileIndex is the async file list from finder. Nil leaves the palette's
// file mode on its loading row.
type FileIndex interface {
	Snapshot() (files []string, loading bool)
	Done() <-chan struct{}
	Close()
}

// FileRanker scores paths for the palette and the browser filter. The fuzzy
// scorer provides it (planned, see fuzzy). Nil leaves the palette's file rows
// unranked.
type FileRanker interface {
	Rank(query string, files []string, limit int) []Ranked
}

// Ranked is one scored path. Indexes are the matched rune offsets, used for
// highlighting.
type Ranked struct {
	Path    string
	Indexes []int
}

// FileOps is the file-operation surface (fsops, planned). Nil until P6.
type FileOps interface {
	CreateFile(ctx context.Context, dir, name string) (string, error)
	Rename(ctx context.Context, path, newName string) (string, error)
	Trash(ctx context.Context, path string) error
}

// LSPSession is the language-server port of the root. The app adapter wraps the
// lsp.Manager of each workspace root. Positions and ranges are rune positions
// (buffer.Pos), and the adapter converts them to and from the UTF-16 positions of
// the protocol. Every call may wait on a server, so the root calls them only from
// a tea.Cmd, never from Update. A nil port disables the language-server features.
type LSPSession interface {
	// SetRoot names the workspace root. Servers are kept per root.
	SetRoot(root string)
	// Supports reports whether a server is configured for languageID.
	Supports(languageID string) bool
	// Open sends the full text of the document at path, starting the server for
	// languageID when it is not running.
	Open(path, languageID, text string) error
	// Change sends the full new text of an open document.
	Change(path, text string) error
	// Close sends the close of the document at path.
	Close(path string) error
	// Diagnostics returns the latest diagnostics of the document at path. ok is
	// false until the server has published for it.
	Diagnostics(path string) (DiagnosticsEvent, bool)
	// Definition, Implementation and References return the places the symbol at
	// at refers to.
	Definition(path string, at buffer.Pos) ([]LSPLocation, error)
	Implementation(path string, at buffer.Pos) ([]LSPLocation, error)
	References(path string, at buffer.Pos) ([]LSPLocation, error)
	// Hover returns the hover text at at, or "" when there is none.
	Hover(path string, at buffer.Pos) (string, error)
	// Completion returns the completion items at at.
	Completion(path string, at buffer.Pos) ([]CompletionItem, error)
	// Rename renames the symbol at at to name and returns the edits per file.
	Rename(path string, at buffer.Pos, name string) ([]FileEdit, error)
	// CodeActions returns the titles of the code actions at at.
	CodeActions(path string, at buffer.Pos) ([]string, error)
	// Request sends the request method with params to the server that has the document at
	// path open, and returns its decoded JSON result (nil for a null result). Params and
	// the result are in the protocol's own form, UTF-16 columns included. The error says
	// what failed through pluginapi codes: a document that is not open is CodeNotFound, a
	// request that outlived its time limit is CodeTimeout.
	Request(path, method string, params any) (any, error)
	// Restart shuts down the servers of the current root. The root reopens its
	// documents after it.
	Restart() error
	// Stop shuts down every server.
	Stop() error
}

// DiagnosticsEvent is the diagnostics of one document. An empty Items list clears
// them.
type DiagnosticsEvent struct {
	Path  string
	Items []Diagnostic
}

// Diagnostic is one problem in a document. Start and End are its rune range, and
// Severity uses the LSP numbers, from editorpane.SeverityError to SeverityHint.
type Diagnostic struct {
	Start, End buffer.Pos
	Severity   int
	Message    string
	Source     string // the tool that found it ("gopls"), or empty
}

// LSPLocation is a place in a document: the path and its rune range.
type LSPLocation struct {
	Path       string
	Start, End buffer.Pos
}

// CompletionItem is one completion candidate. Insert is the text to insert, and
// Range is the document range it replaces, or nil when the server gave none and
// the text goes at the caret.
type CompletionItem struct {
	Label, Detail string
	Insert        string
	Range         *LSPRange
}

// LSPRange is a rune range in a document.
type LSPRange struct {
	Start, End buffer.Pos
}

// FileEdit is the text edits a rename makes in one document.
type FileEdit struct {
	Path  string
	Edits []editor.LSPEdit
}

// MarkdownRenderer renders a markdown preview at a fixed width. Nil until a
// feature needs it (P8).
type MarkdownRenderer interface {
	Render(ctx context.Context, src string, width int) (string, error)
}

// Overlay is a layer drawn over the frame and given the input while it is on
// top: the palette, the find bar, the prompts, the browser, the dialogs, the
// lists and the menus. The contract, and the routing and painting rules, are in
// package overlay; the alias lets the root and its ports use one type.
type Overlay = overlay.Overlay

// Caret is the optional part of an Overlay that owns the terminal caret. The
// root asserts it on the top overlay.
type Caret = overlay.Caret

// KeyPress is one key press in the keymap vocabulary: the key name ("p", "Left",
// "F10", " ") and the modifiers, a subset of "ctrl", "alt", "shift" and "super". The
// keymap adapter canonicalizes it.
type KeyPress struct {
	Name string
	Mods []string
}

// KeyContext names the UI area a key press lands in, as a keymap when-clause.
type KeyContext string

// The contexts the root model resolves keys in.
const (
	KeyContextNormal       KeyContext = "normal"
	KeyContextFileExplorer KeyContext = "file_explorer"
)

// Command is a keymap binding resolved to an action and its arguments.
type Command struct {
	Action string
	Args   map[string]any
}

// Keymap resolves key presses to commands. The app adapter wraps
// *keymap.Map. The root model never sees keymap types.
type Keymap interface {
	Resolve(key KeyPress, ctx KeyContext) (Command, bool)
}

// SequenceKeymap is the optional part of a Keymap that holds multi-key bindings, such
// as Emacs's C-x C-s. A Keymap without it has single-key bindings only.
type SequenceKeymap interface {
	// ResolveSequence looks up the key presses typed in order. ok reports that steps
	// are a whole binding, which has at least two steps. prefix reports that steps
	// begin a longer binding, so one more key may complete it. A single press is
	// never a binding here, only a prefix.
	ResolveSequence(steps []KeyPress, ctx KeyContext) (cmd Command, ok, prefix bool)
}

// Dialogs builds the dialogs the file actions open. The app adapter builds them
// from internal/tui/dialog and checks the disk for the Save As prompt. Each one
// answers with a dialog.Result, which the root applies. A nil Dialogs leaves the
// actions that need a dialog with a notice, and nothing is lost.
type Dialogs interface {
	// Unsaved asks what to do with the edits of the tab called name: Save,
	// Discard or Cancel. The dialog is drawn with the overlay styles ov.
	Unsaved(name string, tag any, ov style.Overlay) Overlay
	// SaveAs asks for a path, prefilled with path. Save sends the path in Text.
	// The dialog asks before it replaces an existing file, and Replace comes back
	// when the user confirms.
	SaveAs(path string, tag any, ov style.Overlay) Overlay
}

// MarkdownTOC is the Contents sidebar section's configuration: whether the
// bundled markdown_toc feature is enabled, and its settings. The settings are
// the plugins.markdown_toc.settings keys; sidebar.ParseSettings reads them.
type MarkdownTOC struct {
	Enabled  bool
	Settings sidebar.Settings
}

// DefaultMarkdownTOC is the configuration for a config with no
// plugins.markdown_toc entry: enabled, with Fresh's defaults.
func DefaultMarkdownTOC() MarkdownTOC {
	return MarkdownTOC{Enabled: true, Settings: sidebar.DefaultSettings()}
}

// Deps is the constructor input. Editor, Keys and Theme are required. Explorer
// is nil when the explorer is hidden. The other optional fields are nil until
// the phase that wires them, and a nil field disables its feature.
type Deps struct {
	Editor   EditorState
	Explorer ExplorerTree // nil when the explorer is hidden
	// ShowExplorer shows the explorer at startup, with the root expanded. The app
	// sets it when a folder argument is given. It has no effect without Explorer.
	ShowExplorer bool
	// HideMenuBar removes the menu bar row. The zero value shows it, as Fresh does.
	HideMenuBar bool
	// HideLineNumbers removes the line-number gutter. The zero value shows it.
	HideLineNumbers bool
	// TabSize is the columns per tab stop of the editor text. A tab expands to the next
	// stop, and the indent guides, visible whitespace and indentation folds use it too.
	// The zero value means 4.
	TabSize int
	// Notice is a startup message for the status row, such as the first config
	// warning. Empty shows nothing.
	Notice string
	// Warnings are config problems that Show Warnings lists, each as one entry, besides the
	// Notice that the status row shows.
	Warnings []string
	// NoticeSink, when set, is called with the text of each notice the root keeps (the warnings
	// of Show Warnings), for --log-file. It runs on the update goroutine and must not block.
	NoticeSink func(text string)
	// Recents are the recent folders, newest first. The welcome screen lists up to
	// five of them. Nil leaves the list empty.
	Recents []string
	Dialogs Dialogs    // nil: the unsaved and Save As dialogs are not offered
	Files   FileIndex  // nil: the palette's file mode shows its loading row
	Ranker  FileRanker // nil: the palette's file rows are not ranked
	// Commands is the palette's command table, as commands builds it. Nil
	// means commands.Commands(nil): the builtin table with the default keymap's
	// shortcuts.
	Commands []commands.Entry
	Ops      FileOps    // nil until P6
	LSP      LSPSession // nil disables the language-server features
	Markdown MarkdownRenderer
	// MarkdownTOC configures the Contents sidebar section from the
	// plugins.markdown_toc entry of the config. Nil enables it with the
	// defaults, as the bundled plugin is on out of the box.
	MarkdownTOC *MarkdownTOC
	Keys        Keymap
	Theme       theme.Theme
	// Settings is the effective configuration that open_settings lists, and whose
	// user file settings_activate opens. The zero value lists blank settings.
	Settings Settings
	// SwitchKeymap builds the keymap of the built-in preset called name.
	// switch_keybinding_map calls it, and the root uses the keymap it returns for the
	// next key press. Nil disables the switch.
	SwitchKeymap func(name string) (Keymap, error)
	// Clock supplies the press times for double-click detection. Nil means time.Now.
	Clock func() time.Time
	// View holds the starting display settings. Nil means DefaultViewOptions.
	View *ViewOptions
	// Languages resolves the per-language settings (config languages.<id> merged over
	// the global options) of a document from its language name and file path. Nil
	// means no language overrides: the global options apply to every document.
	Languages editor.SettingsResolver
	// Root holds the optional ports of the root features: Switch Project, Live
	// Grep, git markers, the file watcher and the auto-save interval.
	Root RootPorts
	// Trust is the workspace-trust state and the recorded decisions. Nil means
	// trust is unavailable: the Workspace Trust… command says so, and the status
	// row paints no trust pill.
	Trust TrustPort
	// SettingsPort is the Settings page's link to the config files. Nil leaves the
	// Settings list read-only (it opens the config file in a tab).
	SettingsPort SettingsPort
}

// SettingsPort is the Settings UI's link to the config files. internal/app
// implements it over config.Load/config.PatchFile and the editor's live setters;
// a test implements it directly. The page edits plain values only, so the TUI
// never sees the config schema or a config type.
//
// A nil port leaves the Settings list read-only: the old list that opens the
// config file in a tab.
type SettingsPort interface {
	// Snapshot returns the settings the page lists, with their current values
	// and the layers that may be written. It reads no files: the app builds it
	// from the config snapshot it last loaded.
	Snapshot() SettingsSnapshot
	// Validate reports why value cannot be used for the setting key. It is the
	// check the page runs while the user types, before Save.
	Validate(key, value string) error
	// Save writes edits to the layer called layer ("user" or "project") and
	// reloads the config, returning what the running editor should apply. It
	// writes the config file, so the root calls it from a tea.Cmd.
	Save(edits []SettingEdit, layer string) (SettingsChange, error)
	// Reload re-reads the config files without writing and returns what the
	// running editor should apply. It reads the files, so the root calls it from
	// a tea.Cmd. Save uses the same reload internally.
	Reload() (SettingsChange, error)
	// Apply pushes the editor-side live values of change (the language settings,
	// formatters, rulers and ephemeral patterns) into the editor. The root calls
	// it on the update loop, because the editor is owned by that goroutine;
	// Save and Reload only read and write files.
	Apply(change SettingsChange)
}

// The config layers the Settings page can write.
const (
	SettingsLayerUser    = "user"
	SettingsLayerProject = "project"
)

// SettingsItem is one row of the Settings page: a scalar setting with its
// current value and the layers it may be written in.
type SettingsItem struct {
	Key     string   // dotted config path, for example "editor.tab_size"
	Name    string   // the last path element, the row's label
	Section string   // the group the row is listed in
	Kind    string   // "bool", "int", "enum" or "string"
	Value   string   // the current value, as the row shows it ("on"/"off" for a bool)
	Choices []string // the values of an enum
	Min     int      // the bounds of an int; Max 0 means no upper bound
	Max     int
	Apply   string   // "live" when a saved value applies at once, "restart" when it needs a restart
	Layers  []string // the layers the setting may be written in: "user", maybe "project"
}

// SettingsSection is one group of the Settings page, such as Display.
type SettingsSection struct {
	Name  string
	Items []SettingsItem
}

// SettingsSnapshot is the page's whole content: the settings by section and the
// layer targets that are available.
type SettingsSnapshot struct {
	Sections        []SettingsSection
	UserFile        string // the user config file, or "" when it is unknown
	ProjectFile     string // the project config file the page would write, or "" when there is no root
	ProjectWritable bool   // the workspace is trusted, so the project layer may be written
}

// SettingEdit is one changed row the page saves: the setting key and the value
// the user set, in the row's display form.
type SettingEdit struct {
	Key   string
	Value string
}

// SettingsChange is the result of a Save or a Reload: the page's new content and
// what the running editor should apply now.
type SettingsChange struct {
	Snapshot SettingsSnapshot
	// Settings is the effective configuration view after the change, the same
	// data the old Settings list shows.
	Settings Settings
	// View is the effective display options after the change. The root applies
	// only the keys named in Live.
	View ViewOptions
	// Theme is the effective theme name, when theme is in Live.
	Theme string
	// Keymap is the name of the keymap to switch to, when keymap is in Live.
	Keymap string
	// UIIconsMode is the resolved icon set after the change ("plain" or "nerd"),
	// and IconsWhy is how it was resolved; both are empty when unchanged.
	UIIconsMode string
	IconsWhy    string
	// Live are the setting keys whose new value applies to the running editor
	// now. Restart are the changed keys read only at startup; the root shows a
	// restart notice for them.
	Live    []string
	Restart []string
	// Layer is the config layer a Save wrote ("user" or "project"), or "" for a
	// Reload. The root uses it in the save notice.
	Layer string
	// Warnings are problems or limits worth a status-row notice: a config
	// diagnostic after the reload, a layer that could not be written, a command
	// line flag that wins over the file.
	Warnings []string
}
