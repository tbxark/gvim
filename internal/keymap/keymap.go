// Package keymap resolves key chords to editor actions using Fresh's keymap
// file format (crates/fresh-editor-core/keymaps/*.json).
//
// Five built-in keymaps are embedded: "default", "macos", "emacs", "vscode"
// and "macos-gui". default.json is the base. macos, emacs and vscode inherit
// from it, and macos-gui inherits from macos. Default returns the keymap for
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
	_ "embed" // for go:embed below
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"
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
}

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

func parse(data []byte, stack []string) (*Map, error) {
	var f rawFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("keymap: parse: %w", err)
	}
	var list []Binding
	if f.Inherits != nil && *f.Inherits != "" {
		parent, err := loadPreset(*f.Inherits, stack)
		if err != nil {
			return nil, fmt.Errorf("keymap: inherits %q: %w", *f.Inherits, err)
		}
		list = append(list, parent.bindings...)
	}
	for i, rb := range f.Bindings {
		b, err := rb.binding()
		if err != nil {
			return nil, fmt.Errorf("keymap: binding %d: %w", i, err)
		}
		list = append(list, b)
	}
	return newMap(list), nil
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
	b := Binding{Action: rb.Action, Args: rb.Args, When: canonicalContext(rb.When), Comment: rb.Comment}
	if len(chords) == 1 {
		b.Chord = chords[0]
	} else {
		b.Sequence = chords
		b.Chord = strings.Join(chords, " ")
	}
	return b, nil
}

// canonicalContext maps a when-clause to a Context, folding Fresh's alternative
// spellings the way Fresh's KeyContext::from_when_clause does.
func canonicalContext(when string) Context {
	switch when {
	case "":
		return ContextGlobal
	case string(ContextFileExplorerCS):
		return ContextFileExplorer
	case "search_prompt":
		return ContextSearchPrompt
	case "composite_buffer":
		return ContextCompositeBuffer
	}
	return Context(when)
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
	return m.lookup(m.index, c, ctx)
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
		return m.lookup(m.index, canon[0], ctx)
	}
	return m.lookup(m.seq, strings.Join(canon, " "), ctx)
}

func (m *Map) lookup(idx map[lookupKey]int, chord string, ctx Context) (Binding, bool) {
	if i, ok := idx[lookupKey{ctx, chord}]; ok {
		return m.bindings[i], true
	}
	if ctx != ContextGlobal {
		if i, ok := idx[lookupKey{ContextGlobal, chord}]; ok {
			return m.bindings[i], true
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

// Primary returns the binding a UI should show for action. It looks in
// ContextNormal first, then ContextGlobal, then any other context. Within one
// tier the simplest chord wins, using the rule Fresh's menu shortcuts follow:
// single-key before multi-key, then fewest modifiers, then lowest modifier
// bits, then chord text. On the macOS keymap that shows Redo as Ctrl+R, where
// default.json also binds Ctrl+Y. Only bindings in effect count: an inherited
// binding that the child keymap rebinds is skipped. ok is false when the action
// has no binding.
func (m *Map) Primary(action string) (Binding, bool) {
	var best [3]*Binding // normal, global, other
	for i := range m.bindings {
		b := &m.bindings[i]
		if b.Action != action || !m.effective(i) {
			continue
		}
		tier := 2
		switch b.When {
		case ContextNormal:
			tier = 0
		case ContextGlobal:
			tier = 1
		}
		if best[tier] == nil || rankOf(b.Chord).less(rankOf(best[tier].Chord)) {
			best[tier] = b
		}
	}
	for _, b := range best {
		if b != nil {
			return *b, true
		}
	}
	return Binding{}, false
}

// MenuEntry is one menu title or dropdown item that needs an accelerator.
// Action is the action the item runs and is empty for a menu title. Letter is
// the preferred accelerator. It is matched inside Label, exact case first and
// then either case. Zero, or a letter that Label lacks or an earlier entry
// already took, falls back to the first free letter of Label.
type MenuEntry struct {
	Label  string
	Action string
	Letter rune
}

// Accelerator is the accelerator of one MenuEntry, as a menu shows it.
type Accelerator struct {
	Label   string
	Letter  rune    // accelerator letter as it appears in Label; 0 when none
	Index   int     // rune index of Letter in Label, for underlining; -1 when none
	Binding Binding // Primary binding of the entry's action
	Bound   bool    // false when the entry has no action or the action is unbound
}

// Accelerators labels the entries of one menu, in order. Preferred letters are
// placed first, then each remaining entry takes the first free letter of its
// label, scanning left to right. Letters are unique within the call, compared
// case-insensitively. Each entry also gets the Primary binding of its action,
// which is the shortcut the menu shows.
func (m *Map) Accelerators(entries []MenuEntry) []Accelerator {
	out := make([]Accelerator, len(entries))
	taken := make(map[rune]bool, len(entries))
	assign := func(i, at int) {
		r := []rune(entries[i].Label)[at]
		out[i].Letter, out[i].Index = r, at
		taken[unicode.ToLower(r)] = true
	}
	for i, e := range entries {
		out[i] = Accelerator{Label: e.Label, Index: -1}
		if e.Letter == 0 {
			continue
		}
		at := indexOfLetter(e.Label, e.Letter, taken)
		if at >= 0 {
			assign(i, at)
		}
	}
	for i, e := range entries {
		if out[i].Letter == 0 {
			for at, r := range []rune(e.Label) {
				if unicode.IsLetter(r) && !taken[unicode.ToLower(r)] {
					assign(i, at)
					break
				}
			}
		}
		if e.Action == "" {
			continue
		}
		if b, ok := m.Primary(e.Action); ok {
			out[i].Binding, out[i].Bound = b, true
		}
	}
	return out
}

// indexOfLetter returns the rune index of want in label, preferring an exact
// case match, or -1 when label lacks it or want is already taken.
func indexOfLetter(label string, want rune, taken map[rune]bool) int {
	if taken[unicode.ToLower(want)] {
		return -1
	}
	runes := []rune(label)
	for i, r := range runes {
		if r == want {
			return i
		}
	}
	for i, r := range runes {
		if unicode.ToLower(r) == unicode.ToLower(want) {
			return i
		}
	}
	return -1
}

// effective reports whether binding i is the one Resolve uses for its context
// and chord. A child keymap that rebinds a chord replaces the inherited entry,
// so the inherited one must not be shown as the shortcut.
func (m *Map) effective(i int) bool {
	b := m.bindings[i]
	idx := m.index
	if b.Sequence != nil {
		idx = m.seq
	}
	return idx[lookupKey{b.When, b.Chord}] == i
}

// chordRank orders chords by how simple they are, for Primary.
type chordRank struct {
	sequence bool
	mods     int
	bits     int
	chord    string
}

// modifierBits matches the bit values Fresh's menu tie-break uses.
var modifierBits = []struct {
	name string
	bit  int
}{{"ctrl", 2}, {"alt", 4}, {"shift", 1}, {"super", 8}}

func rankOf(chord string) chordRank {
	r := chordRank{chord: chord, sequence: strings.Contains(chord, " ")}
	if r.sequence {
		return r
	}
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
	if a.sequence != b.sequence {
		return !a.sequence
	}
	if a.mods != b.mods {
		return a.mods < b.mods
	}
	if a.bits != b.bits {
		return a.bits < b.bits
	}
	return a.chord < b.chord
}

var modifierOrder = []string{"ctrl", "alt", "shift", "super"}

var functionKey = regexp.MustCompile(`^[Ff]([1-9]|1[0-2])$`)

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
}

// Canonical builds a chord from a keymap-file key and its modifiers, using the
// same spelling Normalize produces.
func Canonical(key string, modifiers []string) (string, error) {
	mods := make(map[string]bool, len(modifiers))
	for _, m := range modifiers {
		m = strings.ToLower(m)
		if !contains(modifierOrder, m) {
			return "", fmt.Errorf("unknown modifier %q", m)
		}
		mods[m] = true
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
	for part := range strings.SplitSeq(chord, "+") {
		lower := strings.ToLower(part)
		if contains(modifierOrder, lower) {
			mods[lower] = true
			continue
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
