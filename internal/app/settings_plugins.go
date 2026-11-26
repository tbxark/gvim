package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/pluginapi"
)

// The Settings Plugins page. pluginapi.Runtime exposes the manager rows through
// Plugins() and the live enable/disable/reload actions through Control; it does
// not expose a declared-settings schema or values yet, so this page lists the
// inventory, toggles enablement (a user config layer setting) and shows the
// plugin settings the user file already sets, read-only, with that residual.
//
// Control does work on the plugin runtime and is documented single-goroutine, so
// it is not called from the Act command: Act writes the config file and reloads
// (IO, off the update goroutine), and the root calls Control from Update when the
// Act result arrives (internal/tui/settings.go).

// pluginRuntime is the plugin runtime the model drives: the pluginapi.Runtime plus the
// two file queries the plugin manager asks of the concrete runtime.
type pluginRuntime interface {
	pluginapi.Runtime
	InitFile() string
	NoPlugins() bool
}

// pluginInventory is a copy of the plugin runtime's manager rows and declared settings
// for the Settings page. The runtime is single-goroutine and the page reads it from
// tea.Cmd goroutines, so the page never touches the runtime: the update goroutine
// refreshes this copy (pluginMirror) and the page reads it under its own lock.
type pluginInventory struct {
	mu      sync.RWMutex
	infos   []pluginapi.PluginInfo
	schemas map[string][]pluginapi.PluginSettingSpec
	values  map[string][]pluginapi.PluginSettingValue
	at      time.Time
}

// Plugins returns the copied manager rows.
func (v *pluginInventory) Plugins() []pluginapi.PluginInfo {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return slices.Clone(v.infos)
}

// SettingsSchema returns the copied settings a plugin declared.
func (v *pluginInventory) SettingsSchema(name string) ([]pluginapi.PluginSettingSpec, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	s, ok := v.schemas[name]
	return slices.Clone(s), ok
}

// SettingsValues returns the copied effective values of a plugin's declared settings.
func (v *pluginInventory) SettingsValues(name string) ([]pluginapi.PluginSettingValue, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	s, ok := v.values[name]
	return slices.Clone(s), ok
}

// capture reads rt on the calling goroutine, which must be the one that drives rt, and
// swaps the copy in.
func (v *pluginInventory) capture(rt pluginapi.Runtime) {
	infos := rt.Plugins()
	schemas := make(map[string][]pluginapi.PluginSettingSpec, len(infos))
	values := make(map[string][]pluginapi.PluginSettingValue, len(infos))
	for _, info := range infos {
		if schema, ok := rt.SettingsSchema(info.Name); ok {
			schemas[info.Name] = schema
			if vals, ok := rt.SettingsValues(info.Name); ok {
				values[info.Name] = vals
			}
		}
	}
	v.mu.Lock()
	v.infos, v.schemas, v.values, v.at = infos, schemas, values, time.Now()
	v.mu.Unlock()
}

// drainRefreshEvery bounds how often the model's frequent plugin Drain refreshes the copy.
const drainRefreshEvery = 250 * time.Millisecond

// pluginMirror is the plugin runtime the model sees. Every call passes through to the
// runtime on the update goroutine, and the ones that can change the inventory (start,
// completion, control, a command run, a drain) refresh the copy the Settings page reads.
type pluginMirror struct {
	pluginRuntime
	inv *pluginInventory
}

// newPluginMirror wraps rt. The returned inventory is what the Settings page reads.
func newPluginMirror(rt pluginRuntime) (*pluginMirror, *pluginInventory) {
	inv := &pluginInventory{}
	return &pluginMirror{pluginRuntime: rt, inv: inv}, inv
}

func (m *pluginMirror) Start(h pluginapi.Host) []pluginapi.Work {
	work := m.pluginRuntime.Start(h)
	m.inv.capture(m.pluginRuntime)
	return work
}

func (m *pluginMirror) Resolve(c pluginapi.Completion) {
	m.pluginRuntime.Resolve(c)
	m.inv.capture(m.pluginRuntime)
}

func (m *pluginMirror) Run(action string, args map[string]any, src pluginapi.CommandSource) error {
	err := m.pluginRuntime.Run(action, args, src)
	m.inv.capture(m.pluginRuntime)
	return err
}

func (m *pluginMirror) Control(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
	work, notices := m.pluginRuntime.Control(c)
	m.inv.capture(m.pluginRuntime)
	return work, notices
}

func (m *pluginMirror) Drain(budget time.Duration) ([]pluginapi.Work, bool) {
	work, more := m.pluginRuntime.Drain(budget)
	// The model drains after nearly every event: refresh at most every drainRefreshEvery.
	if m.inv.stale(drainRefreshEvery) {
		m.inv.capture(m.pluginRuntime)
	}
	return work, more
}

// stale reports whether the copy is older than age.
func (v *pluginInventory) stale(age time.Duration) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return time.Since(v.at) >= age
}

// structurePlugins is the Plugins page: one row per runtime plugin with its source
// and state and an enable/disable action, the reload row, and the read-only
// settings the user config holds.
func (s *settingsService) structurePlugins(cfg config.Config, project bool) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "plugins", Title: settingsPage("plugins", "Plugins"), Kind: "fields"}
	st.Note = settingsNote("plugins", "Enablement is the user config layer. Declared plugin settings are listed read-only with their effective values; edit plugins.<name>.settings in the config (docs/settings.md).")
	if s.rt.plugins == nil {
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Name: settingsValue("no_plugin_runtime", "No plugin runtime"), Value: settingsValue("disabled", "disabled"), Locked: true,
			Note: "--no-plugins, or a host without the plugin runtime",
		})
		return st
	}
	infos := slices.Clone(s.rt.plugins.Plugins())
	slices.SortFunc(infos, func(a, b pluginapi.PluginInfo) int { return strings.Compare(a.Name, b.Name) })
	for _, info := range infos {
		state := "enabled"
		if !info.Enabled {
			state = "disabled"
		}
		note := info.Source
		if info.Status != "" {
			note += " · " + info.Status
		}
		if info.Locked != "" {
			note += " · locked: " + info.Locked
		}
		if settings := pluginSettingsText(cfg, info.Name); settings != "" {
			note += " · settings: " + settings
		}
		row := tui.SettingsStructureRow{Name: info.Name, Value: state, Note: note}
		switch {
		case project:
			row.Locked = true
			row.Note = "user layer only · " + note
		case info.Locked != "":
			row.Locked = true
		case info.Enabled:
			row.Action = "act:plugin-disable"
		default:
			row.Action = "act:plugin-enable"
		}
		st.Rows = append(st.Rows, row)
	}
	st.Rows = append(st.Rows, tui.SettingsStructureRow{
		Name: "Reload plugins", Action: "act:plugin-reload",
		Note: "re-reads every plugin source and settings from the config",
	})
	st.Rows = append(st.Rows, pluginSettingRows(cfg, s.rt.plugins, infos)...)
	return st
}

// pluginSettingRows shows the settings a plugin declared with gvim.settings.define:
// one read-only row per setting with its effective value and type/default/choices/bounds
// in the note. The runtime owns the values; the page only reads them (editing goes through
// plugins.<name>.settings in the config for now).
func pluginSettingRows(cfg config.Config, rt settingsPlugins, infos []pluginapi.PluginInfo) []tui.SettingsStructureRow {
	host, ok := rt.(interface {
		SettingsSchema(name string) ([]pluginapi.PluginSettingSpec, bool)
		SettingsValues(name string) ([]pluginapi.PluginSettingValue, bool)
	})
	if !ok {
		return nil
	}
	var rows []tui.SettingsStructureRow
	for _, info := range infos {
		schema, ok := host.SettingsSchema(info.Name)
		if !ok || len(schema) == 0 {
			continue
		}
		values, _ := host.SettingsValues(info.Name)
		effective := make(map[string]pluginapi.PluginSettingValue, len(values))
		for _, v := range values {
			effective[v.Key] = v
		}
		userKeys := cfg.Plugins[info.Name].Settings
		for _, spec := range schema {
			val := effective[spec.Key]
			note := spec.Type
			if spec.Description != "" {
				note += " · " + spec.Description
			}
			note += " · default " + formatPluginSettingValue(spec.Default)
			if len(spec.Choices) > 0 {
				note += " · values " + strings.Join(spec.Choices, "|")
			}
			if spec.Min != nil || spec.Max != nil {
				note += " · range " + rangeText(spec.Min, spec.Max)
			}
			if spec.Project {
				note += " · project"
			}
			if _, ok := userKeys[spec.Key]; ok {
				note += " · set in user config"
			} else if !val.Project {
				note += " · default"
			}
			if val.Project {
				note += " · from project"
			}
			rows = append(rows, tui.SettingsStructureRow{
				Name: info.Name + "." + spec.Key, Value: formatPluginSettingValue(val.Value),
				Note: note, Locked: true, Group: "plugin settings",
			})
		}
	}
	return rows
}

// formatPluginSettingValue renders a declared setting value for the page.
func formatPluginSettingValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return strconv.Quote(x)
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}

// rangeText is the min..max note of a numeric setting.
func rangeText(lower, upper *float64) string {
	lo, hi := "-", "-"
	if lower != nil {
		lo = formatPluginSettingValue(float64(*lower))
	}
	if upper != nil {
		hi = formatPluginSettingValue(float64(*upper))
	}
	return lo + ".." + hi
}

// pluginSettingsText lists the settings keys the user config sets for a plugin,
// for the read-only note. Their values are not shown (the runtime owns the
// effective values and does not expose them yet).
func pluginSettingsText(cfg config.Config, name string) string {
	pc, ok := cfg.Plugins[name]
	if !ok || len(pc.Settings) == 0 {
		return ""
	}
	keys := make([]string, 0, len(pc.Settings))
	for key := range pc.Settings {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, ", ")
}

// pluginAct is the Plugins page's live half: enabling or disabling writes
// plugins.<name>.enabled to the USER config and reloads it, restoring the
// previous file bytes when either step fails (so a failed toggle leaves the file
// as it was); the root then calls the runtime's Control from Update with the
// action the page reported. "plugin-reload" writes nothing.
func (s *settingsService) pluginAct(action, value string) (string, error) {
	switch action {
	case "plugin-reload":
		return "Reloading plugins", nil
	case "plugin-enable":
		if err := s.setPluginEnabled(value, true); err != nil {
			return "", err
		}
		return "Plugin " + value + " enabled", nil
	case "plugin-disable":
		if err := s.setPluginEnabled(value, false); err != nil {
			return "", err
		}
		return "Plugin " + value + " disabled", nil
	}
	return "", fmt.Errorf("settings: plugin action %q is not available", action)
}

// setPluginEnabled writes plugins.<name>.enabled to the user config file and
// reloads the config. On a write or reload failure the previous file content is
// restored, so the toggle never half-applies.
func (s *settingsService) setPluginEnabled(name string, enabled bool) error {
	if name == "" {
		return errors.New("settings: the plugin name is empty")
	}
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	path := s.userFile
	if path == "" {
		return errors.New("settings: no user config file is known")
	}
	previous, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return fmt.Errorf("config: read %s: %w", path, readErr)
	}
	perm := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	raw, err := json.Marshal(enabled)
	if err != nil {
		return err
	}
	// What the file already reports (an unknown or retired key the user left in it)
	// is not damage the toggle causes, so it is remembered and not held against it.
	_, existing, _ := config.Load(config.Options{UserFile: path})
	patch := []config.Edit{{Path: []string{"plugins", name, "enabled"}, Raw: raw}}
	if err := patchConfigFileFn(path, patch); err != nil {
		return err
	}
	rollback := func() error {
		// The previous bytes when there was a file, otherwise remove the one the
		// patch created.
		var werr error
		if errors.Is(readErr, fs.ErrNotExist) {
			werr = os.Remove(path)
			if errors.Is(werr, fs.ErrNotExist) {
				werr = nil
			}
		} else {
			werr = os.WriteFile(path, previous, perm)
		}
		_, _ = s.reloadUnlocked([]string{"plugins." + name + ".enabled"}, "user")
		return werr
	}
	if _, err := s.reloadUnlocked([]string{"plugins." + name + ".enabled"}, "user"); err != nil {
		if rerr := rollback(); rerr != nil {
			return fmt.Errorf("%w (rollback failed: %w)", err, rerr)
		}
		return err
	}
	// Writing the flag must leave a config file that still loads; a new diagnostic
	// in the file we just wrote means the toggle would run the runtime on a config
	// the next start rejects, so it is rolled back too.
	if diags := newDiagnostics(s.diagnosticsOf(path), existing); len(diags) > 0 {
		err := fmt.Errorf("the config file does not load after the write: %s", diags[0])
		if rerr := rollback(); rerr != nil {
			return fmt.Errorf("%w (rollback failed: %w)", err, rerr)
		}
		return err
	}
	return nil
}
