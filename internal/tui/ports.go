package tui

import (
	"context"
	"encoding/json"
	"image/color"
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

// EnvPort is the workspace environment the plugin API sees and controls. The app
// implements it over its env.Provider; activation is host-gated on trust.
type EnvPort interface {
	DetectedEnv() (pluginapi.EnvInfo, bool)
	ActiveEnv() (pluginapi.EnvInfo, bool)
	// CachedEnvStatus is the side-effect-free status-row read: the cached detected and
	// active environment names plus the live trust level. Rendering calls only this
	// method; unlike DetectedEnv it never triggers the watcher-retry recovery, detection,
	// capture or activation (Env: Reload owns that path).
	CachedEnvStatus() (active, detected pluginapi.EnvInfo, hasActive, hasDetected bool, trust string)
	TrustLevel() string
	SetEnv(snippet, dir, kind string) error
	ClearEnv() error
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
	// Supports reports whether a server starts on its own for languageID.
	Supports(languageID string) bool
	// Open sends the full text of the document at path, starting the servers
	// that should run for languageID, and returns the protocol version the
	// document was opened at.
	Open(path, languageID, text string) (int32, error)
	// Change sends the full new text of an open document and returns the new
	// protocol version.
	Change(path, text string) (int32, error)
	// Close sends the close of the document at path.
	Close(path string) error
	// Diagnostics returns the latest diagnostics of the document at path,
	// merged over the servers that published them, or pulled from the servers
	// that serve diagnostics that way. ok is false until a server answers.
	Diagnostics(path string) (DiagnosticsEvent, bool)
	// WorkspaceDiagnostics returns the stored diagnostics of every file any
	// server reported them for, open or not, sorted by path. The diagnostics
	// panel lists them.
	WorkspaceDiagnostics() []FileDiagnostics
	// Progress returns the live servers' unfinished $/progress entries, as text,
	// in server order and capped across the workspace. The diagnostics panel shows
	// them; there is no animation.
	Progress() []LSPProgress
	// InlayHints returns the inlay hints of the document at path.
	InlayHints(path string) ([]InlayHint, error)
	// SemanticTokens returns the semantic tokens of the document's lines
	// [firstLine, lastLine], both zero-based and inclusive.
	SemanticTokens(path string, firstLine, lastLine int) ([]SemanticToken, error)
	// FoldingRanges returns the server's folding ranges for the document, in
	// zero-based inclusive lines.
	FoldingRanges(path string) ([]FoldRange, error)
	// Definition, Implementation and References return the places the symbol at
	// at refers to.
	Definition(path string, at buffer.Pos) ([]LSPLocation, error)
	Implementation(path string, at buffer.Pos) ([]LSPLocation, error)
	References(path string, at buffer.Pos) ([]LSPLocation, error)
	// Hover returns the hover text at at, or "" when there is none.
	Hover(path string, at buffer.Pos) (string, error)
	// Completion returns the completion items at at, merged over the servers
	// that serve completion for the language.
	Completion(path string, at buffer.Pos) ([]CompletionItem, error)
	// Rename renames the symbol at at to name and returns the workspace edit to
	// apply: the text edits per file and any file create, rename or delete.
	Rename(path string, at buffer.Pos, name string) (WorkspaceEdit, error)
	// CodeActions returns the code actions at at, merged over the servers that
	// serve them for the language. The stored diagnostics that overlap the
	// position are sent as context, as servers that offer quick fixes need them.
	CodeActions(path string, at buffer.Pos) ([]CodeAction, error)
	// ResolveCodeAction completes an action that was offered with only its data:
	// the server fills in the edit and/or the command.
	ResolveCodeAction(path string, action CodeAction) (CodeAction, error)
	// ExecuteCommand runs a command of the server that offered it. Some commands
	// make the server ask for an edit, which arrives through WorkspaceEdits.
	ExecuteCommand(path string, command LSPCommand) error
	// WorkspaceEdits drains the edits servers asked to apply with
	// workspace/applyEdit, in arrival order. The root applies them through its
	// multi-file edit helper.
	WorkspaceEdits() []WorkspaceEdit
	// Request sends the request method with params to a server that has the document at
	// path open, and returns its decoded JSON result (nil for a null result). Params and
	// the result are in the protocol's own form, UTF-16 columns included. The error says
	// what failed through pluginapi codes: a document that is not open is CodeNotFound, a
	// request that outlived its time limit is CodeTimeout.
	Request(path, method string, params any) (any, error)
	// StartLanguage starts every configured server of the language, a manual start
	// that overrides auto_start, lsp_enabled and the mute. It may wait on the
	// servers, so call it from a command.
	StartLanguage(language string) error
	// Restart shuts down the servers. The root reopens its documents after it. A
	// language named here also counts as started by hand, so its servers run
	// whatever auto_start says.
	Restart(language string) error
	// Stop shuts down every server.
	Stop() error
	// Servers lists every configured server with its state, for the status popup.
	Servers() []LSPServerStatus
	// RestartServer stops the named server and starts it again.
	RestartServer(language, name string) error
	// StopServer stops the named server.
	StopServer(language, name string) error
	// SetLanguageEnabled mutes or unmutes a language: a muted language starts no
	// server of its own and its running servers are stopped.
	SetLanguageEnabled(language string, enabled bool) error
}

// DiagnosticsEvent is the diagnostics of one document. An empty Items list clears
// them.
type DiagnosticsEvent struct {
	Path  string
	Items []Diagnostic
	// Version is the document version the server published the diagnostics for,
	// and Versioned says whether it gave one. The root dims inline messages whose
	// version is older than the document's current version.
	Version   int32
	Versioned bool
}

// InlayHint is one inlay hint. Col is the insertion point (a rune column of the
// line); the pane inserts the hint cells there display-only (tier 2), and Text
// remains the joined label the end-of-line tier-1 form uses when the pane cannot.
type InlayHint struct {
	Line int
	// Col is the rune column the hint is anchored to, clamped to the line's
	// length. The hint is drawn before the character there, and at the line's end
	// when Col is the length.
	Col  int
	Text string
	// Parts are the protocol label parts the port joined into Text, in order.
	Parts []string
	// PaddingLeft and PaddingRight ask for an extra space on that side of the
	// hint's cells, from the protocol's paddingLeft/paddingRight.
	PaddingLeft, PaddingRight bool
	// Kind is the protocol's InlayHintKind: 1 type, 2 parameter, 0 unknown.
	Kind int
}

// SemanticToken is one semantic token, in rune columns of its line. Type and
// Modifiers are the names from the server's legend.
type SemanticToken struct {
	Line, Start, End int
	Type             string
	Modifiers        []string
}

// FoldRange is one foldable range from a language server, in zero-based lines,
// both ends inclusive. The editor merges these with its indentation folds.
type FoldRange struct {
	StartLine, EndLine int
}

// LSPProgress is one unfinished progress entry a language server reported with
// $/progress. It is shown as text; Percentage is -1 when the server gave none.
type LSPProgress struct {
	Server     string
	Title      string
	Message    string
	Percentage int
}

// FileDiagnostics is the stored diagnostics of one file, for the diagnostics
// panel. Items are in the order the server sent them. Source is "publish" for the
// sets the servers published, and "workspace" for a file that came from a
// workspace/diagnostic pull (usually a file that is not open).
type FileDiagnostics struct {
	Path   string
	Items  []Diagnostic
	Source string
}

// Diagnostic is one problem in a document. Start and End are its rune range, and
// Severity uses the LSP numbers, from editorpane.SeverityError to SeverityHint.
type Diagnostic struct {
	Start, End buffer.Pos
	Severity   int
	Message    string
	Source     string // the tool that found it ("gopls"), or empty
	// Code is the server's diagnostic code (for example "unused"), when it sent
	// one. The plugin diagnostic info keeps its own shape; this is for the native
	// panel and list.
	Code string
}

// LSPLocation is a place in a document: the path and its rune range.
type LSPLocation struct {
	Path       string
	Start, End buffer.Pos
	// ReadOnly opens generated/read-only content (for example a builtin module
	// resolved from an external URI provider).
	ReadOnly bool
}

// CompletionEdit is one extra edit a completion item asks for together with its
// insert, such as the import of an auto-import candidate. Range is in rune
// columns of the document.
type CompletionEdit struct {
	Range LSPRange
	Text  string
}

// CompletionItem is one completion candidate. Insert is the text to insert, and
// Range is the document range it replaces, or nil when the server gave none and
// the text goes at the caret.
type CompletionItem struct {
	Label, Detail string
	Insert        string
	Range         *LSPRange
	// Additional are the edits the server asks for with the insert (auto-import).
	// They are applied in the same transaction as the insert, never afterwards.
	Additional []CompletionEdit
	// Snippet says Insert came from snippet-format text that the port already
	// expanded; CursorOffset is then the final stop. A plain item puts the caret at
	// the end of Insert.
	Snippet bool
	// CursorOffset is where the caret goes inside Insert after the edits: a rune
	// offset from the start of Insert (a snippet's final stop). It is meaningful
	// when Snippet is set; a plain item puts the caret after Insert.
	CursorOffset int
	// ResolveToken names an item whose server fills in lazy fields
	// (completionItem/resolve) before it can be accepted. 0 means the item is
	// complete.
	ResolveToken int
	// rangeSynth says Range was made by the live filter (the server gave none), so a
	// later re-filter/backspace recomputes it for the new word prefix instead of
	// keeping a range that covers text the user has since deleted.
	rangeSynth bool
}

// LSPRange is a rune range in a document.
type LSPRange struct {
	Start, End buffer.Pos
}

// CodeAction is one action a language server offers for a position. The root
// applies it three ways: an Edit is applied, a Command is executed, and an action
// with neither but with data is resolved first (ResolveCodeAction) and then the
// edit and/or command of the resolved action is applied.
type CodeAction struct {
	Title  string
	Kind   string // "" for a bare command
	Source string // the server that offered it, for a merged list
	// Preferred marks the action its server recommends.
	Preferred bool
	// Edit is the workspace edit the action applies, when it has one.
	Edit *WorkspaceEdit
	// Command is the server command the action runs, when it has one. An action
	// applies its edit first and then runs its command.
	Command *LSPCommand
	// NeedsResolve says the action has neither edit nor command yet: it must be
	// resolved before it can be applied.
	NeedsResolve bool
	// Data is the port's opaque handle on the action as the server sent it. The
	// root passes it back unchanged to ResolveCodeAction.
	Data any
}

// LSPCommand is a command a language server executes. Source names the server
// that offered it, so the command runs on that server; Data is the port's opaque
// payload and is passed back unchanged.
type LSPCommand struct {
	Title     string
	Command   string
	Source    string
	Arguments any // the server's argument list, in the protocol's JSON form; opaque to the root
	Data      any // the port's opaque payload, passed back unchanged
}

// WorkspaceEdit is the set of changes a server asks the editor to apply: text
// edits across files and file create, rename and delete operations. The
// operations are in the order the server sent them.
type WorkspaceEdit struct {
	// Label is what the server calls the edit, for the notice that reports it.
	Label string
	Ops   []WorkspaceOp
}

// WorkspaceOpKind is the kind of one workspace operation.
type WorkspaceOpKind int

// The kinds of workspace operation.
const (
	WorkspaceOpText   WorkspaceOpKind = iota // text edits of one document
	WorkspaceOpCreate                        // create a file
	WorkspaceOpRename                        // rename a file: Path to NewPath
	WorkspaceOpDelete                        // delete a file
)

// WorkspaceOp is one change of a WorkspaceEdit.
type WorkspaceOp struct {
	Kind    WorkspaceOpKind
	Path    string
	NewPath string // the target of a rename
	// Edits are the text edits of a text operation, in rune positions.
	Edits []editor.LSPEdit
	// Version is the document version the edits were computed against, when the
	// server gave one. The root rejects the edits when the open document has
	// moved past it.
	Version *int32
	// Overwrite and IgnoreIfExists are the file-operation options of the server.
	// gvim refuses to overwrite (and refuses deletes), as Fresh does.
	Overwrite      bool
	IgnoreIfExists bool
}

// LSPServerRunState is where a configured server stands, for the status popup.
type LSPServerRunState int

// The states of a configured server.
const (
	LSPServerStopped  LSPServerRunState = iota // not running
	LSPServerStarting                          // a start is in flight
	LSPServerRunning                           // live
	LSPServerFailed                            // the last start failed
)

// LSPServerStatus is one configured server as the status popup shows it.
type LSPServerStatus struct {
	Language  string // "" for a universal server
	Name      string
	Command   string
	Install   string
	Universal bool
	AutoStart bool
	State     LSPServerRunState
	Error     string // why the last start failed, when State is LSPServerFailed
	// Limits is what happened to the server's configured process_limits: the methods
	// that applied, or why none could on this platform. Empty when none are configured.
	Limits string
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
	KeyContextGlobal       KeyContext = "global"
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

// ExactKeymap is the optional part of a Keymap that resolves in exactly one context
// without the global fallback. Surfaces with their own context precedence (the dock,
// the remote-menu action) use it so a context binding or unbind is not shadowed by a
// global one.
type ExactKeymap interface {
	ResolveExact(key KeyPress, ctx KeyContext) (Command, bool)
}

// ExactUnbindKeymap is the exact-context form of UnbindKeymap: it reports a user's
// explicit unbind in ctx only, with no global fallback.
type ExactUnbindKeymap interface {
	UnboundExact(key KeyPress, ctx KeyContext) bool
}

// UnbindKeymap is the optional part of a Keymap that remembers a user's explicit unbind:
// a key the user disabled in a context must do nothing there, including the built-in
// fallback an overlay would otherwise run, rather than being a free key again.
type UnbindKeymap interface {
	Unbound(key KeyPress, ctx KeyContext) bool
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
	Editor EditorState
	// Env is the workspace environment provider (detection, activation, trust
	// level). Nil disables the gvim.env API's detection and activation.
	Env      EnvPort
	Explorer ExplorerTree // nil when the explorer is hidden
	// ShowExplorer shows the explorer at startup, with the root expanded. The app
	// sets it when a folder argument is given. It has no effect without Explorer.
	ShowExplorer bool
	// Sidebar is the restored session's sidebar UI state (sections open/collapsed,
	// rows, width form/side, scroll). The app sets it only when it restores the
	// saved layout (not for a hot-exit restore or a file launch); nil keeps the
	// config defaults.
	Sidebar *SidebarState
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
	// Updates is the app release check painted by the status row element {update};
	// the zero value disables it.
	Updates UpdateHooks
	// StartupOpen is a path opened by the root's first command, off the update loop.
	// A remote launch sets the remote file here: its load goes through the remote file
	// access (an editor with a non-local FileAccess reports itself as needing the
	// asynchronous large-file path), so startup never blocks on remote I/O, a failed or
	// disconnected read is a retryable notice rather than an empty buffer, and the
	// open is retried after the authority reconnects.
	StartupOpen string
	// StartupLine and StartupCol are the optional 1-based startup position.
	StartupLine, StartupCol int
	// Warnings are config problems that Show Warnings lists, each as one entry, besides the
	// Notice that the status row shows.
	Warnings []string
	// NoticeSink, when set, is called with the text of each notice the root keeps (the warnings
	// of Show Warnings), for --log-file. It runs on the update goroutine and must not block.
	NoticeSink func(text string)
	// Recents are the recent folders, newest first. The welcome screen lists up to
	// five of them. Nil leaves the list empty.
	Recents []string
	// FileLaunch is the immutable startup-invocation property exposed to plugins as
	// gvim.workspace.file_launch(): true when this editor was launched to open
	// command-line files, --simple or --diff rather than a session. A standalone stdin
	// launch is not Fresh's file launch; a restored session and a daemon workspace
	// build that restores tabs leave it false.
	FileLaunch bool
	Dialogs    Dialogs    // nil: the unsaved and Save As dialogs are not offered
	Files      FileIndex  // nil: the palette's file mode shows its loading row
	Ranker     FileRanker // nil: the palette's file rows are not ranked
	// FileRoot is the root the palette's file rows are relative to when it is not the
	// explorer's root (a remote workspace: the rows are remote relative paths). Empty
	// uses the explorer root. A non-empty FileRoot also marks the session as remote for
	// the palette: paths are joined with slashes and the local file preview is disabled.
	FileRoot string
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
	// LargeWindows loads windows of a remote windowed large document. Nil keeps such
	// documents blank where their windows are not loaded (no polling is armed).
	LargeWindows LargeWindowLoader
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
	// ThemeCatalog is the available themes: bundled and user themes. Nil keeps the
	// picker and the theme commands on the bundled themes only.
	ThemeCatalog ThemeCatalog
	// ThemeRef is the config theme value the session started with (a bundled name,
	// builtin://name, a user-dir relative path or file://); empty means the theme
	// field is already the resolved name.
	ThemeRef string
	// Detach ends the attached daemon client while the hosted editor keeps running
	// (the "Detach from Daemon" command). Nil when this program is not hosted by a
	// daemon: the command then explains that nothing is attached.
	Detach func()
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
	// Structure returns the sub-page of the structured setting structureKey for
	// the layer: the formatters map, the language entry list, the scalar fields of
	// one language, the status-bar element lists, terminal.shell.args, the LSP
	// servers of a language, or the plugin/keybinding/environment pages. It reads
	// the layer's file, so the page calls it from a tea.Cmd.
	Structure(structureKey, layer string) (SettingsStructure, error)
	// Act runs a structure page's action on the service's live parts: an LSP
	// server start, stop or restart, a plugin enable or reload. It may do IO and
	// block, so the page calls it from a tea.Cmd, never from Update. The message
	// is the notice the page shows; err is why the action failed.
	Act(action, value string) (message string, err error)
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
	Path    []string // the config path components, for Reset and structured edits
	Name    string   // the last path element, the row's label
	Section string   // the group the row is listed in
	Kind    string   // "bool", "int", "enum", "string" or "page" (a structured sub-page)
	Value   string   // the current value, as the row shows it ("on"/"off" for a bool)
	Choices []string // the values of an enum
	Min     int      // the bounds of an int; Max 0 means no upper bound
	Max     int
	Apply   string   // "live" when a saved value applies at once, "restart" when it needs a restart
	Layers  []string // the layers the setting may be written in: "user", maybe "project"
	// Note is a short explanation of a read-only row (Kind "info"), drawn after
	// the value: where the fact comes from, or why the row cannot be edited.
	Note string
	// Preview is a short row of colour chips drawn after the name (the theme row's
	// background/foreground/accent/popup selection swatches). Empty means none.
	Preview []SettingsSwatch
}

// SettingsSwatch is one colour chip of a settings row's preview (the theme row):
// Label names the role and FG/BG are the chip's text and fill. The app builds
// them from the catalogue's resolved themes, so the page shows colours without
// reading a theme file.
type SettingsSwatch struct {
	Label string
	FG    color.Color
	BG    color.Color
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

// SettingEdit is one changed row the page saves. A scalar row names its setting
// Key and the display-form Value the user set. A structured row (a formatters
// entry, a language override, a status-bar element list) carries the explicit
// config Path and the JSON Raw to write, because dotted keys are ambiguous for
// names like formatter extensions (".go"). A reset removes the key from the
// layer instead: the value then falls back to a lower layer or the built-in
// default.
type SettingEdit struct {
	Key    string
	Path   []string
	Value  string
	Raw    json.RawMessage
	Remove bool
}

// SettingsStructure is one structured setting's sub-page: a map (formatters), a
// list (terminal.shell.args), a dual list (the status-bar element lists), the
// language entry list, or the scalar fields of one language. Rows carry their
// explicit config paths so the page can edit one entry without knowing the
// schema; Available is the set of values that can be added (language ids, or the
// status-bar elements not on a side).
type SettingsStructure struct {
	Key       string // structure id, for example "formatters" or "languages.go"
	Title     string
	Kind      string // "map", "list", "dual", "languages", "fields" or "entries"
	AddLabel  string // the label of the add row; empty when the structure cannot add
	Rows      []SettingsStructureRow
	Available []string
	// Path is the whole-array config path of a "list" structure (for example
	// ["terminal","shell","args"]), used to write the list even when it is empty.
	// A map or fields structure derives its paths from the rows instead.
	Path []string
	// Note is a page-level hint drawn under the title, such as the legend of the
	// LSP status column or why the page is read-only.
	Note string
	// Filter lists the page's rows through a typed filter string (the Keybindings
	// list): printable keys narrow it, Backspace shortens it and Esc clears it
	// before it closes the page.
	Filter bool
}

// SettingsStructureRow is one row of a structure sub-page. Group separates the
// dual list's sides ("left", "right"); the other kinds leave it empty.
type SettingsStructureRow struct {
	Path    []string
	Group   string
	Name    string
	Value   string
	Kind    string // "bool", "int", "enum" or "string" for an inline-editable row
	Choices []string
	Min     int
	Max     int
	Child   string // structure id this row opens, for a language entry row
	// Locked marks a row the current layer may not change; Note says why (for
	// example "user layer only" or "the project may only disable it"). Enter and
	// Del show the reason.
	Locked bool
	// Inherited marks a row whose value is inherited rather than set in the
	// layer: the page shows it distinctly, Enter writes an override and Del is a
	// no-op notice.
	Inherited bool
	// Note is a short suffix drawn after the value: where the value comes from
	// (builtin, user, universal) or the live state of the thing the row names.
	Note string
	// Action runs something instead of editing the row: "ui:<action>" runs a TUI
	// action (the keybinding editor), "act:<op>" calls SettingsPort.Act (an LSP
	// server start/stop or a plugin reload). The action's Result shows a notice.
	Action string
	// Raw is the JSON of a whole entry of an "entries" structure (one on_save
	// action), so the page can remove or add entries by writing the array back.
	Raw json.RawMessage
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
	// Reload is set for a SettingsPort.Reload result: the config files were read
	// again without a write, so the page says "reloaded" instead of "saved".
	Reload bool
	// Warnings are problems or limits worth a status-row notice: a config
	// diagnostic after the reload, a layer that could not be written, a command
	// line flag that wins over the file.
	Warnings []string
}

// ThemeCatalog is the set of themes the TUI offers: the bundled ones and the
// user themes in config.Dir()/themes. internal/app implements it over
// theme.Registry; a test implements it directly. Nil falls back to the bundled
// themes only.
type ThemeCatalog interface {
	// Infos returns the available themes, sorted by display name.
	Infos() []ThemeInfo
	// Load resolves a config theme value (a bundled name, builtin://name, a path
	// relative to the user theme dir, or file://) into a resolved theme.
	Load(ref string) (theme.Theme, error)
	// Canonical returns the portable ref of ref.
	Canonical(ref string) (string, error)
	// Reload rescans the user theme dir, so an edited theme file is read again.
	Reload() error
	// Warnings are registry problems worth a status-row notice.
	Warnings() []string
	// SaveTheme writes th to the user theme dir as name and returns its portable
	// ref. Save As and the theme editor's Save use it.
	SaveTheme(name string, th theme.Theme) (string, error)
	// DeleteTheme removes a user theme file.
	DeleteTheme(ref string) error
	// UserDir is the directory user theme files live in.
	UserDir() string
	// ThemePath is where the user theme file for ref lives (it may not exist yet).
	ThemePath(ref string) (string, error)
	// Data is the resolved theme ref as theme-file JSON.
	Data(ref string) (string, error)
}

// ThemeInfo is one theme the catalog offers.
type ThemeInfo struct {
	Name    string // display name
	Ref     string // portable config value
	Builtin bool
}
