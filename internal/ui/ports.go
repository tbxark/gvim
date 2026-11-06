package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/explorer"
	palmodel "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/editorpane"
	"github.com/tbxark/gvim/internal/ui/grid"
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

// TabInfo describes one tab of the tab strip. Path is the file the tab holds, or ""
// for an untitled tab. The root uses it to watch the folders of the open tabs and to
// reload the tabs whose files change.
type TabInfo struct {
	Name     string
	Path     string
	Modified bool
	Preview  bool
	Active   bool
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

// FileIndex is the async file list from internal/finder. Nil leaves the palette's
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
// top: the palette, the find bar, the prompts, the browser and the menu
// dropdowns. Sub-packages implement it. The root keeps a stack of overlays.
//
// Routing. A key or mouse message goes to the top overlay first, and the root
// does not handle it further. Other messages, such as the palette's file ranking
// results, also go to the top overlay, and the root handles them as usual. Esc
// is not forwarded: it pops the top overlay, so every overlay is cancelled by
// Esc. After an Update, the root replaces the top overlay with the one Update
// returned, or pops it when Update returns nil. It also pops any overlay that
// reports Active false, so an overlay that is done can say so from Update or
// from its state.
//
// Painting. Every active overlay is drawn after the frame, bottom of the stack
// first. An overlay covers only the cells it draws. The cells it does not draw
// keep the frame under them.
type Overlay interface {
	// Update handles one key or mouse message. It returns the overlay that
	// takes this one's place (the same overlay to keep it, nil to close it), and
	// the command the message starts.
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	// Draw paints the overlay over the frame in g. The grid's size is the frame
	// size.
	Draw(g *grid.Grid)
	// Active reports whether the overlay is still open.
	Active() bool
}

// KeyPress is one key press in the keymap vocabulary: the key name ("p", "Left",
// "F10", " ") and the modifiers, a subset of "ctrl", "alt" and "shift". The
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

// Dialogs builds the dialogs the file actions open. The app adapter builds them
// from internal/ui/dialog and checks the disk for the Save As prompt. Each one
// answers with a dialog.Result, which the root applies. A nil Dialogs leaves the
// actions that need a dialog with a notice, and nothing is lost.
type Dialogs interface {
	// Unsaved asks what to do with the edits of the tab called name: Save,
	// Discard or Cancel.
	Unsaved(name string, tag any) Overlay
	// SaveAs asks for a path, prefilled with path. Save sends the path in Text.
	// The dialog asks before it replaces an existing file, and Replace comes back
	// when the user confirms.
	SaveAs(path string, tag any) Overlay
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
	// Recents are the recent folders, newest first. The welcome screen lists up to
	// five of them. Nil leaves the list empty.
	Recents []string
	Dialogs Dialogs    // nil: the unsaved and Save As dialogs are not offered
	Files   FileIndex  // nil: the palette's file mode shows its loading row
	Ranker  FileRanker // nil: the palette's file rows are not ranked
	// Commands is the palette's command table, as internal/palette builds it. Nil
	// means palmodel.Commands(nil): the builtin table with the default keymap's
	// shortcuts.
	Commands []palmodel.Entry
	Ops      FileOps    // nil until P6
	LSP      LSPSession // nil disables the language-server features
	Markdown MarkdownRenderer
	Keys     Keymap
	Theme    theme.Theme
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
	// Root holds the optional ports of the root features: Switch Project, Live
	// Grep, git markers, the file watcher and the auto-save interval.
	Root RootPorts
}
