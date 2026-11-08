package pluginhost

import (
	"os"
	"path/filepath"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// Wiring is what the Lua plugin runtime adds to a session: the runtime, the editor as
// the plugins see it, and the builder of the keymap that holds their bindings.
type Wiring struct {
	Runtime *plugin.Runtime
	Editor  pluginapi.BufferEditor
	// OS is the hostos.OS the runtime uses. The app mounts the local authority over the
	// same instance, so a launch routed through the slot sees the same machine rules.
	OS hostos.OS
	// Keys builds the keymap of a preset with the plugin bindings layered over it: the
	// preset, then the bindings, then the user's own. An empty name is the preset the
	// session started with.
	Keys func(preset string, b []pluginapi.Binding) (*keymap.Map, error)
}

// Options are the plugin parts of the command line and the session.
type Options struct {
	// Paths are the plugins to load besides the discovered ones (--plugin).
	Paths []string
	// Disabled skips plugin discovery and loading, including the user's init.lua
	// (--no-plugins).
	Disabled bool
	// LogSink receives the plugin log, or nil.
	LogSink func(pluginapi.LogEntry)
	// Version is the module version the plugins see.
	Version string
	// Preset is the keymap before the user's keybindings, which the plugin bindings are
	// layered on.
	Preset *keymap.Map
	// Root reports the workspace root as it is now, since opening a project changes it.
	Root func() string
	// SpawnEnv is the active project environment's set-only overlay for plugin
	// processes, or nil.
	SpawnEnv func() []string
}

// New builds the plugin runtime of a session. It starts nothing: the model's Init starts
// the runtime, whose first Work discovers the plugins, and the plugins load when that Work
// comes back. The sources are the plugins embedded in the binary, the user's plugin folder
// and init.lua (config.Dir()), and the --plugin paths.
//
// With --no-plugins the runtime is still created, with the loader disabled: it discovers
// and loads nothing but the --plugin paths, and the plugin manager can say why.
func New(cfg config.Config, ed *editor.Editor, markEdited func(*editor.Document), o Options) *Wiring {
	cfgDir, _ := config.Dir()
	ld := &plugin.LoaderOptions{
		Paths:    o.Paths,
		Disabled: o.Disabled,
		Enabled:  map[string]bool{},
		Settings: map[string]plugin.PluginSettings{},
	}
	if cfgDir != "" {
		ld.UserDir = filepath.Join(cfgDir, "plugins")
		ld.InitFile = filepath.Join(cfgDir, "init.lua")
		ld.StateFile = filepath.Join(cfgDir, "plugin-state.json")
	}
	for name, pc := range cfg.Plugins {
		if pc.Enabled != nil {
			ld.Enabled[name] = *pc.Enabled
		}
		if pc.Settings != nil || pc.ProjectSettings != nil {
			ld.Settings[name] = plugin.PluginSettings{User: pc.Settings, Project: pc.ProjectSettings}
		}
	}
	osys := newPluginOS(pluginDirs(cfgDir, o.Root), o.SpawnEnv)
	return &Wiring{
		OS: osys,
		Runtime: plugin.New(plugin.Options{
			Timeout:  cfg.PluginTimeout,
			OS:       osys,
			Services: pluginServices{},
			Version:  o.Version,
			LogSink:  o.LogSink,
			Loader:   ld,
			Locale:   i18n.Locale(),
		}),
		Editor: newPluginEditor(ed, markEdited),
		Keys: func(name string, bs []pluginapi.Binding) (*keymap.Map, error) {
			base := o.Preset
			if name != "" {
				// A keymap switch: the plugin bindings go over the preset it chose.
				b, err := keybindings.Keymap(name)
				if err != nil {
					return nil, err
				}
				base = b
			}
			return keybindings.PluginKeymap(base, cfg, bs)
		},
	}
}

// pluginDirs reports the directories gvim.dirs gives the plugins. It is evaluated at every
// call, because the workspace root changes when a project is opened.
func pluginDirs(cfgDir string, root func() string) func() hostos.Dirs {
	home, _ := os.UserHomeDir()
	return func() hostos.Dirs {
		cwd, _ := os.Getwd()
		return hostos.Dirs{Config: cfgDir, Home: home, Temp: os.TempDir(), Cwd: cwd, Root: root()}
	}
}
