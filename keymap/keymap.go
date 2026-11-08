// Package keymap resolves key chords to editor actions using Fresh's keymap
// file format (crates/fresh-editor-core/keymaps/*.json).
//
// Five built-in keymaps are embedded: "default", "macos", "emacs", "vscode"
// and "macos-gui". default.json is the base. macos, emacs and vscode inherit
// from it, and macos-gui inherits from macos. They hold Fresh's bindings for the
// contexts gvim resolves (ConsultedContexts), plus a few gvim-specific keys. Default returns the keymap for
// the current platform, as Fresh does: "macos" on darwin and "default"
// elsewhere. ByName selects any of them explicitly.
//
// Chords use the canonical spelling the TUI produces: modifiers in the fixed
// order ctrl, alt, shift, super, joined to a key name with "+", for example
// "ctrl+shift+f", "alt+f", "shift+left". Named keys are lowercase ("left",
// "pagedown", "enter", "space"); function keys keep their upper-case form
// ("F10"). Fresh's "BackTab" is spelled "shift+tab".
package keymap

import (
	"bytes"
	_ "embed" // for go:embed below
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
)

//go:embed default.json
var defaultData []byte

//go:embed macos.json
var macosData []byte

//go:embed macos-gui.json
var macosGUIData []byte

//go:embed emacs.json
var emacsData []byte

//go:embed vscode.json
var vscodeData []byte

// presetFile returns the embedded keymap file for a built-in name.
func presetFile(name string) ([]byte, bool) {
	switch name {
	case "default":
		return defaultData, true
	case "macos":
		return macosData, true
	case "macos-gui":
		return macosGUIData, true
	case "emacs":
		return emacsData, true
	case "vscode":
		return vscodeData, true
	}
	return nil, false
}

// Context is a when-clause value from a keymap file. It names the UI area a
// binding applies to. Bindings with ContextGlobal apply everywhere.
type Context string

// The when-clause values that appear in the embedded keymaps.
const (
	ContextGlobal          Context = "global"
	ContextNormal          Context = "normal"
	ContextPrompt          Context = "prompt"
	ContextFileExplorer    Context = "file_explorer"
	ContextFileExplorerCS  Context = "fileExplorer" // alias; Load folds it into ContextFileExplorer
	ContextSearchPrompt    Context = "searchPrompt"
	ContextCompositeBuffer Context = "compositeBuffer"
	ContextTerminal        Context = "terminal"
	ContextPopup           Context = "popup"
	ContextMenu            Context = "menu"
	ContextSettings        Context = "settings"
	ContextDock            Context = "dock"
	ContextCompletion      Context = "completion"
)

// Binding is one keymap entry: a chord in a context mapped to an action.
// Action is the Fresh action name, for example "quick_open" or "save".
//
// A single-key binding has Chord set to its canonical chord and Sequence nil.
// A multi-key binding (the "keys" array in Fresh's schema, such as Emacs's
// C-x C-s) has Sequence set to the canonical chord of each step, and Chord set
// to those steps joined by spaces. Resolve never matches a sequence; use
// ResolveSequence for those.
type Binding struct {
	Chord    string
	Sequence []string
	Action   string
	Args     map[string]any
	When     Context
	Comment  string

	// inherited is set on a binding a keymap took from its parent, so that Primary can
	// prefer the keymap's own bindings when it picks the one to show.
	inherited bool
}

// actionTwins names, for an action, the action whose bindings also run it: Close Buffer
// ("close") is closing the current tab, which the keymaps bind as "close_tab".
var actionTwins = map[string]string{"close": "close_tab"}

// Map holds the parsed bindings of one keymap, with inherited bindings
// included. A Map is immutable after construction and is safe for concurrent
// reads.
type Map struct {
	bindings []Binding
	index    map[lookupKey]int // single-key bindings; last binding wins
	seq      map[lookupKey]int // multi-key bindings, keyed by the space-joined chord
}

type lookupKey struct {
	when  Context
	chord string
}

// rawFile and rawBinding mirror the JSON schema of the keymap files.
type rawFile struct {
	Inherits *string      `json:"inherits"`
	Bindings []rawBinding `json:"bindings"`
}

type rawBinding struct {
	Comment   string         `json:"comment"`
	Key       string         `json:"key"`
	Modifiers []string       `json:"modifiers"`
	Keys      []rawKey       `json:"keys"`
	Action    string         `json:"action"`
	Args      map[string]any `json:"args"`
	When      string         `json:"when"`
}

type rawKey struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers"`
}

var (
	defaultOnce sync.Once
	defaultMap  *Map

	presetMu    sync.Mutex
	presetCache = map[string]*Map{}
)

// DefaultName returns the built-in keymap Default uses on this platform. It
// mirrors Fresh's default_keybinding_map_name: "macos" on darwin, else
// "default".
func DefaultName() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return "default"
}

// Default returns the keymap for this platform, the same map ByName returns
// for DefaultName. The embedded files are fixed at build time, so a parse
// failure is a programming error and Default panics; TestDefaultParses guards
// it.
func Default() *Map {
	defaultOnce.Do(func() {
		m, err := ByName(DefaultName())
		if err != nil {
			panic(fmt.Sprintf("keymap: embedded %s keymap: %v", DefaultName(), err))
		}
		defaultMap = m
	})
	return defaultMap
}

// ByName returns the built-in keymap called name: "default", "macos", "emacs",
// "vscode" or "macos-gui". Inherited bindings are included, so "macos" holds
// every default binding that the macOS file does not override. Results are
// cached; repeated calls return the same *Map. An unknown name is an error.
func ByName(name string) (*Map, error) {
	return loadPreset(name, nil)
}

// loadPreset returns the built-in keymap name. stack lists the presets being
// loaded above it, so an inheritance cycle is an error instead of unbounded
// recursion. Presets are cached once they parse.
func loadPreset(name string, stack []string) (*Map, error) {
	presetMu.Lock()
	m, ok := presetCache[name]
	presetMu.Unlock()
	if ok {
		return m, nil
	}
	data, ok := presetFile(name)
	if !ok {
		return nil, fmt.Errorf("keymap: unknown keymap %q", name)
	}
	if slices.Contains(stack, name) {
		return nil, fmt.Errorf("keymap: inheritance cycle through %q", name)
	}
	m, err := parse(data, append(stack, name))
	if err != nil {
		return nil, fmt.Errorf("keymap: %s: %w", name, err)
	}
	presetMu.Lock()
	defer presetMu.Unlock()
	if prev, ok := presetCache[name]; ok {
		return prev, nil
	}
	presetCache[name] = m
	return m, nil
}

// Load parses a keymap file in Fresh's schema. Every binding needs an action
// and either key or keys; an empty when defaults to ContextGlobal, and the
// alternative spellings "fileExplorer", "search_prompt" and "composite_buffer"
// are folded into the Context constants. Unknown modifiers are an error, so a
// typo cannot silently drop a binding.
//
// If the file names an "inherits" keymap, that built-in keymap's bindings come
// first and the file's own bindings override them. The parent must be one of
// the built-in names that ByName accepts.
func Load(data []byte) (*Map, error) {
	return parse(data, nil)
}

// overrideFile is the JSON object WithOverrides reads. Only "bindings" is
// allowed, so a misspelled key is an error and cannot silently drop bindings.
type overrideFile struct {
	Bindings []rawBinding `json:"bindings"`
}

// WithOverrides returns a new Map with the bindings in data applied on top of
// m, as a child keymap file applies them to its parent. data is a JSON object in
// the keymap file schema with only "bindings", for example
//
//	{"bindings": [{"key": "s", "modifiers": ["ctrl"], "action": "noop", "when": "normal"}]}
//
// A binding replaces the entry m has for the same context and chord, and a
// "noop" binding unbinds the chord. A global binding, noop or not, also replaces
// the chord in each non-global context where m binds it, because Resolve checks
// the context before the global bindings. A binding for that context in data
// still wins over the replacement. The entries of data are checked the way Load
// checks a file: one chord bound to two actions is an error. m is not changed,
// because built-in maps are shared. On error no map is returned.
func (m *Map) WithOverrides(data []byte) (*Map, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f overrideFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("keymap: overrides: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("keymap: overrides: unexpected data after the object")
	}
	own := make([]Binding, 0, len(f.Bindings))
	for i, rb := range f.Bindings {
		b, err := rb.binding()
		if err != nil {
			return nil, fmt.Errorf("keymap: overrides: binding %d: %w", i, err)
		}
		own = append(own, b)
	}
	if err := checkConflicts(own); err != nil {
		return nil, err
	}
	list := make([]Binding, 0, len(m.bindings)+len(own))
	list = append(list, m.bindings...)
	list = append(list, m.expandGlobal(own)...)
	list = append(list, own...)
	return newMap(list), nil
}

// expandGlobal returns a copy of each global binding in own for each non-global
// context where m binds the same chord. Without these copies the preset's
// context binding would still win in its context. They go before own, so an
// explicit binding in own for the same context and chord still wins.
func (m *Map) expandGlobal(own []Binding) []Binding {
	var out []Binding
	seen := make(map[lookupKey]bool)
	for _, g := range own {
		if g.When != ContextGlobal {
			continue
		}
		for _, b := range m.bindings {
			k := lookupKey{b.When, b.Chord}
			if b.Chord != g.Chord || b.When == ContextGlobal || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, Binding{Chord: g.Chord, Sequence: g.Sequence, Action: g.Action, Args: g.Args, When: b.When})
		}
	}
	return out
}

func parse(data []byte, stack []string) (*Map, error) {
	var f rawFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("keymap: parse: %w", err)
	}
	own := make([]Binding, 0, len(f.Bindings))
	for i, rb := range f.Bindings {
		b, err := rb.binding()
		if err != nil {
			return nil, fmt.Errorf("keymap: binding %d: %w", i, err)
		}
		own = append(own, b)
	}
	if err := checkConflicts(own); err != nil {
		return nil, err
	}
	var list []Binding
	if f.Inherits != nil && *f.Inherits != "" {
		parent, err := loadPreset(*f.Inherits, stack)
		if err != nil {
			return nil, fmt.Errorf("keymap: inherits %q: %w", *f.Inherits, err)
		}
		for _, b := range parent.bindings {
			b.inherited = true
			list = append(list, b)
		}
	}
	list = append(list, own...)
	return newMap(list), nil
}

// checkConflicts rejects a file that binds one chord in one context to two
// different actions, or to one action with different args. Repeating an
// identical entry is allowed, because Fresh's default keymap lists the BackTab
// alias of dedent_selection three times. Overrides of inherited bindings are
// not checked here; a child preset replaces its parent's entry on purpose.
func checkConflicts(own []Binding) error {
	seen := make(map[lookupKey]Binding, len(own))
	for _, b := range own {
		k := lookupKey{b.When, b.Chord}
		prev, ok := seen[k]
		if !ok {
			seen[k] = b
			continue
		}
		if prev.Action != b.Action || !sameArgs(prev.Args, b.Args) {
			return fmt.Errorf("keymap: %s in %s is bound to both %q and %q", b.Chord, b.When, prev.Action, b.Action)
		}
	}
	return nil
}

// sameArgs treats a nil args map and an empty one as equal.
func sameArgs(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// binding validates one raw entry and converts it to a Binding.
func (rb rawBinding) binding() (Binding, error) {
	if rb.Action == "" {
		return Binding{}, fmt.Errorf("action is required")
	}
	steps := rb.Keys
	if rb.Key != "" {
		if len(steps) > 0 {
			return Binding{}, fmt.Errorf("give key or keys, not both")
		}
		steps = []rawKey{{Key: rb.Key, Modifiers: rb.Modifiers}}
	}
	if len(steps) == 0 {
		return Binding{}, fmt.Errorf("key or keys is required")
	}
	chords := make([]string, len(steps))
	for i, s := range steps {
		c, err := Canonical(s.Key, s.Modifiers)
		if err != nil {
			return Binding{}, err
		}
		chords[i] = c
	}
	when, err := canonicalContext(rb.When)
	if err != nil {
		return Binding{}, err
	}
	b := Binding{Action: rb.Action, Args: rb.Args, When: when, Comment: rb.Comment}
	if len(chords) == 1 {
		b.Chord = chords[0]
	} else {
		b.Sequence = chords
		b.Chord = strings.Join(chords, " ")
	}
	return b, nil
}

// ConsultedContexts are the contexts gvim resolves a key in: the editor (normal, with global as
// the fallback), the file explorer, the terminal dock, and the overlay contexts whose surfaces
// consult the keymap before their own key handlers (the prompt, the find bar, lists and popups,
// the menu bar, the settings page, the dock). A user binding in any other context does nothing;
// the keybinding editor lists these contexts only, and a binding in another one is skipped with
// a warning.
var ConsultedContexts = []Context{
	ContextGlobal, ContextNormal,
	ContextPrompt, ContextSearchPrompt, ContextPopup, ContextMenu, ContextSettings,
	ContextFileExplorer, ContextTerminal, ContextDock,
}

// OverlayContexts are the contexts of surfaces that take their keys in code and consult the
// keymap first. A binding there applies while the surface is on top. Only single-key bindings
// apply: the overlay key router resolves one key at a time, so a sequence in one of these
// contexts is skipped with a warning.
var OverlayContexts = []Context{
	ContextPrompt, ContextSearchPrompt, ContextPopup, ContextMenu, ContextSettings, ContextDock,
}

// Consulted reports whether gvim resolves keys in the context named by when, one of
// ConsultedContexts.
func Consulted(when Context) bool {
	return slices.Contains(ConsultedContexts, when)
}

// IsOverlayContext reports whether when is one of OverlayContexts.
func IsOverlayContext(when Context) bool {
	return slices.Contains(OverlayContexts, when)
}

// ConsultedNames lists the names of ConsultedContexts in order, for a warning or a document.
func ConsultedNames() []string {
	out := make([]string, len(ConsultedContexts))
	for i, c := range ConsultedContexts {
		out[i] = string(c)
	}
	return out
}

// knownContexts lists every when-clause value the port accepts, in the spelling
// the embedded keymaps use. Fresh's default keymap uses exactly these.
var knownContexts = []Context{
	ContextGlobal, ContextNormal, ContextPrompt, ContextFileExplorer,
	ContextSearchPrompt, ContextCompositeBuffer, ContextTerminal, ContextPopup,
	ContextMenu, ContextSettings, ContextDock, ContextCompletion,
}

// canonicalContext maps a when-clause to a Context, folding Fresh's alternative
// spellings the way Fresh's KeyContext::from_when_clause does. Any other value
// is an error, so a typo cannot create a binding that never fires. Mode clauses
// ("mode:...") are rejected because the port has no modes yet.
func canonicalContext(when string) (Context, error) {
	switch when {
	case "":
		return ContextGlobal, nil
	case string(ContextFileExplorerCS):
		return ContextFileExplorer, nil
	case "search_prompt":
		return ContextSearchPrompt, nil
	case "composite_buffer":
		return ContextCompositeBuffer, nil
	}
	for _, c := range knownContexts {
		if string(c) == when {
			return c, nil
		}
	}
	return "", fmt.Errorf("unknown when clause %q", when)
}

// newMap indexes list. Later entries override earlier ones for the same
// (context, chord), so inherited bindings are replaced by the child's.
func newMap(list []Binding) *Map {
	m := &Map{
		bindings: list,
		index:    make(map[lookupKey]int, len(list)),
		seq:      make(map[lookupKey]int),
	}
	for i, b := range list {
		k := lookupKey{b.When, b.Chord}
		if b.Sequence != nil {
			m.seq[k] = i
		} else {
			m.index[k] = i
		}
	}
	return m
}

// Bindings returns every binding in file order, inherited bindings first. The
// slice is a copy.
func (m *Map) Bindings() []Binding {
	return append([]Binding(nil), m.bindings...)
}

// Resolve returns the binding for a single-key chord in a context. The chord
// may be given in any modifier order and with the key name in any case; it is
// normalized with Normalize first. Bindings for ctx win; if none exists, a
// ContextGlobal binding applies. ok is false when neither exists.
func (m *Map) Resolve(chord string, ctx Context) (Binding, bool) {
	c, err := Normalize(chord)
	if err != nil {
		return Binding{}, false
	}
	return m.lookup(m.index, append([]string{c}, terminalEquivalents(c)...), ctx)
}

// ResolveSequence returns the binding for a sequence of chords typed in order,
// such as []string{"ctrl+x", "ctrl+s"}. Each step is normalized. A one-step
// sequence resolves like Resolve. ok is false when no binding matches.
func (m *Map) ResolveSequence(steps []string, ctx Context) (Binding, bool) {
	if len(steps) == 0 {
		return Binding{}, false
	}
	canon := make([]string, len(steps))
	for i, s := range steps {
		c, err := Normalize(s)
		if err != nil {
			return Binding{}, false
		}
		canon[i] = c
	}
	if len(canon) == 1 {
		return m.lookup(m.index, append([]string{canon[0]}, terminalEquivalents(canon[0])...), ctx)
	}
	return m.lookup(m.seq, []string{strings.Join(canon, " ")}, ctx)
}

// HasSequencePrefix reports whether steps are the leading chords of a multi-key
// binding that ctx or the global context holds, so that one more key may complete
// it. Each step is normalized. A sequence that ends at steps is not a longer one,
// so only a binding with more steps counts. It is false for no steps.
func (m *Map) HasSequencePrefix(steps []string, ctx Context) bool {
	if len(steps) == 0 {
		return false
	}
	canon := make([]string, len(steps))
	for i, s := range steps {
		c, err := Normalize(s)
		if err != nil {
			return false
		}
		canon[i] = c
	}
	prefix := strings.Join(canon, " ") + " "
	for k := range m.seq {
		if (k.when == ctx || k.when == ContextGlobal) && strings.HasPrefix(k.chord, prefix) {
			return true
		}
	}
	return false
}

// lookup finds the first of chords that ctx binds, and then the first that the
// global context binds. chords lists the chord as pressed, then the chords whose
// binding it stands for on a terminal (see terminalEquivalents). A binding of the
// chord itself therefore wins over a binding of an equivalent one.
func (m *Map) lookup(idx map[lookupKey]int, chords []string, ctx Context) (Binding, bool) {
	for _, chord := range chords {
		if i, ok := idx[lookupKey{ctx, chord}]; ok {
			return m.bindings[i], true
		}
	}
	if ctx != ContextGlobal {
		for _, chord := range chords {
			if i, ok := idx[lookupKey{ContextGlobal, chord}]; ok {
				return m.bindings[i], true
			}
		}
	}
	return Binding{}, false
}

// BindingsFor returns every binding that runs action, in file order.
func (m *Map) BindingsFor(action string) []Binding {
	var out []Binding
	for _, b := range m.bindings {
		if b.Action == action {
			out = append(out, b)
		}
	}
	return out
}

// Primary returns the binding a UI should show for action. A preset's own binding comes
// before one it only inherits, so Emacs shows Save As as C-x C-w and not the inherited
// Ctrl+Shift+S. Among bindings of one origin a single chord beats a multi-key sequence:
// Emacs shows Undo as Ctrl+/ and not C-x u, but Save as C-x C-s because it has no single
// chord. The normal and global contexts come before any other context, and normal before
// global for one kind of binding. Within one tier the simplest single chord wins, using the
// rule Fresh's menu shortcuts follow: fewest modifiers, then lowest modifier bits, then
// chord text. Among sequences the shorter wins, then the chord text. Only bindings the
// editor runs count. An inherited binding that the child keymap rebinds is skipped, a global
// binding that a normal binding of the same chord takes is skipped, and a single chord that
// starts a sequence is skipped because it is a prefix and never runs by itself. An action
// with a twin in actionTwins also counts the twin's bindings: Close Buffer shows the key
// that closes the tab. ok is false when the action has no such binding.
func (m *Map) Primary(action string) (Binding, bool) {
	// best is indexed by origin (own, inherited), kind (single, sequence) and tier (normal,
	// global, other).
	var best [2][2][3]*Binding
	for i := range m.bindings {
		b := &m.bindings[i]
		if (b.Action != action && b.Action != actionTwins[action]) || !m.effective(i) {
			continue
		}
		tier := 2
		switch b.When {
		case ContextNormal:
			tier = 0
		case ContextGlobal:
			tier = 1
		}
		origin, kind := 0, 0
		if b.inherited {
			origin = 1
		}
		if b.Sequence != nil {
			kind = 1
		}
		if cur := &best[origin][kind][tier]; *cur == nil || simpler(*b, **cur) {
			*cur = b
		}
	}
	// The keys of the editor itself, own before inherited and single before sequence, come
	// before the keys of other contexts, which a menu or palette does not show.
	for origin := range best {
		for kind := range best[origin] {
			for tier := range 2 {
				if b := best[origin][kind][tier]; b != nil {
					return *b, true
				}
			}
		}
	}
	for kind := range 2 {
		for origin := range best {
			if b := best[origin][kind][2]; b != nil {
				return *b, true
			}
		}
	}
	return Binding{}, false
}

// simpler reports whether a is the simpler binding to show than b, which has the same
// kind: by chordRank for single chords, by step count and then chord text for sequences.
func simpler(a, b Binding) bool {
	if a.Sequence == nil {
		return rankOf(a.Chord).less(rankOf(b.Chord))
	}
	if len(a.Sequence) != len(b.Sequence) {
		return len(a.Sequence) < len(b.Sequence)
	}
	return a.Chord < b.Chord
}

// effective reports whether binding i is in effect for its chord. A
// child keymap that rebinds a chord replaces the inherited entry, so the
// inherited one is not in effect and must not be shown as the shortcut. A global
// binding is also not in effect when the normal context binds the same chord,
// because Resolve checks the normal context before the global bindings.
func (m *Map) effective(i int) bool {
	b := m.bindings[i]
	if b.Sequence != nil {
		return m.seq[lookupKey{b.When, b.Chord}] == i
	}
	if b.When == ContextGlobal {
		if _, shadowed := m.index[lookupKey{ContextNormal, b.Chord}]; shadowed {
			return false
		}
	}
	if m.index[lookupKey{b.When, b.Chord}] != i {
		return false
	}
	// A chord that starts a sequence is a prefix and does not run on its own.
	return !m.HasSequencePrefix([]string{b.Chord}, b.When)
}

// chordRank orders single chords by how simple they are, for Primary.
type chordRank struct {
	mods  int
	bits  int
	chord string
}

// modifierBits matches the bit values Fresh's menu tie-break uses.
var modifierBits = []struct {
	name string
	bit  int
}{{"ctrl", 2}, {"alt", 4}, {"shift", 1}, {"super", 8}}

func rankOf(chord string) chordRank {
	r := chordRank{chord: chord}
	rest := chord
	for _, m := range modifierBits {
		if after, ok := strings.CutPrefix(rest, m.name+"+"); ok {
			rest = after
			r.mods++
			r.bits += m.bit
		}
	}
	return r
}

func (a chordRank) less(b chordRank) bool {
	if a.mods != b.mods {
		return a.mods < b.mods
	}
	if a.bits != b.bits {
		return a.bits < b.bits
	}
	return a.chord < b.chord
}

var modifierOrder = []string{"ctrl", "alt", "shift", "super"}

// functionKey matches f1 to f35, the range Fresh's keyboard.md documents.
var functionKey = regexp.MustCompile(`^[Ff]([1-9]|[12][0-9]|3[0-5])$`)

// modifierAliases are the modifier spellings Fresh's parse_modifiers accepts besides
// the canonical four: control is ctrl, and cmd, command and meta are super.
var modifierAliases = map[string]string{
	"control": "ctrl", "cmd": "super", "command": "super", "meta": "super",
}

// modifierName returns the canonical modifier for a spelling, folding case and the
// aliases of modifierAliases. ok is false for anything that is not a modifier.
func modifierName(s string) (string, bool) {
	s = strings.ToLower(s)
	if a, ok := modifierAliases[s]; ok {
		s = a
	}
	return s, contains(modifierOrder, s)
}

var namedKeys = map[string]string{
	"backspace": "backspace",
	"backtab":   "shift+tab", // Fresh's BackTab is Tab with Shift
	"delete":    "delete",
	"down":      "down",
	"end":       "end",
	"enter":     "enter",
	"escape":    "escape",
	"home":      "home",
	"insert":    "insert",
	"left":      "left",
	"pagedown":  "pagedown",
	"pageup":    "pageup",
	"right":     "right",
	"tab":       "tab",
	"up":        "up",
	" ":         "space",
	"space":     "space",

	// The aliases and the non-printing keys of Fresh's docs/configuration/keyboard.md.
	"esc": "escape", "ret": "enter", "return": "enter", "bs": "backspace", "del": "delete",
	"ins": "insert", "spc": "space", "pgup": "pageup", "pgdn": "pagedown",
	"capslock": "capslock", "scrolllock": "scrolllock", "numlock": "numlock",
	"printscreen": "printscreen", "pause": "pause", "menu": "menu",

	// X11 keysym spellings of punctuation.
	"asterisk": "*", "star": "*", "plus": "+", "minus": "-", "hyphen": "-", "slash": "/",
	"period": ".", "dot": ".", "equal": "=", "equals": "=", "backslash": `\`, "comma": ",",
	"semicolon": ";", "colon": ":", "apostrophe": "'", "quote": "'", "quotedbl": `"`,
	"doublequote": `"`, "grave": "`", "backtick": "`", "tilde": "~", "exclam": "!",
	"exclamation": "!", "at": "@", "numbersign": "#", "hash": "#", "dollar": "$",
	"percent": "%", "asciicircum": "^", "caret": "^", "ampersand": "&", "underscore": "_",
	"bar": "|", "pipe": "|", "question": "?", "less": "<", "lessthan": "<", "greater": ">",
	"greaterthan": ">", "parenleft": "(", "parenright": ")", "bracketleft": "[",
	"bracketright": "]", "braceleft": "{", "braceright": "}",

	// The keypad keys are aliases of the main keyboard, as in Fresh; kp_begin is the one
	// key with no main-keyboard twin.
	"kp_0": "0", "kp_1": "1", "kp_2": "2", "kp_3": "3", "kp_4": "4", "kp_5": "5",
	"kp_6": "6", "kp_7": "7", "kp_8": "8", "kp_9": "9", "kp_decimal": ".", "kp_divide": "/",
	"kp_multiply": "*", "kp_subtract": "-", "kp_add": "+", "kp_enter": "enter",
	"kp_equal": "=", "kp_separator": ",", "kp_left": "left", "kp_right": "right",
	"kp_up": "up", "kp_down": "down", "kp_page_up": "pageup", "kp_page_down": "pagedown",
	"kp_home": "home", "kp_end": "end", "kp_insert": "insert", "kp_delete": "delete",
	"kp_begin": "keypadbegin",
}

// Canonical builds a chord from a keymap-file key and its modifiers, using the
// same spelling Normalize produces. The plus key is "+", or "ctrl++" with
// modifiers; Normalize reads both back.
func Canonical(key string, modifiers []string) (string, error) {
	mods := make(map[string]bool, len(modifiers))
	for _, m := range modifiers {
		name, ok := modifierName(m)
		if !ok {
			return "", fmt.Errorf("unknown modifier %q", strings.ToLower(m))
		}
		mods[name] = true
	}
	name, err := keyName(key)
	if err != nil {
		return "", err
	}
	// BackTab is already "shift+tab"; fold its shift into the modifier set.
	if after, ok := strings.CutPrefix(name, "shift+"); ok {
		name = after
		mods["shift"] = true
	}
	return join(mods, name), nil
}

// Normalize rewrites a chord such as "Shift+Ctrl+F" into canonical form,
// "ctrl+shift+f". It is the function the TUI should use on every key it
// receives, so that the spelling it compares against always matches.
func Normalize(chord string) (string, error) {
	mods := make(map[string]bool, 3)
	var key string
	for _, part := range chordParts(chord) {
		if name, ok := modifierName(part); ok {
			mods[name] = true
			continue
		}
		if part == "" {
			return "", fmt.Errorf("keymap: %q: empty key name", chord)
		}
		if key != "" {
			return "", fmt.Errorf("keymap: %q has more than one key", chord)
		}
		key = part
	}
	name, err := keyName(key)
	if err != nil {
		return "", fmt.Errorf("keymap: %q: %w", chord, err)
	}
	if after, ok := strings.CutPrefix(name, "shift+"); ok {
		name = after
		mods["shift"] = true
	}
	return join(mods, name), nil
}

// chordParts splits a chord at its "+" separators. The plus key is "+" as well,
// so Canonical writes it last: "+" alone, or after the modifiers as in "ctrl++".
// A plain split would read those as trailing empty parts.
func chordParts(chord string) []string {
	switch {
	case chord == "+":
		return []string{"+"}
	case len(chord) > 2 && strings.HasSuffix(chord, "++"):
		return append(strings.Split(chord[:len(chord)-2], "+"), "+")
	}
	return strings.Split(chord, "+")
}

func keyName(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("empty key name")
	}
	if functionKey.MatchString(key) {
		return strings.ToUpper(key[:1]) + key[1:], nil
	}
	if name, ok := namedKeys[strings.ToLower(key)]; ok {
		return name, nil
	}
	if len([]rune(key)) == 1 {
		return strings.ToLower(key), nil
	}
	return "", fmt.Errorf("unknown key name %q", key)
}

func join(mods map[string]bool, name string) string {
	var b strings.Builder
	for _, m := range modifierOrder {
		if mods[m] {
			b.WriteString(m)
			b.WriteByte('+')
		}
	}
	b.WriteString(name)
	return b.String()
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}
