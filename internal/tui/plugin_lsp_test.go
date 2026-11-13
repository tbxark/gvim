package tui

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// lspPE is the plugin editor port of the LSP host tests: the three buffers of the fake
// port, with line text.
type lspPE struct {
	*fakePluginEditor
	text map[int]string
}

func (p *lspPE) Lines(_ pluginapi.BufferID, first, count int) ([]string, error) {
	var out []string
	for i := first; i < first+count; i++ {
		s, ok := p.text[i]
		if !ok {
			break
		}
		out = append(out, s)
	}
	return out, nil
}

// newPluginLSPModel is a model with a plugin runtime, three buffers and a language-server
// port, whose first buffer (/w/a.txt) a server has open.
func newPluginLSPModel(t *testing.T, rt *fakeRuntime, port *fakeLSP) (*Model, *lspPE) {
	t.Helper()
	ed := newFakeEditor("hello", "world")
	ed.quitAction = "quit"
	ed.tabs = []TabInfo{{Name: "a.txt", Path: "/w/a.txt", Active: true}, {Name: "b.txt", Path: "/w/b.txt"}, {Name: "c.txt"}}
	pe := &lspPE{fakePluginEditor: &fakePluginEditor{ed: ed, active: 1, bufs: []pluginapi.BufferInfo{
		{ID: 1, Path: "/w/a.txt", Name: "a.txt", Active: true},
		{ID: 2, Path: "/w/b.txt", Name: "b.txt"},
		{ID: 3, Name: "c.txt"},
	}}, text: map[int]string{0: "héllo wörld", 1: "plain"}}
	deps := Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}}
	if port != nil {
		deps.LSP = port
	}
	m := New(deps)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	m.lsp.tracks = map[string]*lspTrack{"/w/a.txt": {path: "/w/a.txt", state: lspOpen}, "/w/b.txt": {path: "/w/b.txt", state: lspOpening}}
	return m, pe
}

func TestPluginDiagnostics(t *testing.T) {
	t.Parallel()
	diag := func(sl, sc, el, ec, sev int, msg, src string) Diagnostic {
		return Diagnostic{Start: buffer.Pos{Line: sl, Col: sc}, End: buffer.Pos{Line: el, Col: ec}, Severity: sev, Message: msg, Source: src}
	}
	m, _ := newPluginLSPModel(t, newFakeRuntime(t), newFakeLSP("go"))
	m.lsp.diags = map[string][]Diagnostic{
		"/w/a.txt": {
			diag(0, 6, 0, 11, editorpane.SeverityError, "bad word", "gopls"),
			diag(1, 0, 1, 99, editorpane.SeverityHint, "tail", ""),
			diag(0, 0, 0, 1, editorpane.SeverityWarning, "w", ""),
			diag(0, 1, 0, 2, editorpane.SeverityInfo, "i", ""),
		},
	}
	got, err := m.plug.host.Diagnostics(1)
	if err != nil {
		t.Fatal(err)
	}
	// "héllo wörld": h=0 é=1..2 l=3 l=4 o=5 ' '=6 w=7 ö=8..9 r=10 l=11 d=12; rune col 6 is byte 7
	want := []pluginapi.DiagnosticInfo{
		{Range: pluginapi.Range{From: pluginapi.Pos{Line: 0, Col: 7}, To: pluginapi.Pos{Line: 0, Col: len("héllo wörld")}}, Severity: "error", Message: "bad word", Source: "gopls"},
		{Range: pluginapi.Range{From: pluginapi.Pos{Line: 1, Col: 0}, To: pluginapi.Pos{Line: 1, Col: 5}}, Severity: "hint", Message: "tail"},
		{Range: pluginapi.Range{From: pluginapi.Pos{Line: 0, Col: 0}, To: pluginapi.Pos{Line: 0, Col: 1}}, Severity: "warning", Message: "w"},
		{Range: pluginapi.Range{From: pluginapi.Pos{Line: 0, Col: 1}, To: pluginapi.Pos{Line: 0, Col: 3}}, Severity: "info", Message: "i"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Diagnostics =\n%+v\nwant\n%+v", got, want)
	}
}

func TestPluginDiagnosticsRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		port  *fakeLSP
		id    pluginapi.BufferID
		want  pluginapi.Code
		empty bool
	}{
		{name: "no language server", port: nil, id: 1, want: pluginapi.CodeUnsupported},
		{name: "unknown buffer", port: newFakeLSP("go"), id: 9, want: pluginapi.CodeClosed},
		{name: "buffer id 0 is no buffer", port: newFakeLSP("go"), id: 0, want: pluginapi.CodeClosed},
		{name: "buffer without a file has none", port: newFakeLSP("go"), id: 3, empty: true},
		{name: "nothing published yet", port: newFakeLSP("go"), id: 2, empty: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := newPluginLSPModel(t, newFakeRuntime(t), tt.port)
			got, err := m.plug.host.Diagnostics(tt.id)
			if tt.empty {
				if err != nil || len(got) != 0 {
					t.Fatalf("Diagnostics = %v, %v; want none", got, err)
				}
				return
			}
			if pluginapi.CodeOf(err) != tt.want {
				t.Fatalf("Diagnostics error = %v, want code %s", err, tt.want)
			}
		})
	}
}

// pumpMsgs runs the pump and delivers the commands it returns.
func pumpMsgs(m *Model) []tea.Msg { return deliver(m.pluginPump()) }

func TestPluginLSPRequest(t *testing.T) {
	t.Parallel()
	coded := pluginapi.Errf(pluginapi.CodeTimeout, "hover did not answer")
	tests := []struct {
		name      string
		id        pluginapi.BufferID
		reply     any
		replyErr  error
		wantStart pluginapi.Code // the code Host.LSPRequest refuses with, when it does
		wantVals  []any
		wantErr   string
	}{
		{name: "answer", id: 1, reply: map[string]any{"contents": "doc"}, wantVals: []any{map[string]any{"contents": "doc"}}},
		{name: "null answer", id: 1, wantVals: []any{nil}},
		{name: "coded error passes through", id: 1, replyErr: coded, wantErr: "timeout: hover did not answer"},
		{name: "other errors are io", id: 1, replyErr: errors.New("pipe closed"), wantErr: "io: pipe closed"},
		{name: "server does not have it open yet", id: 2, wantStart: pluginapi.CodeNotFound},
		{name: "buffer without a file", id: 3, wantStart: pluginapi.CodeInvalid},
		{name: "closed buffer", id: 9, wantStart: pluginapi.CodeClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t)
			port := newFakeLSP("go")
			port.reply, port.reqErr = tt.reply, tt.replyErr
			m, _ := newPluginLSPModel(t, rt, port)
			err := m.plug.host.LSPRequest(7, tt.id, "textDocument/hover", map[string]any{"a": 1.0})
			if tt.wantStart != "" {
				if pluginapi.CodeOf(err) != tt.wantStart {
					t.Fatalf("LSPRequest = %v, want code %s", err, tt.wantStart)
				}
				if len(port.requests) != 0 || len(m.plug.waits) != 0 {
					t.Fatalf("a refused request left requests %v, waits %v", port.requests, m.plug.waits)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(port.requests) != 0 {
				t.Fatalf("the request was sent inside the Host call: %v", port.requests)
			}
			var done *pluginLSPMsg
			for _, msg := range pumpMsgs(m) {
				if d, ok := msg.(pluginLSPMsg); ok {
					done = &d
				}
			}
			if done == nil {
				t.Fatal("the pump produced no answer")
			}
			if want := fmt.Sprintf("/w/a.txt textDocument/hover %v", map[string]any{"a": 1.0}); len(port.requests) != 1 || port.requests[0] != want {
				t.Fatalf("requests = %v, want [%s]", port.requests, want)
			}
			if _, cmd := m.Update(*done); cmd != nil {
				deliver(cmd)
			}
			if len(rt.resolved) != 1 {
				t.Fatalf("resolved %d completions, want 1", len(rt.resolved))
			}
			c := rt.resolved[0]
			if c.Token != 7 || c.Err != tt.wantErr || fmt.Sprint(c.Values) != fmt.Sprint(tt.wantVals) {
				t.Fatalf("completion = %+v, want token 7 err %q values %v", c, tt.wantErr, tt.wantVals)
			}
		})
	}
}

func TestPluginLSPRequestCancelledDropsTheAnswer(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _ := newPluginLSPModel(t, rt, newFakeLSP("go"))
	if err := m.plug.host.LSPRequest(3, 1, "textDocument/hover", nil); err != nil {
		t.Fatal(err)
	}
	m.plug.host.CancelWait(3)
	for _, msg := range pumpMsgs(m) {
		m.Update(msg)
	}
	if len(rt.resolved) != 0 {
		t.Fatalf("a cancelled request was resolved: %+v", rt.resolved)
	}
}

func TestPluginDiagnosticsUpdatedEvent(t *testing.T) {
	t.Parallel()
	d := func(sev int) Diagnostic { return Diagnostic{Severity: sev, Message: "m"} }
	a1, a2 := []Diagnostic{d(editorpane.SeverityError), d(editorpane.SeverityError), d(editorpane.SeverityHint)}, []Diagnostic{d(editorpane.SeverityWarning)}
	tests := []struct {
		name   string
		wanted bool
		steps  [][]DiagnosticsEvent
		want   []string // the counts of each event raised, "path error/warning/info/hint"
	}{
		{name: "an event with counts", wanted: true,
			steps: [][]DiagnosticsEvent{{{Path: "/w/a.txt", Items: a1}}},
			want:  []string{"/w/a.txt 2/0/0/1"}},
		{name: "unchanged diagnostics raise nothing", wanted: true,
			steps: [][]DiagnosticsEvent{{{Path: "/w/a.txt", Items: a1}}, {{Path: "/w/a.txt", Items: slices.Clone(a1)}}},
			want:  []string{"/w/a.txt 2/0/0/1"}},
		{name: "a change raises again", wanted: true,
			steps: [][]DiagnosticsEvent{{{Path: "/w/a.txt", Items: a1}}, {{Path: "/w/a.txt", Items: a2}}},
			want:  []string{"/w/a.txt 2/0/0/1", "/w/a.txt 0/1/0/0"}},
		{name: "clearing raises zero counts", wanted: true,
			steps: [][]DiagnosticsEvent{{{Path: "/w/a.txt", Items: a1}}, {{Path: "/w/a.txt"}}},
			want:  []string{"/w/a.txt 2/0/0/1", "/w/a.txt 0/0/0/0"}},
		{name: "nothing before and nothing now raises nothing", wanted: true,
			steps: [][]DiagnosticsEvent{{{Path: "/w/a.txt"}}}},
		{name: "a path without a buffer raises nothing", wanted: true,
			steps: [][]DiagnosticsEvent{{{Path: "/w/gone.txt", Items: a1}}}},
		{name: "not wanted raises nothing", wanted: false,
			steps: [][]DiagnosticsEvent{{{Path: "/w/a.txt", Items: a1}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t)
			rt.wants["diagnostics_updated"] = tt.wanted
			m, _ := newPluginLSPModel(t, rt, newFakeLSP("go"))
			for _, ev := range tt.steps {
				_, cmd := m.Update(lspDiagsMsg{events: ev})
				deliver(cmd)
				deliver(m.pluginPump())
			}
			var got []string
			for _, e := range rt.emitted {
				if e.Name != "diagnostics_updated" {
					continue
				}
				c := e.Data["counts"].(map[string]any)
				if e.Data["buf"] != pluginapi.BufferID(1) {
					t.Fatalf("event buf = %v, want buffer 1", e.Data["buf"])
				}
				got = append(got, fmt.Sprintf("%v %v/%v/%v/%v", e.Data["path"], c["error"], c["warning"], c["info"], c["hint"]))
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPluginDiagnosticsUpdatedWhenDiagnosticsAreDropped: every way a document loses its
// diagnostics other than a poll (the server stops, LSP goes off for the buffer, the tab
// goes away) raises diagnostics_updated with zero counts.
func TestPluginDiagnosticsUpdatedWhenDiagnosticsAreDropped(t *testing.T) {
	d := func(sev int) []Diagnostic { return []Diagnostic{{Severity: sev, Message: "m"}} }
	tests := []struct {
		name string
		drop func(lt *lspTest)
		want []string // events after the drop
	}{
		{name: "server stopped", drop: func(lt *lspTest) { lt.m.lspServerDone(lspServerMsg{action: "stop"}) },
			want: []string{"/p/a.go 0/0/0/0", "/p/b.go 0/0/0/0"}},
		{name: "off for the buffer", drop: func(lt *lspTest) {
			for _, msg := range lt.act("lsp_toggle_for_buffer") {
				lt.feed(msg)
			}
		}, want: []string{"/p/a.go 0/0/0/0"}},
		{name: "tab closed", drop: func(lt *lspTest) {
			lt.ed.tabs = lt.ed.tabs[:1]
			lt.touch()
		}, want: []string{"/p/b.go 0/0/0/0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newFakeRuntime(t)
			rt.wants["diagnostics_updated"] = true
			pe := &fakePluginEditor{ed: newFakeEditor("x"), active: 1, bufs: []pluginapi.BufferInfo{
				{ID: 1, Path: "/p/a.go", Name: "a.go", Active: true}, {ID: 2, Path: "/p/b.go", Name: "b.go"}}}
			lt := newLSPTestWith(t, newFakeLSP("go"), RootPorts{Plugins: rt, PluginEditor: pe},
				&lspTab{path: "/p/a.go", lines: []string{"package a"}}, &lspTab{path: "/p/b.go", lines: []string{"package b"}})
			useHost(rt, lt.m)
			lt.open()
			lt.touch()
			lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{
				{Path: "/p/a.go", Items: d(editorpane.SeverityError)}, {Path: "/p/b.go", Items: d(editorpane.SeverityWarning)}}})
			deliver(lt.m.pluginPump())
			before := len(rt.emitted)
			if before != 2 {
				t.Fatalf("events after the diagnostics = %d, want 2", before)
			}
			tt.drop(lt)
			deliver(lt.m.pluginPump())
			var got []string
			for _, e := range rt.emitted[before:] {
				c := e.Data["counts"].(map[string]any)
				got = append(got, fmt.Sprintf("%v %v/%v/%v/%v", e.Data["path"], c["error"], c["warning"], c["info"], c["hint"]))
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("events = %v, want %v", got, tt.want)
			}
		})
	}
}
