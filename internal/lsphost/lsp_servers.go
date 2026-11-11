package lsphost

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/manager"
)

// NewFor returns the language-server port for a config: the built-in table of
// servers (manager.Defaults, which mirrors Fresh's) with the lsp and universal_lsp
// keys laid over it. The port always exists, so that its messages can say why a
// language has no server. Nothing is looked up or started here: a server starts
// when the first document of its language opens, and the PATH search happens
// there, in the command that opens the document.
func NewFor(cfg config.Config) tui.LSPSession {
	return NewForEnv(cfg, nil)
}

// NewForEnv is NewFor with the active project environment's set-only overlay,
// applied to every language-server process it starts. spawnEnv may be nil.
func NewForEnv(cfg config.Config, spawnEnv func() []string) tui.LSPSession {
	plan := lspPlanFor(cfg)
	return newPort(plan, cfg.LSPEnabled, spawnEnv)
}

// lspPlan is the effective server table of a config.
type lspPlan struct {
	// servers holds the configured servers by the names a language is detected
	// under, in config order. Built-in languages get their default entry, laid
	// over with the user's lsp entries.
	servers map[string][]manager.ServerConfig
	// universal holds the universal_lsp servers, which serve every language.
	universal []manager.ServerConfig
	// info describes each language for the messages: whether it has a server, and
	// what the configuration does with it.
	info map[string]tui.LSPServerInfo
	// auto holds the languages with a server that starts on its own.
	auto map[string]bool
	// startable holds the languages with any configured server, auto or manual.
	startable map[string]bool
}

// lspPlanFor builds the effective server table of cfg.
func lspPlanFor(cfg config.Config) lspPlan {
	plan := lspPlan{
		servers:   make(map[string][]manager.ServerConfig),
		info:      make(map[string]tui.LSPServerInfo),
		auto:      make(map[string]bool),
		startable: make(map[string]bool),
	}
	handled := make(map[string]bool) // the config keys the built-in table used
	for _, spec := range manager.Defaults() {
		user, userKey, hasUser := userEntries(cfg, spec)
		if hasUser {
			handled[userKey] = true
		}
		state := tui.LSPServerOn
		switch {
		case !cfg.LSPEnabled:
			state = tui.LSPServerOffAll
		case cfg.LSPDisabled[spec.Language] || disabledByAlias(cfg, spec):
			state = tui.LSPServerOffLanguage
		case spec.Manual && !hasUser:
			state = tui.LSPServerManual
		}
		configs := mergeUserEntries(spec, user, hasUser, state)
		if hasUser && state == tui.LSPServerOn && !anyAutoStart(configs) {
			// Every entry says auto_start: false, so the language has a manual
			// server: the status row says so, and Start/Restart runs it.
			state = tui.LSPServerManual
		}
		if len(configs) > 0 {
			plan.put(spec, configs, state, configs[0].Command[0], spec.Install)
		} else {
			plan.put(spec, nil, state, "", spec.Install)
		}
	}
	// A language the table does not know runs the program the config names.
	for _, lang := range slices.Sorted(maps.Keys(cfg.LSP)) {
		if handled[lang] {
			continue
		}
		entries := cfg.LSP[lang]
		var configs []manager.ServerConfig
		for _, entry := range entries {
			if entry.Command == "" {
				continue
			}
			configs = append(configs, serverConfig(entry, manager.Spec{}))
		}
		state := tui.LSPServerOn
		switch {
		case !cfg.LSPEnabled:
			state = tui.LSPServerOffAll
		case len(configs) == 0 || entries[0].Dormant:
			state = tui.LSPServerManual
		}
		command := ""
		if len(configs) > 0 {
			command = configs[0].Command[0]
		}
		plan.servers[lang] = configs
		plan.info[lang] = tui.LSPServerInfo{State: state, Language: lang, Command: command}
		if len(configs) > 0 {
			plan.startable[lang] = true
			if state == tui.LSPServerOn {
				plan.auto[lang] = true
			}
		}
	}
	for lang := range cfg.LSPDisabled {
		if _, known := plan.info[lang]; !known {
			plan.info[lang] = tui.LSPServerInfo{State: tui.LSPServerOffLanguage, Language: lang}
		}
	}
	// Universal servers serve every language.
	for _, name := range slices.Sorted(maps.Keys(cfg.UniversalLSP)) {
		for _, entry := range cfg.UniversalLSP[name] {
			if entry.Command == "" {
				continue
			}
			plan.universal = append(plan.universal, serverConfig(entry, manager.Spec{}))
		}
	}
	return plan
}

// put records the servers of one built-in language under every name it is detected
// by, so a document detected as "c++" finds the servers of "cpp".
func (p *lspPlan) put(spec manager.Spec, configs []manager.ServerConfig, state tui.LSPServerState, command, install string) {
	for _, name := range spec.Names() {
		p.servers[name] = configs
		p.info[name] = tui.LSPServerInfo{State: state, Language: spec.Language, Command: command, Install: install}
		if len(configs) > 0 {
			p.startable[name] = true
			if state == tui.LSPServerOn && anyAutoStart(configs) {
				p.auto[name] = true
			}
		}
	}
}

// anyAutoStart reports whether one of the servers starts on its own.
func anyAutoStart(configs []manager.ServerConfig) bool {
	return slices.ContainsFunc(configs, func(c manager.ServerConfig) bool { return c.AutoStart })
}

// userEntries finds the lsp entries that apply to a built-in language: the ones
// under its own name, or under one of its aliases. It returns the key they were
// found under.
func userEntries(cfg config.Config, spec manager.Spec) ([]config.LSPServer, string, bool) {
	for _, name := range spec.Names() {
		if list, ok := cfg.LSP[name]; ok && len(list) > 0 {
			return list, name, true
		}
	}
	return nil, "", false
}

// disabledByAlias reports whether the config disabled the language under one of its aliases.
func disabledByAlias(cfg config.Config, spec manager.Spec) bool {
	return slices.ContainsFunc(spec.Aliases, func(a string) bool { return cfg.LSPDisabled[a] })
}

// mergeUserEntries builds the effective servers of a built-in language: the
// built-in entry when the user gives none, else one server per user entry, each
// laid over the built-in default (a command and args may be replaced, an entry
// that only turns the server on keeps the built-in program).
func mergeUserEntries(spec manager.Spec, user []config.LSPServer, hasUser bool, state tui.LSPServerState) []manager.ServerConfig {
	if !hasUser {
		cfg := serverConfig(config.LSPServer{}, spec)
		cfg.AutoStart = state == tui.LSPServerOn
		return []manager.ServerConfig{cfg}
	}
	out := make([]manager.ServerConfig, 0, len(user))
	for _, entry := range user {
		cfg := serverConfig(entry, spec)
		cfg.AutoStart = state == tui.LSPServerOn && !entry.Dormant
		out = append(out, cfg)
	}
	return out
}

// serverConfig converts one config entry (or none, for a built-in default) to the
// server the Manager runs.
func serverConfig(entry config.LSPServer, spec manager.Spec) manager.ServerConfig {
	cfg := manager.ServerConfig{
		Command:               []string{spec.Command},
		Args:                  slices.Clone(spec.Args),
		Install:               spec.Install,
		LanguageID:            spec.LanguageID,
		LanguageIDOverrides:   maps.Clone(entry.LanguageIDOverrides),
		RootMarkers:           slices.Clone(spec.RootMarkers),
		Env:                   maps.Clone(entry.Env),
		InitializationOptions: entry.InitializationOptions,
		OnlyFeatures:          slices.Clone(entry.OnlyFeatures),
		ExceptFeatures:        slices.Clone(entry.ExceptFeatures),
	}
	if spec.Command == "" && spec.Install == "" && len(entry.RootMarkers) > 0 {
		cfg.RootMarkers = nil
	}
	if entry.Name != "" {
		cfg.Name = entry.Name
	}
	if entry.Command != "" {
		cfg.Command = []string{entry.Command}
		cfg.Args = slices.Clone(entry.Args)
		// The install hint belongs to the program it names; a replaced program has
		// none.
		cfg.Install = ""
	} else if entry.Args != nil {
		cfg.Args = slices.Clone(entry.Args)
	}
	if entry.RootMarkers != nil {
		cfg.RootMarkers = slices.Clone(entry.RootMarkers)
	}
	if entry.ProcessLimits != nil {
		// Fresh's process_limits: enabled unless the entry says otherwise, and a limit
		// with no cap applies nothing.
		enabled := true
		if entry.ProcessLimits.Enabled != nil {
			enabled = *entry.ProcessLimits.Enabled
		}
		cfg.Limits = lsp.ProcessLimits{
			Enabled:          enabled,
			MaxMemoryPercent: entry.ProcessLimits.MaxMemoryPercent,
			MaxCPUPercent:    entry.ProcessLimits.MaxCPUPercent,
		}
	}
	return cfg
}

// openLanguage is the language a document is opened under. TypeScript's detection gives a
// .tsx file the name of TypeScript, but the server must be told "typescriptreact", or it
// reads the JSX as a syntax error. A .h file in a directory with a C++ source or header is
// opened as C++, as Fresh does, so that clangd reads it as C++.
func openLanguage(path, lang string) string {
	if lang == "typescript" && strings.EqualFold(filepath.Ext(path), ".tsx") {
		if _, ok := manager.DefaultFor("tsx"); ok {
			return "tsx"
		}
	}
	if lang == "c" && filepath.Ext(path) == ".h" && cppSibling(filepath.Dir(path)) {
		if _, ok := manager.DefaultFor("cpp"); ok {
			return "cpp"
		}
	}
	return lang
}

// cppSiblingExts are the extensions that mark a directory as C++, Fresh's list
// (crates/fresh-editor-core/src/language_detect.rs). They are matched exactly, as Fresh does.
var cppSiblingExts = []string{".cc", ".cpp", ".cxx", ".C", ".c++", ".hpp", ".hh", ".hxx"}

// cppSibling reports whether dir holds a file with a C++ extension. It reads the directory,
// so it runs in the command that opens the document, never in the update loop. A directory that
// cannot be read has none. Fresh also looks for a C++ compile_commands.json in the ancestors;
// gvim does not, so a header there is opened as C.
func cppSibling(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && slices.Contains(cppSiblingExts, filepath.Ext(e.Name())) {
			return true
		}
	}
	return false
}

// Server describes the server of a language from the configuration. It does no
// lookup, so the messages can say why a language has no running server.
func (p *lspPort) Server(languageID string) tui.LSPServerInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	if info, ok := p.info[languageID]; ok {
		return info
	}
	if list, ok := p.servers[languageID]; ok && len(list) > 0 {
		return tui.LSPServerInfo{State: tui.LSPServerOn, Language: languageID, Command: list[0].Command[0]}
	}
	return tui.LSPServerInfo{}
}

// serverRoot returns the root a server runs in for the file at path. With markers, as in
// Fresh's root_markers, it is the nearest directory from the file's own upward that holds one
// of them (a name, or a glob such as "*.csproj"). The walk stops at the workspace root when
// the file is inside it, so a stray marker above the workspace is not taken. With no markers,
// or when none is found, it is the workspace root. It reads the file system, so it runs in the
// command that opens the document.
func serverRoot(path string, markers []string, workspace string) string {
	if len(markers) == 0 || path == "" {
		return workspace
	}
	inside := false
	if workspace != "" {
		rel, err := filepath.Rel(workspace, path)
		inside = err == nil && !strings.HasPrefix(rel, "..")
	}
	for dir := filepath.Dir(path); ; {
		if hasMarker(dir, markers) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || (inside && dir == filepath.Clean(workspace)) {
			return workspace
		}
		dir = parent
	}
}

// hasMarker reports whether dir holds a file or directory that one of the markers names.
func hasMarker(dir string, markers []string) bool {
	for _, m := range markers {
		if strings.ContainsAny(m, "*?[") {
			if hits, _ := filepath.Glob(filepath.Join(dir, m)); len(hits) > 0 {
				return true
			}
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}
