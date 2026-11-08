package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// keymapChoices are the built-in keymaps the Settings page offers for keymap.
// They are the preset names keymap.ByName knows.
var keymapChoices = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// settingsService implements tui.SettingsPort over the config files: it lists the
// settings the config schema holds, validates a value through that schema, writes
// the chosen layer with config.PatchFile, reloads the config with config.Load and
// applies the values that can change in the running editor. It keeps the last
// loaded config, so Snapshot reads no files.
type settingsService struct {
	mu sync.Mutex

	opts    Options // the command line, whose flags win over the files
	root    string  // the workspace root, for the project layer
	ed      *editor.Editor
	trusted func() bool // whether the project layer may be written

	cfg      config.Config // the effective config of the last load
	userFile string        // the user config file the page writes
}

var _ tui.SettingsPort = (*settingsService)(nil)

// newSettingsService returns the Settings page's service. trusted reports whether
// the workspace is trusted now; the page asks on every snapshot, so a trust
// decision taken during the session makes the project layer writable at once.
func newSettingsService(root string, opts Options, ed *editor.Editor, trusted func() bool, cfg config.Config) *settingsService {
	user := opts.Config
	if user == "" {
		if files, err := config.Paths(); err == nil {
			user = files.User
		}
	}
	if trusted == nil {
		trusted = func() bool { return true }
	}
	return &settingsService{opts: opts, root: root, ed: ed, trusted: trusted, cfg: cfg, userFile: user}
}

// Snapshot returns the settings the page lists.
func (s *settingsService) Snapshot() tui.SettingsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot()
}

// snapshotLocked is Snapshot under the lock.
func (s *settingsService) snapshot() tui.SettingsSnapshot {
	projectWritable := s.trusted()
	snap := tui.SettingsSnapshot{
		UserFile:        s.userFile,
		ProjectFile:     s.projectFile(),
		ProjectWritable: projectWritable,
	}
	sectionAt := make(map[string]int)
	for _, setting := range config.Settings() {
		value := setting.Value(s.cfg)
		switch setting.Key() {
		case "theme":
			if s.opts.Theme != "" {
				value = s.opts.Theme
			}
		case "keymap":
			if s.opts.Keymap != "" {
				value = s.opts.Keymap
			}
		}
		item := tui.SettingsItem{
			Key:     setting.Key(),
			Path:    slices.Clone(setting.Path),
			Name:    setting.Name(),
			Section: setting.Section,
			Kind:    string(setting.Kind),
			Value:   value,
			Choices: s.choicesOf(setting),
			Min:     setting.Min,
			Max:     setting.Max,
			Apply:   string(setting.Apply),
			Layers:  layersOf(setting),
		}
		i, ok := sectionAt[setting.Section]
		if !ok {
			i = len(snap.Sections)
			sectionAt[setting.Section] = i
			snap.Sections = append(snap.Sections, tui.SettingsSection{Name: setting.Section})
		}
		snap.Sections[i].Items = append(snap.Sections[i].Items, item)
	}
	return snap
}

// layersOf returns the layers a setting can be written in, as the page names
// them.
func layersOf(setting config.Setting) []string {
	layers := []string{tui.SettingsLayerUser}
	if setting.Layers.Has(config.LayerProject) {
		layers = append(layers, tui.SettingsLayerProject)
	}
	return layers
}

// choicesOf returns the values of an enum setting: the schema's own, or the
// theme and keymap names the app knows.
func (s *settingsService) choicesOf(setting config.Setting) []string {
	switch setting.ChoicesFrom {
	case "themes":
		return theme.Names()
	case "keymaps":
		return slices.Clone(keymapChoices)
	}
	return slices.Clone(setting.Choices)
}

// Validate reports why value cannot be used for key. It checks the schema's
// bounds and choices, then the app-side checks a theme name and a keymap name
// need, then the value through the config tables.
func (s *settingsService) Validate(key, value string) error {
	setting, ok := config.SettingByKey(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	raw, err := setting.Encode(value)
	if err != nil {
		return err
	}
	switch key {
	case "theme":
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return err
		}
		if _, err := theme.Load(name); err != nil {
			return fmt.Errorf("theme %q: %w", name, err)
		}
	case "keymap":
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return err
		}
		if _, err := keybindings.Keymap(name); err != nil {
			return fmt.Errorf("keymap %q: %w", name, err)
		}
	}
	if diags := config.ValidateEdits([]config.Edit{{Path: setting.Path, Raw: raw}}, false); len(diags) > 0 {
		return errors.New(diags[0].String())
	}
	return nil
}

// Save writes the edits to the layer and reloads the config, returning what the
// running editor should apply. The page's edits are one patch, so a failure
// leaves the file as it was.
func (s *settingsService) Save(edits []tui.SettingEdit, layer string) (tui.SettingsChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project := layer == tui.SettingsLayerProject
	if project && !s.trusted() {
		return tui.SettingsChange{}, errors.New("the project config can be written only in a trusted workspace")
	}
	var patch []config.Edit
	var changed []string
	for _, edit := range edits {
		switch {
		case edit.Key != "":
			setting, ok := config.SettingByKey(edit.Key)
			if !ok {
				return tui.SettingsChange{}, fmt.Errorf("unknown setting %q", edit.Key)
			}
			if project && !setting.Layers.Has(config.LayerProject) {
				return tui.SettingsChange{}, fmt.Errorf("%s can only be set in the user config", edit.Key)
			}
			if edit.Remove {
				patch = append(patch, config.Edit{Path: setting.Path, Remove: true})
				changed = append(changed, edit.Key)
				continue
			}
			if err := s.validateSetting(setting, edit.Value, project); err != nil {
				return tui.SettingsChange{}, err
			}
			raw, err := setting.Encode(edit.Value)
			if err != nil {
				return tui.SettingsChange{}, fmt.Errorf("%s: %w", edit.Key, err)
			}
			patch = append(patch, config.Edit{Path: setting.Path, Raw: raw})
			changed = append(changed, edit.Key)
		case len(edit.Path) > 0:
			if !config.RemovablePath(edit.Path) {
				return tui.SettingsChange{}, fmt.Errorf("the settings page cannot write %s", strings.Join(edit.Path, "."))
			}
			if edit.Remove {
				patch = append(patch, config.Edit{Path: edit.Path, Remove: true})
			} else {
				if len(edit.Raw) == 0 {
					return tui.SettingsChange{}, fmt.Errorf("%s: the value is empty", strings.Join(edit.Path, "."))
				}
				if diags := config.ValidateEdits([]config.Edit{{Path: edit.Path, Raw: edit.Raw}}, project); len(diags) > 0 {
					return tui.SettingsChange{}, errors.New(diags[0].String())
				}
				patch = append(patch, config.Edit{Path: edit.Path, Raw: edit.Raw})
			}
			changed = append(changed, settingKeyOfPath(edit.Path))
		default:
			return tui.SettingsChange{}, errors.New("an edit needs a setting key or a config path")
		}
	}
	if len(patch) == 0 {
		return s.reload(nil, "")
	}
	path := s.userFile
	if project {
		path = s.projectFile()
	}
	if path == "" {
		return tui.SettingsChange{}, fmt.Errorf("no %s config file is known", layer)
	}
	if err := patchConfigFile(path, patch); err != nil {
		return tui.SettingsChange{}, err
	}
	return s.reload(uniqueStrings(changed), layer)
}

// settingKeyOfPath returns the schema key a structured edit belongs to, for the
// live/restart split: a formatters entry belongs to the Formatters page, a
// language override to Languages, and so on.
func settingKeyOfPath(path []string) string {
	if len(path) == 0 {
		return ""
	}
	switch path[0] {
	case "formatters":
		return "formatters"
	case "languages":
		return "languages"
	case "terminal":
		if len(path) >= 2 && path[1] == "shell" {
			if len(path) == 3 && path[2] == "command" {
				return "terminal.shell.command"
			}
			return "terminal.shell.args"
		}
	}
	return strings.Join(path, ".")
}

// uniqueStrings returns s without duplicates, keeping the first order.
func uniqueStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, item := range s {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// validateSetting is Validate for a setting already looked up, with the layer the
// value is written to.
func (s *settingsService) validateSetting(setting config.Setting, value string, project bool) error {
	raw, err := setting.Encode(value)
	if err != nil {
		return fmt.Errorf("%s: %w", setting.Key(), err)
	}
	switch setting.Key() {
	case "theme":
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return err
		}
		if _, err := theme.Load(name); err != nil {
			return fmt.Errorf("theme %q: %w", name, err)
		}
	case "keymap":
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return err
		}
		if _, err := keybindings.Keymap(name); err != nil {
			return fmt.Errorf("keymap %q: %w", name, err)
		}
	}
	if diags := config.ValidateEdits([]config.Edit{{Path: setting.Path, Raw: raw}}, project); len(diags) > 0 {
		return errors.New(diags[0].String())
	}
	return nil
}

// Reload re-reads the config files and returns what the running editor should
// apply.
func (s *settingsService) Reload() (tui.SettingsChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reload(nil, "")
}

// Apply pushes the editor-side live values of the last load into the editor. It
// is called on the update loop, which owns the editor; Save and Reload only
// touch files. The edge cases of the editor options that are read at startup are
// in the restart list of the change, not here.
func (s *settingsService) Apply(tui.SettingsChange) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	s.ed.SetLanguageSettings(languageSettings(cfg))
	s.ed.SetFormatters(formattersFor(cfg, lookPath))
	s.ed.SetEphemeralFilePatterns(cfg.EphemeralFilePatterns)
	s.ed.SetRulers(cfg.Rulers)
}

// reload loads the config again and returns the change. changed are the setting
// keys a Save wrote; nil means a Reload, which reapplies every live value.
func (s *settingsService) reload(changed []string, layer string) (tui.SettingsChange, error) {
	previous := s.cfg
	cfg, diags, err := config.Load(config.Options{
		Root:      s.root,
		UserFile:  s.opts.Config,
		Untrusted: !s.trusted(),
	})
	if err != nil {
		return tui.SettingsChange{}, err
	}
	reloaded := changed == nil
	var keys []string
	if reloaded {
		// A Reload reports only the settings whose effective value changed, so the
		// restart notice does not list the whole restart set for an untouched file.
		keys = changedSettings(previous, cfg)
	} else {
		keys = s.changedKeys(changed)
	}
	s.cfg = cfg
	icon := icons.Resolve(cfg.UIIcons, cfg.UIIconsFont, os.Getenv)
	settings := settingsFor(s.opts, cfg)
	settings.IconsMode, settings.IconsWhy = icon.Mode.String(), icon.Why
	settings.Keymap = effectiveKeymap(s.opts, cfg)
	settings.Theme = effectiveTheme(s.opts, cfg)
	change := tui.SettingsChange{
		Snapshot:    s.snapshot(),
		Settings:    settings,
		View:        *viewOptions(cfg, icon.Mode.String()),
		Theme:       settings.Theme,
		Keymap:      settings.Keymap,
		UIIconsMode: icon.Mode.String(),
		IconsWhy:    icon.Why,
		Layer:       layer,
		Reload:      reloaded,
	}
	for _, d := range diags {
		change.Warnings = append(change.Warnings, d.String())
	}
	for _, key := range keys {
		setting, ok := config.SettingByKey(key)
		if !ok {
			continue
		}
		// A command-line flag wins over the file, for the running editor and for
		// the next start: the file gets the edit, the session keeps the flag.
		if (key == "theme" && s.opts.Theme != "") || (key == "keymap" && s.opts.Keymap != "") {
			change.Warnings = append(change.Warnings,
				"--"+key+" wins over the config file; the edit applies to the file only")
			continue
		}
		if setting.Apply == config.ApplyRestart {
			change.Restart = append(change.Restart, key)
		} else {
			change.Live = append(change.Live, key)
		}
	}
	return change, nil
}

// changedKeys returns the settings to report: changed, in schema order, or every
// schema key for a Reload.
func (s *settingsService) changedKeys(changed []string) []string {
	if changed == nil {
		keys := make([]string, 0, len(config.Settings()))
		for _, setting := range config.Settings() {
			keys = append(keys, setting.Key())
		}
		return keys
	}
	want := make(map[string]bool, len(changed))
	for _, key := range changed {
		want[key] = true
	}
	keys := make([]string, 0, len(changed))
	for _, setting := range config.Settings() {
		if want[setting.Key()] {
			keys = append(keys, setting.Key())
		}
	}
	return keys
}

// Structure returns the sub-page of a structured setting for one layer: the
// formatters map, the language entry list, one language's scalar overrides, the
// status-bar element lists, or terminal.shell.args. It reads the layer's file
// (the keys that layer sets, not the merged config), so the page can show what
// the file itself holds; the values of a list the layer leaves alone fall back
// to the effective config. The page calls it from a tea.Cmd.
func (s *settingsService) Structure(structureKey, layer string) (tui.SettingsStructure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project := layer == tui.SettingsLayerProject
	path := s.userFile
	if project {
		if !s.trusted() {
			return tui.SettingsStructure{}, errors.New("the project config can be written only in a trusted workspace")
		}
		path = s.projectFile()
	}
	if path == "" {
		return tui.SettingsStructure{}, fmt.Errorf("no %s config file is known", layer)
	}
	object, err := readLayerFile(path)
	if err != nil {
		return tui.SettingsStructure{}, err
	}
	switch {
	case structureKey == "formatters":
		return s.structureFormatters(object), nil
	case structureKey == "languages":
		return s.structureLanguages(object), nil
	case strings.HasPrefix(structureKey, "languages."):
		return s.structureLanguageFields(object, strings.TrimPrefix(structureKey, "languages.")), nil
	case structureKey == "editor.status_bar", structureKey == "editor.status_bar.left", structureKey == "editor.status_bar.right":
		return s.structureStatusBar(object, structureKey), nil
	case structureKey == "terminal.shell.args":
		return s.structureShellArgs(object), nil
	}
	return tui.SettingsStructure{}, fmt.Errorf("unknown settings structure %q", structureKey)
}

// readLayerFile reads and decodes one config layer. A missing file is an empty
// layer.
func readLayerFile(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	object, err := config.DecodeLayer(data)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return object, nil
}

// rawAt returns the JSON at path inside object.
func layerRawAt(object map[string]json.RawMessage, path []string) (json.RawMessage, bool) {
	if len(path) == 0 {
		return nil, false
	}
	raw := object[path[0]]
	if raw == nil {
		return nil, false
	}
	if len(path) == 1 {
		return raw, true
	}
	var next map[string]json.RawMessage
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, false
	}
	return layerRawAt(next, path[1:])
}

// objectAt returns the JSON object at path inside object.
func layerObjectAt(object map[string]json.RawMessage, path []string) (map[string]json.RawMessage, bool) {
	raw, ok := layerRawAt(object, path)
	if !ok {
		return nil, false
	}
	var next map[string]json.RawMessage
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, false
	}
	return next, true
}

// stringList decodes a JSON array of strings.
func stringList(raw json.RawMessage) ([]string, bool) {
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	return list, true
}

// displayJSON is the display form of a config value of the given kind.
func displayJSON(raw json.RawMessage, kind config.SettingKind) string {
	switch kind {
	case config.SettingBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err == nil {
			if b {
				return "on"
			}
			return "off"
		}
	case config.SettingInt:
		var n int
		if err := json.Unmarshal(raw, &n); err == nil {
			return strconv.Itoa(n)
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

// overrideCount is the summary of a languages entry: how many fields it sets.
func overrideCount(fields int) string {
	if fields == 1 {
		return "1 override"
	}
	return strconv.Itoa(fields) + " overrides"
}

// structureFormatters is the formatters map sub-page.
func (s *settingsService) structureFormatters(object map[string]json.RawMessage) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "formatters", Title: "Formatters", Kind: "map", AddLabel: "Add formatter…"}
	formatters, _ := layerObjectAt(object, []string{"formatters"})
	for _, name := range slices.Sorted(maps.Keys(formatters)) {
		var command string
		if err := json.Unmarshal(formatters[name], &command); err != nil {
			continue
		}
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Path: []string{"formatters", name}, Name: name, Value: command,
		})
	}
	return st
}

// structureLanguages is the language entry list sub-page.
func (s *settingsService) structureLanguages(object map[string]json.RawMessage) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "languages", Title: "Languages", Kind: "languages", AddLabel: "Add language…"}
	entries, _ := layerObjectAt(object, []string{"languages"})
	for _, id := range slices.Sorted(maps.Keys(entries)) {
		fields, _ := layerObjectAt(entries, []string{id})
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Path: []string{"languages", id}, Name: id, Value: overrideCount(len(fields)),
			Child: "languages." + id,
		})
	}
	known := config.KnownLanguageIDs()
	for _, id := range known {
		if _, ok := entries[id]; !ok {
			st.Available = append(st.Available, id)
		}
	}
	return st
}

// structureLanguageFields is one language's scalar override sub-page. A field
// the layer does not set shows as unset; the page resets it by removing it.
func (s *settingsService) structureLanguageFields(object map[string]json.RawMessage, id string) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "languages." + id, Title: "Languages · " + id, Kind: "fields"}
	entry, _ := layerObjectAt(object, []string{"languages", id})
	for _, field := range config.LanguageOverrideFields() {
		row := tui.SettingsStructureRow{
			Path: []string{"languages", id, field.Key}, Name: field.Key,
			Kind: string(field.Kind), Choices: slices.Clone(field.Choices),
			Min: field.Min, Max: field.Max,
		}
		if raw, ok := layerRawAt(entry, []string{field.Key}); ok {
			row.Value = displayJSON(raw, field.Kind)
		}
		st.Rows = append(st.Rows, row)
	}
	return st
}

// structureStatusBar is the dual list of the status-bar elements. The page
// writes both sides as whole arrays, and only those two paths.
func (s *settingsService) structureStatusBar(object map[string]json.RawMessage, key string) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: key, Title: "Status Bar", Kind: "dual"}
	left := s.cfg.StatusBar.Left
	if raw, ok := layerRawAt(object, []string{"editor", "status_bar", "left"}); ok {
		if list, ok := stringList(raw); ok {
			left = list
		}
	}
	right := s.cfg.StatusBar.Right
	if raw, ok := layerRawAt(object, []string{"editor", "status_bar", "right"}); ok {
		if list, ok := stringList(raw); ok {
			right = list
		}
	}
	for _, name := range left {
		st.Rows = append(st.Rows, tui.SettingsStructureRow{Path: []string{"editor", "status_bar", "left"}, Group: "left", Name: name})
	}
	for _, name := range right {
		st.Rows = append(st.Rows, tui.SettingsStructureRow{Path: []string{"editor", "status_bar", "right"}, Group: "right", Name: name})
	}
	// Available lists the elements neither side has, in their config spelling
	// with braces ("{filename}"); an entry already on a side may be written with
	// or without them.
	present := make(map[string]bool, len(left)+len(right))
	for _, name := range append(slices.Clone(left), right...) {
		present[strings.Trim(name, "{}")] = true
	}
	for _, name := range config.StatusBarElementNames() {
		if !present[name] {
			st.Available = append(st.Available, "{"+name+"}")
		}
	}
	return st
}

// structureShellArgs is the terminal.shell.args list sub-page.
func (s *settingsService) structureShellArgs(object map[string]json.RawMessage) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "terminal.shell.args", Title: "Shell Arguments", Kind: "list", AddLabel: "Add argument…"}
	args := s.cfg.Terminal.Shell.Args
	if raw, ok := layerRawAt(object, []string{"terminal", "shell", "args"}); ok {
		if list, ok := stringList(raw); ok {
			args = list
		}
	}
	for _, arg := range args {
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Path: []string{"terminal", "shell", "args"}, Name: arg, Value: arg,
		})
	}
	return st
}

// changedSettings returns the schema keys whose effective value differs between
// two loaded configs. Scalars compare by the value the page shows; the
// structured sections compare their config fields.
func changedSettings(old, next config.Config) []string {
	var keys []string
	for _, setting := range config.Settings() {
		if setting.Kind == config.SettingPage {
			if structuredSettingChanged(setting.Key(), old, next) {
				keys = append(keys, setting.Key())
			}
			continue
		}
		if setting.Value(old) != setting.Value(next) {
			keys = append(keys, setting.Key())
		}
	}
	return keys
}

// structuredSettingChanged compares the config fields behind one page row.
func structuredSettingChanged(key string, old, next config.Config) bool {
	switch key {
	case "languages":
		return !reflect.DeepEqual(old.Languages, next.Languages)
	case "formatters":
		return !maps.Equal(old.Formatters, next.Formatters)
	case "editor.status_bar.left":
		return !slices.Equal(old.StatusBar.Left, next.StatusBar.Left)
	case "editor.status_bar.right":
		return !slices.Equal(old.StatusBar.Right, next.StatusBar.Right)
	case "terminal.shell.args":
		return !slices.Equal(old.Terminal.Shell.Args, next.Terminal.Shell.Args)
	}
	return false
}

// projectFile returns the project config file the page writes: the nearest
// existing one at or above the root, or root/.gvim/config.json to create.
func (s *settingsService) projectFile() string {
	if s.root == "" {
		return ""
	}
	if path, ok, err := config.ProjectFile(s.root); err == nil && ok {
		return path
	}
	return filepath.Join(s.root, ".gvim", "config.json")
}

// effectiveTheme is the theme the running editor shows: the --theme flag wins
// over the config.
func effectiveTheme(opts Options, cfg config.Config) string {
	if opts.Theme != "" {
		return opts.Theme
	}
	return cfg.Theme
}

// effectiveKeymap is the keymap the running editor uses: the --keymap flag wins
// over the config, and the platform default fills an unset config.
func effectiveKeymap(opts Options, cfg config.Config) string {
	if opts.Keymap != "" {
		return opts.Keymap
	}
	if cfg.Keymap != "" {
		return cfg.Keymap
	}
	return keymap.DefaultName()
}

// patchConfigFile applies the edits to the JSONC config file at path, replacing
// it atomically. It is the Settings page's one write: config owns the hujson
// patch (config.PatchBytes) because it owns the schema, and the app owns the
// file replace through atomicfile, because the config package is a leaf package
// in the import rules and may not import atomicfile. A missing file is created
// with the edits; an invalid file is left as it was.
func patchConfigFile(path string, edits []config.Edit) error {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = nil
	case err != nil:
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	patched, err := config.PatchBytes(data, edits)
	if err != nil {
		return fmt.Errorf("config: %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: make the directory of %s: %w", path, err)
	}
	if _, err := atomicfile.WriteFile(path, patched); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}
