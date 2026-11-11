package keybindings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tailscale/hujson"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

// BuiltinKeymaps are the embedded keymaps. keymap does not export the list.
var BuiltinKeymaps = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// knownActions is every action a keybinding of the config can name: what the UI and the
// editor run, and every action the built-in keymaps bind.
var knownActions = sync.OnceValue(func() map[string]bool {
	set := make(map[string]bool)
	for _, a := range tui.KnownActions() {
		set[a] = true
	}
	for _, name := range BuiltinKeymaps {
		if km, err := keymap.ByName(name); err == nil {
			for _, b := range km.Bindings() {
				set[b.Action] = true
			}
		}
	}
	return set
})

// knownAction reports whether name is an action gvim runs.
func knownAction(name string) bool { return knownActions()[name] }

// ApplyOverrides applies the keybindings of the config to km. A binding that cannot be
// applied is left out, and keybindingWarnings reports it, so a typo in the config costs
// one binding and never stops gvim from starting. Without keybindings km is returned
// unchanged.
func ApplyOverrides(km *keymap.Map, cfg config.Config) *keymap.Map {
	if cfg.Keybindings == nil {
		return km
	}
	out, _ := km.WithUserBindings(cfg.Keybindings, knownAction)
	return out
}

// Warnings lists what is wrong with the keybindings of the config: an unknown
// action, a key or modifier that does not exist, a when-clause that does not exist, a field
// that is misspelt, or a list that is not a list. Each entry names its position in the list.
func Warnings(cfg config.Config) []string {
	if cfg.Keybindings == nil {
		return nil
	}
	_, warnings := keymap.ParseUserBindings(cfg.Keybindings, knownAction)
	return warnings
}

// Replaced lists the bindings of km that the keybindings of the config take over, one
// line each. They are not problems: the user's entries apply as written. They go to Show Warnings
// only, so that a chord the user took from the keymap is never a silent change.
func Replaced(km *keymap.Map, cfg config.Config) []string {
	if cfg.Keybindings == nil {
		return nil
	}
	var out []string
	for _, line := range km.Replacements(cfg.Keybindings, knownAction) {
		out = append(out, "keybindings: "+line)
	}
	return out
}

// Notice is the status row text for the config problems. The row cuts a long text at its
// end, so with several problems it starts with how many there are and where to read them, then
// gives the first. It is empty when there are none.
func Notice(problems []string) string {
	switch len(problems) {
	case 0:
		return ""
	case 1:
		return "config: " + problems[0]
	}
	return fmt.Sprintf("config: %d problems (Show Warnings lists them): %s", len(problems), problems[0])
}

// Store is the user's custom keybindings during a session. It starts as the
// keybindings of the user config file and is replaced when the keybinding editor saves,
// which also writes the file. It implements tui.KeybindingStore, and gives the keymap
// builders the current list through config.
type Store struct {
	mu     sync.Mutex
	path   string
	custom []tui.CustomBinding
	// skipped are the entries of the file that cannot apply (an unknown action, a typo). They
	// are not in custom, so the editor does not list them, but a save writes them back, so
	// that an entry the user has yet to fix is not lost.
	skipped []json.RawMessage
	raw     json.RawMessage
}

// NewStore returns the store of the keybindings in cfg, which the file at path
// holds. path is the user config file, or empty when there is none to write. An entry that
// cannot apply is left out of the editor's list and kept in the file (see skipped).
func NewStore(path string, cfg config.Config) *Store {
	s := &Store{path: path, raw: cfg.Keybindings}
	list, skipped, _ := keymap.ParseUserBindingsRaw(cfg.Keybindings, knownAction)
	s.skipped = skipped
	for _, b := range list {
		s.custom = append(s.custom, tui.CustomBinding{Chord: b.Chord, Action: b.Action, Context: string(b.When), Args: b.Args})
	}
	return s
}

// Custom lists the user's bindings.
func (s *Store) Custom() []tui.CustomBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tui.CustomBinding(nil), s.custom...)
}

// Current is cfg with the keybindings the session has now.
func (s *Store) Current(cfg config.Config) config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg.Keybindings = s.raw
	return cfg
}

// Preview returns the keymap of preset with list applied, without writing anything, so the
// keybinding editor can show pending changes. A list that cannot be applied is an error.
func (s *Store) Preview(list []tui.CustomBinding, preset string) (tui.Keymap, error) {
	km, _, err := s.build(list, preset)
	return km, err
}

// build checks list and returns the keymap of preset with it applied, and the JSON entries
// of the config file.
func (s *Store) build(list []tui.CustomBinding, preset string) (tui.Keymap, []any, error) {
	bindings, entries, err := userBindings(list)
	if err != nil {
		return nil, nil, err
	}
	base, err := Keymap(preset)
	if err != nil {
		return nil, nil, err
	}
	return adapter{km: base.WithUser(bindings)}, entries, nil
}

// PreviewWithPlugins is Preview with the plugin bindings layered over the preset, the way the
// keymap applies them: the preset, then the plugins, then list. A plugin binding that cannot apply
// is left out, as the root reports it when the keymap is rebuilt.
func (s *Store) PreviewWithPlugins(list []tui.CustomBinding, preset string, plugins []pluginapi.Binding) (tui.Keymap, error) {
	bindings, _, err := userBindings(list)
	if err != nil {
		return nil, err
	}
	base, err := Keymap(preset)
	if err != nil {
		return nil, err
	}
	if len(plugins) > 0 {
		if layered, _ := PluginLayer(base, plugins); layered != nil {
			base = layered
		}
	}
	return adapter{km: base.WithUser(bindings)}, nil
}

// Save writes list to the keybindings of the user config file and returns the keymap of
// preset with list applied. The file keeps its other settings and comments. An unreadable
// file is not overwritten.
func (s *Store) Save(list []tui.CustomBinding, preset string) (tui.Keymap, error) {
	if s.path == "" {
		return nil, errors.New("there is no user config file to write")
	}
	km, entries, err := s.build(list, preset)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeKeybindings(s.path, entries, s.skipped); err != nil {
		return nil, err
	}
	s.custom = append([]tui.CustomBinding(nil), list...)
	raw, err := json.Marshal(append(rawEntries(s.skipped), entries...))
	if err != nil {
		return nil, err
	}
	s.raw = raw
	return km, nil
}

// userBindings converts the editor's list into keymap bindings, and into the JSON entries of
// the config file. It refuses an entry the keymap would skip, so what is written is what
// applies.
func userBindings(list []tui.CustomBinding) ([]keymap.Binding, []any, error) {
	bindings := make([]keymap.Binding, 0, len(list))
	entries := make([]any, 0, len(list))
	for i, c := range list {
		chord, err := keymap.NormalizeChord(c.Chord)
		if err != nil {
			return nil, nil, fmt.Errorf("binding %d: %w", i+1, err)
		}
		b := keymap.Binding{Chord: chord, Action: c.Action, Args: c.Args, When: keymap.Context(c.Context)}
		if steps := strings.Fields(chord); len(steps) > 1 {
			b.Sequence = steps
		}
		entries = append(entries, keymap.UserEntry(b))
		bindings = append(bindings, b)
	}
	// Run the entries through the reader the config goes through, so that a bad context or
	// action is an error here and not a warning at the next start.
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, nil, err
	}
	parsed, warnings := keymap.ParseUserBindings(data, knownAction)
	if len(warnings) > 0 || len(parsed) != len(bindings) {
		if len(warnings) == 0 {
			warnings = []string{"two entries bind the same key"}
		}
		return nil, nil, errors.New(warnings[0])
	}
	return parsed, entries, nil
}

// writeKeybindings sets the "keybindings" value of the config file at path to the entries the
// user made, in the form Fresh documents: a list. The entries kept (the ones that cannot apply)
// come first, so that a later entry for the same chord still wins, as it did in the file. A
// file that already holds the object form, {"bindings": [...]}, keeps it. The rest of the file,
// comments included, is kept. A missing or empty file becomes an object with the list. A file
// that is not JSON with comments is left as it is and the error says so.
func writeKeybindings(path string, entries []any, kept []json.RawMessage) error {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = []byte("{\n}\n")
	case err != nil:
		return err
	case len(bytes.TrimSpace(data)) == 0:
		data = []byte("{\n}\n")
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return fmt.Errorf("%s is not valid JSON with comments, so it was not changed: %w", filepath.Base(path), err)
	}
	all := append(rawEntries(kept), entries...)
	value, err := keybindingsValue(data, all)
	if err != nil {
		return err
	}
	patch := `[{"op":"add","path":"/keybindings","value":` + value + `}]`
	if err := v.Patch([]byte(patch)); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	v.Format()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// atomicfile keeps a symlinked (dotfile-managed) config a symlink, and the mode,
	// owner and extended attributes of the file it replaces.
	_, err = atomicfile.WriteFile(path, v.Pack())
	return err
}

// rawEntries returns kept as the values a JSON list is made of.
func rawEntries(kept []json.RawMessage) []any {
	out := make([]any, 0, len(kept))
	for _, r := range kept {
		out = append(out, r)
	}
	return out
}

// keybindingsValue is the JSON value of the "keybindings" key for entries: the list, one entry to
// a line as the documentation shows them, or the object form {"bindings": [...]} when the file
// already holds that form. data is the file before the change.
func keybindingsValue(data []byte, entries []any) (string, error) {
	if entries == nil {
		entries = []any{}
	}
	var list strings.Builder
	list.WriteString("[")
	for i, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		if i > 0 {
			list.WriteString(",")
		}
		list.WriteString("\n" + string(line))
	}
	if len(entries) > 0 {
		list.WriteString("\n")
	}
	list.WriteString("]")
	if objectForm(data) {
		return `{"bindings": ` + list.String() + `}`, nil
	}
	return list.String(), nil
}

// objectForm reports whether the file's "keybindings" value is an object, the {"bindings": [...]}
// form. A file that does not hold one, or has no keybindings, is not in that form.
func objectForm(data []byte) bool {
	// Standardize may rewrite its input in place, so it reads a copy of the file.
	std, err := hujson.Standardize(bytes.Clone(data))
	if err != nil {
		return false
	}
	var f struct {
		Keybindings json.RawMessage `json:"keybindings"`
	}
	if json.Unmarshal(std, &f) != nil {
		return false
	}
	return bytes.HasPrefix(bytes.TrimSpace(f.Keybindings), []byte("{"))
}

// Keymap returns the named built-in keymap. An empty name is the platform
// default.
func Keymap(name string) (*keymap.Map, error) {
	if name == "" {
		return keymap.Default(), nil
	}
	return keymap.ByName(name)
}
