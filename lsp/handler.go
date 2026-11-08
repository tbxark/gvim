package lsp

import (
	"context"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// DiagnosticsEvent is one textDocument/publishDiagnostics notification from the
// server. An empty Diagnostics slice is a real event: it clears the document's
// diagnostics.
type DiagnosticsEvent struct {
	URI         string
	Version     int32 // document version reported by the server; 0 when absent
	Diagnostics []protocol.Diagnostic
}

// clientHandler answers server-to-client requests and notifications. It embeds
// protocol.UnimplementedClient, so any method not listed here returns the
// library default (requests get an error, notifications are ignored). The
// methods that a server may call during normal operation are implemented
// explicitly here.
//
// Handlers run inline on the connection's read goroutine. None of them blocks.
type clientHandler struct {
	protocol.UnimplementedClient

	c       *Client
	rootURI string
}

// PublishDiagnostics forwards the notification to the Diagnostics channel. The
// send never blocks; see Client.publish for the drop-oldest policy.
func (h *clientHandler) PublishDiagnostics(_ context.Context, p *protocol.PublishDiagnosticsParams) error {
	ev := DiagnosticsEvent{
		URI:         string(p.URI),
		Diagnostics: p.Diagnostics,
	}
	if v, ok := p.Version.Get(); ok {
		ev.Version = v
	}
	h.c.publish(ev)
	return nil
}

// RegisterCapability accepts dynamic registrations without acting on them.
func (h *clientHandler) RegisterCapability(context.Context, *protocol.RegistrationParams) error {
	return nil
}

// UnregisterCapability accepts unregistrations without acting on them.
func (h *clientHandler) UnregisterCapability(context.Context, *protocol.UnregistrationParams) error {
	return nil
}

// WorkDoneProgressCreate accepts progress tokens. Progress is not displayed.
func (h *clientHandler) WorkDoneProgressCreate(context.Context, *protocol.WorkDoneProgressCreateParams) error {
	return nil
}

// ShowMessage drops the message. The UI has no message surface for it yet.
func (h *clientHandler) ShowMessage(context.Context, *protocol.ShowMessageParams) error {
	return nil
}

// LogMessage drops the message.
func (h *clientHandler) LogMessage(context.Context, *protocol.LogMessageParams) error {
	return nil
}

// Configuration returns null for every requested item, meaning "no settings".
func (h *clientHandler) Configuration(_ context.Context, p *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	out := make([]protocol.LSPAny, len(p.Items))
	for i := range out {
		out[i] = protocol.LSPAny("null")
	}
	return out, nil
}

// WorkspaceFolders reports the configured root as the only workspace folder.
func (h *clientHandler) WorkspaceFolders(context.Context) ([]protocol.WorkspaceFolder, error) {
	if h.rootURI == "" {
		return []protocol.WorkspaceFolder{}, nil
	}
	return []protocol.WorkspaceFolder{{URI: uri.URI(h.rootURI), Name: "root"}}, nil
}
