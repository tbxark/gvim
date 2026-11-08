package app

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/lsp"
	"github.com/tbxark/gvim/internal/ui"
)

// newLSPPortFor returns the language-server port for a config: the built-in table of
// servers (lsp.Defaults, which mirrors Fresh's) with the lsp key laid over it. The port always
// exists, so that its messages can say why a language has no server. Nothing is looked up
// or started here: a server starts when the first document of its language opens, and the
// PATH search happens there, in the command that opens the document.
func newLSPPortFor(cfg config.Config) ui.LSPSession {
	servers, startable, info := lspTable(cfg)
	return &lspPort{servers: servers, startable: startable, info: info, managers: make(map[string]*lsp.Manager), docs: make(map[string]lspDocRec)}
}

// lspServers builds the servers that start on their own and the description of every language
// the table or the config knows, by the names a language is detected under. See lspTable.
func lspServers(cfg config.Config) (map[string]lsp.ServerConfig, map[string]ui.LSPServerInfo) {
	servers, _, info := lspTable(cfg)
	return servers, info
}

// lspTable builds, by the names a language is detected under, the servers that start when a
// file of the language opens (servers), the servers that can run at all (startable), and the
// description of every language the table or the config knows (info). A language starts on
// its own unless the master switch is off (lsp_enabled), its entries are all disabled, its
// built-in server is manual (ols, Godot) and the config does not turn it on, or its entry
// says "auto_start": false. A server that does not start on its own is still startable: as in
// Fresh, Start/Restart LSP Server runs it, whatever the switches say.
func lspTable(cfg config.Config) (servers, startable map[string]lsp.ServerConfig, info map[string]ui.LSPServerInfo) {
	servers = make(map[string]lsp.ServerConfig)
	startable = make(map[string]lsp.ServerConfig)
	info = make(map[string]ui.LSPServerInfo)
	handled := make(map[string]bool) // the config keys the table used
	for _, spec := range lsp.Defaults() {
		user, userKey, hasUser := userEntry(cfg, spec)
		if hasUser {
			handled[userKey] = true
		}
		sc := lsp.ServerConfig{Command: []string{spec.Command}, Args: slices.Clone(spec.Args), Name: spec.Command, Install: spec.Install,
			LanguageID: spec.LanguageID, RootMarkers: spec.RootMarkers}
		if hasUser {
			// The entry replaces the program, or only the arguments of the built-in one.
			if user.Command != "" {
				sc = lsp.ServerConfig{Command: []string{user.Command}, Args: slices.Clone(user.Args), Name: user.Command,
					LanguageID: spec.LanguageID, RootMarkers: spec.RootMarkers}
			} else if user.Args != nil {
				sc.Args = slices.Clone(user.Args)
			}
			if user.RootMarkers != nil {
				sc.RootMarkers = slices.Clone(user.RootMarkers)
			}
		}
		state := ui.LSPServerOn
		switch {
		case !cfg.LSPEnabled:
			state = ui.LSPServerOffAll
		case cfg.LSPDisabled[spec.Language] || disabledByAlias(cfg, spec):
			state = ui.LSPServerOffLanguage
		case spec.Manual && !hasUser, hasUser && user.Dormant:
			state = ui.LSPServerManual
		}
		for _, name := range spec.Names() {
			info[name] = ui.LSPServerInfo{State: state, Language: spec.Language, Command: sc.Command[0], Install: sc.Install}
			startable[name] = sc
			if state == ui.LSPServerOn {
				servers[name] = sc
			}
		}
	}
	// A language the table does not know runs the program the config names.
	for _, lang := range slices.Sorted(maps.Keys(cfg.LSP)) {
		if handled[lang] || cfg.LSP[lang].Command == "" {
			continue
		}
		s := cfg.LSP[lang]
		state := ui.LSPServerOn
		switch {
		case !cfg.LSPEnabled:
			state = ui.LSPServerOffAll
		case s.Dormant:
			state = ui.LSPServerManual
		}
		info[lang] = ui.LSPServerInfo{State: state, Language: lang, Command: s.Command}
		sc := lsp.ServerConfig{Command: []string{s.Command}, Args: slices.Clone(s.Args), Name: s.Command, RootMarkers: slices.Clone(s.RootMarkers)}
		startable[lang] = sc
		if state == ui.LSPServerOn {
			servers[lang] = sc
		}
	}
	for lang := range cfg.LSPDisabled {
		if _, known := info[lang]; !known {
			info[lang] = ui.LSPServerInfo{State: ui.LSPServerOffLanguage, Language: lang}
		}
	}
	return servers, startable, info
}

// userEntry finds the lsp entry that applies to a built-in language: the one under its own
// name, or under one of its aliases. It returns the key it was found under.
func userEntry(cfg config.Config, spec lsp.Spec) (config.LSPServer, string, bool) {
	for _, name := range spec.Names() {
		if s, ok := cfg.LSP[name]; ok {
			return s, name, true
		}
	}
	return config.LSPServer{}, "", false
}

// disabledByAlias reports whether the config disabled the language under one of its aliases.
func disabledByAlias(cfg config.Config, spec lsp.Spec) bool {
	return slices.ContainsFunc(spec.Aliases, func(a string) bool { return cfg.LSPDisabled[a] })
}

// Server describes the server of a language from the configuration. It does no lookup.
func (p *lspPort) Server(languageID string) ui.LSPServerInfo {
	if info, ok := p.info[languageID]; ok {
		return info
	}
	if _, ok := p.servers[languageID]; ok {
		return ui.LSPServerInfo{State: ui.LSPServerOn, Language: languageID, Command: p.servers[languageID].Command[0]}
	}
	return ui.LSPServerInfo{}
}

// openLanguage is the language a document is opened under. TypeScript's detection gives a
// .tsx file the name of TypeScript, but the server must be told "typescriptreact", or it
// reads the JSX as a syntax error. A .h file in a directory with a C++ source or header is
// opened as C++, as Fresh does, so that clangd reads it as C++.
func openLanguage(path, lang string) string {
	if lang == "typescript" && strings.EqualFold(filepath.Ext(path), ".tsx") {
		if _, ok := lsp.DefaultFor("tsx"); ok {
			return "tsx"
		}
	}
	if lang == "c" && filepath.Ext(path) == ".h" && cppSibling(filepath.Dir(path)) {
		if _, ok := lsp.DefaultFor("cpp"); ok {
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

// unavailable turns the failure to start the server of lang into the error the root model
// remembers for the language: a program that is not installed, or a server that failed its
// handshake. Any other error is returned as it is.
func unavailable(lang string, err error) error {
	var ni *lsp.NotInstalledError
	var se *lsp.StartError
	switch {
	case errors.As(err, &ni):
		return &ui.LSPUnavailableError{Language: lang, Command: ni.Command, Install: ni.Install, Missing: true, Err: err}
	case errors.As(err, &se):
		return &ui.LSPUnavailableError{Language: lang, Command: se.Command, Err: err}
	}
	return err
}

// Format asks the server of the document at path to format it and returns the edits in rune
// columns of text. When the server holds older text than text, it is sent first, so the
// positions fit. A server with no formatting answers with no edits.
func (p *lspPort) Format(path, text string, tabSize int, insertSpaces bool) ([]editor.LSPEdit, error) {
	rec, ok := p.doc(path)
	if !ok {
		return nil, errors.New("the document is not open on a language server")
	}
	if rec.text != text {
		if err := p.Change(path, text); err != nil {
			return nil, err
		}
	}
	var edits []protocol.TextEdit
	err := p.call(path, buffer.Pos{}, func(ctx context.Context, c *lsp.Client, u string, _ protocol.Position) error {
		var err error
		edits, err = c.Formatting(ctx, u, tabSize, insertSpaces)
		return err
	})
	if err != nil {
		return nil, err
	}
	lines := newTextLines(text)
	out := make([]editor.LSPEdit, 0, len(edits))
	for _, e := range edits {
		out = append(out, editor.LSPEdit{From: lines.fromLSP(e.Range.Start), To: lines.fromLSP(e.Range.End), Text: e.NewText})
	}
	return out, nil
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
