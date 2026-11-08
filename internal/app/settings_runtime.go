package app

import (
	"os"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp/manager"
	"github.com/tbxark/gvim/pluginapi"
)

// settingsLSP is the Settings page's view of the running language servers: the
// state of every configured server and the manual actions the page offers. The
// app implements it over the LSP port; nil leaves the LSP page with the
// config-side view only and no actions.
type settingsLSP interface {
	Servers() []tui.LSPServerStatus
	StartLanguage(language string) error
	StopServer(language, name string) error
	RestartServer(language, name string) error
}

// settingsPlugins is the Settings page's view of the plugin runtime: the plugin
// manager rows, the live enable/disable and reload actions, and the plugin
// settings values. Nil leaves the Plugins page with what the config alone says.
type settingsPlugins interface {
	Plugins() []pluginapi.PluginInfo
	Control(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice)
}

// settingsRuntime is what the Settings page's live parts need, captured from the
// runtime when the page's service is built. Each field is optional: a field that
// is nil turns its page section into the config-only view.
type settingsRuntime struct {
	lsp        settingsLSP
	plugins    settingsPlugins
	pluginsCfg config.Config // the config the plugin runtime was built from
	// keymapOf returns the effective keymap of cfg: the preset with the user's
	// config keybindings applied. Nil leaves the Keybindings page without a list.
	keymapOf func(cfg config.Config) tui.Keymap
	// presetName names the keymap preset the session runs, for the page note.
	presetName string
	// iconsMode and iconsWhy are the resolved icon set and its reason, for the
	// read-only Environment rows.
	iconsMode, iconsWhy string
	// version is the gvim version, for the Environment rows.
	version string
}

// envFacts are the read-only Environment rows: the detected facts of the process
// and the workspace, not config keys. Fresh shows its own environment detection
// in settings; gvim can only report what it knows, and project-environment
// activation (env.detectors) is not implemented (docs/settings.md §6).
type envFact struct {
	name, value, note string
}

// envFacts gathers the Environment rows for cfg. It reads the process environment
// and PATH (the shell lookup), never a config file.
func (s *settingsService) envFacts(cfg config.Config) []envFact {
	shell, note := effectiveShell(cfg)
	facts := []envFact{
		{name: "Shell", value: shell, note: note},
	}
	if term := os.Getenv("TERM_PROGRAM"); term != "" {
		facts = append(facts, envFact{name: "Terminal program", value: term, note: "TERM_PROGRAM"})
	}
	if term := os.Getenv("TERM"); term != "" {
		facts = append(facts, envFact{name: "TERM", value: term, note: "TERM"})
	}
	if s.rt.iconsMode != "" {
		facts = append(facts, envFact{name: "Icons", value: s.rt.iconsMode, note: s.rt.iconsWhy})
	}
	facts = append(facts,
		envFact{name: "OS", value: goruntime.GOOS + "/" + goruntime.GOARCH, note: goruntime.Version()},
		envFact{name: "Workspace root", value: s.root, note: "the explorer's root"},
		envFact{name: "User config", value: s.userFile, note: "the layer the page writes"},
	)
	if project := s.projectFile(); project != "" {
		facts = append(facts, envFact{name: "Project config", value: project, note: boolNote(s.trusted(), "trusted, writable", "read-only: the workspace is not trusted")})
	}
	if s.rt.version != "" {
		facts = append(facts, envFact{name: "Version", value: s.rt.version, note: "gvim"})
	}
	return facts
}

// effectiveShell is the shell the terminal dock would run and where that choice
// comes from: the terminal.shell.command option, else $SHELL, else /bin/sh.
func effectiveShell(cfg config.Config) (shell, note string) {
	switch {
	case cfg.Terminal.Shell.Command != "":
		shell, note = cfg.Terminal.Shell.Command, "terminal.shell.command"
	case os.Getenv("SHELL") != "":
		shell, note = os.Getenv("SHELL"), "from $SHELL"
	default:
		shell, note = "/bin/sh", "default"
	}
	if !strings.Contains(shell, "/") {
		if path, err := exec.LookPath(shell); err == nil {
			shell = path
		} else {
			note += " (not found on PATH)"
		}
	}
	return shell, note
}

// boolNote returns yes when b, else no.
func boolNote(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

// extraSections are the page sections that are not scalar config keys: the
// Language Servers page row and the read-only Environment facts. The Plugins and
// Keybindings rows join them with their pages.
func (s *settingsService) extraSections(cfg config.Config) []tui.SettingsSection {
	languages := make(map[string]bool)
	for _, spec := range manager.Defaults() {
		languages[spec.Language] = true
	}
	for lang := range cfg.LSP {
		languages[lang] = true
	}
	out := []tui.SettingsSection{{Name: "Language Servers", Items: []tui.SettingsItem{{
		Key: "lsp", Path: []string{"lsp"}, Name: "Language Servers", Section: "Language Servers",
		Kind: "page", Value: strconv.Itoa(len(languages)) + " languages",
		Layers: []string{tui.SettingsLayerUser, tui.SettingsLayerProject},
	}}}}
	out = append(out, tui.SettingsSection{Name: "Plugins", Items: []tui.SettingsItem{{
		Key: "plugins", Path: []string{"plugins"}, Name: "Plugins", Section: "Plugins",
		Kind: "page", Value: s.pluginSummary(), Layers: []string{tui.SettingsLayerUser},
	}}})
	out = append(out, tui.SettingsSection{Name: "Keybindings", Items: []tui.SettingsItem{{
		Key: "keybindings", Path: []string{"keybindings"}, Name: "Keybindings", Section: "Keybindings",
		Kind: "page", Value: s.keybindingsSummary(), Layers: []string{tui.SettingsLayerUser},
	}}})
	items := make([]tui.SettingsItem, 0, 8)
	for _, fact := range s.envFacts(cfg) {
		items = append(items, tui.SettingsItem{
			Key:  "env." + strings.ReplaceAll(strings.ToLower(fact.name), " ", "_"),
			Name: fact.name, Section: "Environment", Kind: "info", Value: fact.value, Note: fact.note,
		})
	}
	out = append(out, tui.SettingsSection{Name: "Environment", Items: items})
	return out
}

// pluginSummary is the Plugins page row's value: the enabled count of the runtime
// inventory.
func (s *settingsService) pluginSummary() string {
	if s.rt.plugins == nil {
		return "no plugin runtime"
	}
	infos := s.rt.plugins.Plugins()
	enabled := 0
	for _, info := range infos {
		if info.Enabled {
			enabled++
		}
	}
	return strconv.Itoa(enabled) + " of " + strconv.Itoa(len(infos)) + " enabled"
}

// keybindingsSummary is the Keybindings page row's value: the effective binding
// count and the preset.
func (s *settingsService) keybindingsSummary() string {
	count := 0
	if s.rt.keymapOf != nil && s.cfg.Keybindings != nil {
		if lister, ok := s.rt.keymapOf(s.cfg).(tui.BindingLister); ok {
			count = len(lister.Bindings())
		}
	}
	summary := strconv.Itoa(count) + " bindings"
	if s.rt.presetName != "" {
		summary += " · " + s.rt.presetName
	}
	return summary
}
