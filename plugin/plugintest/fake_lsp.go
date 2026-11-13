package plugintest

import (
	"sort"
	"strconv"

	"github.com/tbxark/gvim/pluginapi"
)

// The language-server half of the fake host: the diagnostics a plugin reads and
// the requests it sends. It is off until a test calls EnableLSP, as the ui's is without a
// configured server: both calls then answer "unsupported".

// LSPCall is one request of Host.LSPRequest.
type LSPCall struct {
	Token  pluginapi.Token
	Buffer pluginapi.BufferID
	Method string
	Params any
}

// FakeLSP is the state behind FakeHost.Diagnostics and FakeHost.LSPRequest.
type FakeLSP struct {
	// Enabled turns the fake on; off, both calls answer CodeUnsupported.
	Enabled bool
	// Diags are the diagnostics by buffer.
	Diags map[pluginapi.BufferID][]pluginapi.DiagnosticInfo
	// Calls are the requests in order, and Cancelled the tokens CancelWait was called with.
	Calls     []LSPCall
	Cancelled []pluginapi.Token
	// Reply answers a request: the result and an error text ("<code>: <msg>"). Nil: a null
	// result.
	Reply func(c LSPCall) (result any, errText string)
	// Err makes LSPRequest itself fail (the request is not recorded as pending).
	Err error

	pending []LSPCall
}

// LSP returns the language-server state of the fake host.
func (f *FakeHost) LSP() *FakeLSP {
	if l, ok := f.Ext["lsp"].(*FakeLSP); ok {
		return l
	}
	l := &FakeLSP{Diags: map[pluginapi.BufferID][]pluginapi.DiagnosticInfo{}}
	f.Ext["lsp"] = l
	return l
}

// Diagnostics returns the diagnostics set for buffer id.
func (f *FakeHost) Diagnostics(id pluginapi.BufferID) ([]pluginapi.DiagnosticInfo, error) {
	f.rec("Diagnostics", id)
	l := f.LSP()
	if !l.Enabled {
		return nil, pluginapi.Errf(pluginapi.CodeUnsupported, "no language server is configured")
	}
	if f.find(id) == nil {
		return nil, bufClosedErr(id)
	}
	return append([]pluginapi.DiagnosticInfo(nil), l.Diags[id]...), nil
}

// AllDiagnostics returns the diagnostics of every buffer that has some, by path, sorted.
func (f *FakeHost) AllDiagnostics() ([]pluginapi.FileDiagnostics, error) {
	f.rec("AllDiagnostics")
	l := f.LSP()
	if !l.Enabled {
		return nil, pluginapi.Errf(pluginapi.CodeUnsupported, "no language server is configured")
	}
	var out []pluginapi.FileDiagnostics
	for _, b := range f.Buffers {
		if items := l.Diags[b.Info.ID]; len(items) > 0 {
			out = append(out, pluginapi.FileDiagnostics{Path: b.Info.Path, Diagnostics: append([]pluginapi.DiagnosticInfo(nil), items...)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// LSPRequest records the request; CompleteLSP answers it.
func (f *FakeHost) LSPRequest(t pluginapi.Token, id pluginapi.BufferID, method string, params any) error {
	f.rec("LSPRequest", t, id, method, params)
	l := f.LSP()
	switch {
	case !l.Enabled:
		return pluginapi.Errf(pluginapi.CodeUnsupported, "no language server is configured")
	case l.Err != nil:
		return l.Err
	case f.find(id) == nil:
		return bufClosedErr(id)
	}
	c := LSPCall{Token: t, Buffer: id, Method: method, Params: params}
	l.Calls = append(l.Calls, c)
	l.pending = append(l.pending, c)
	return nil
}

func cancelLSP(f *FakeHost, t pluginapi.Token) {
	l := f.LSP()
	l.Cancelled = append(l.Cancelled, t)
	for i, c := range l.pending {
		if c.Token == t {
			l.pending = append(l.pending[:i], l.pending[i+1:]...)
			break
		}
	}
}

func init() {
	OnCancelWait(cancelLSP)
	RegisterResourceKind(func(f *FakeHost) []string {
		l, ok := f.Ext["lsp"].(*FakeLSP)
		if !ok {
			return nil
		}
		var out []string
		for _, c := range l.pending {
			out = append(out, "lsp:"+strconv.FormatUint(uint64(c.Token), 10))
		}
		return out
	})
}

// CompleteLSP answers the pending requests with FakeLSP.Reply, the way the ui does when
// the server has answered, and settles.
func (h *Harness) CompleteLSP() {
	h.T.Helper()
	l := h.Host.LSP()
	pending := l.pending
	l.pending = nil
	for _, c := range pending {
		out := pluginapi.Completion{Token: c.Token}
		if l.Reply != nil {
			res, errText := l.Reply(c)
			if errText != "" {
				out.Err = errText
			} else {
				out.Values = []any{res}
			}
		} else {
			out.Values = []any{nil}
		}
		h.Resolve(out)
	}
}
