package manager

import (
	"context"
	"errors"
	"slices"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/lsptest"
)

// capsFor builds the capabilities a fake server advertises for the given
// features. Diagnostics is always on, as every language server may publish them.
func capsFor(features ...Feature) protocol.ServerCapabilities {
	var caps protocol.ServerCapabilities
	for _, f := range features {
		switch f {
		case FeatureHover:
			caps.HoverProvider = protocol.Boolean(true)
		case FeatureCompletion:
			caps.CompletionProvider = &protocol.CompletionOptions{}
		case FeatureCodeAction:
			caps.CodeActionProvider = protocol.Boolean(true)
		case FeatureDefinition:
			caps.DefinitionProvider = protocol.Boolean(true)
		case FeatureImplementation:
			caps.ImplementationProvider = protocol.Boolean(true)
		case FeatureReferences:
			caps.ReferencesProvider = protocol.Boolean(true)
		case FeatureFormat:
			caps.DocumentFormattingProvider = protocol.Boolean(true)
		case FeatureRename:
			caps.RenameProvider = protocol.Boolean(true)
		case FeatureSignatureHelp:
			caps.SignatureHelpProvider = &protocol.SignatureHelpOptions{}
		case FeatureInlayHints:
			caps.InlayHintProvider = protocol.Boolean(true)
		case FeatureFoldingRange:
			caps.FoldingRangeProvider = protocol.Boolean(true)
		case FeatureSemanticTokens:
			caps.SemanticTokensProvider = &protocol.SemanticTokensOptions{
				Legend: protocol.SemanticTokensLegend{TokenTypes: []string{"keyword"}},
			}
		case FeatureDocumentHighlight:
			caps.DocumentHighlightProvider = protocol.Boolean(true)
		case FeatureDocumentSymbols:
			caps.DocumentSymbolProvider = protocol.Boolean(true)
		case FeatureWorkspaceSymbols:
			caps.WorkspaceSymbolProvider = protocol.Boolean(true)
		case FeatureDiagnostics:
		}
	}
	return caps
}

// capsByCommand gives each fake server the capabilities of the feature list its
// command names.
func capsByCommand(features map[string][]Feature) func(lsp.Config) protocol.ServerCapabilities {
	return func(cfg lsp.Config) protocol.ServerCapabilities {
		if len(cfg.Command) == 0 {
			return protocol.ServerCapabilities{}
		}
		return capsFor(features[cfg.Command[0]]...)
	}
}

// TestRouteMergedAndExclusiveAcrossServers checks the routing core: two servers
// for one language, both sent didOpen; merged features route to both in config
// order, exclusive features to the first eligible one, and a server's initialize
// capabilities decide eligibility (a filter that allows a feature is not enough).
func TestRouteMergedAndExclusiveAcrossServers(t *testing.T) {
	log := &startLog{t: t, caps: capsByCommand(map[string][]Feature{
		"first":  {FeatureHover, FeatureCompletion, FeatureCodeAction},
		"second": {FeatureDefinition, FeatureCompletion, FeatureCodeAction},
	})}
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {
			{Command: []string{"first"}, AutoStart: true},
			{Command: []string{"second"}, AutoStart: true},
		},
	}, nil)
	m.start = log.start

	if err := m.Open(mgrAPath, "go", "package a"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(log.cfgs) != 2 {
		t.Fatalf("servers started = %d, want 2", len(log.cfgs))
	}
	if !slices.Equal(log.cfgs[0].Command, []string{"first"}) || !slices.Equal(log.cfgs[1].Command, []string{"second"}) {
		t.Fatalf("start order = %v, want first then second", []string{log.cfgs[0].Command[0], log.cfgs[1].Command[0]})
	}
	for i := range 2 {
		open := lsptest.Recv(t, log.pairs[i].srv.OpenCh, "didOpen on server")
		if open.TextDocument.URI != mgrAURI || open.TextDocument.Version != 1 {
			t.Fatalf("server %d got didOpen %+v", i, open.TextDocument)
		}
	}
	if version, ok := m.DocumentVersion(mgrAPath); !ok || version != 1 {
		t.Fatalf("DocumentVersion = %d, %v; want 1", version, ok)
	}

	merged := m.AllFor(mgrAPath, FeatureCodeAction)
	if len(merged) != 2 || merged[0].Name() != "first" || merged[1].Name() != "second" {
		t.Fatalf("AllFor(code action) = %v, want both servers in order", handleNames(merged))
	}
	if got := m.AllFor(mgrAPath, FeatureCompletion); len(got) != 2 {
		t.Fatalf("AllFor(completion) = %v, want both servers", handleNames(got))
	}

	for _, tt := range []struct {
		name  string
		first Feature
		want  string
	}{
		{"hover is served by the first capable server", FeatureHover, "first"},
		{"definition skips the server that does not advertise it", FeatureDefinition, "second"},
	} {
		if h, ok := m.FirstFor(mgrAPath, tt.first); !ok || h.Name() != tt.want {
			t.Errorf("%s: FirstFor = %v, want %s", tt.name, handleName(h, ok), tt.want)
		}
	}
	if h, ok := m.FirstFor(mgrAPath, FeatureReferences); ok {
		t.Fatalf("FirstFor(references) = %s, want no server", h.Name())
	}
}

// TestFeatureFiltersRouteServers checks only_features and except_features.
func TestFeatureFiltersRouteServers(t *testing.T) {
	log := &startLog{t: t, caps: capsByCommand(map[string][]Feature{
		"only": {FeatureHover, FeatureDefinition, FeatureCompletion},
		"not":  {FeatureHover, FeatureDefinition, FeatureCompletion},
	})}
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {
			{Command: []string{"only"}, OnlyFeatures: []string{"completion"}, AutoStart: true},
			{Command: []string{"not"}, ExceptFeatures: []string{"hover"}, AutoStart: true},
		},
	}, nil)
	m.start = log.start
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if h, ok := m.FirstFor(mgrAPath, FeatureHover); ok {
		t.Errorf("hover routed to %s, but one server only takes completion and the other excludes hover", h.Name())
	}
	if h, ok := m.FirstFor(mgrAPath, FeatureDefinition); !ok || h.Name() != "not" {
		t.Errorf("definition routed to %v, want the server that allows it", handleName(h, ok))
	}
	if got := m.AllFor(mgrAPath, FeatureCompletion); len(got) != 2 {
		t.Errorf("AllFor(completion) = %v, want both", handleNames(got))
	}
}

// TestUniversalServerServesEveryLanguage checks that a universal_lsp server
// attaches to buffers of any language, including a language with no server of its
// own, and appears in Servers() as universal.
func TestUniversalServerServesEveryLanguage(t *testing.T) {
	log := &startLog{t: t, caps: capsByCommand(map[string][]Feature{"spell": {FeatureHover}})}
	m := NewManager(mgrRoot, nil, []ServerConfig{{Command: []string{"spell"}, AutoStart: true}})
	m.start = log.start

	if err := m.Open(mgrAPath, "go", "package a"); err != nil {
		t.Fatalf("Open with a universal server only: %v", err)
	}
	open := lsptest.Recv(t, log.pairs[0].srv.OpenCh, "universal didOpen")
	if open.TextDocument.LanguageID != "go" {
		t.Fatalf("universal didOpen languageId = %q, want go", open.TextDocument.LanguageID)
	}
	h, ok := m.FirstFor(mgrAPath, FeatureHover)
	if !ok || !h.Universal() || h.Name() != "spell" {
		t.Fatalf("FirstFor(hover) = %v, want the universal server", handleName(h, ok))
	}
	servers := m.Servers()
	if len(servers) != 1 || !servers[0].Universal || servers[0].State != ServerRunning {
		t.Fatalf("Servers = %+v, want one running universal server", servers)
	}

	// A language server and a universal server merge: language-specific first.
	m2log := &startLog{t: t, caps: capsByCommand(map[string][]Feature{
		"go":    {FeatureCodeAction},
		"spell": {FeatureCodeAction},
	})}
	m2 := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {{Command: []string{"go"}, AutoStart: true}},
	}, []ServerConfig{{Command: []string{"spell"}, AutoStart: true}})
	m2.start = m2log.start
	if err := m2.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatalf("Open with both: %v", err)
	}
	got := handleNames(m2.AllFor(mgrAPath, FeatureCodeAction))
	if !slices.Equal(got, []string{"go", "spell"}) {
		t.Fatalf("merged handles = %v, want the language server then the universal one", got)
	}
}

// TestServerConfigReachesTheProcess checks env, initialization_options and
// language_id_overrides reach the launched server and its didOpen.
func TestServerConfigReachesTheProcess(t *testing.T) {
	log := &startLog{t: t, caps: capsByCommand(map[string][]Feature{"gopls": {FeatureHover}})}
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {{
			Command:               []string{"gopls"},
			Env:                   map[string]string{"GOFLAGS": "-mod=mod"},
			InitializationOptions: protocol.LSPAny(`{"usePlaceholders":true}`),
			LanguageIDOverrides:   map[string]string{"go": "golang"},
			AutoStart:             true,
		}},
	}, nil)
	m.start = log.start
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	cfg := log.cfgs[0]
	if !slices.Equal(cfg.Env, []string{"GOFLAGS=-mod=mod"}) {
		t.Errorf("process env = %v", cfg.Env)
	}
	if string(cfg.InitializationOptions) != `{"usePlaceholders":true}` {
		t.Errorf("initializationOptions = %s", cfg.InitializationOptions)
	}
	open := lsptest.Recv(t, log.pairs[0].srv.OpenCh, "didOpen")
	if open.TextDocument.LanguageID != "golang" {
		t.Errorf("didOpen languageId = %q, want the language_id_overrides value", open.TextDocument.LanguageID)
	}
}

// TestManualStartOverridesAutoStart checks that a dormant server is not started
// by Open, and that the manual start paths start it.
func TestManualStartOverridesAutoStart(t *testing.T) {
	log := &startLog{t: t, caps: capsByCommand(map[string][]Feature{"gopls": {FeatureHover}})}
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {{Command: []string{"gopls"}, AutoStart: false}},
	}, nil)
	m.start = log.start

	err := m.Open(mgrAPath, "go", "x")
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("Open of a dormant-only language = %v, want ErrStopped", err)
	}
	if len(log.cfgs) != 0 {
		t.Fatalf("a dormant server started on Open")
	}
	if err := m.StartLanguage("go"); err != nil {
		t.Fatalf("StartLanguage: %v", err)
	}
	if len(log.cfgs) != 1 {
		t.Fatalf("StartLanguage started %d servers, want 1", len(log.cfgs))
	}
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatalf("Open after the manual start: %v", err)
	}
}

// TestServersRestartStopAndMute drives the per-server lifecycle the status popup
// uses: list state, stop one server, restart it, mute the language and start it
// again by hand.
func TestServersRestartStopAndMute(t *testing.T) {
	log := &startLog{t: t, caps: capsByCommand(map[string][]Feature{
		"first":  {FeatureHover},
		"second": {FeatureDefinition},
	})}
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {
			{Command: []string{"first"}, AutoStart: true},
			{Command: []string{"second"}, AutoStart: true},
		},
	}, nil)
	m.start = log.start
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := states(m.Servers()); !slices.Equal(got, []string{"go/first=running", "go/second=running"}) {
		t.Fatalf("Servers = %v", got)
	}

	if err := m.StopServer("go", "first"); err != nil {
		t.Fatalf("StopServer: %v", err)
	}
	if got := states(m.Servers()); !slices.Equal(got, []string{"go/first=stopped", "go/second=running"}) {
		t.Fatalf("Servers after stop = %v", got)
	}
	// A document opened now reaches only the server that still runs.
	if err := m.Open(mgrBPath, "go", "y"); err != nil {
		t.Fatalf("Open b.go: %v", err)
	}
	lsptest.Recv(t, log.pairs[1].srv.OpenCh, "second didOpen b.go")

	if err := m.RestartServer("go", "first"); err != nil {
		t.Fatalf("RestartServer: %v", err)
	}
	if got := states(m.Servers()); !slices.Equal(got, []string{"go/first=running", "go/second=running"}) {
		t.Fatalf("Servers after restart = %v", got)
	}
	if len(log.cfgs) != 3 {
		t.Fatalf("starts = %d, want the first server started again", len(log.cfgs))
	}

	m.SetLanguageEnabled("go", false)
	if got := states(m.Servers()); !slices.Equal(got, []string{"go/first=stopped", "go/second=stopped"}) {
		t.Fatalf("Servers after mute = %v", got)
	}
	err := m.Open("/work/proj/c.go", "go", "z")
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("Open of a muted language = %v, want ErrStopped", err)
	}

	if err := m.StartLanguage("go"); err != nil {
		t.Fatalf("StartLanguage after the mute: %v", err)
	}
	if err := m.Open("/work/proj/c.go", "go", "z"); err != nil {
		t.Fatalf("Open after the manual start: %v", err)
	}
}

// TestDynamicRegistrationUnlocksRouting checks that a provider the server
// registers after initialize takes part in routing, which is how many servers
// advertise their features.
func TestDynamicRegistrationUnlocksRouting(t *testing.T) {
	ctx := newTestContext(t)
	// The fake server publishes diagnostics from its didOpen handler while the test
	// sends client/registerCapability; with synchronous dispatch the two io.Pipe
	// writes can block each other (read loop held in the handler), so this test
	// dispatches asynchronously.
	log := &startLog{t: t, async: true}
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {{Command: []string{"gopls"}, AutoStart: true}},
	}, nil)
	m.start = log.start
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := m.FirstFor(mgrAPath, FeatureCompletion); ok {
		t.Fatal("completion is routed before the server registered it")
	}
	err := log.pairs[0].srv.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{ID: "completion", Method: "textDocument/completion"}},
	})
	if err != nil {
		t.Fatalf("registerCapability: %v", err)
	}
	if h, ok := m.FirstFor(mgrAPath, FeatureCompletion); !ok || h.Name() != "gopls" {
		t.Fatalf("completion is not routed after the registration")
	}
}

// handleNames returns the display names of the handles in order.
func handleNames(hs []*Handle) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name()
	}
	return out
}

// handleName names the handle, or "none" when there is none.
func handleName(h *Handle, ok bool) string {
	if !ok || h == nil {
		return "none"
	}
	return h.Name()
}

// states renders the Servers list as "language/name=state" strings.
func states(list []ServerStatus) []string {
	out := make([]string, len(list))
	for i, s := range list {
		lang := s.Language
		if lang == "" {
			lang = "universal"
		}
		out[i] = lang + "/" + s.Name + "=" + stateName(s.State)
	}
	return out
}

// stateName names a ServerState.
func stateName(s ServerState) string {
	switch s {
	case ServerRunning:
		return "running"
	case ServerStarting:
		return "starting"
	case ServerError:
		return "error"
	}
	return "stopped"
}

var _ = context.Background
