package pluginapi

// Host side (plugin -> editor). All methods run on the UI goroutine inside a Lua
// call and never block. They never call Runtime.Emit/Run/Resolve/Drain/Control/Veto/
// Stop themselves: a method that could reach such a call is deferred by the ui (it
// validates, queues a closure and returns), see BufferHost and CommandHost.

// BufferEditor is implemented by internal/app (editorAdapter) and passed through ui as ui.PluginEditor.
// It speaks API coordinates (byte columns, byte offsets); the app adapter converts with the
// buffer helpers. Every method returns *Error values (CodeClosed, CodeReadOnly, CodeTooLarge, ...).
type BufferEditor interface {
	ListBuffers() []BufferInfo // tab order
	Buffer(id BufferID) (BufferInfo, bool)
	ActiveBuffer() BufferID
	FindBuffer(path string) BufferID
	Lines(id BufferID, first, count int) ([]string, error) // count < 0: to the end
	Slice(id BufferID, r Range) (string, error)
	ByteLength(id BufferID) (int, error)
	OffsetOf(id BufferID, p Pos) (int, error)
	PosOf(id BufferID, offset int) (Pos, error)
	// Edit applies all edits atomically as ONE undo step; ranges are in pre-edit coordinates,
	// may be unsorted, must not overlap. A non-nil o.Expect that differs from the
	// document's Version fails with CodeStale.
	Edit(id BufferID, edits []Edit, o EditOptions) error
	UndoGroup(id BufferID, fn func() error) error // fn must not yield; nests
	Undo(id BufferID) error
	Redo(id BufferID) error
	Cursors(id BufferID) ([]Caret, error) // primary first
	SetCursor(id BufferID, p Pos, extend, reveal, center bool) error
	SetSelection(id BufferID, r Range) error
	Search(id BufferID, q SearchQuery) (SearchResult, error)
	FindLines(id BufferID, q FindLinesQuery) (FindLinesResult, error)
	// FullText returns the LF-normalised decoded text (<= 16 MiB) and its Version, for diff.buffer.
	FullText(id BufferID) (text string, v Version, err error)
	SetLanguage(id BufferID, lang string) error
	SavedLines(id BufferID) ([]string, error) // the saved text; CodeNotFound for untitled
	Activate(id BufferID) error
	Close(id BufferID, force bool) error // unsaved without force: CodeConflict, never a dialog
	MoveTab(id BufferID, delta int) error
	// TrackChanges is reference counted by key ("events", "decor"): the change observer is on
	// while any key is on.
	TrackChanges(key string, on bool)
	// TakeChanges returns what the document did since this consumer's last call. Every consumer
	// has its own cursor into the document's ring.
	TakeChanges(id BufferID, consumer string) Changes
	// Stage S18 (virtual buffers):
	CreateBuffer(o CreateBuffer) (BufferID, error)
	SetReadOnly(id BufferID, ro bool) error // virtual buffers only
	SetContent(id BufferID, entries []ContentEntry) error
	PropsAt(id BufferID, p Pos) (map[string]any, error)
	SetBufferMode(id BufferID, mode string) error
	SetOption(id BufferID, name string, v any) error // "wrap", "line_numbers", "tab_size"
	GetOption(id BufferID, name string) (any, error)
}

// BufferHost adds the async buffer calls. Open, Save and Revert only validate and
// enqueue; the work runs after the Lua slice has returned.
type BufferHost interface {
	BufferEditor
	Open(t Token, r OpenRequest) error // async; completes with (BufferID) or Err
	Save(t Token, id BufferID, path string) error
	Revert(t Token, id BufferID) error
}

// WindowHost is the split API (stage S17).
type WindowHost interface {
	Windows() []WindowInfo
	CurrentWindow() WinID
	SetWindowBuffer(w WinID, b BufferID) error
	FocusWindow(w WinID) error
	CloseWindow(w WinID) error
	SplitWindow(w WinID, o SplitOptions) (WinID, error)
	ScrollWindow(w WinID, line int, center bool) error
	Equalize() error
	// VisibleRows lists the painted rows of w top to bottom, fold-hidden lines never listed, <= 500 rows.
	VisibleRows(w WinID) ([]VisibleRow, error)
}

// CommandHost runs editor and ui actions.
type CommandHost interface {
	// Do runs an editor or ui action count times on the active document. Actions that
	// could reach the Runtime are deferred: Do validates, enqueues and returns nil.
	// The tea.Cmd an action returns is collected by the ui; Do drops nothing.
	Do(action string, args map[string]any, count int) error
	DoAll(steps []Step) error // one undo group; stops at the first error; deferral as for Do
	Holds(name string) bool   // built-in contexts, via Model.Holds
	KeyLabel(action string) string
	OpenPalette(query string)
	Actions() []string
}

// UIHost is overlays, registrations and status.
type UIHost interface {
	Notify(plugin string, level Level, msg string) // info: status row; warn/error: also warnings log
	Input(t Token, o InputSpec) error              // async: Values{string} or nil on cancel
	Confirm(t Token, o ConfirmSpec) error          // async: Values{bool}
	OpenPicker(t Token, id uint64, o PickerSpec) error
	UpdatePicker(id uint64, u PickerUpdate) error
	ClosePicker(id uint64)
	ShowPopup(t Token, id uint64, o PopupSpec) error
	ClosePopup(id uint64)
	// ShowActionPopup shows a message with action buttons; the wait t resolves with the
	// chosen action id (Values{string}), or nil when it closed without one.
	ShowActionPopup(t Token, id uint64, o ActionPopupSpec) error
	// SetPromptSuggestions replaces the suggestion list of the plugin's open prompt and the
	// selected row (0-based, -1 for none). An empty list closes it. CodeNotFound when no
	// plugin prompt is open.
	SetPromptSuggestions(items []PromptSuggestion, selected int) error
	WaitKey(t Token, timeoutMS int) error // async: Values{Key} or Err "timeout"/"cancelled"
	CancelWait(t Token)
	// Declarative widget panels and sidebar sections (tier 1a). Mount, update and
	// command are cheap validation + state writes on the UI goroutine; the panel keeps
	// the focus, the instance state of its widgets and its placement. OwnerGone drops
	// every panel a plugin mounted.
	MountWidgetPanel(plugin string, id PanelID, o WidgetMount) error
	UpdateWidgetPanel(plugin string, id PanelID, p WidgetPanel) error
	UnmountWidgetPanel(id PanelID)
	WidgetCommand(id PanelID, a WidgetAction) error
	WidgetMutate(id PanelID, m WidgetMutation) error
	// Registrations: whole-set replace per plugin, idempotent, cheap.
	SetCommands(plugin string, cmds []CommandInfo)
	SetBindings(plugin string, b []Binding)
	SetModes(plugin string, m []ModeInfo)
	SetContext(plugin, name string, on bool)
	SetStatusItems(plugin string, items []StatusItem)
	// SetMenuItems replaces the plugin's menu bar rows (whole-set replace, like the other
	// registrations). OwnerGone drops them.
	SetMenuItems(plugin string, items []MenuItem)
	SetInputMode(plugin, mode string) error // "" clears
	SetClipboard(text string)
	Focus(area string) error // "editor" | "explorer"
	ScreenSize() (w, h int)
	// OwnerGone removes EVERYTHING (plugin, gen) owns in the ui: commands, bindings, modes, contexts,
	// status items, decorations, overlays, key waits, input mode. Idempotent.
	OwnerGone(plugin string, gen uint64)
}

// DecorHost is the decoration API (stage S15).
type DecorHost interface {
	// DecorSet replaces everything the namespace has on the buffer. A non-nil expect that differs
	// from the buffer's Version fails with CodeStale (positions were computed against other text).
	DecorSet(plugin, ns string, id BufferID, items []Decoration, expect *Version) error
	DecorAdd(plugin, ns string, id BufferID, item Decoration, expect *Version) (int, error)
	DecorRemove(plugin, ns string, id BufferID, item int)
	DecorClear(plugin, ns string, id BufferID) // id 0: all buffers
}

// WorkspaceHost is read access to the workspace and the theme.
type WorkspaceHost interface {
	Root() string
	Files(limit int) (files []string, loading bool) // finder index snapshot, no IO
	Config(key string) (any, bool)                  // allowlisted effective settings
	Theme() ThemeInfo
	ThemeColor(key string) (r, g, b uint8, ok bool)
	// ThemeNames lists the themes the editor has built in, sorted.
	ThemeNames() []string
	// ThemeOverrides is the current session override map ("section.key" -> text color),
	// and whether one is set. SetThemeOverrides replaces it; an empty or nil map clears
	// it. The editor applies overrides on top of the active theme and resets them on the
	// next theme change; validation errors are reported to the plugin.
	ThemeOverrides() (map[string]string, bool)
	SetThemeOverrides(overrides map[string]string) error
	Diagnostics(id BufferID) ([]DiagnosticInfo, error)                // stage S23
	AllDiagnostics() ([]FileDiagnostics, error)                       // every open document, by path
	LSPRequest(t Token, id BufferID, method string, params any) error // stage S23, async
}

// Host is everything a plugin can reach in the editor.
type Host interface {
	BufferHost
	WindowHost
	CommandHost
	UIHost
	DecorHost
	WorkspaceHost
}
