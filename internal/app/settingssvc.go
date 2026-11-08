package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
		setting, ok := config.SettingByKey(edit.Key)
		if !ok {
			return tui.SettingsChange{}, fmt.Errorf("unknown setting %q", edit.Key)
		}
		if project && !setting.Layers.Has(config.LayerProject) {
			return tui.SettingsChange{}, fmt.Errorf("%s can only be set in the user config", edit.Key)
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
	}
	if len(patch) == 0 {
		return s.reload(nil, "")
	}
	if diags := config.ValidateEdits(patch, project); len(diags) > 0 {
		return tui.SettingsChange{}, errors.New(diags[0].String())
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
	return s.reload(changed, layer)
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
	cfg, diags, err := config.Load(config.Options{
		Root:      s.root,
		UserFile:  s.opts.Config,
		Untrusted: !s.trusted(),
	})
	if err != nil {
		return tui.SettingsChange{}, err
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
	}
	for _, d := range diags {
		change.Warnings = append(change.Warnings, d.String())
	}
	keys := s.changedKeys(changed)
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
