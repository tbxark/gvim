// Package config loads gvim's settings from JSON-with-comments files.
//
// Three layers are merged, lowest first: the built-in defaults, the user file
// (Dir()/config.json (~/.config/gvim/config.json)), and the project file (.gvim/config.json
// in the nearest directory at or above the project root that has one). A layer
// sets only the keys it names. A later layer replaces the value of each key it
// sets and keeps the others.
//
// Files may contain // and /* */ comments and trailing commas (JWCC, parsed by
// github.com/tailscale/hujson). A file that cannot be read or parsed is skipped
// and reported as a Diagnostic. A key with an invalid value, or an unknown key,
// is skipped and reported the same way. Load does not panic on file content.
//
// The package does not choose the platform keymap. Keymap is empty unless a
// file sets it, and the caller applies its own default.
//
// Workspace trust. A caller that knows the workspace is not trusted sets
// Options.Untrusted, and the project file is not read at all: a repository that
// has not been trusted must not configure the editor it is opened in. The user
// file still applies. See package trust and docs/trust.md.
//
// Fresh compatibility. The files also accept the nested layout of Fresh's
// config.json: an "editor" object holds the editing and display options under
// Fresh's names, a "languages" object holds per-language overrides, and
// "file_explorer" holds the explorer options. gvim's older flat top-level keys
// (tab_size, insert_spaces, show_whitespace, ...) keep working as aliases of the
// nested keys. When one file sets both spellings of an option, the nested one
// wins. Across layers the later layer wins whichever spelling it uses. Fresh keys
// that gvim has no use for (clipboard, terminal, ...) are accepted and
// ignored, so a Fresh config file loads without diagnostics. See editoropts.go
// for the editor options and language.go for the per-language settings and the
// resolver Config.EditorFor.
//
// The plugins key, {"plugins": {"name": {"enabled": false, "settings": {...}}}}, is
// read per layer into Config.Plugins (see PluginConfig). The project layer can only
// disable a plugin, and plugin_timeout_ms from the project layer can only lower the
// user's value.
//
// The keymap and keybindings keys change what the user's keys do, so only the user file
// sets them: the project layer is refused with a diagnostic (unlike Fresh, where a project's
// keybindings replace the user's). keybindings is Fresh's list of {key, modifiers, action,
// when} entries; an object {"bindings": [...]} is accepted too.
//
// The lsp key maps a language ID to one or more language servers, as Fresh's does:
// {"lsp": {"go": {"command": "gopls", "args": []}}}. A language may also give a list
// of servers, which is how Fresh configures several servers for one language. The
// key overrides the built-in table of servers (lsp), which starts a server for a
// language when its program is on PATH. An entry with "enabled": false turns that
// entry off, and a language whose entries are all off gets no server, the built-in
// one included. The top-level lsp_enabled key turns all of them off. A later layer
// that sets lsp replaces the whole map. The universal_lsp key maps a name to
// servers that run alongside those of every language (Fresh's universal servers).
// The other fields Fresh writes for a server (name, env, language_id_overrides,
// initialization_options, only_features, except_features, process_limits) are kept
// and handed to the server; process_limits is parsed but not enforced, because
// gvim is pure Go and sets no rlimits (docs/lsp.md). The project layer's lsp key
// may only turn servers on or off and name their root markers and feature filters:
// a field that chooses or shapes the program is refused with a diagnostic.
//
// The formatters key maps a language name, or a file extension with its dot, to the
// command that formats a buffer: {"formatters": {"go": "gofmt", ".go": "gofmt"}}. Keys
// are matched without regard to case, and the language name is tried before the
// extension. A later layer that sets formatters replaces the whole map.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/tailscale/hujson"
)

// Line ending policies accepted by line_ending.
const (
	LineEndingAuto = "auto"
	LineEndingLF   = "lf"
	LineEndingCRLF = "crlf"
	LineEndingCR   = "cr" // accepted for Fresh configs; gvim has no CR documents, so it opens LF
)

// Tab width bounds accepted by tab_size.
const (
	MinTabSize = 1
	MaxTabSize = 16
)

// Cursor shapes accepted by cursor_style.
const (
	CursorStyleBlock     = "block"
	CursorStyleBar       = "bar"
	CursorStyleUnderline = "underline"
)

// Icon sets accepted by ui_icons. Auto lets the app pick from ui_icons_font and the
// terminal.
const (
	UIIconsAuto  = "auto"
	UIIconsPlain = "plain"
	UIIconsNerd  = "nerd"
)

// DefaultPluginTimeout is the plugin_timeout_ms default. The plugin host applies the
// same default when it is given no timeout.
const DefaultPluginTimeout = 500 * time.Millisecond

// Plugin timeout bounds, in milliseconds, accepted by plugin_timeout_ms.
const (
	MinPluginTimeoutMS = 1
	MaxPluginTimeoutMS = 60000
)

// LSPServer is one language server, as the lsp and universal_lsp keys write it.
// Command is the program, and Args are passed to it in order. An entry that turns a
// built-in server on ("enabled": true or "auto_start": true) may leave Command empty:
// the built-in program runs, with Args when they are given.
type LSPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	// Dormant is "auto_start": false: the server is not started when a file opens, only by
	// Start/Restart LSP Server.
	Dormant bool `json:"-"`
	// RootMarkers are the file names that mark the root of a project (root_markers). The
	// server of a file runs in the nearest directory above the file that holds one of them,
	// and in the workspace root when none does.
	RootMarkers []string `json:"root_markers,omitempty"`

	// Name is the server's display name (name). Empty means its program.
	Name string `json:"name,omitempty"`
	// Env holds extra environment variables for the server (env), laid over the
	// inherited environment.
	Env map[string]string `json:"env,omitempty"`
	// LanguageIDOverrides maps a file extension, without the dot, to the languageId
	// didOpen sends for it (language_id_overrides).
	LanguageIDOverrides map[string]string `json:"language_id_overrides,omitempty"`
	// InitializationOptions is the JSON sent as initialize's initializationOptions,
	// verbatim, or nil for none.
	InitializationOptions json.RawMessage `json:"initialization_options,omitempty"`
	// OnlyFeatures and ExceptFeatures are the feature filters (only_features,
	// except_features): a server handles only these features, or all but these. Both
	// set is not a Fresh config; OnlyFeatures wins. The names are Fresh's LspFeature
	// names, such as "diagnostics" and "code_action".
	OnlyFeatures   []string `json:"only_features,omitempty"`
	ExceptFeatures []string `json:"except_features,omitempty"`
	// ProcessLimits is the memory and CPU limit Fresh writes for a server
	// (process_limits). gvim applies it on Linux (cgroup v2, or an sh ulimit memory
	// wrapper) and records why it cannot on other platforms in the server status; the
	// per-OS table is in docs/lsp.md.
	ProcessLimits *ProcessLimits `json:"process_limits,omitempty"`
}

// ProcessLimits is Fresh's process_limits object for a language server. gvim applies
// the caps where a pure-Go binary can (Linux cgroup v2, or an sh ulimit -v memory
// wrapper) and reports the ones it cannot apply per OS in the server status and
// docs/lsp.md.
type ProcessLimits struct {
	// MaxMemoryPercent is the server's memory cap as a percent of system memory,
	// or nil for none.
	MaxMemoryPercent *uint32 `json:"max_memory_percent,omitempty"`
	// MaxCPUPercent is the server's CPU cap, where 100 is one core.
	MaxCPUPercent *uint32 `json:"max_cpu_percent,omitempty"`
	// Enabled says whether Fresh would apply the limits.
	Enabled *bool `json:"enabled,omitempty"`
}

// Config is the effective set of settings after all layers are merged.
type Config struct {
	Theme                  string // theme value: a bundled name, builtin://name, a user-dir relative path, or file://; default "dark"
	Keymap                 string // keymap name as written in a file; empty when no file sets one
	Locale                 string // UI locale code ("pt-BR"); empty means automatic (LC_ALL/LC_MESSAGES/LANG, then en)
	TabSize                int    // columns per tab stop, default 4
	InsertSpaces           bool   // insert spaces for Tab, default true
	LineEnding             string // "auto", "lf", "crlf" or "cr"; default "auto"
	TrimTrailingWhitespace bool   // strip trailing blanks on save
	EnsureFinalNewline     bool   // end the file with a newline on save
	AutoIndent             bool   // copy indentation to new lines, default true
	LineNumbers            bool   // show the line-number gutter, default true
	ShowMenuBar            bool   // show the menu bar row, default true
	ExplorerShowHidden     bool   // list dot-files in the explorer
	ExplorerShowIgnored    bool   // list files matched by .gitignore in the explorer

	// FileExplorer is the file_explorer section: width, side and the navigation
	// behaviour options (see config/fileexplorer.go). Fresh's defaults.
	FileExplorer FileExplorerConfig

	// Contents is the contents section: settings of the native Contents (Markdown
	// outline) sidebar section, formerly Fresh's bundled markdown_toc plugin (see
	// config/contents.go).
	Contents ContentsConfig

	// Clipboard is the clipboard section (see config/clipboard.go): the child
	// OSC 52 clipboard escape is the implemented key.
	Clipboard ClipboardConfig

	// Update is the release-check section (see config/update.go): Fresh's
	// check_for_updates and the gvim extension that names the feed.
	Update UpdateConfig

	// The view keys set the starting state of the display toggles. The
	// defaults keep the layout gvim had before they existed.
	ShowTabBar              bool   // show the tab strip row, default true
	ShowStatusBar           bool   // show the status row, default true
	ShowPromptLine          bool   // show the prompt row under the status row, default false
	ShowVerticalScrollbar   bool   // draw the vertical scrollbar, default true
	ShowHorizontalScrollbar bool   // draw the horizontal scrollbar when lines are wider, default true
	LineWrap                bool   // wrap long lines, default true
	HighlightCurrentLine    bool   // tint the caret line, default true
	HighlightOccurrences    bool   // highlight the other occurrences of the word at the caret, default false
	ShowWhitespace          bool   // show spaces as dots and tabs as arrows, default false
	MouseCapture            bool   // gvim takes the mouse; false leaves it to the terminal, default true
	MouseHover              bool   // hover information on mouse movement, default false
	ScrollSync              bool   // scroll split views of one buffer together, default false
	CursorStyle             string // "block", "bar" or "underline"; default "block"
	UIIcons                 string // "auto", "plain" or "nerd" glyphs for interface controls; default "auto"
	UIIconsFont             string // font family the terminal uses, for ui_icons "auto"; default empty

	// Terminal is the terminal section: the shell override, close_on_exit,
	// scrollback_lines and mouse_forwarding (see terminal.go).
	Terminal TerminalConfig

	// Daemon is the daemon section (see daemon.go): the memory bound for the
	// materialized workspaces of a daemon host. Only the user config may set it.
	Daemon DaemonConfig

	// Env is the env section: the environment detectors (see env.go). Only the
	// user config may set it.
	Env EnvConfig

	// SSH is the ssh section (see ssh.go): host-key policy, host-key algorithm
	// preferences and keyring consent. User-layer only.
	SSH SSHConfig

	// AutoAttach makes a bare launch (no files, no direct-mode flags, a terminal on
	// stdin) attach to a daemon that already hosts this workspace instead of refusing
	// as a second writer. Default false (Fresh defaults true); an explicit --attach or
	// --daemon is unchanged, and no daemon is started when none is running.
	AutoAttach bool

	// PluginTimeout is the longest one plugin call may run, default 500ms. The
	// project layer may only lower it (see lowerPluginTimeout).
	PluginTimeout time.Duration

	// Plugins holds the plugins.<name> entries that the files set, or is nil when
	// no file sets any. Names are kept as written, known or not.
	Plugins map[string]PluginConfig

	// pluginUserEnabled remembers the plugins.<name>.enabled value from the USER
	// layer only. Built-in surfaces that were once bundled plugins (the native
	// Contents section) must ignore a project file's disabled flag, whose merged
	// value in Plugins.Enabled cannot be attributed to a layer.
	pluginUserEnabled map[string]bool

	// The fields below are the Fresh "editor" options that gvim added with the
	// nested config layout. Each default is Fresh's. The doc of the option and its
	// consumer are in editoropts.go and in the contract. The existing fields above
	// that have a Fresh name are aliases of editor.<name>: LineNumbers, LineWrap,
	// AutoIndent, TabSize, HighlightCurrentLine, HighlightOccurrences,
	// ShowMenuBar, ShowTabBar, ShowStatusBar, ShowPromptLine and the scrollbars.
	// InsertSpaces is the inverse of editor.use_tabs.
	EditorOptions

	// DefaultLanguage is default_language: the language whose settings apply to a
	// file whose type is not detected. Empty means none.
	DefaultLanguage string

	// Languages holds the languages.<id> entries that the files set, with ids
	// lowercased. It is nil when no file sets any. The built-in per-language
	// defaults (Go and Makefile use tabs, Markdown does not auto-close) are not
	// stored here: EditorFor merges them in.
	Languages map[string]LanguageSettings

	// Keybindings is the raw JSON of the user's keybinding overrides, or nil. It is the
	// list Fresh documents, [{"key": "s", "modifiers": ["ctrl"], "action": "save"}], or
	// an object {"bindings": [...]}. Only the user file can set it: the project layer
	// is refused with a diagnostic.
	Keybindings json.RawMessage

	// LSP maps a language ID to its language servers, in the order the config lists
	// them, or is nil when no file sets the lsp key. A language that is not in the map
	// runs the built-in server for it, if there is one and its program is on PATH.
	LSP map[string][]LSPServer

	// UniversalLSP maps a server name to language servers that run alongside those of
	// every language (universal_lsp), or is nil. The name identifies the server; a
	// name may hold several servers, as in Fresh.
	UniversalLSP map[string][]LSPServer

	// LSPDisabled holds the languages whose lsp entries are all "enabled": false, so
	// their built-in server does not run either. It is nil when there are none.
	LSPDisabled map[string]bool

	// LSPEnabled is the lsp_enabled master switch, default true. False starts no
	// language server on its own.
	LSPEnabled bool

	// Formatters maps a language name, or a file extension with its dot, to the shell
	// command that formats a buffer, or is nil when no file sets the formatters key.
	// The command reads the text on standard input and writes the result to standard
	// output. A buffer whose language and extension are not in the map has no formatter.
	Formatters map[string]string

	// sources maps each key that a file set to the path of that file.
	sources map[string]string
}

// Defaults returns the built-in settings, the lowest layer.
func Defaults() Config {
	return Config{
		Theme:        "dark",
		TabSize:      4,
		InsertSpaces: true,
		LineEnding:   LineEndingAuto,
		AutoIndent:   true,
		LineNumbers:  true,
		ShowMenuBar:  true,

		ShowTabBar:              true,
		ShowStatusBar:           true,
		ShowVerticalScrollbar:   true,
		ShowHorizontalScrollbar: true,
		LineWrap:                true,
		HighlightCurrentLine:    true,
		MouseCapture:            true,
		LSPEnabled:              true,
		CursorStyle:             CursorStyleBlock,
		UIIcons:                 UIIconsAuto,

		ExplorerShowHidden: true,
		PluginTimeout:      DefaultPluginTimeout,

		Terminal:      DefaultTerminalOptions(),
		Daemon:        DefaultDaemonConfig(),
		FileExplorer:  DefaultFileExplorer(),
		Env:           DefaultEnvConfig(),
		SSH:           DefaultSSHConfig(),
		Clipboard:     DefaultClipboard(),
		Update:        DefaultUpdate(),
		EditorOptions: defaultEditorOptions(),
	}
}

// PluginConfig is the plugins.<name> entry of the config files.
//
// The two layers are kept apart because they have different trust: the user layer
// is the user's own, the project layer comes from a repository that may be
// hostile. Settings holds the user layer's "settings" object and ProjectSettings
// the project layer's. They are never merged here. The plugin host merges a
// project value over the user value only for keys the plugin declared
// project = true.
type PluginConfig struct {
	// Enabled is the "enabled" flag, or nil when no file sets it. The project
	// layer can only set it to false.
	Enabled         *bool
	Settings        map[string]any // "settings" from the user layer, or nil
	ProjectSettings map[string]any // "settings" from the project layer, or nil
}

// layer is the kind of config file being applied.
type layer int

const (
	layerUser layer = iota
	layerProject
)

// Source returns the path of the file that last set key, or "" when no file
// sets it and the default applies. key is the setting name as the files write
// it, for example "keybindings".
func (c Config) Source(key string) string {
	return c.sources[key]
}

// Diagnostic describes one problem found while loading. Loading continues past
// it, and the value it names keeps the value from the layer below.
type Diagnostic struct {
	Path    string // file the problem is in; empty when no file is involved
	Key     string // setting name; empty when the whole file is affected
	Message string // what is wrong, for display
}

// String formats the diagnostic as "path: key: message", leaving out the parts
// that are empty.
func (d Diagnostic) String() string {
	s := d.Message
	if d.Key != "" {
		s = d.Key + ": " + s
	}
	if d.Path != "" {
		s = d.Path + ": " + s
	}
	return s
}

// Options selects the files Load reads.
type Options struct {
	// Root is the project directory. The project file is searched for from
	// Root upward, through each ancestor, and the search stops at the
	// filesystem root. An empty Root skips the project layer.
	Root string

	// UserFile is the user config file. An empty value means the default,
	// Dir()/config.json (~/.config/gvim/config.json).
	UserFile string

	// Untrusted skips the project layer: the workspace has no trust decision
	// yet, or the user restricted it, so a file that came with the repository
	// must not configure the editor. Only the user layer applies. The zero
	// value keeps the project layer, so the callers that have no trust
	// decision (the config viewer, the tests) read the layers as before. See
	// package trust and docs/trust.md.
	Untrusted bool
}

// Files lists the config files gvim reads by default.
type Files struct {
	User string // Dir()/config.json (~/.config/gvim/config.json)
}

// Paths returns the default config file locations. It fails only when
// Dir fails, which happens when no home directory is known.
func Paths() (Files, error) {
	user, err := defaultUserFile()
	if err != nil {
		return Files{}, err
	}
	return Files{User: user}, nil
}

// Dir returns the directory that holds what the user writes: config.json,
// init.lua, plugins and themes. It is gvim under $XDG_CONFIG_HOME when that is an
// absolute path, and ~/.config/gvim otherwise, on macOS as on Linux. Windows uses
// os.UserConfigDir. Runtime state (the recent list, trust decisions, plugin
// toggles, update stamps) lives in StateDir, never here. It fails only when no
// home directory is known.
func Dir() (string, error) {
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("config: user config dir: %w", err)
		}
		return filepath.Join(base, "gvim"), nil
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "gvim"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: user config dir: %w", err)
	}
	return filepath.Join(home, ".config", "gvim"), nil
}

// StateDir returns the directory of gvim's runtime state: the recent project
// list, the workspace trust decisions, the plugin toggles and the update-check
// stamp. None of it is configuration, and none of it lives in the project: a
// repository must not be able to vouch for itself. It is gvim under
// $XDG_STATE_HOME when that is an absolute path, and ~/.local/state/gvim
// otherwise. Windows uses os.UserConfigDir()/gvim/state.
func StateDir() (string, error) {
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("config: user config dir: %w", err)
		}
		return filepath.Join(base, "gvim", "state"), nil
	}
	if base := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "gvim"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: user state dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "gvim"), nil
}

// CacheDir returns gvim's regenerable cache directory (fetched URL themes).
// It is gvim under os.UserCacheDir().
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("config: user cache dir: %w", err)
	}
	return filepath.Join(base, "gvim"), nil
}

// MoveLegacyFile renames oldPath to newPath when the new file does not exist yet
// and the old one does. It is the one-time move of a state file that left the
// config directory; a failure is reported to the caller, which then simply starts
// without the old file.
func MoveLegacyFile(oldPath, newPath string) error {
	if _, err := os.Stat(newPath); err == nil {
		return nil
	}
	if _, err := os.Stat(oldPath); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	return nil
}

// defaultUserFile returns the default user config file path.
func defaultUserFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// ProjectFile returns the nearest .gvim/config.json at or above root. ok is
// false when no such file exists between root and the filesystem root. err is
// non-nil only when root cannot be made absolute.
func ProjectFile(root string) (path string, ok bool, err error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false, fmt.Errorf("config: project root: %w", err)
	}
	path, ok = findUp(abs, isRegularFile)
	return path, ok, nil
}

// findUp checks dir/.gvim/config.json, then the same file in each parent
// directory, and returns the first that isFile reports as present. It stops
// after dir reaches the filesystem root, where filepath.Dir returns its input.
func findUp(dir string, isFile func(string) bool) (string, bool) {
	for {
		candidate := filepath.Join(dir, ".gvim", "config.json")
		if isFile(candidate) {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Load reads the three layers and returns the merged settings. The returned
// Diagnostics list every file or key that was skipped. A missing file is not a
// problem and produces no Diagnostic.
//
// The error is non-nil only when opts.Root is set and cannot be made absolute.
// In that case the settings returned are the defaults with the user layer
// applied.
func Load(opts Options) (Config, []Diagnostic, error) {
	cfg := Defaults()
	var diags []Diagnostic

	userPath := opts.UserFile
	if userPath == "" {
		p, err := defaultUserFile()
		if err != nil {
			diags = append(diags, Diagnostic{Message: err.Error()})
		} else {
			userPath = p
		}
	}
	if userPath != "" {
		diags = append(diags, applyFile(&cfg, userPath, layerUser)...)
	}

	if opts.Root == "" || opts.Untrusted {
		return cfg, diags, nil
	}
	projectPath, ok, err := ProjectFile(opts.Root)
	if err != nil {
		return cfg, diags, err
	}
	if ok {
		diags = append(diags, applyFile(&cfg, projectPath, layerProject)...)
	}
	return cfg, diags, nil
}

// applyFile reads one layer file and applies each of its keys to cfg. It
// returns the diagnostics for the file. A missing file applies nothing. l says
// which layer the file is: the project layer is restricted for the plugin keys.
func applyFile(cfg *Config, path string, l layer) []Diagnostic {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []Diagnostic{{Path: path, Message: err.Error()}}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	standard, err := hujson.Standardize(data)
	if err != nil {
		return []Diagnostic{{Path: path, Message: "invalid JSON with comments: " + err.Error()}}
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(standard, &keys); err != nil {
		return []Diagnostic{{Path: path, Message: "top level must be a JSON object: " + err.Error()}}
	}
	return applyObject(cfg, keys, path, l)
}

// applyObject applies the top-level keys of one config layer to cfg. It is the
// one place a layer's keys become settings, shared by applyFile and by
// ValidateEdits, so the Settings UI validates a value through exactly the same
// tables and project-layer rules as a real load. path is the file the keys came
// from, for the source notes and the diagnostics; it may be empty.
func applyObject(cfg *Config, keys map[string]json.RawMessage, path string, l layer) []Diagnostic {
	var diags []Diagnostic
	// The flat keys are applied first and the nested sections after them, so that
	// when a file sets both spellings of an option the nested one wins.
	for _, key := range sortedKeys(keys) {
		if isSection(key) {
			continue
		}
		switch {
		case key == "plugins":
			applied, failures := cfg.setPlugins(keys[key], l)
			for _, k := range applied {
				cfg.note(k, path)
			}
			for _, f := range failures {
				diags = append(diags, Diagnostic{Path: path, Key: f.key, Message: f.err.Error()})
			}
			continue
		case (key == "check_for_updates" || key == "update_check_url") && l == layerProject:
			// The release check contacts a host; a repository must not choose it or
			// silence the user check. Say so instead of ignoring it.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set " + key + ": the release check is a user preference, so set it in the user config"})
			continue
		case key == "update_command" && l == layerProject:
			// A repository must never choose a command the editor will run.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set update_command: the update command is a user preference, so set it in the user config"})
			continue
		case key == "locale" && l == layerProject:
			// The UI language is a user preference; a repository choosing it would be a
			// surprise, so the project layer is refused like keymap.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set locale: the UI language is a user preference, so set it in the user config"})
			continue
		case key == "plugin_timeout_ms" && l == layerProject:
			if err := cfg.lowerPluginTimeout(keys[key]); err != nil {
				diags = append(diags, Diagnostic{Path: path, Key: key, Message: err.Error()})
				continue
			}
		case (key == "keybindings" || key == "keymap" || key == "active_keybinding_map" || key == "keybinding_maps") && l == layerProject:
			// A repository must not decide what the user's keys do, so the project layer
			// can neither switch the keymap nor bind a key. Say so instead of ignoring it.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set " + key + ": it changes what keys do, so set it in the user config"})
			continue
		case key == "terminal":
			diags = append(diags, cfg.setTerminal(keys[key], l, path)...)
			cfg.note(key, path)
			continue
		case key == "contents":
			diags = append(diags, cfg.setContents(keys[key], l, path)...)
			cfg.note(key, path)
			continue
		case key == "auto_attach" && l == layerProject:
			// Attaching to a running daemon is the user's choice; a repository cannot
			// redirect launches into a daemon.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set auto_attach: it changes what a launch does, so set it in the user config"})
			continue
		case key == "auto_attach":
			var autoAttach bool
			if err := json.Unmarshal(keys[key], &autoAttach); err != nil {
				diags = append(diags, Diagnostic{Path: path, Key: key, Message: "must be true or false"})
				continue
			}
			cfg.AutoAttach = autoAttach
			cfg.note(key, path)
			continue
		case key == "ssh" && l == layerProject:
			// Host-key verification and keyring consent are security choices of the user;
			// a repository must not weaken them for its own host.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set ssh: host-key verification and keyring consent are user preferences, so set them in the user config"})
			continue
		case key == "ssh":
			diags = append(diags, cfg.setSSH(keys[key], path)...)
			continue
		case key == "daemon" && l == layerProject:
			// The daemon's memory bound is a user preference: a repository cannot spend
			// the daemon's memory on warm workspaces of its choosing.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set daemon: it bounds the daemon's memory, so set it in the user config"})
			continue
		case key == "daemon":
			diags = append(diags, cfg.setDaemon(keys[key])...)
			cfg.note(key, path)
			continue
		case key == "env" && l == layerProject:
			// A repository must not decide which activation command runs in it, so the
			// project layer cannot define detectors. Say so instead of ignoring it.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set env: it defines commands that run in the workspace, so set it in the user config"})
			continue
		case key == "env":
			diags = append(diags, cfg.setEnv(keys[key])...)
			cfg.note(key, path)
			continue
		case key == "universal_lsp" && l == layerProject:
			// A universal server runs for every buffer, so a repository naming one would
			// run a program of its choosing in a workspace the user only opened.
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: "project config cannot set universal_lsp: it chooses a program to run, so set it in the user config"})
			continue
		case key == "lsp" && l == layerProject:
			// A repository must not choose the program gvim starts, so the project layer
			// can only switch servers off or on, never name a command.
			ds, ok := cfg.setProjectLSP(path, keys[key])
			diags = append(diags, ds...)
			if !ok {
				continue
			}
		default:
			if err := cfg.set(key, keys[key]); err != nil {
				diags = append(diags, Diagnostic{Path: path, Key: key, Message: err.Error()})
				continue
			}
		}
		cfg.note(key, path)
		if key == "active_keybinding_map" {
			cfg.note("keymap", path)
		}
	}
	for _, section := range sections {
		raw, ok := keys[section]
		if !ok {
			continue
		}
		applied, failures := cfg.setSection(section, raw)
		for _, key := range applied {
			cfg.note(key, path)
		}
		for _, f := range failures {
			diags = append(diags, Diagnostic{Path: path, Key: f.key, Message: f.err.Error()})
		}
	}
	return diags
}

// note records that the file at path set key. A later layer that sets the key
// replaces the record.
func (c *Config) note(key, path string) {
	if c.sources == nil {
		c.sources = make(map[string]string)
	}
	c.sources[key] = path
}

// sortedKeys returns the keys in order, so diagnostics come out in a stable
// order.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// set applies one setting. On error the field is left unchanged.
func (c *Config) set(key string, raw json.RawMessage) error {
	if isNull(raw) {
		if key == "locale" {
			// Fresh writes null for the automatic locale.
			c.Locale = ""
			return nil
		}
		return fmt.Errorf("null is not a valid value")
	}
	switch key {
	case "locale":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if s == "" {
			return fmt.Errorf("must be a locale code or auto")
		}
		if strings.EqualFold(s, "auto") {
			c.Locale = ""
			return nil
		}
		// The i18n package normalises and falls back; the config only has to reject a
		// value that is not a string.
		c.Locale = s
	case "theme":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if s == "" {
			return fmt.Errorf("must not be empty")
		}
		c.Theme = s
	case "keymap", "active_keybinding_map":
		// Fresh's documented name for the keymap is active_keybinding_map, so a Fresh
		// config picks the same keymap here. keymap is processed after it, so it wins when
		// a file has both.
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		c.Keymap = s
	case "keybinding_maps":
		// Fresh's custom keymap files. gvim has none: its own bindings go in keybindings.
		if trimmed := bytes.TrimSpace(raw); string(trimmed) != "{}" {
			return fmt.Errorf("custom keybinding maps are not supported; put the keys in keybindings")
		}
	case "daemon":
		if diags := c.setDaemon(raw); len(diags) > 0 {
			return errors.New(diags[0].Message)
		}
	case "plugin_timeout_ms":
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return fmt.Errorf("must be an integer")
		}
		if n < MinPluginTimeoutMS || n > MaxPluginTimeoutMS {
			return fmt.Errorf("must be between %d and %d, got %d", MinPluginTimeoutMS, MaxPluginTimeoutMS, n)
		}
		c.PluginTimeout = time.Duration(n) * time.Millisecond
	case "ui_icons":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if !slices.Contains([]string{UIIconsAuto, UIIconsPlain, UIIconsNerd}, s) {
			return fmt.Errorf("must be %q, %q or %q, got %q", UIIconsAuto, UIIconsPlain, UIIconsNerd, s)
		}
		c.UIIcons = s
	case "ui_icons_font":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		c.UIIconsFont = strings.TrimSpace(s)
	case "keybindings":
		trimmed := bytes.TrimSpace(raw)
		if isEmptyArray(trimmed) {
			// Fresh writes "keybindings": [] for no overrides.
			return nil
		}
		if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
			return fmt.Errorf("must be a list of bindings")
		}
		c.Keybindings = append(json.RawMessage(nil), trimmed...)
	case "default_language":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		c.DefaultLanguage = strings.TrimSpace(s)
	case "lsp":
		servers, disabled, err := decodeLSP(raw)
		if err != nil {
			return err
		}
		c.LSP, c.LSPDisabled = servers, disabled
	case "universal_lsp":
		servers, err := decodeUniversalLSP(raw)
		if err != nil {
			return err
		}
		c.UniversalLSP = servers
	case "formatters":
		formatters, err := decodeFormatters(raw)
		if err != nil {
			return err
		}
		c.Formatters = formatters
	case "check_for_updates", "update_check_url", "update_command":
		return c.setUpdate(key, raw)
	case "terminal":
		// The flat setters are the user layer: a project file never comes here,
		// see applyFile, which checks the terminal object against its layer.
		if diags := c.setTerminal(raw, layerUser, ""); len(diags) > 0 {
			return errors.New(diags[0].Message)
		}
	default:
		if slices.Contains(fileOnlyKeys, key) {
			return nil // a Fresh key that gvim has no use for
		}
		if opt, ok := editorByFlat[key]; ok {
			return opt.set(c, raw)
		}
		field, known := boolKeys[key]
		if !known {
			return fmt.Errorf("unknown setting")
		}
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("must be true or false")
		}
		*field(c) = v
	}
	return nil
}

// boolKeys maps each boolean setting that has no Fresh counterpart to the field it
// sets. The settings that do have one are in editorOptions.
var boolKeys = map[string]func(*Config) *bool{
	"insert_spaces":         func(c *Config) *bool { return &c.InsertSpaces },
	"explorer_show_hidden":  func(c *Config) *bool { return &c.ExplorerShowHidden },
	"explorer_show_ignored": func(c *Config) *bool { return &c.ExplorerShowIgnored },
	"mouse_capture":         func(c *Config) *bool { return &c.MouseCapture },
	"scroll_sync":           func(c *Config) *bool { return &c.ScrollSync },
	"lsp_enabled":           func(c *Config) *bool { return &c.LSPEnabled },
}

// lspEntry is one language server entry, as Fresh writes it. gvim keeps every field:
// the program, its environment and initialization options, the feature filters and the
// process limits (parsed but not enforced). auto_start and "enabled" are pointers so the
// code can tell "set" from "unset".
type lspEntry struct {
	Command     string           `json:"command"`
	Args        []string         `json:"args"`
	Enabled     *bool            `json:"enabled"`
	AutoStart   *json.RawMessage `json:"auto_start"`
	RootMarkers []string         `json:"root_markers"`

	Name                  *string           `json:"name"`
	Env                   map[string]string `json:"env"`
	LanguageIDOverrides   map[string]string `json:"language_id_overrides"`
	InitializationOptions json.RawMessage   `json:"initialization_options"`
	OnlyFeatures          []string          `json:"only_features"`
	ExceptFeatures        []string          `json:"except_features"`
	ProcessLimits         *ProcessLimits    `json:"process_limits"`
}

// lspFeatureNames are the feature names only_features and except_features accept:
// Fresh's LspFeature variants in snake_case.
var lspFeatureNames = map[string]bool{
	"diagnostics": true, "completion": true, "code_action": true,
	"document_symbols": true, "workspace_symbols": true,
	"hover": true, "definition": true, "implementation": true, "references": true,
	"format": true, "rename": true, "signature_help": true, "inlay_hints": true,
	"folding_range": true, "semantic_tokens": true, "document_highlight": true,
}

// decodeLSP reads the lsp key: an object from language ID to a server or a list of
// servers (Fresh accepts both forms). Each server must name a command, unless it turns a
// built-in server on. A server with "enabled": false starts nothing; a language whose
// servers are all disabled is returned in disabled, so its built-in server stays off too.
// The servers of a language keep the order the config lists them in.
func decodeLSP(raw json.RawMessage) (servers map[string][]LSPServer, disabled map[string]bool, err error) {
	trimmed := bytes.TrimSpace(raw)
	var entries map[string]json.RawMessage
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &entries) != nil {
		return nil, nil, fmt.Errorf("must be a JSON object")
	}
	servers = make(map[string][]LSPServer, len(entries))
	for _, lang := range sortedKeys(entries) {
		list, off, err := decodeServerList(lang, entries[lang])
		if err != nil {
			return nil, nil, err
		}
		if len(list) > 0 {
			servers[lang] = list
			continue
		}
		if off {
			if disabled == nil {
				disabled = make(map[string]bool)
			}
			disabled[lang] = true
		}
	}
	return servers, disabled, nil
}

// decodeUniversalLSP reads the universal_lsp key: an object from server name to a
// server or a list of servers that run for every language. Entries with
// "enabled": false are dropped; the key replaces the whole map when a later layer sets
// it.
func decodeUniversalLSP(raw json.RawMessage) (map[string][]LSPServer, error) {
	trimmed := bytes.TrimSpace(raw)
	var entries map[string]json.RawMessage
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &entries) != nil {
		return nil, fmt.Errorf("must be a JSON object")
	}
	servers := make(map[string][]LSPServer, len(entries))
	for _, name := range sortedKeys(entries) {
		if name == "" {
			return nil, fmt.Errorf("server name must not be empty")
		}
		list, _, err := decodeServerList("universal_lsp."+name, entries[name])
		if err != nil {
			return nil, err
		}
		if len(list) > 0 {
			servers[name] = list
		}
	}
	if len(servers) == 0 {
		return nil, nil
	}
	return servers, nil
}

// decodeServerList reads one lsp or universal_lsp entry: a server object, or an array
// of server objects. It returns the enabled servers in order, and whether an entry was
// explicitly turned off. what names the entry in a diagnostic.
func decodeServerList(what string, raw json.RawMessage) (servers []LSPServer, anyDisabled bool, err error) {
	list := []json.RawMessage{raw}
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '[' {
		if err := json.Unmarshal(t, &list); err != nil {
			return nil, false, fmt.Errorf("%s: %w", what, err)
		}
	}
	for _, item := range list {
		var e lspEntry
		if err := decodeStrict(item, &e); err != nil {
			return nil, false, fmt.Errorf("%s: %w", what, err)
		}
		if e.Enabled != nil && !*e.Enabled {
			anyDisabled = true
			continue
		}
		if e.Command == "" && !turnsOn(e) && !setsAutoStart(e) {
			return nil, false, fmt.Errorf("%s: command must not be empty", what)
		}
		if err := validLSPEntry(what, e); err != nil {
			return nil, false, err
		}
		servers = append(servers, lspServer(e))
	}
	return servers, anyDisabled, nil
}

// validLSPEntry checks the fields of one entry that gvim reads.
func validLSPEntry(what string, e lspEntry) error {
	for _, f := range e.OnlyFeatures {
		if !lspFeatureNames[f] {
			return fmt.Errorf("%s: unknown feature %q in only_features", what, f)
		}
	}
	for _, f := range e.ExceptFeatures {
		if !lspFeatureNames[f] {
			return fmt.Errorf("%s: unknown feature %q in except_features", what, f)
		}
	}
	if p := e.ProcessLimits; p != nil {
		if m := p.MaxMemoryPercent; m != nil && (*m < 1 || *m > 100) {
			return fmt.Errorf("%s: max_memory_percent must be between 1 and 100, got %d", what, *m)
		}
		if c := p.MaxCPUPercent; c != nil && (*c < 1 || *c > 1000) {
			return fmt.Errorf("%s: max_cpu_percent must be between 1 and 1000 (100 is one core), got %d", what, *c)
		}
	}
	return nil
}

// lspServer converts one decoded entry to the effective server. Slices and maps are
// copied so a later edit of the raw config cannot change a running server.
func lspServer(e lspEntry) LSPServer {
	initOptions := slices.Clone(e.InitializationOptions)
	if isNull(initOptions) {
		initOptions = nil
	}
	s := LSPServer{
		Command:               e.Command,
		Args:                  slices.Clone(e.Args),
		Dormant:               autoStartOff(e),
		RootMarkers:           slices.Clone(e.RootMarkers),
		Env:                   maps.Clone(e.Env),
		LanguageIDOverrides:   maps.Clone(e.LanguageIDOverrides),
		InitializationOptions: initOptions,
		OnlyFeatures:          slices.Clone(e.OnlyFeatures),
		ExceptFeatures:        slices.Clone(e.ExceptFeatures),
		ProcessLimits:         e.ProcessLimits,
	}
	if e.Name != nil {
		s.Name = *e.Name
	}
	return s
}

// setProjectLSP applies the lsp key of the project file. Unlike the user's, it adds to the
// entries below it instead of replacing them, and it cannot run a program of the
// repository's choosing: an entry with a command is dropped, and the fields that shape the
// program (args, name, env, initialization_options, process_limits) are removed, each with
// a diagnostic that says to set it in the user config. What is left switches languages off
// ("enabled": false), turns a built-in server on, keeps it dormant (auto_start), names the
// root markers or routes its features. An entry cannot replace the user's entry for the
// language. ok is false when the key is not valid, and the diagnostic says why.
func (c *Config) setProjectLSP(path string, raw json.RawMessage) (diags []Diagnostic, ok bool) {
	servers, disabled, err := decodeLSP(raw)
	if err != nil {
		return []Diagnostic{{Path: path, Key: "lsp", Message: err.Error()}}, false
	}
	refuse := func(lang, what string) {
		diags = append(diags, Diagnostic{Path: path, Key: "lsp", Message: fmt.Sprintf(
			"project config cannot set lsp.%s.%s: it chooses or shapes a program to run, so set it in the user config", lang, what)})
	}
	for _, lang := range slices.Sorted(maps.Keys(servers)) {
		var kept []LSPServer
		for _, s := range servers[lang] {
			if s.Command != "" {
				// The entry would run the repository's own program: drop it whole, and
				// do not report its other fields one by one.
				refuse(lang, "command")
				continue
			}
			keep := true
			if len(s.Args) > 0 {
				refuse(lang, "args")
				s.Args = nil
			}
			if s.Name != "" {
				refuse(lang, "name")
				s.Name = ""
			}
			if len(s.Env) > 0 {
				refuse(lang, "env")
				s.Env = nil
			}
			if len(s.InitializationOptions) > 0 {
				refuse(lang, "initialization_options")
				s.InitializationOptions = nil
			}
			if s.ProcessLimits != nil {
				refuse(lang, "process_limits")
				s.ProcessLimits = nil
			}
			if keep {
				kept = append(kept, s)
			}
		}
		// An entry cannot replace the user's entry for the language: the user's
		// program is what runs. The project may still add one for a language the
		// user has none for, to turn a built-in server on.
		if _, has := c.LSP[lang]; has {
			continue
		}
		if len(kept) == 0 {
			continue
		}
		if c.LSP == nil {
			c.LSP = make(map[string][]LSPServer)
		}
		c.LSP[lang] = kept
	}
	for lang := range disabled {
		if c.LSPDisabled == nil {
			c.LSPDisabled = make(map[string]bool)
		}
		c.LSPDisabled[lang] = true
	}
	return diags, true
}

// turnsOn reports whether an entry without a command asks for the built-in server:
// "enabled": true or "auto_start": true.
func turnsOn(e lspEntry) bool {
	return (e.Enabled != nil && *e.Enabled) || autoStartIs(e, true)
}

// setsAutoStart reports whether the entry gives auto_start a boolean value.
func setsAutoStart(e lspEntry) bool { return autoStartIs(e, true) || autoStartIs(e, false) }

// autoStartOff reports whether the entry sets "auto_start": false.
func autoStartOff(e lspEntry) bool { return autoStartIs(e, false) }

// autoStartIs reports whether the entry's auto_start is the boolean want.
func autoStartIs(e lspEntry, want bool) bool {
	var b bool
	return e.AutoStart != nil && json.Unmarshal(*e.AutoStart, &b) == nil && b == want
}

// decodeFormatters reads the formatters key: an object from language name to the
// command that formats it. Each command must be non-blank, and each language name
// must be non-empty.
func decodeFormatters(raw json.RawMessage) (map[string]string, error) {
	var formatters map[string]string
	if err := json.Unmarshal(raw, &formatters); err != nil {
		return nil, fmt.Errorf("must be a JSON object of strings")
	}
	for _, lang := range slices.Sorted(maps.Keys(formatters)) {
		if lang == "" {
			return nil, fmt.Errorf("language name must not be empty")
		}
		if strings.TrimSpace(formatters[lang]) == "" {
			return nil, fmt.Errorf("%s: command must not be empty", lang)
		}
	}
	return formatters, nil
}

func decodeString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("must be a string")
	}
	return s, nil
}

func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// isEmptyArray reports whether raw, already trimmed, is "[]" with optional blanks inside.
func isEmptyArray(raw []byte) bool {
	return len(raw) >= 2 && raw[0] == '[' && len(bytes.TrimSpace(raw[1:len(raw)-1])) == 0 && raw[len(raw)-1] == ']'
}
