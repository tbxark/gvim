package pluginapi

// Unsupported implements Host with CodeUnsupported errors and zero values. ui.pluginHost
// embeds it so a stage can ship with only its own methods implemented.
type Unsupported struct{}

var _ Host = Unsupported{}

func unsupported(what string) error {
	return Errf(CodeUnsupported, "%s is not supported", what)
}

// BufferEditor.

func (Unsupported) ListBuffers() []BufferInfo                       { return nil }
func (Unsupported) Buffer(BufferID) (BufferInfo, bool)              { return BufferInfo{}, false }
func (Unsupported) ActiveBuffer() BufferID                          { return 0 }
func (Unsupported) FindBuffer(string) BufferID                      { return 0 }
func (Unsupported) Lines(BufferID, int, int) ([]string, error)      { return nil, unsupported("Lines") }
func (Unsupported) Slice(BufferID, Range) (string, error)           { return "", unsupported("Slice") }
func (Unsupported) ByteLength(BufferID) (int, error)                { return 0, unsupported("ByteLength") }
func (Unsupported) OffsetOf(BufferID, Pos) (int, error)             { return 0, unsupported("OffsetOf") }
func (Unsupported) PosOf(BufferID, int) (Pos, error)                { return Pos{}, unsupported("PosOf") }
func (Unsupported) Edit(BufferID, []Edit, EditOptions) error        { return unsupported("Edit") }
func (Unsupported) UndoGroup(BufferID, func() error) error          { return unsupported("UndoGroup") }
func (Unsupported) Undo(BufferID) error                             { return unsupported("Undo") }
func (Unsupported) Redo(BufferID) error                             { return unsupported("Redo") }
func (Unsupported) Cursors(BufferID) ([]Caret, error)               { return nil, unsupported("Cursors") }
func (Unsupported) SetCursor(BufferID, Pos, bool, bool, bool) error { return unsupported("SetCursor") }
func (Unsupported) SetSelection(BufferID, Range) error              { return unsupported("SetSelection") }
func (Unsupported) Search(BufferID, SearchQuery) (SearchResult, error) {
	return SearchResult{}, unsupported("Search")
}
func (Unsupported) FindLines(BufferID, FindLinesQuery) (FindLinesResult, error) {
	return FindLinesResult{Next: -1}, unsupported("FindLines")
}
func (Unsupported) FullText(BufferID) (string, Version, error) {
	return "", Version{}, unsupported("FullText")
}
func (Unsupported) SetLanguage(BufferID, string) error    { return unsupported("SetLanguage") }
func (Unsupported) SavedLines(BufferID) ([]string, error) { return nil, unsupported("SavedLines") }
func (Unsupported) Activate(BufferID) error               { return unsupported("Activate") }
func (Unsupported) Close(BufferID, bool) error            { return unsupported("Close") }
func (Unsupported) MoveTab(BufferID, int) error           { return unsupported("MoveTab") }
func (Unsupported) TrackChanges(string, bool)             {}
func (Unsupported) TakeChanges(BufferID, string) Changes  { return Changes{} }
func (Unsupported) CreateBuffer(CreateBuffer) (BufferID, error) {
	return 0, unsupported("CreateBuffer")
}
func (Unsupported) SetReadOnly(BufferID, bool) error              { return unsupported("SetReadOnly") }
func (Unsupported) SetContent(BufferID, []ContentEntry) error     { return unsupported("SetContent") }
func (Unsupported) PropsAt(BufferID, Pos) (map[string]any, error) { return nil, unsupported("PropsAt") }
func (Unsupported) SetBufferMode(BufferID, string) error          { return unsupported("SetBufferMode") }
func (Unsupported) SetOption(BufferID, string, any) error         { return unsupported("SetOption") }
func (Unsupported) GetOption(BufferID, string) (any, error)       { return nil, unsupported("GetOption") }

// BufferHost.

func (Unsupported) Open(Token, OpenRequest) error      { return unsupported("Open") }
func (Unsupported) Save(Token, BufferID, string) error { return unsupported("Save") }
func (Unsupported) Revert(Token, BufferID) error       { return unsupported("Revert") }

// WindowHost.

func (Unsupported) Windows() []WindowInfo                 { return nil }
func (Unsupported) CurrentWindow() WinID                  { return 0 }
func (Unsupported) SetWindowBuffer(WinID, BufferID) error { return unsupported("SetWindowBuffer") }
func (Unsupported) FocusWindow(WinID) error               { return unsupported("FocusWindow") }
func (Unsupported) CloseWindow(WinID) error               { return unsupported("CloseWindow") }
func (Unsupported) SplitWindow(WinID, SplitOptions) (WinID, error) {
	return 0, unsupported("SplitWindow")
}
func (Unsupported) ScrollWindow(WinID, int, bool) error     { return unsupported("ScrollWindow") }
func (Unsupported) Equalize() error                         { return unsupported("Equalize") }
func (Unsupported) VisibleRows(WinID) ([]VisibleRow, error) { return nil, unsupported("VisibleRows") }

// CommandHost.

func (Unsupported) Do(string, map[string]any, int) error { return unsupported("Do") }
func (Unsupported) DoAll([]Step) error                   { return unsupported("DoAll") }
func (Unsupported) Holds(string) bool                    { return false }
func (Unsupported) KeyLabel(string) string               { return "" }
func (Unsupported) OpenPalette(string)                   {}
func (Unsupported) Actions() []string                    { return nil }

// UIHost.

func (Unsupported) Notify(string, Level, string)               {}
func (Unsupported) Input(Token, InputSpec) error               { return unsupported("Input") }
func (Unsupported) Confirm(Token, ConfirmSpec) error           { return unsupported("Confirm") }
func (Unsupported) OpenPicker(Token, uint64, PickerSpec) error { return unsupported("OpenPicker") }
func (Unsupported) UpdatePicker(uint64, PickerUpdate) error    { return unsupported("UpdatePicker") }
func (Unsupported) ClosePicker(uint64)                         {}
func (Unsupported) ShowPopup(Token, uint64, PopupSpec) error   { return unsupported("ShowPopup") }
func (Unsupported) ClosePopup(uint64)                          {}
func (Unsupported) WaitKey(Token, int) error                   { return unsupported("WaitKey") }
func (Unsupported) CancelWait(Token)                           {}
func (Unsupported) SetCommands(string, []CommandInfo)          {}
func (Unsupported) SetBindings(string, []Binding)              {}
func (Unsupported) SetModes(string, []ModeInfo)                {}
func (Unsupported) SetContext(string, string, bool)            {}
func (Unsupported) SetStatusItems(string, []StatusItem)        {}
func (Unsupported) SetInputMode(string, string) error          { return unsupported("SetInputMode") }
func (Unsupported) SetClipboard(string)                        {}
func (Unsupported) Focus(string) error                         { return unsupported("Focus") }
func (Unsupported) ScreenSize() (int, int)                     { return 0, 0 }
func (Unsupported) OwnerGone(string, uint64)                   {}

// DecorHost.

func (Unsupported) DecorSet(string, string, BufferID, []Decoration, *Version) error {
	return unsupported("DecorSet")
}
func (Unsupported) DecorAdd(string, string, BufferID, Decoration, *Version) (int, error) {
	return 0, unsupported("DecorAdd")
}
func (Unsupported) DecorRemove(string, string, BufferID, int) {}
func (Unsupported) DecorClear(string, string, BufferID)       {}

// WorkspaceHost.

func (Unsupported) Root() string                               { return "" }
func (Unsupported) Files(int) ([]string, bool)                 { return nil, false }
func (Unsupported) Config(string) (any, bool)                  { return nil, false }
func (Unsupported) Theme() ThemeInfo                           { return ThemeInfo{} }
func (Unsupported) ThemeColor(string) (r, g, b uint8, ok bool) { return 0, 0, 0, false }
func (Unsupported) Diagnostics(BufferID) ([]DiagnosticInfo, error) {
	return nil, unsupported("Diagnostics")
}
func (Unsupported) LSPRequest(Token, BufferID, string, any) error { return unsupported("LSPRequest") }
