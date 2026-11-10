package tui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

// The keybinding editor follows Fresh's docs/features/keybinding-editor.md: a table of
// the active bindings (key, action, description, context, source) that can be searched by
// text or by a recorded key, filtered by context and source, edited, added, deleted (a
// custom binding is reset to the keymap's, a keymap binding is unbound) and disabled.
// Changes are pending: the table shows them at once, Ctrl+S writes them to the
// "keybindings" list of the user config file in the documented form and makes them live,
// and closing with pending changes asks Save, Discard or Cancel. Like Fresh's editor it
// cannot record a sequence: write those in the config file.

// kbContexts are the when-clauses the edit dialog cycles through: the contexts gvim resolves
// keys in. The editor keeps its own order — the editor contexts first, then the overlay
// contexts, the file explorer and the terminal — so the first context offered stays Normal.
// A binding in another context would never run, so the editor does not offer one.
var kbContexts = func() []string {
	out := []string{string(keymap.ContextNormal), string(keymap.ContextGlobal)}
	for _, c := range keymap.ConsultedNames() {
		if c != string(keymap.ContextNormal) && c != string(keymap.ContextGlobal) {
			out = append(out, c)
		}
	}
	return out
}()

const (
	kbList    = iota // the table
	kbEdit           // the add and edit dialog
	kbRecord         // the dialog's Key field takes the next press, whatever it is
	kbConfirm        // closing with pending changes: save, discard or cancel
)

// kbRow is one row of the table.
type kbRow struct {
	chord   string
	action  string
	context string
	custom  int // the index in the custom list that makes the row, or -1 for the keymap's
}

// kbForm is the add and edit dialog.
type kbForm struct {
	orig    *kbRow // the row being edited, nil when adding
	chord   string
	action  []rune
	ctx     int // index in kbContexts
	field   int // 0 key, 1 action, 2 context
	suggest int // the highlighted suggestion
}

// keybindEditor is the overlay of the keybinding editor. It is used through a pointer, so
// the root can find it again when a save finishes.
type keybindEditor struct {
	store    KeybindingStore
	lister   BindingLister
	preset   string
	plugins  func() []pluginapi.Binding // the plugin bindings the keymap layers, nil when none
	describe func(action string) string
	actions  []string
	file     string // the config file the changes go to, for the status line

	custom     []CustomBinding // the list with the pending changes
	saved      []CustomBinding // the list the config file holds
	closeAfter bool            // close once the save in flight succeeds
	drawn      bool            // Draw has run, so box is the real size
	ctxFilter  string          // "" for all contexts
	srcFilter  string          // "" for all sources, else "custom" or "keymap"
	keyFilter  string          // a canonical chord, or ""
	searchKey  bool            // the search takes a key press, not text
	rows       []kbRow
	shown      []int // indexes into rows that pass the filter
	filter     []rune
	searching  bool
	sel, top   int
	page       int
	mode       int
	form       kbForm
	status     string
	failed     bool
	saving     bool
	active     bool
	chrome     style.Overlay
	box        rect
}

// The editor is an overlay of the root's stack.
var _ Overlay = (*keybindEditor)(nil)

// openKeybindingEditor shows the editor over the active keymap.
func (m *Model) openKeybindingEditor() {
	l, _ := m.keys.(BindingLister)
	if l == nil {
		v := list.New("Keybinding Editor", nil, m.listChrome(), nil)
		v.Empty = noBindingsText
		v.Hint = list.ReadOnlyHint
		m.pushOverlay(v)
		return
	}
	e := &keybindEditor{
		store:    m.root.ports.Keybindings,
		lister:   l,
		preset:   m.util.settings.Keymap,
		plugins:  m.pluginBindingsForEditor,
		describe: m.actionDescriber(),
		actions:  KnownActions(),
		file:     m.util.settings.ConfigFile,
		active:   true,
		chrome:   m.listChrome(),
	}
	if e.store == nil {
		e.status = "Read only: this build has no way to write the config file"
		e.failed = true
	} else {
		e.custom = e.store.Custom()
		e.saved = slices.Clone(e.custom)
	}
	e.rebuild()
	m.pushOverlay(e)
}

// actionDescriber returns the human name of an action: the palette's name for it, or the
// action with its underscores taken out.
func (m *Model) actionDescriber() func(string) string {
	names := make(map[string]string, len(m.commands))
	for _, c := range m.commands {
		if _, dup := names[c.Action]; !dup && c.Action != "" {
			names[c.Action] = c.Name
		}
	}
	return func(action string) string {
		if n, ok := names[action]; ok {
			return n
		}
		return humanAction(action)
	}
}

// humanAction writes an action id as words: "move_line_up" is "Move line up".
func humanAction(action string) string {
	s := strings.ReplaceAll(action, "_", " ")
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// rebuild reads the rows from the lister and the custom list. A binding that a later one of
// the same chord and context replaces is left out, and so is the noop that disables a key
// (it shows as a disabled row).
func (e *keybindEditor) rebuild() {
	type slot struct{ context, chord string }
	list := e.lister.Bindings()
	last := make(map[slot]int, len(list))
	for i, b := range list {
		last[slot{b.Context, b.Chord}] = i
	}
	var keep *kbRow
	if r, ok := e.selected(); ok {
		keep = &r
	}
	e.rows = e.rows[:0]
	for i, b := range list {
		if last[slot{b.Context, b.Chord}] != i || b.Action == "" || !keymap.Consulted(keymap.Context(b.Context)) {
			continue
		}
		e.rows = append(e.rows, kbRow{chord: b.Chord, action: b.Action, context: b.Context, custom: e.customFor(b)})
	}
	e.recompute(keep)
}

// customFor is the index of the custom entry that makes binding b, or -1. A custom binding in
// the global context also stands in the contexts that bind the same chord.
func (e *keybindEditor) customFor(b KeyBinding) int {
	for i, c := range e.custom {
		if c.Chord == b.Chord && c.Action == b.Action && (c.Context == b.Context || c.Context == "global") {
			return i
		}
	}
	return -1
}

// recompute filters the rows by the filter text. The highlight moves to the row keep when it
// is still listed, and the window follows it.
func (e *keybindEditor) recompute(keep *kbRow) {
	needle := strings.ToLower(string(e.filter))
	e.shown = e.shown[:0]
	for i, r := range e.rows {
		hay := strings.ToLower(keymap.FormatChord(r.chord) + " " + r.chord + " " + r.action + " " + e.describe(r.action) + " " + r.context + " " + e.source(r))
		switch {
		case needle != "" && !strings.Contains(hay, needle):
		case e.ctxFilter != "" && r.context != e.ctxFilter:
		case e.srcFilter != "" && e.source(r) != e.srcFilter:
		case e.keyFilter != "" && r.chord != e.keyFilter:
		default:
			e.shown = append(e.shown, i)
		}
	}
	e.sel = 0
	if keep != nil {
		for k, i := range e.shown {
			if e.rows[i].chord == keep.chord && e.rows[i].context == keep.context && e.rows[i].action == keep.action {
				e.sel = k
			}
		}
	}
	e.top = 0
	e.keepVisible()
}

// current is the highlighted row, or nil.
func (e *keybindEditor) current() *kbRow {
	if r, ok := e.selected(); ok {
		return &r
	}
	return nil
}

// source is "custom" for a row the user's list makes, else "keymap".
func (e *keybindEditor) source(r kbRow) string {
	if r.custom >= 0 {
		return "custom"
	}
	return "keymap"
}

// selected is the highlighted row.
func (e *keybindEditor) selected() (kbRow, bool) {
	if e.sel < 0 || e.sel >= len(e.shown) {
		return kbRow{}, false
	}
	return e.rows[e.shown[e.sel]], true
}

func (e *keybindEditor) pageRows() int {
	if e.page > 0 {
		return e.page
	}
	return list.DefaultPage
}

func (e *keybindEditor) keepVisible() {
	page := e.pageRows()
	if e.sel < e.top {
		e.top = e.sel
	}
	if e.sel >= e.top+page {
		e.top = e.sel - page + 1
	}
	e.top = max(0, min(e.top, len(e.shown)-page))
}

func (e *keybindEditor) move(d int) {
	e.sel = max(0, min(e.sel+d, len(e.shown)-1))
	e.keepVisible()
}

// TakesEscape tells the root to pass Esc to the editor instead of closing it: Esc leaves the
// filter, the dialog and the key recording first, and asks what to do with pending changes.
func (e *keybindEditor) TakesEscape() bool {
	return e.mode != kbList || e.searching || len(e.filter) > 0 || e.keyFilter != "" || e.dirty()
}

// dirty reports whether there are pending changes that the config file does not hold.
func (e *keybindEditor) dirty() bool {
	return !slices.EqualFunc(e.custom, e.saved, func(a, b CustomBinding) bool {
		return a.Chord == b.Chord && a.Action == b.Action && a.Context == b.Context && reflect.DeepEqual(a.Args, b.Args)
	})
}

// Active reports whether the editor is open.
func (e *keybindEditor) Active() bool { return e.active }

// Update handles a key press or a mouse message.
func (e *keybindEditor) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd = e.key(tea.Key(msg))
	case tea.PasteMsg:
		e.typeText(msg.Content)
	case tea.MouseClickMsg:
		if e.mode == kbList && tea.Mouse(msg).Button == tea.MouseLeft {
			if k, ok := e.rowAt(tea.Mouse(msg).X, tea.Mouse(msg).Y); ok {
				e.sel = k
			}
		}
	case tea.MouseWheelMsg:
		if e.mode == kbList {
			switch tea.Mouse(msg).Button {
			case tea.MouseWheelUp:
				e.move(-list.WheelRows)
			case tea.MouseWheelDown:
				e.move(list.WheelRows)
			}
		}
	}
	if !e.active {
		return nil, cmd
	}
	return e, cmd
}

// rowAt is the index in shown of the row drawn at (x, y) in the last draw.
func (e *keybindEditor) rowAt(x, y int) (int, bool) {
	b := e.box
	if !b.Contains(x, y) || y < b.Y+2 || y >= b.Y+b.H-2 {
		return 0, false
	}
	k := e.top + (y - b.Y - 2)
	return k, k < len(e.shown)
}

// key handles one key press in the current mode.
func (e *keybindEditor) key(k tea.Key) tea.Cmd {
	switch e.mode {
	case kbEdit:
		return e.formKey(k)
	case kbRecord:
		e.recordKey(k)
		return nil
	case kbConfirm:
		return e.confirmKey(k)
	}
	return e.listKey(k)
}

// listKey handles a key press in the table.
func (e *keybindEditor) listKey(k tea.Key) tea.Cmd {
	page := e.pageRows()
	if e.searching && e.searchKey && k.Code != tea.KeyEscape && k.Code != tea.KeyTab {
		e.recordSearchKey(k)
		return nil
	}
	if k.Code == 's' && k.Mod&tea.ModCtrl != 0 {
		return e.saveAll(false)
	}
	switch k.Code {
	case tea.KeyEscape:
		// The search takes Esc first, then pending changes ask what to do. The root closes
		// the editor otherwise.
		if e.searching || len(e.filter) > 0 || e.keyFilter != "" {
			e.searching, e.searchKey = false, false
			e.filter, e.keyFilter = nil, ""
			e.recompute(e.current())
		} else if e.dirty() {
			e.mode = kbConfirm
		}
		return nil
	case tea.KeyUp:
		e.move(-1)
		return nil
	case tea.KeyDown:
		e.move(1)
		return nil
	case tea.KeyPgUp:
		e.move(-page)
		return nil
	case tea.KeyPgDown:
		e.move(page)
		return nil
	case tea.KeyHome:
		e.move(-len(e.shown))
		return nil
	case tea.KeyEnd:
		e.move(len(e.shown))
		return nil
	case tea.KeyEnter:
		if e.searching {
			e.searching = false
			return nil
		}
		e.startEdit()
		return nil
	case tea.KeyTab:
		// Tab switches the search between text and a recorded key.
		if e.searching {
			e.searchKey = !e.searchKey
		}
		return nil
	case tea.KeyBackspace:
		if e.searching && len(e.filter) > 0 {
			e.filter = e.filter[:len(e.filter)-1]
			e.recompute(e.current())
		}
		return nil
	case tea.KeyDelete:
		if !e.searching {
			return e.remove()
		}
		return nil
	}
	if k.Mod&(tea.ModCtrl|tea.ModAlt) != 0 || k.Text == "" {
		return nil
	}
	if e.searching {
		e.typeText(k.Text)
		return nil
	}
	switch k.Text {
	case "/":
		e.searching, e.searchKey = true, false
	case "r":
		e.searching, e.searchKey = true, true
	case "c":
		e.cycleContext()
	case "s":
		e.cycleSource()
	case "a":
		e.startAdd()
	case "d":
		return e.remove()
	case "x":
		return e.disable()
	}
	return nil
}

// typeText adds printable text to the filter, or to the action field of the dialog.
func (e *keybindEditor) typeText(text string) {
	if text == "" || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return
	}
	switch {
	case e.mode == kbEdit && e.form.field == 1:
		e.form.action = append(e.form.action, []rune(text)...)
		e.form.suggest = 0
	case e.mode == kbList && e.searching:
		e.filter = append(e.filter, []rune(text)...)
		e.recompute(e.current())
	}
}

// recordSearchKey takes the press as the key the table is searched for: the rows that
// hold exactly that key stay. The search then ends, so the arrows move in the result.
func (e *keybindEditor) recordSearchKey(k tea.Key) {
	kp, _ := translateKey(k)
	if kp.Name == "" {
		return
	}
	chord, err := keymap.Canonical(kp.Name, kp.Mods)
	if err != nil {
		return
	}
	e.keyFilter = chord
	e.searching, e.searchKey = false, false
	e.recompute(e.current())
}

// cycleContext steps the context filter through All and the contexts that have rows.
func (e *keybindEditor) cycleContext() {
	order := append([]string{"global"}, slices.DeleteFunc(slices.Clone(kbContexts), func(c string) bool { return c == "global" })...)
	e.ctxFilter = e.nextFilter(e.ctxFilter, order, func(r kbRow) string { return r.context })
	e.recompute(e.current())
}

// cycleSource steps the source filter through All, custom and keymap.
func (e *keybindEditor) cycleSource() {
	e.srcFilter = e.nextFilter(e.srcFilter, []string{"custom", "keymap"}, e.source)
	e.recompute(e.current())
}

// nextFilter is the value after cur in All, then the values of order that some row has. It
// returns "" for All.
func (e *keybindEditor) nextFilter(cur string, order []string, of func(kbRow) string) string {
	var live []string
	for _, v := range order {
		if slices.ContainsFunc(e.rows, func(r kbRow) bool { return of(r) == v }) || v == cur {
			live = append(live, v)
		}
	}
	if cur == "" {
		if len(live) > 0 {
			return live[0]
		}
		return ""
	}
	if i := slices.Index(live, cur); i >= 0 && i+1 < len(live) {
		return live[i+1]
	}
	return ""
}

// saveAll writes the pending changes to the config file. With closeAfter the editor closes
// once that worked. Without changes it says so.
func (e *keybindEditor) saveAll(closeAfter bool) tea.Cmd {
	if e.store == nil {
		return nil
	}
	if !e.dirty() {
		e.status, e.failed = "No changes to save", false
		if closeAfter {
			e.active = false
		}
		return nil
	}
	e.closeAfter = closeAfter
	n := len(e.custom)
	return e.save(slices.Clone(e.custom), fmt.Sprintf("Saved %d custom keybindings", n))
}

// confirmKey handles a key press while the editor asks what to do with pending changes:
// Save and close, Discard and close, or Cancel to go on editing.
func (e *keybindEditor) confirmKey(k tea.Key) tea.Cmd {
	switch {
	case k.Code == tea.KeyEscape || k.Text == "c" || k.Text == "C":
		e.mode = kbList
	case k.Text == "d" || k.Text == "D":
		e.active = false
	case k.Text == "s" || k.Text == "S" || k.Code == tea.KeyEnter || (k.Code == 's' && k.Mod&tea.ModCtrl != 0):
		e.mode = kbList
		return e.saveAll(true)
	}
	return nil
}

// canShowForm reports whether the dialog fits in a box of the size of parent.
func canShowForm(parent rect) bool {
	return parent.W >= formMinW+4 && parent.H >= formCompactH
}

// startEdit opens the dialog on the highlighted row.
func (e *keybindEditor) startEdit() {
	r, ok := e.selected()
	if !ok {
		return
	}
	if e.store == nil || !e.formFits() {
		return
	}
	e.form = kbForm{orig: &r, chord: r.chord, action: []rune(r.action), ctx: max(slices.Index(kbContexts, r.context), 0)}
	e.mode, e.status = kbEdit, ""
}

// formFits reports whether the dialog can be drawn. Without it the keys would go to a
// dialog nobody sees, so the table says why nothing opened. Before the first draw the size
// is not known and the dialog opens.
func (e *keybindEditor) formFits() bool {
	if e.drawn && !canShowForm(e.box) {
		e.status, e.failed = "Terminal too small for the dialog", true
		return false
	}
	return true
}

// startAdd opens the dialog for a new binding in the normal context.
func (e *keybindEditor) startAdd() {
	if e.store == nil || !e.formFits() {
		return
	}
	e.form = kbForm{}
	e.mode, e.status = kbEdit, ""
}

// recordKey takes the press as the new chord of the dialog, whatever the key is.
func (e *keybindEditor) recordKey(k tea.Key) {
	e.mode = kbEdit
	e.setChord(k)
}

// setChord sets the chord of the dialog to the press k, when k is a key of the keymap.
func (e *keybindEditor) setChord(k tea.Key) {
	kp, _ := translateKey(k)
	if kp.Name == "" {
		return
	}
	chord, err := keymap.Canonical(kp.Name, kp.Mods)
	if err != nil {
		return
	}
	e.form.chord = chord
}

// suggestions are the actions that contain the text of the action field, the ones that
// start with it first. At most five are listed.
func (e *keybindEditor) suggestions() []string {
	text := strings.ToLower(strings.TrimSpace(string(e.form.action)))
	var prefix, rest []string
	for _, a := range e.actions {
		switch {
		case text == "" || strings.HasPrefix(a, text):
			prefix = append(prefix, a)
		case strings.Contains(a, text):
			rest = append(rest, a)
		}
	}
	return slices.Concat(prefix, rest)[:min(5, len(prefix)+len(rest))]
}

// formKey handles a key press in the dialog.
func (e *keybindEditor) formKey(k tea.Key) tea.Cmd {
	f := &e.form
	switch {
	case k.Code == tea.KeyEscape:
		e.mode = kbList
		return nil
	case k.Code == 's' && k.Mod&tea.ModCtrl != 0 && f.field != 0:
		return e.saveForm()
	case k.Code == tea.KeyTab && k.Mod&tea.ModShift == 0 && f.field == 1 && e.acceptSuggestion():
		return nil
	case k.Code == tea.KeyTab && k.Mod&tea.ModShift != 0:
		f.field = (f.field + 2) % 3
		return nil
	case k.Code == tea.KeyTab:
		f.field = (f.field + 1) % 3
		return nil
	}
	switch f.field {
	case 0:
		if k.Code == tea.KeyEnter {
			e.mode = kbRecord
			return nil
		}
		e.setChord(k)
	case 1:
		e.actionKey(k)
		if k.Code == tea.KeyEnter {
			if e.acceptSuggestion() {
				return nil
			}
			f.field = 2
		}
	case 2:
		switch k.Code {
		case tea.KeyLeft:
			f.ctx = (f.ctx + len(kbContexts) - 1) % len(kbContexts)
		case tea.KeyRight:
			f.ctx = (f.ctx + 1) % len(kbContexts)
		case tea.KeyEnter:
			return e.saveForm()
		}
	}
	return nil
}

// actionKey edits the action text and moves through the suggestions.
func (e *keybindEditor) actionKey(k tea.Key) {
	f := &e.form
	switch k.Code {
	case tea.KeyBackspace:
		if n := len(f.action); n > 0 {
			f.action = f.action[:n-1]
			f.suggest = 0
		}
	case tea.KeyUp:
		f.suggest = max(f.suggest-1, 0)
	case tea.KeyDown:
		f.suggest = min(f.suggest+1, max(len(e.suggestions())-1, 0))
	default:
		if k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			e.typeText(k.Text)
		}
	}
}

// acceptSuggestion puts the highlighted suggestion in the action field. It reports whether
// that changed the text, so Tab and Enter move on once the text is the action.
func (e *keybindEditor) acceptSuggestion() bool {
	sug := e.suggestions()
	if len(sug) == 0 {
		return false
	}
	pick := sug[min(e.form.suggest, len(sug)-1)]
	if string(e.form.action) == pick {
		return false
	}
	e.form.action = []rune(pick)
	e.form.suggest = 0
	return true
}

// conflict finds the row that holds chord in context ctx, other than the row being edited.
// A global binding conflicts with one of the same key in the normal context, which the keymap
// resolves first and which hides it there. The modal contexts (prompt, popup, menu, ...) have
// their own keys, so a global binding does not clash with them.
func (e *keybindEditor) conflict(chord, ctx string) (kbRow, bool) {
	for _, r := range e.rows {
		if r.chord != chord || r.action == "" {
			continue
		}
		if e.form.orig != nil && r.chord == e.form.orig.chord && r.context == e.form.orig.context && r.action == e.form.orig.action {
			continue
		}
		if r.context == ctx || (r.context == "global" && ctx == "normal") || (r.context == "normal" && ctx == "global") {
			return r, true
		}
	}
	return kbRow{}, false
}

// validate says why the dialog cannot be saved, or "" when it can.
func (e *keybindEditor) validate() string {
	f := e.form
	action := strings.TrimSpace(string(f.action))
	switch {
	case f.chord == "":
		return "Press the key to bind first"
	case action == "":
		return "Name the action"
	case action != keymap.ActionNoop && action != keymap.ActionUnbind && !strings.Contains(action, ".") && !slices.Contains(e.actions, action):
		return fmt.Sprintf("%q is not an action", action)
	}
	return ""
}

// saveForm applies the dialog to the custom list and saves it. Editing a keymap binding
// unbinds the old chord and adds the new one, and editing a custom one replaces it.
func (e *keybindEditor) saveForm() tea.Cmd {
	if msg := e.validate(); msg != "" {
		e.status, e.failed = msg, true
		return nil
	}
	f := e.form
	next := slices.Clone(e.custom)
	entry := CustomBinding{Chord: f.chord, Action: strings.TrimSpace(string(f.action)), Context: kbContexts[f.ctx]}
	switch {
	case f.orig == nil:
		next = append(next, entry)
	case f.orig.custom >= 0:
		entry.Args = next[f.orig.custom].Args
		next[f.orig.custom] = entry
	default:
		if f.orig.chord != entry.Chord || f.orig.context != entry.Context {
			next = append(next, CustomBinding{Chord: f.orig.chord, Action: keymap.ActionUnbind, Context: f.orig.context})
		}
		next = append(next, entry)
	}
	if e.apply(keepLast(next), "Set "+keymap.FormatChord(entry.Chord)+" to "+entry.Action) {
		e.mode = kbList
	}
	return nil
}

// apply makes list the pending list: the table shows the keymap with it applied, and
// nothing is written until the user saves. It reports whether the list could be applied.
func (e *keybindEditor) apply(list []CustomBinding, text string) bool {
	km, err := e.preview(list)
	l, ok := km.(BindingLister)
	switch {
	case err != nil:
	case !ok:
		err = errors.New("the keymap cannot list its bindings")
	}
	if err != nil {
		e.status, e.failed = "Not applied: "+err.Error(), true
		return false
	}
	e.custom, e.lister = list, l
	e.status, e.failed = text+" (not saved yet, Ctrl+S saves)", false
	e.rebuild()
	return true
}

// keepLast drops an entry that a later one for the same chord and context replaces, so that
// binding a key the user unbound earlier leaves one entry, not a clash.
func keepLast(list []CustomBinding) []CustomBinding {
	var out []CustomBinding
	for i, c := range list {
		later := slices.ContainsFunc(list[i+1:], func(d CustomBinding) bool { return d.Chord == c.Chord && d.Context == c.Context })
		if !later {
			out = append(out, c)
		}
	}
	return out
}

// remove deletes the highlighted binding: a custom one is dropped, which gives back the
// keymap's binding of the chord, and a keymap one is unbound.
func (e *keybindEditor) remove() tea.Cmd {
	r, ok := e.selected()
	if !ok || e.store == nil {
		return nil
	}
	next := slices.Clone(e.custom)
	var text string
	if r.custom >= 0 {
		next = slices.Delete(next, r.custom, r.custom+1)
		text = "Reset " + keymap.FormatChord(r.chord) + " to the keymap"
	} else {
		next = append(next, CustomBinding{Chord: r.chord, Action: keymap.ActionUnbind, Context: r.context})
		text = "Unbound " + keymap.FormatChord(r.chord)
	}
	e.apply(next, text)
	return nil
}

// disable makes the highlighted key do nothing in its context, with a noop binding.
func (e *keybindEditor) disable() tea.Cmd {
	r, ok := e.selected()
	if !ok || e.store == nil || r.action == keymap.ActionNoop {
		return nil
	}
	next := slices.Clone(e.custom)
	if r.custom >= 0 {
		next[r.custom].Action = keymap.ActionNoop
	} else {
		next = append(next, CustomBinding{Chord: r.chord, Action: keymap.ActionNoop, Context: r.context})
	}
	e.apply(next, "Disabled "+keymap.FormatChord(r.chord))
	return nil
}

// preview is the keymap of list over the preset: with the plugin bindings when the store can
// layer them, else over the preset alone.
func (e *keybindEditor) preview(list []CustomBinding) (Keymap, error) {
	if pp, ok := e.store.(PluginPreviewer); ok && e.plugins != nil {
		return pp.PreviewWithPlugins(list, e.preset, e.plugins())
	}
	return e.store.Preview(list, e.preset)
}

// keybindSavedMsg is the answer of the command that writes the custom list.
type keybindSavedMsg struct {
	list []CustomBinding
	keys Keymap
	text string
	err  error
}

// save starts the command that writes list. The editor shows the result when it arrives.
func (e *keybindEditor) save(list []CustomBinding, text string) tea.Cmd {
	if e.saving {
		return nil
	}
	e.saving = true
	store, preset := e.store, e.preset
	return func() tea.Msg {
		km, err := store.Save(list, preset)
		return keybindSavedMsg{list: list, keys: km, text: text, err: err}
	}
}

// keybindSaved applies the answer of a save: the keymap of the next key press, the palette
// labels, and the editor's table and status line.
func (m *Model) keybindSaved(msg keybindSavedMsg) {
	var e *keybindEditor
	for _, o := range m.overlays {
		if ed, ok := o.(*keybindEditor); ok {
			e = ed
		}
	}
	if e != nil {
		e.saving = false
	}
	if msg.err != nil {
		if e != nil {
			e.status, e.failed = "Not saved: "+msg.err.Error(), true
			e.closeAfter = false
		}
		m.notice = "Keybindings: " + msg.err.Error()
		return
	}
	if msg.keys != nil {
		m.keys = m.pluginSwitchKeymap(m.util.settings.Keymap, msg.keys)
		m.relabelCommands()
	}
	m.notice = msg.text
	if e == nil {
		return
	}
	e.saved = msg.list
	e.status, e.failed = msg.text+" ("+shortPath(e.file)+")", false
	if e.closeAfter {
		e.active = false
		for len(m.overlays) > 0 && !m.overlays[len(m.overlays)-1].Active() {
			m.popOverlay()
		}
	}
}

// shortPath is the last element of a path, for a status line.
func shortPath(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// The dialog needs at least formMinW cells of width and formCompactH rows. With
// formFullH rows it spaces its fields out and shows the description of the action.
const (
	formMinW     = 30
	formCompactH = 7
	formFullH    = 11
)

// kbColumns are the widths of the table columns for a box with inner cells to spare. The
// key, context and source columns keep their room; the action id and then the description
// take what is left, and the description goes first when there is little.
type kbColumns struct{ key, action, desc, context, source int }

func kbColumnsFor(inner int) kbColumns {
	c := kbColumns{key: 16, context: 16, source: 6}
	rest := inner - c.key - c.context - c.source
	if rest < 12 { // very narrow: the context gives way
		c.context = max(inner-c.key-c.source-12, 8)
		rest = inner - c.key - c.context - c.source
	}
	rest = max(rest, 0)
	c.action = min(max(rest/2, 12), 26)
	if rest-c.action < 10 {
		c.action, c.desc = rest, 0
	} else {
		c.desc = rest - c.action
	}
	return c
}

// Draw paints the table, and the dialog over it.
func (e *keybindEditor) Draw(g *grid.Grid) {
	box := list.Centred(g.Width(), g.Height(), 100, list.MaxHeight)
	e.box, e.drawn = box, true
	if box.W == 0 {
		if e.mode == kbEdit || e.mode == kbRecord {
			e.mode = kbList
		}
		return
	}
	e.page = max(box.H-4, 1)
	e.keepVisible()
	if (e.mode == kbEdit || e.mode == kbRecord) && !canShowForm(box) {
		// The dialog cannot be shown, so it must not keep taking keys unseen.
		e.mode = kbList
		e.status, e.failed = "Terminal too small for the dialog", true
	}
	ov := e.chrome
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, "Keybinding Editor")
	end := box.X + box.W - 2
	x0 := box.X + 2
	col := kbColumnsFor(end - x0)

	filtering := e.searching || len(e.filter) > 0 || e.keyFilter != "" || e.ctxFilter != "" || e.srcFilter != ""
	var head string
	if filtering {
		head = e.filterLine()
	} else {
		head = space.PadTo("Key", col.key) + space.PadTo("Action", col.action)
		if col.desc > 0 {
			head += space.PadTo("Description", col.desc)
		}
		head += space.PadTo("Context", col.context) + "Source"
	}
	g.Put(x0, box.Y+1, end, space.End(head, end-x0), ov.Muted)

	for k := range e.page {
		idx := e.top + k
		if idx >= len(e.shown) {
			break
		}
		r := e.rows[e.shown[idx]]
		y := box.Y + 2 + k
		st := ov.Box
		if idx == e.sel {
			st = ov.Selected
			g.Fill(box.X+1, box.X+box.W-1, y, st)
		}
		action, desc := r.action, e.describe(r.action)
		if r.action == keymap.ActionNoop {
			action, desc = keymap.ActionNoop, "(disabled)"
		}
		line := space.PadTo(space.End(keymap.FormatChord(r.chord), col.key-1), col.key) +
			space.PadTo(space.End(action, col.action-1), col.action)
		if col.desc > 0 {
			line += space.PadTo(space.End(desc, col.desc-1), col.desc)
		}
		line += space.PadTo(space.End(r.context, col.context-1), col.context) + e.source(r)
		g.Put(x0, y, end, space.End(line, end-x0), st)
	}
	if len(e.shown) == 0 {
		g.Put(x0, box.Y+2, end, "No matching bindings", ov.Muted)
	}
	statusStyle := ov.Muted
	status := e.status
	switch {
	case e.mode == kbConfirm:
		status, statusStyle = "You have unsaved keybinding changes", ov.Error
	case e.failed:
		statusStyle = ov.Error
	case status == "" && e.dirty():
		status = "Unsaved changes: Ctrl+S saves them"
	}
	g.Put(x0, box.Y+box.H-2, end, space.End(status, end-x0), statusStyle)
	if e.mode == kbEdit || e.mode == kbRecord {
		// The dialog carries the keys of its mode on its own bottom border.
		e.drawForm(g, box)
	} else {
		ov.PaintFooter(g, box.X, box.Y+box.H-1, box.W, e.hint(box.W-2*space.PadX-3))
	}
}

// filterLine is the header while a filter is on: the search and the context, source and key
// filters that apply.
func (e *keybindEditor) filterLine() string {
	var parts []string
	switch {
	case e.searching && e.searchKey:
		parts = append(parts, "Key: (press the key to find)_")
	case e.searching || len(e.filter) > 0:
		text := "Filter: " + string(e.filter)
		if e.searching {
			text += "_"
		}
		parts = append(parts, text)
	}
	if e.keyFilter != "" {
		parts = append(parts, "Key: "+keymap.FormatChord(e.keyFilter))
	}
	if e.ctxFilter != "" {
		parts = append(parts, "Context: "+e.ctxFilter)
	}
	if e.srcFilter != "" {
		parts = append(parts, "Source: "+e.srcFilter)
	}
	return strings.Join(parts, "   ")
}

// hint is the footer: the keys that work in the current mode, in the longest wording that
// fits in width cells, always ending in the key that closes.
func (e *keybindEditor) hint(width int) string {
	var options []string
	switch {
	case e.mode == kbConfirm:
		options = []string{"S save and close, D discard, Esc keep editing", "S save, D discard, Esc back"}
	case e.mode == kbRecord:
		options = []string{"Press the key to bind (any key)", "Press the key"}
	case e.mode == kbEdit && e.form.field == 0:
		options = []string{"Press the new key, Enter record any key, Tab next field, Esc cancel", "Press a key, Enter any key, Tab next, Esc cancel", "Tab next, Esc cancel"}
	case e.mode == kbEdit && e.form.field == 2:
		options = []string{"Left/Right context, Enter or Ctrl+S apply, Tab next field, Esc cancel", "Left/Right context, Enter apply, Esc cancel", "Enter apply, Esc cancel"}
	case e.mode == kbEdit:
		options = []string{"Type the action, Up/Down pick, Tab next field, Ctrl+S apply, Esc cancel", "Type the action, Tab next, Ctrl+S apply, Esc cancel", "Ctrl+S apply, Esc cancel"}
	case e.searching && e.searchKey:
		options = []string{"Press the key to find, Tab text search, Esc clear", "Press a key, Esc clear"}
	case e.searching:
		options = []string{"Type to filter, Tab search by key, Enter keep, Esc clear", "Type to filter, Enter keep, Esc clear", "Enter keep, Esc clear"}
	case e.store == nil:
		options = []string{"Up/Down move, / filter, r key, c context, s source, Esc close", "Up/Down move, / filter, Esc close", "Esc close"}
	default:
		r, _ := e.selected()
		del := "d unbind"
		if r.custom >= 0 {
			del = "d reset"
		}
		// Every option keeps Ctrl+S save: a narrow footer drops the other keys first, and saving
		// is the one a user must not lose sight of.
		options = []string{
			"Enter edit, a add, " + del + ", x disable, / filter, r key, c ctx, s src, Ctrl+S save, Esc close",
			"Enter edit, a add, " + del + ", x disable, / filter, Ctrl+S save, Esc close",
			"Enter edit, a add, " + del + ", x disable, Ctrl+S save, Esc close",
			"Enter edit, a add, " + del + ", Ctrl+S save, Esc close",
			"Enter edit, Ctrl+S save, Esc close",
			"Ctrl+S save, Esc close",
		}
	}
	for _, o := range options {
		if ansi.StringWidth(o) <= width {
			return o
		}
	}
	return options[len(options)-1]
}

// drawForm paints the dialog in the middle of the box, its fields directly under the top
// border and its key hints on the bottom border. A box with room for formFullH rows spaces
// the fields out; a shorter one packs them one to a row and leaves out the action's
// description.
func (e *keybindEditor) drawForm(g *grid.Grid, parent rect) {
	f := e.form
	// The dialog sits inside the table's border with a cell to spare on every side, so it never
	// paints over the border or the title. Short or narrow, it packs its fields.
	inner := rect{X: parent.X + 1, Y: parent.Y + 1, W: parent.W - 2, H: parent.H - 2}
	w, h := min(inner.W-2, 64), formFullH
	compact := inner.H-2 < formFullH
	if compact {
		h = formCompactH
	}
	box := rect{X: inner.X + (inner.W-w)/2, Y: inner.Y + (inner.H-h)/2, W: w, H: h}
	ov := e.chrome
	title := "Add binding"
	if f.orig != nil {
		title = "Edit binding"
	}
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, title)
	ov.PaintFooter(g, box.X, box.Y+box.H-1, box.W, e.hint(box.W-2*space.PadX-3))
	end := box.X + box.W - 2
	x0 := box.X + 2
	// The rows of the key, action and context fields, the message line, the suggestions and
	// the description.
	rows := struct{ key, action, ctx, msg, sug, desc int }{1, 3, 5, 7, 8, 9}
	if compact {
		rows = struct{ key, action, ctx, msg, sug, desc int }{1, 2, 3, 4, 5, -1}
	}
	label := func(row, field int, name, value string) {
		st := ov.Idle
		if f.field == field {
			st = ov.Focus
		}
		g.Put(x0, box.Y+row, end, name, ov.Muted)
		g.Fill(x0+10, end, box.Y+row, st)
		g.Put(x0+10, box.Y+row, end, space.End(value, end-x0-10), st)
	}
	keyText := "(press a key)"
	switch {
	case e.mode == kbRecord:
		keyText = "press any key…"
	case f.chord != "":
		keyText = keymap.FormatChord(f.chord)
	}
	label(rows.key, 0, "Key:", keyText)
	action := string(f.action)
	if f.field == 1 {
		action += "_"
	}
	label(rows.action, 1, "Action:", action)
	label(rows.ctx, 2, "Context:", "< "+kbContexts[f.ctx]+" >")
	line := ""
	switch {
	case e.status != "" && e.failed:
		line = e.status
	case f.chord != "":
		if c, ok := e.conflict(f.chord, kbContexts[f.ctx]); ok {
			line = fmt.Sprintf("Conflict: %s is bound to %s in %s", keymap.FormatChord(c.chord), c.action, c.context)
		}
	}
	if line != "" {
		g.Put(x0, box.Y+rows.msg, end, space.End(line, end-x0), ov.Error)
	}
	if f.field == 1 {
		sug := e.suggestions()
		x := x0
		for i, a := range sug {
			st := ov.Muted
			if i == f.suggest {
				st = ov.Selected
			}
			x += g.Put(x, box.Y+rows.sug, end, a, st) + 2
		}
		if len(sug) > 0 && rows.desc >= 0 {
			g.Put(x0, box.Y+rows.desc, end, space.End(e.describe(sug[min(f.suggest, len(sug)-1)]), end-x0), ov.Box)
		}
	} else if a := strings.TrimSpace(string(f.action)); a != "" && rows.desc >= 0 {
		g.Put(x0, box.Y+rows.desc, end, space.End(e.describe(a), end-x0), ov.Muted)
	}
}
