package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.lsp (stage S23): what the language servers know, read only.
//
//	gvim.lsp.diagnostics(buf?) -> { {range=Range, severity=, message=, source=}, ... } | nil, err
//	      severity is "error", "warning", "info" or "hint"; the range has byte columns, as
//	      every range of the API (AMENDMENTS C24); buf defaults to the current buffer
//	gvim.lsp.request(method, params, opts?) ⏳ -> result | nil, err
//	      opts {buf = Buffer}: the buffer whose server is asked (default: the current one);
//	      params and result are the protocol's own JSON, with its UTF-16 columns
//
// A request goes to the server that has the buffer open, so the buffer must be a file with a
// configured language server. Only requests of the read allowlist (lspReadMethods) are sent:
// the lifecycle of the connection, every notification of document synchronisation and
// workspace/executeCommand, which lets a server do anything, are refused (lspMethodErr).

func init() {
	registerAwait("lsp_request", awaitLSPRequest)
}

func installLSP(e *apiEnv) {
	p := e.p
	t := e.area("lsp")
	e.set(t, "diagnostics", func(L *lua.LState) int {
		id := p.r.guard.ActiveBuffer()
		if L.Get(1) != lua.LNil {
			id = pluginapi.BufferID(checkID(L, 1, kindBuffer))
		}
		if id == 0 {
			L.Push(L.CreateTable(0, 0))
			return 1
		}
		items, err := p.r.guard.Diagnostics(id)
		if err != nil {
			return bufRet(L, err)
		}
		out := L.CreateTable(len(items), 0)
		for _, d := range items {
			row := L.CreateTable(0, 4)
			row.RawSetString("range", p.rangeTable(d.Range))
			row.RawSetString("severity", lua.LString(d.Severity))
			row.RawSetString("message", lua.LString(d.Message))
			if d.Source != "" {
				row.RawSetString("source", lua.LString(d.Source))
			}
			out.Append(row)
		}
		L.Push(out)
		return 1
	})
	t.RawSetString("request", e.async("lsp_request"))
}

// lspReadMethods is the allowlist of gvim.lsp.request: the requests of the protocol that
// only read (or compute edits and never apply them). An allowlist, not a denylist: a server
// dispatches on the method name whether or not the message has an id, so every method of
// document synchronisation (textDocument/did*, will*, notebookDocument/*, workspace/did*),
// of the lifecycle and workspace/executeCommand, which lets a server do anything, stays out.
var lspReadMethods = map[string]bool{
	"textDocument/hover": true, "textDocument/definition": true, "textDocument/declaration": true,
	"textDocument/typeDefinition": true, "textDocument/implementation": true,
	"textDocument/references": true, "textDocument/documentSymbol": true,
	"textDocument/documentHighlight": true, "textDocument/documentLink": true,
	"textDocument/documentColor": true, "textDocument/colorPresentation": true,
	"textDocument/codeAction": true, "textDocument/codeLens": true,
	"textDocument/completion": true, "textDocument/signatureHelp": true,
	"textDocument/formatting": true, "textDocument/rangeFormatting": true,
	"textDocument/onTypeFormatting": true, "textDocument/rename": true,
	"textDocument/prepareRename": true, "textDocument/foldingRange": true,
	"textDocument/selectionRange": true, "textDocument/linkedEditingRange": true,
	"textDocument/inlayHint": true, "textDocument/inlineValue": true,
	"textDocument/moniker": true, "textDocument/diagnostic": true,
	"textDocument/semanticTokens/full": true, "textDocument/semanticTokens/range": true,
	"textDocument/prepareCallHierarchy": true, "textDocument/prepareTypeHierarchy": true,
	"callHierarchy/incomingCalls": true, "callHierarchy/outgoingCalls": true,
	"typeHierarchy/supertypes": true, "typeHierarchy/subtypes": true,
	"workspace/symbol":       true,
	"completionItem/resolve": true, "codeAction/resolve": true, "codeLens/resolve": true,
	"documentLink/resolve": true, "inlayHint/resolve": true, "workspaceSymbol/resolve": true,
}

// lspMethodErr says why a request method is refused, or nil when a plugin may send it.
func lspMethodErr(method string) error {
	if method == "" || len(method) > 128 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "request: the method name must be 1 to 128 bytes")
	}
	for _, r := range method {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '/', r == '_', r == '$':
		default:
			return pluginapi.Errf(pluginapi.CodeInvalid, "request: bad method name %q", method)
		}
	}
	if !lspReadMethods[method] {
		return pluginapi.Errf(pluginapi.CodeInvalid, "request: %s is not allowed: only requests that read are", method)
	}
	return nil
}

// awaitLSPRequest is gvim.lsp.request(method, params, opts?).
func awaitLSPRequest(c *awaitCall) {
	method, ok := stringArg(c.args, 0)
	if !ok {
		c.Bad(1, "request", "string expected")
		return
	}
	if err := lspMethodErr(method); err != nil {
		c.Fail(err)
		return
	}
	var params any
	if len(c.args) > 1 && c.args[1] != lua.LNil {
		if _, isTable := c.args[1].(*lua.LTable); !isTable {
			c.Bad(2, "request", "table expected, got "+c.args[1].Type().String())
			return
		}
		v, err := c.p.fromLua(c.args[1])
		if err != nil {
			c.Fail(pluginapi.Errf(pluginapi.CodeInvalid, "request: params: %v", err))
			return
		}
		params = v
	}
	id := c.r.guard.ActiveBuffer()
	opts, ok := bufOptional(c, 2, "request")
	if !ok {
		return
	}
	if opts != nil {
		if bv := opts.RawGetString("buf"); bv != lua.LNil {
			var hid idHandle
			isHandle := false
			if ud, isUD := bv.(*lua.LUserData); isUD {
				hid, isHandle = ud.Value.(idHandle)
			}
			if !isHandle || hid.kind != kindBuffer {
				c.Bad(3, "request", "option 'buf' must be a Buffer")
				return
			}
			id = pluginapi.BufferID(hid.id)
		}
	}
	if id == 0 {
		c.Fail(pluginapi.Errf(pluginapi.CodeNotFound, "request: there is no buffer"))
		return
	}
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	tok := c.NewToken()
	g := c.r.guard
	if err := g.LSPRequest(tok, id, method, params); err != nil {
		c.Fail(err)
		return
	}
	pl := c.p
	c.Park(&parkSpec{Token: tok, Cancel: func() { g.CancelWait(tok) }, Convert: func(cp pluginapi.Completion) []lua.LValue {
		if cp.Err != "" || len(cp.Values) == 0 {
			return pl.defaultConvert(cp)
		}
		return []lua.LValue{lua.LTrue, pl.jsonResult(cp.Values[0], true)}
	}})
}

// jsonResult converts a decoded JSON value to Lua the way json.decode does, so that the
// result of a request can be read and passed back without loss: a null inside is json.null
// (an array keeps its length) and an empty array is marked with json.array. A null result
// as a whole is nil.
func (p *plugin) jsonResult(v any, top bool) lua.LValue {
	switch x := v.(type) {
	case nil:
		if top {
			return lua.LNil
		}
		return p.data.null
	case []any:
		t := p.L.CreateTable(len(x), 0)
		for _, e := range x {
			t.Append(p.jsonResult(e, false))
		}
		if len(x) == 0 {
			p.L.SetMetatable(t, p.data.arrayMT)
		}
		return t
	case map[string]any:
		t := p.L.CreateTable(0, len(x))
		for k, e := range x {
			t.RawSetString(k, p.jsonResult(e, false))
		}
		return t
	}
	return p.toLua(v)
}
