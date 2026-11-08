package lsphost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp/manager"
)

// lspConfig is the default config with the lsp parts of a user file laid on it.
// Each language gets one server; a test that needs several builds cfg.LSP itself.
func lspConfig(servers map[string]config.LSPServer, disabled map[string]bool) config.Config {
	cfg := config.Defaults()
	if servers != nil {
		cfg.LSP = make(map[string][]config.LSPServer, len(servers))
		for lang, s := range servers {
			cfg.LSP[lang] = []config.LSPServer{s}
		}
	}
	cfg.LSPDisabled = disabled
	return cfg
}

func TestBuiltInServersNeedNoConfig(t *testing.T) {
	plan := lspPlanFor(config.Defaults())
	servers, info := plan.servers, plan.info
	tests := []struct {
		lang, command string
		args          []string
	}{
		{"go", "gopls", nil},
		{"rust", "rust-analyzer", nil},
		{"python", "pylsp", nil},
		{"typescript", "typescript-language-server", []string{"--stdio"}},
		{"c++", "clangd", nil}, // the name gvim's detection gives C++
		{"cpp", "clangd", nil},
		{"c#", "csharp-ls", nil},
		{"tex", "texlab", nil},
		{"markdown", "marksman", []string{"server"}},
		{"mysql", "sqls", nil},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			list, ok := servers[tt.lang]
			if !ok || len(list) == 0 {
				t.Fatalf("no server for %q", tt.lang)
			}
			sc := list[0]
			if sc.Command[0] != tt.command || !slices.Equal(sc.Args, tt.args) {
				t.Errorf("server = %v %v, want %s %v", sc.Command, sc.Args, tt.command, tt.args)
			}
			if got := info[tt.lang]; got.State != tui.LSPServerOn || got.Command != tt.command || got.Install == "" {
				t.Errorf("info = %+v, want an on server with an install hint", got)
			}
		})
	}
	if sc := servers["c++"][0]; sc.LanguageID != "cpp" {
		t.Errorf("c++ languageId = %q, want cpp", sc.LanguageID)
	}
}

func TestManualServersWaitForTheConfig(t *testing.T) {
	plan := lspPlanFor(config.Defaults())
	servers, info := plan.servers, plan.info
	for _, lang := range []string{"odin", "gdscript"} {
		if plan.auto[lang] {
			t.Errorf("%s runs without being turned on", lang)
		}
		if len(servers[lang]) == 0 {
			t.Errorf("%s cannot be started by hand", lang)
		}
		if info[lang].State != tui.LSPServerManual {
			t.Errorf("%s state = %v, want manual", lang, info[lang].State)
		}
	}
	// "enabled": true without a command runs the built-in program.
	plan = lspPlanFor(lspConfig(map[string]config.LSPServer{"odin": {}}, nil))
	servers, info = plan.servers, plan.info
	if list, ok := servers["odin"]; !ok || len(list) == 0 || list[0].Command[0] != "ols" || info["odin"].State != tui.LSPServerOn {
		t.Errorf("odin turned on: server %v ok %v info %+v, want ols running", list, ok, info["odin"])
	}
}

func TestUserEntriesOverrideTheTable(t *testing.T) {
	servers := lspPlanFor(lspConfig(map[string]config.LSPServer{
		"go":      {Command: "my-gopls", Args: []string{"-remote=auto"}},
		"python":  {Command: "pyright-langserver", Args: []string{"--stdio"}},
		"cpp":     {Command: "ccls"},
		"nim":     {Args: []string{"--x"}}, // args only: keep the built-in program
		"newlang": {Command: "newls"},
	}, nil)).servers
	tests := []struct {
		lang    string
		command string
		args    []string
		install bool // the built-in install hint is kept
	}{
		{"go", "my-gopls", []string{"-remote=auto"}, false},
		{"python", "pyright-langserver", []string{"--stdio"}, false},
		{"cpp", "ccls", nil, false},
		{"c++", "ccls", nil, false}, // the entry under cpp serves the alias too
		{"nim", "nimlangserver", []string{"--x"}, true},
		{"newlang", "newls", nil, false},
		{"rust", "rust-analyzer", nil, true}, // untouched
	}
	for _, tt := range tests {
		list, ok := servers[tt.lang]
		if !ok || len(list) == 0 {
			t.Errorf("%s: no server", tt.lang)
			continue
		}
		sc := list[0]
		if sc.Command[0] != tt.command || !slices.Equal(sc.Args, tt.args) || (sc.Install != "") != tt.install {
			t.Errorf("%s: server %+v, want %s %v install hint %v", tt.lang, sc, tt.command, tt.args, tt.install)
		}
	}
}

func TestUserCanSwitchServersOff(t *testing.T) {
	// One language off by its entry.
	plan := lspPlanFor(lspConfig(nil, map[string]bool{"go": true}))
	if plan.auto["go"] {
		t.Error("go still starts after its entry was disabled")
	}
	if plan.info["go"].State != tui.LSPServerOffLanguage {
		t.Errorf("go state = %v, want off for the language", plan.info["go"].State)
	}
	if !plan.auto["rust"] {
		t.Error("disabling go turned rust off")
	}
	// A language off under its alias.
	plan = lspPlanFor(lspConfig(nil, map[string]bool{"c++": true}))
	if plan.auto["cpp"] {
		t.Error("cpp still starts after c++ was disabled")
	}
	// All off by the master switch, even with an entry.
	cfg := lspConfig(map[string]config.LSPServer{"go": {Command: "gopls"}, "newlang": {Command: "x"}}, nil)
	cfg.LSPEnabled = false
	plan = lspPlanFor(cfg)
	if len(plan.auto) != 0 {
		t.Errorf("%d servers start with lsp_enabled false", len(plan.auto))
	}
	for _, lang := range []string{"go", "rust", "newlang"} {
		if plan.info[lang].State != tui.LSPServerOffAll {
			t.Errorf("%s state = %v, want off for all", lang, plan.info[lang].State)
		}
	}
}

func TestOpeningWithAMissingProgramGivesTheInstallHint(t *testing.T) {
	cfg := config.Defaults()
	// gopls is looked up on PATH; an empty PATH makes it missing on any machine.
	t.Setenv("PATH", t.TempDir())
	port := NewFor(cfg)
	dir := t.TempDir()
	port.SetRoot(dir)
	_, err := port.Open(filepath.Join(dir, "a.go"), "go", "package a")
	var ue *tui.LSPUnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("Open error = %v, want a *tui.LSPUnavailableError", err)
	}
	if !ue.Missing || ue.Command != "gopls" || ue.Language != "go" || !strings.Contains(ue.Install, "go install golang.org/x/tools/gopls@latest") {
		t.Errorf("error = %+v, want gopls missing with its install command", ue)
	}
	for _, want := range []string{"gopls is not installed", "language server for go", "go install golang.org/x/tools/gopls@latest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
}

func TestOpeningWithAServerThatDiesNamesTheCommandAndItsOutput(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeLSPEnv, "die")
	port := New(map[string]config.LSPServer{"go": {Command: exe, Args: []string{"-test.run=^TestFakeLSPServerProcess$"}}})
	dir := t.TempDir()
	port.SetRoot(dir)
	_, err = port.Open(filepath.Join(dir, "a.go"), "go", "package a")
	var ue *tui.LSPUnavailableError
	if !errors.As(err, &ue) || ue.Missing {
		t.Fatalf("Open error = %v, want an unavailable server that is not missing", err)
	}
	for _, want := range []string{"language server for go", "initialize", "-test.run=^TestFakeLSPServerProcess$", "fake server: cannot start"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("message %q spans lines, want one line for the status row", err.Error())
	}
}

func TestPortDescribesLanguagesFromTheConfig(t *testing.T) {
	port := NewFor(config.Defaults())
	d, ok := port.(interface {
		Server(string) tui.LSPServerInfo
	})
	if !ok {
		t.Fatal("the port does not describe servers")
	}
	if got := d.Server("go"); got.State != tui.LSPServerOn || got.Command != "gopls" {
		t.Errorf("go = %+v", got)
	}
	if got := d.Server("text"); got.State != tui.LSPServerNone {
		t.Errorf("text = %+v, want none", got)
	}
	if !port.Supports("go") || port.Supports("odin") || port.Supports("text") {
		t.Errorf("Supports: go %v odin %v text %v, want true false false", port.Supports("go"), port.Supports("odin"), port.Supports("text"))
	}
}

func TestOpenLanguageTellsTsxFromTypeScript(t *testing.T) {
	tests := []struct{ path, lang, want string }{
		{"/w/a.tsx", "typescript", "tsx"},
		{"/w/A.TSX", "typescript", "tsx"},
		{"/w/a.ts", "typescript", "typescript"},
		{"/w/a.tsx", "go", "go"},
	}
	for _, tt := range tests {
		if got := openLanguage(tt.path, tt.lang); got != tt.want {
			t.Errorf("openLanguage(%q, %q) = %q, want %q", tt.path, tt.lang, got, tt.want)
		}
	}
	plan := lspPlanFor(config.Defaults())
	if got := plan.servers["tsx"][0].LanguageID; got != "typescriptreact" {
		t.Errorf("tsx languageId = %q, want typescriptreact", got)
	}
}

func TestPortFormatReturnsTheServersEditsInRuneColumns(t *testing.T) {
	port, path := startFakeLSPPort(t)
	f, ok := port.(interface {
		Format(path, text string, tabSize int, insertSpaces bool) ([]editor.LSPEdit, error)
	})
	if !ok {
		t.Fatal("the port cannot format")
	}
	// The server holds "package a\n"; the buffer has moved on, so the port sends it first.
	edits, err := f.Format(path, "package a\nfunc  f() {}", 4, false)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	want := []editor.LSPEdit{{From: buffer.Pos{Line: 1, Col: 4}, To: buffer.Pos{Line: 1, Col: 6}, Text: " "}}
	if !slices.Equal(edits, want) {
		t.Errorf("edits = %+v, want %+v", edits, want)
	}
	if _, err := f.Format(filepath.Join(t.TempDir(), "other.go"), "x", 4, true); err == nil {
		t.Error("Format of a document that is not open succeeded")
	}
}

// TestProjectConfigCannotChooseTheProgram is the LSP-1 regression: a command written in a
// repository's .gvim/config.json must not become the server that opening a file starts.
func TestProjectConfigCannotChooseTheProgram(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	apptest.Must(t, os.WriteFile(userFile, []byte(`{}`), 0o644))
	root := filepath.Join(dir, "evil")
	apptest.Must(t, os.MkdirAll(filepath.Join(root, ".gvim"), 0o755))
	apptest.Must(t, os.WriteFile(filepath.Join(root, ".gvim", "config.json"),
		[]byte(`{"lsp": {"go": {"command": "touch", "args": ["`+filepath.Join(dir, "pwned")+`"]}, "newlang": {"command": "touch"}}}`), 0o644))
	cfg, diags, err := config.Load(config.Options{Root: root, UserFile: userFile})
	apptest.Must(t, err)
	if len(diags) != 2 {
		t.Fatalf("diagnostics = %v, want one for each refused entry", diags)
	}
	plan := lspPlanFor(cfg)
	if sc := plan.servers["go"][0]; sc.Command[0] != "gopls" || len(sc.Args) != 0 {
		t.Errorf("go server = %v %v, want the built-in gopls", sc.Command, sc.Args)
	}
	if list, ok := plan.servers["newlang"]; ok && len(list) > 0 {
		t.Error("a language that only the project names runs its program")
	}
}

// TestDormantAndSwitchedOffServersCanBeStartedByHand checks the rule of Fresh's docs that a
// manual start overrides lsp_enabled and the mute of a language: lspServers does not run such
// a server on its own, but the port can start it.
func TestDormantAndSwitchedOffServersCanBeStartedByHand(t *testing.T) {
	tests := []struct {
		name  string
		cfg   func() config.Config
		lang  string
		state tui.LSPServerState
	}{
		{"lsp_enabled false", func() config.Config { c := config.Defaults(); c.LSPEnabled = false; return c }, "go", tui.LSPServerOffAll},
		{"a language muted", func() config.Config { return lspConfig(nil, map[string]bool{"go": true}) }, "go", tui.LSPServerOffLanguage},
		{"a manual server", config.Defaults, "odin", tui.LSPServerManual},
		{"auto_start false", func() config.Config { return lspConfig(map[string]config.LSPServer{"go": {Dormant: true}}, nil) }, "go", tui.LSPServerManual},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg()
			plan := lspPlanFor(cfg)
			if plan.auto[tt.lang] {
				t.Fatalf("%s starts on its own", tt.lang)
			}
			if plan.info[tt.lang].State != tt.state || plan.info[tt.lang].Command == "" {
				t.Fatalf("info = %+v, want state %v and the program named", plan.info[tt.lang], tt.state)
			}
			port := NewFor(cfg).(*lspPort)
			if port.Supports(tt.lang) {
				t.Errorf("Supports(%s) is true, but the server does not start on its own", tt.lang)
			}
			if list := port.servers[tt.lang]; len(list) == 0 {
				t.Errorf("the manager cannot start %s by hand", tt.lang)
			}
		})
	}
	// A language with no server at all cannot be started.
	if list := NewFor(config.Defaults()).(*lspPort).servers["text"]; len(list) > 0 {
		t.Error("plain text can be started")
	}
}

func TestServerRootWalksUpToTheNearestMarker(t *testing.T) {
	ws := t.TempDir()
	svc := filepath.Join(ws, "svc")
	src := filepath.Join(svc, "src")
	apptest.Must(t, os.MkdirAll(src, 0o755))
	apptest.Must(t, os.WriteFile(filepath.Join(svc, "Cargo.toml"), nil, 0o644))
	apptest.Must(t, os.WriteFile(filepath.Join(ws, "Makefile"), nil, 0o644))
	apptest.Must(t, os.WriteFile(filepath.Join(svc, "app.csproj"), nil, 0o644))
	outside := t.TempDir()
	tests := []struct {
		name    string
		path    string
		markers []string
		want    string
	}{
		{"the nearest directory with a marker", filepath.Join(src, "main.rs"), []string{"Cargo.toml", ".git"}, svc},
		{"the file's own directory first", filepath.Join(svc, "lib.rs"), []string{"Cargo.toml"}, svc},
		{"a glob marker", filepath.Join(src, "a.cs"), []string{"*.csproj"}, svc},
		{"no marker found: the workspace root", filepath.Join(src, "main.go"), []string{"go.mod"}, ws},
		{"no markers: the workspace root", filepath.Join(src, "main.zig"), nil, ws},
		{"the workspace root can be the project", filepath.Join(ws, "a.c"), []string{"Makefile"}, ws},
		{"a file outside the workspace keeps the workspace root", filepath.Join(outside, "x.rs"), []string{"Cargo.toml"}, ws},
	}
	for _, tt := range tests {
		if got := serverRoot(tt.path, tt.markers, ws); got != tt.want {
			t.Errorf("%s: serverRoot = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestOpenRunsTheServerInTheProjectRootOfTheFile(t *testing.T) {
	ws := t.TempDir()
	svc := filepath.Join(ws, "svc")
	apptest.Must(t, os.MkdirAll(svc, 0o755))
	apptest.Must(t, os.WriteFile(filepath.Join(svc, "go.mod"), nil, 0o644))
	exe, err := os.Executable()
	apptest.Must(t, err)
	t.Setenv(fakeLSPEnv, "1")
	port := NewFor(config.Defaults()).(*lspPort)
	port.servers = map[string][]manager.ServerConfig{"go": {{
		Command: []string{exe}, Args: []string{"-test.run=^TestFakeLSPServerProcess$"}, RootMarkers: []string{"go.mod"}, AutoStart: true,
	}}}
	port.auto = map[string]bool{"go": true}
	port.startable = map[string]bool{"go": true}
	port.SetRoot(ws)
	t.Cleanup(func() { _ = port.Stop() })
	path := filepath.Join(svc, "a.go")
	if _, err := port.Open(path, "go", "package a\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if rec, _ := port.doc(path); rec.root != svc {
		t.Errorf("document root = %q, want %q", rec.root, svc)
	}
	// A file without a go.mod above it uses the workspace root.
	other := filepath.Join(ws, "b.go")
	if _, err := port.Open(other, "go", "package b\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if rec, _ := port.doc(other); rec.root != ws {
		t.Errorf("document root = %q, want %q", rec.root, ws)
	}
}

// TestOpenLanguageRoutesAHeaderWithACppSibling checks Fresh's C/C++ header routing: a .h file
// is opened as C++ when its directory holds a C++ source or header, and as C otherwise
// (docs/features/lsp.md, C/C++ Header Routing). The sibling extensions are Fresh's list
// (language_detect.rs), matched case-sensitively as Fresh matches them.
func TestOpenLanguageRoutesAHeaderWithACppSibling(t *testing.T) {
	tests := []struct {
		name     string
		siblings []string
		file     string
		lang     string
		want     string
	}{
		{"a .cpp beside it makes a header C++", []string{"foo.cpp"}, "foo.h", "c", "cpp"},
		{"a .hpp beside it makes a header C++", []string{"foo.hpp"}, "foo.h", "c", "cpp"},
		{"a .cc beside it makes a header C++", []string{"util.cc"}, "foo.h", "c", "cpp"},
		{"a .cxx beside it makes a header C++", []string{"util.cxx"}, "foo.h", "c", "cpp"},
		{"a .hxx beside it makes a header C++", []string{"util.hxx"}, "foo.h", "c", "cpp"},
		{"a .hh beside it makes a header C++", []string{"util.hh"}, "foo.h", "c", "cpp"},
		{"a .c++ beside it makes a header C++", []string{"util.c++"}, "foo.h", "c", "cpp"},
		{"no sibling leaves a header C", nil, "foo.h", "c", "c"},
		{"a C source beside it leaves a header C", []string{"foo.c"}, "foo.h", "c", "c"},
		{"a .c file is never routed to C++", []string{"foo.cpp"}, "foo.c", "c", "c"},
		{"a header whose language is already C++ stays C++", nil, "foo.h", "cpp", "cpp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.siblings {
				if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := openLanguage(filepath.Join(dir, tt.file), tt.lang); got != tt.want {
				t.Errorf("openLanguage(%s, %q) = %q, want %q", tt.file, tt.lang, got, tt.want)
			}
		})
	}
}

// TestLSPPlanCarriesServerSettings checks the config-to-manager plumbing: a user
// entry's env, initialization options and feature filter reach the manager's
// server, and universal_lsp becomes the universal list.
func TestLSPPlanCarriesServerSettings(t *testing.T) {
	cfg := config.Defaults()
	cfg.LSP = map[string][]config.LSPServer{
		"go": {{
			Command:               "my-gopls",
			Env:                   map[string]string{"GOFLAGS": "-mod=mod"},
			InitializationOptions: json.RawMessage(`{"usePlaceholders":true}`),
			OnlyFeatures:          []string{"diagnostics", "hover"},
			LanguageIDOverrides:   map[string]string{"go": "golang"},
			RootMarkers:           []string{"go.mod"},
		}},
	}
	cfg.UniversalLSP = map[string][]config.LSPServer{
		"spell": {{Command: "harper-ls", Args: []string{"--stdio"}}},
	}
	plan := lspPlanFor(cfg)
	server := plan.servers["go"][0]
	if server.Command[0] != "my-gopls" || !server.AutoStart {
		t.Errorf("go server = %+v, want the user program, auto-starting", server)
	}
	if !reflect.DeepEqual(server.Env, map[string]string{"GOFLAGS": "-mod=mod"}) {
		t.Errorf("env = %v", server.Env)
	}
	if string(server.InitializationOptions) != `{"usePlaceholders":true}` {
		t.Errorf("initialization options = %s", server.InitializationOptions)
	}
	if !reflect.DeepEqual(server.OnlyFeatures, []string{"diagnostics", "hover"}) {
		t.Errorf("only features = %v", server.OnlyFeatures)
	}
	if !reflect.DeepEqual(server.LanguageIDOverrides, map[string]string{"go": "golang"}) {
		t.Errorf("language id overrides = %v", server.LanguageIDOverrides)
	}
	if len(plan.universal) != 1 || plan.universal[0].Command[0] != "harper-ls" {
		t.Errorf("universal = %+v, want harper-ls", plan.universal)
	}
}
