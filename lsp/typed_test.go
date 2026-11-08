package lsp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tbxark/gvim/lsp/lsptest"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// jsonrpc2Async lets the fake server's handler send a request back to the client
// while its read loop keeps running.
func jsonrpc2Async(ctx context.Context) { jsonrpc2.Async(ctx) }

// capsWith returns server capabilities that advertise the given providers, for
// the routing snapshot tests.
func capsWith(hover, completion, codeAction, format bool) protocol.ServerCapabilities {
	return protocol.ServerCapabilities{
		HoverProvider:              protocol.Boolean(hover),
		CompletionProvider:         completionOrNil(completion),
		CodeActionProvider:         protocol.Boolean(codeAction),
		DocumentFormattingProvider: protocol.Boolean(format),
		SignatureHelpProvider:      nil,
		DeclarationProvider:        nil,
	}
}

func completionOrNil(on bool) *protocol.CompletionOptions {
	if !on {
		return nil
	}
	return &protocol.CompletionOptions{}
}

func TestCapabilitiesAreUnknownBeforeInitialize(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	if f.client.Capabilities().Initialized {
		t.Fatal("a client that has not initialized reports capabilities")
	}
	if _, ok := f.client.InitializeResult(); ok {
		t.Fatal("a client that has not initialized has an initialize result")
	}
}

func TestInitializeKeepsCapabilitiesAndOptions(t *testing.T) {
	options := protocol.LSPAny(`{"checkOnSave":true}`)
	f := newFixture(t, Config{InitializationOptions: options}, nil)
	ctx := newTestContext(t)

	f.srv.Canned.Caps = capsWith(true, true, true, false)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	init := lsptest.Recv(t, f.srv.InitCh, "initialize params")
	if string(init.InitializationOptions) != string(options) {
		t.Fatalf("initializationOptions = %s, want %s", init.InitializationOptions, options)
	}

	res, ok := f.client.InitializeResult()
	if !ok || res == nil {
		t.Fatal("InitializeResult reports no result after Initialize")
	}
	caps := f.client.Capabilities()
	if !caps.Initialized {
		t.Error("capabilities are not initialized after Initialize")
	}
	for _, tc := range []struct {
		name string
		got  bool
		want bool
	}{
		{"hover", caps.Hover, true},
		{"completion", caps.Completion, true},
		{"code action", caps.CodeAction, true},
		{"format", caps.Format, false},
		{"rename", caps.Rename, false},
		{"diagnostics", caps.Diagnostics, true},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestDynamicRegistrationUpdatesCapabilities(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if f.client.Capabilities().InlayHints {
		t.Fatal("inlay hints are on before the server registered them")
	}

	options := protocol.LSPAny(`{"resolveProvider":true}`)
	err := f.srv.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{
			{ID: "inlay", Method: "textDocument/inlayHint"},
			{ID: "code-action", Method: "textDocument/codeAction", RegisterOptions: options},
		},
	})
	_ = err
	caps := f.client.Capabilities()
	if !caps.InlayHints {
		t.Error("inlay hints are off after the server registered them")
	}
	if !caps.CodeActionResolve {
		t.Error("code action resolve is off after the server registered it")
	}

	_ = f.srv.UnregisterCapability(ctx, &protocol.UnregistrationParams{
		Unregisterations: []protocol.Unregistration{{ID: "inlay", Method: "textDocument/inlayHint"}},
	})
	if f.client.Capabilities().InlayHints {
		t.Error("inlay hints are on after the server unregistered them")
	}
	if !f.client.Capabilities().CodeActionResolve {
		t.Error("unregistering one provider cleared another")
	}
}

func TestApplyEditHandlerAnswersOnTheReadPath(t *testing.T) {
	var got *protocol.ApplyWorkspaceEditParams
	f := newFixture(t, Config{ApplyEdit: func(p protocol.ApplyWorkspaceEditParams) bool {
		got = &p
		return true
	}}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	label := "rename"
	edit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		uri.URI(reqDocURI): {{Range: rng(0, 0, 0, 1), NewText: "X"}},
	}}
	res, err := f.srv.ApplyEdit(ctx, &protocol.ApplyWorkspaceEditParams{Label: &label, Edit: edit})
	if err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	if res == nil || !res.Applied {
		t.Fatalf("applyEdit result = %+v, want applied", res)
	}
	if got == nil || got.Label == nil || *got.Label != "rename" {
		t.Fatalf("handler got %+v, want the labeled edit", got)
	}
}

func TestApplyEditWithoutAHandlerIsRefused(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	res, err := f.srv.ApplyEdit(ctx, &protocol.ApplyWorkspaceEditParams{Edit: protocol.WorkspaceEdit{}})
	if err != nil {
		t.Fatalf("applyEdit: %v", err)
	}
	if res == nil || res.Applied {
		t.Fatalf("applyEdit result = %+v, want not applied", res)
	}
}

func TestConfigurationAnswersFromInitializationOptions(t *testing.T) {
	options := protocol.LSPAny(`{"rust-analyzer":{"checkOnSave":true},"other":1}`)
	f := newFixture(t, Config{InitializationOptions: options}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	section := "rust-analyzer"
	unknown := "nope"
	items, err := f.srv.Configuration(ctx, &protocol.ConfigurationParams{Items: []protocol.ConfigurationItem{
		{Section: &section},
		{Section: &unknown},
		{},
	}})
	if err != nil {
		t.Fatalf("configuration: %v", err)
	}
	want := []string{
		`{"checkOnSave":true}`,
		"null",
		`{"rust-analyzer":{"checkOnSave":true},"other":1}`,
	}
	if len(items) != len(want) {
		t.Fatalf("items = %d, want %d", len(items), len(want))
	}
	for i := range want {
		var wantValue, gotValue any
		if err := json.Unmarshal([]byte(want[i]), &wantValue); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(items[i], &gotValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("item %d = %s, want %s", i, items[i], want[i])
		}
	}
}

func TestResolveCodeActionSendsTheData(t *testing.T) {
	data := protocol.LSPAny(`{"id":7}`)
	edit := &protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		uri.URI(reqDocURI): {{Range: rng(0, 0, 0, 1), NewText: "fixed"}},
	}}
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	f.srv.Canned.CodeActions = []protocol.CommandOrCodeAction{
		&protocol.CodeAction{Title: "Fix", Data: data},
	}
	f.srv.Canned.ResolvedCodeAction = &protocol.CodeAction{Title: "Fix", Edit: edit}

	actions, err := f.client.CodeActions(ctx, reqDocURI, rng(0, 0, 0, 1), nil)
	if err != nil {
		t.Fatalf("CodeActions: %v", err)
	}
	nextRequest[*protocol.CodeActionParams](t, f.srv, "code action params")
	if len(actions) != 1 || !actions[0].HasData {
		t.Fatalf("actions = %+v, want one with data", actions)
	}

	resolved, err := f.client.ResolveCodeAction(ctx, actions[0])
	if err != nil {
		t.Fatalf("ResolveCodeAction: %v", err)
	}
	sent := nextRequest[*protocol.CodeAction](t, f.srv, "code action resolve params")
	if string(sent.Data) != string(data) {
		t.Fatalf("resolve sent data %s, want %s", sent.Data, data)
	}
	if resolved.Edit == nil || resolved.Edit.Changes[uri.URI(reqDocURI)][0].NewText != "fixed" {
		t.Fatalf("resolved action = %+v, want the edit", resolved)
	}
	if resolved.HasData {
		t.Error("the resolved action still reports data")
	}
}

func TestExecuteCommandThatAppliesAnEditDoesNotDeadlock(t *testing.T) {
	// The fake server's executeCommand handler sends workspace/applyEdit back and
	// waits for the answer, while the client's caller is still waiting for the
	// executeCommand result. This is the nested-request shape that a reader that
	// queued applyEdit behind the main thread would deadlock on.
	var applied bool
	f := newFixture(t, Config{ApplyEdit: func(p protocol.ApplyWorkspaceEditParams) bool {
		applied = true
		return true
	}}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	f.srv.Canned.OnExecuteCommand = func(ctx context.Context, _ *protocol.ExecuteCommandParams) {
		jsonrpc2Async(ctx)
		params := &protocol.ApplyWorkspaceEditParams{Edit: protocol.WorkspaceEdit{}}
		if _, err := f.srv.ApplyEdit(ctx, params); err != nil {
			t.Errorf("applyEdit from executeCommand: %v", err)
		}
	}

	if err := f.client.ExecuteCommand(ctx, protocol.Command{Title: "Fix", Command: "quickfix"}); err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}
	sent := nextRequest[*protocol.ExecuteCommandParams](t, f.srv, "execute command params")
	if sent.Command != "quickfix" {
		t.Fatalf("command = %q, want quickfix", sent.Command)
	}
	if !applied {
		t.Fatal("the applyEdit sent from executeCommand did not reach the handler")
	}
}

func TestTypedRequests(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)

	f.srv.Canned.InlayHints = []protocol.InlayHint{{Position: protocol.Position{Line: 1, Character: 2}, Label: protocol.String("hint")}}
	hints, err := f.client.InlayHints(ctx, reqDocURI, rng(0, 0, 4, 0))
	if err != nil {
		t.Fatalf("InlayHints: %v", err)
	}
	if len(hints) != 1 {
		t.Fatalf("hints = %+v, want one", hints)
	}
	p := nextRequest[*protocol.InlayHintParams](t, f.srv, "inlay hint params")
	if p.Range != rng(0, 0, 4, 0) || string(p.TextDocument.URI) != reqDocURI {
		t.Fatalf("inlay hint params = %+v", p)
	}

	resultID := "1"
	f.srv.Canned.SemanticTokens = &protocol.SemanticTokens{ResultID: &resultID, Data: []uint32{0, 0, 3, 1, 0}}
	tokens, err := f.client.SemanticTokensFull(ctx, reqDocURI)
	if err != nil || tokens == nil || len(tokens.Data) != 5 {
		t.Fatalf("SemanticTokensFull = %+v, %v", tokens, err)
	}
	nextRequest[*protocol.SemanticTokensParams](t, f.srv, "semantic tokens params")

	f.srv.Canned.SemanticTokensDelta = &protocol.SemanticTokensDelta{Edits: []protocol.SemanticTokensEdit{{Start: 0, DeleteCount: 1}}}
	delta, err := f.client.SemanticTokensDelta(ctx, reqDocURI, "1")
	if err != nil {
		t.Fatalf("SemanticTokensDelta: %v", err)
	}
	if delta == nil || len(delta.(*protocol.SemanticTokensDelta).Edits) != 1 {
		t.Fatalf("delta = %+v, want one edit", delta)
	}
	dp := nextRequest[*protocol.SemanticTokensDeltaParams](t, f.srv, "semantic tokens delta params")
	if dp.PreviousResultID != "1" {
		t.Fatalf("previousResultId = %q, want 1", dp.PreviousResultID)
	}

	f.srv.Canned.SemanticTokensRange = &protocol.SemanticTokens{Data: []uint32{1, 0, 3, 1, 0}}
	if _, err := f.client.SemanticTokensRange(ctx, reqDocURI, rng(2, 0, 3, 0)); err != nil {
		t.Fatalf("SemanticTokensRange: %v", err)
	}
	rp := nextRequest[*protocol.SemanticTokensRangeParams](t, f.srv, "semantic tokens range params")
	if rp.Range != rng(2, 0, 3, 0) {
		t.Fatalf("range = %+v", rp.Range)
	}

	f.srv.Canned.FoldingRanges = []protocol.FoldingRange{{StartLine: 1, EndLine: 4}}
	ranges, err := f.client.FoldingRanges(ctx, reqDocURI)
	if err != nil {
		t.Fatalf("FoldingRanges: %v", err)
	}
	if len(ranges) != 1 || ranges[0].StartLine != 1 {
		t.Fatalf("ranges = %+v, want one from line 1", ranges)
	}
	nextRequest[*protocol.FoldingRangeParams](t, f.srv, "folding range params")
}

func TestPullDiagnostics(t *testing.T) {
	items := []protocol.Diagnostic{{
		Range:    rng(1, 0, 1, 3),
		Severity: protocol.DiagnosticSeverityWarning,
		Message:  protocol.String("warned"),
	}}
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	f.srv.Canned.PullItems = items
	f.srv.Canned.PullResultID = "r1"

	got, err := f.client.PullDiagnostics(ctx, reqDocURI, "")
	if err != nil {
		t.Fatalf("PullDiagnostics: %v", err)
	}
	p := nextRequest[*protocol.DocumentDiagnosticParams](t, f.srv, "diagnostic params")
	if string(p.TextDocument.URI) != reqDocURI {
		t.Fatalf("diagnostic params uri = %q, want %q", p.TextDocument.URI, reqDocURI)
	}
	if got.Unchanged || got.ResultID != "r1" || len(got.Items) != 1 {
		t.Fatalf("pull = %+v, want one full item with result r1", got)
	}
}

func TestPullDiagnosticsUnchanged(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	f.srv.Canned.PullUnchanged = true
	f.srv.Canned.PullResultID = "r2"
	got, err := f.client.PullDiagnostics(ctx, reqDocURI, "r1")
	if err != nil {
		t.Fatalf("PullDiagnostics: %v", err)
	}
	if !got.Unchanged || got.ResultID != "r2" || len(got.Items) != 0 {
		t.Fatalf("pull = %+v, want unchanged with result r2", got)
	}
}

func TestDiagnosticPullCapability(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if f.client.Capabilities().DiagnosticPull {
		t.Fatal("pull diagnostics on before the server advertised the provider")
	}
	// A server that registered textDocument/diagnostic dynamically can pull.
	if err := f.srv.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{ID: "diag", Method: "textDocument/diagnostic"}},
	}); err != nil {
		t.Fatalf("registerCapability: %v", err)
	}
	if !f.client.Capabilities().DiagnosticPull {
		t.Fatal("pull diagnostics off after the dynamic registration")
	}
}

func TestInitializeWithDiagnosticProvider(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	f.srv.Canned.Caps = protocol.ServerCapabilities{DiagnosticProvider: &protocol.DiagnosticOptions{}}
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !f.client.Capabilities().DiagnosticPull {
		t.Fatal("pull diagnostics off although the initialize result advertised diagnosticProvider")
	}
}

func TestWorkspaceDiagnostics(t *testing.T) {
	items := []protocol.Diagnostic{{
		Range:    rng(0, 0, 0, 3),
		Severity: protocol.DiagnosticSeverityError,
		Message:  protocol.String("bad"),
	}}
	resultID := "r1"
	version := int32(4)
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	f.srv.Canned.WorkspaceItems = []protocol.WorkspaceDocumentDiagnosticReport{
		&protocol.WorkspaceFullDocumentDiagnosticReport{
			FullDocumentDiagnosticReport: protocol.FullDocumentDiagnosticReport{
				Kind: string(protocol.DocumentDiagnosticReportKindFull), ResultID: &resultID, Items: items,
			},
			URI:     "file:///work/proj/b.go",
			Version: &version,
		},
		&protocol.WorkspaceUnchangedDocumentDiagnosticReport{
			UnchangedDocumentDiagnosticReport: protocol.UnchangedDocumentDiagnosticReport{
				Kind: string(protocol.DocumentDiagnosticReportKindUnchanged), ResultID: "r2",
			},
			URI: "file:///work/proj/c.go",
		},
	}
	got, err := f.client.WorkspaceDiagnostics(ctx, map[string]string{"file:///work/proj/c.go": "r1"})
	if err != nil {
		t.Fatalf("WorkspaceDiagnostics: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("items = %+v, want two reports", got)
	}
	if got[0].URI != "file:///work/proj/b.go" || got[0].Version == nil || *got[0].Version != 4 ||
		len(got[0].Items) != 1 || got[0].Unchanged {
		t.Fatalf("full report = %+v", got[0])
	}
	if !got[1].Unchanged || got[1].ResultID != "r2" {
		t.Fatalf("unchanged report = %+v", got[1])
	}
	f.srv.Canned.Fail = true
	if _, err := f.client.WorkspaceDiagnostics(ctx, nil); err == nil {
		t.Fatal("a failing request answered without an error")
	}
}

func TestDiagnosticWorkspaceCapability(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	f.srv.Canned.Caps = protocol.ServerCapabilities{
		DiagnosticProvider: &protocol.DiagnosticOptions{WorkspaceDiagnostics: true},
	}
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	caps := f.client.Capabilities()
	if !caps.DiagnosticPull || !caps.DiagnosticWorkspace {
		t.Fatalf("caps = %+v, want pull and workspace diagnostics", caps)
	}
	// A dynamic registration of workspace/diagnostic also turns it on, and both a
	// registration and a server refresh request bump the revision the workspace
	// diagnostics cache watches.
	before := f.client.CapabilitiesRevision()
	if err := f.srv.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{ID: "ws", Method: "workspace/diagnostic"}},
	}); err != nil {
		t.Fatalf("registerCapability: %v", err)
	}
	afterRegister := f.client.CapabilitiesRevision()
	if afterRegister <= before {
		t.Fatalf("revision = %d after the registration, want > %d", afterRegister, before)
	}
	if err := f.srv.RequestDiagnosticRefresh(ctx); err != nil {
		t.Fatalf("diagnostic refresh: %v", err)
	}
	if got := f.client.CapabilitiesRevision(); got <= afterRegister {
		t.Fatalf("revision = %d after workspace/diagnostic/refresh, want > %d", got, afterRegister)
	}
}
