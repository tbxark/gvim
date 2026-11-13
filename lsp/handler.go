package lsp

import (
	"context"
	"encoding/json"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// DiagnosticsEvent is one textDocument/publishDiagnostics notification from the
// server. An empty Diagnostics slice is a real event: it clears the document's
// diagnostics.
type DiagnosticsEvent struct {
	URI     string
	Version int32 // document version reported by the server; 0 when absent
	// Versioned says the server reported a version with the diagnostics. Only
	// then can the editor tell whether they are stale for a newer text.
	Versioned   bool
	Diagnostics []protocol.Diagnostic
}

// clientHandler answers server-to-client requests and notifications. It embeds
// protocol.UnimplementedClient, so any method not listed here returns the
// library default (requests get an error, notifications are ignored). The
// methods that a server may call during normal operation are implemented
// explicitly here.
//
// Handlers run inline on the connection's read goroutine. None of them blocks:
// workspace/applyEdit hands the edit to its callback, which enqueues it for the
// UI and answers at once. That is what keeps a server's executeCommand, whose
// answer the UI goroutine is waiting for, from deadlocking when the command
// sends an applyEdit request back.
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
		ev.Version, ev.Versioned = v, true
	}
	h.c.publish(ev)
	return nil
}

// WorkDoneProgressCreate acknowledges a server's request to create a progress
// token. The editor shows progress as text from the $/progress notifications, so
// the token only has to be accepted; a refused create would make the server skip
// its progress reporting.
func (h *clientHandler) WorkDoneProgressCreate(context.Context, *protocol.WorkDoneProgressCreateParams) error {
	return nil
}

// Progress records one $/progress notification: begin adds an entry, report updates
// it and end removes it. The entries are the bounded text list the diagnostics
// panel shows. It runs on the read goroutine and only takes a mutex.
func (h *clientHandler) Progress(_ context.Context, p *protocol.ProgressParams) error {
	h.c.trackProgress(p)
	return nil
}

// RegisterCapability records the providers the server registered dynamically.
// Many servers advertise little or nothing in their initialize result and
// register their providers here instead; without this the feature would stay
// gated off for the whole session. The answer is sent when this returns.
func (h *clientHandler) RegisterCapability(_ context.Context, p *protocol.RegistrationParams) error {
	h.c.registerCapabilities(p.Registrations)
	return nil
}

// UnregisterCapability removes the providers the server withdrew. The snapshot is
// recomputed from the static capabilities plus what is left.
func (h *clientHandler) UnregisterCapability(_ context.Context, p *protocol.UnregistrationParams) error {
	h.c.unregisterCapabilities(p.Unregisterations)
	return nil
}

// DiagnosticRefresh marks the workspace diagnostics cache stale: the server's
// workspace/diagnostic result changed, so the next poll asks again. The refresh
// request asks the client to re-pull; answering nil accepts it.
func (h *clientHandler) DiagnosticRefresh(context.Context) error {
	h.c.bumpCapabilitiesRevision()
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

// ApplyEdit answers a server's workspace/applyEdit request. The edit is handed to
// the configured callback and the answer goes back on the read goroutine, so the
// server is never left waiting on the UI. Without a callback every edit is
// refused. A refusal carries its reason as failureReason. It never blocks: the
// callback enqueues the edit and returns.
func (h *clientHandler) ApplyEdit(_ context.Context, p *protocol.ApplyWorkspaceEditParams) (*protocol.ApplyWorkspaceEditResult, error) {
	applied, reason := false, "the editor does not take workspace edits from servers"
	if h.c.applyEdit != nil {
		applied, reason = h.c.applyEdit(*p)
	}
	res := &protocol.ApplyWorkspaceEditResult{Applied: applied}
	if !applied && reason != "" {
		res.FailureReason = &reason
	}
	return res, nil
}

// Configuration answers a server's workspace/configuration request from the
// initialization options this server was started with, as Fresh does: a section
// is looked up in the options object, a dotted section follows nested objects, and
// a section that is not there answers null ("use your defaults").
func (h *clientHandler) Configuration(_ context.Context, p *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	out := make([]protocol.LSPAny, len(p.Items))
	for i, item := range p.Items {
		section := ""
		if item.Section != nil {
			section = *item.Section
		}
		if v, ok := configurationSection(h.c.initOptions, section); ok {
			out[i] = v
		} else {
			out[i] = protocol.LSPAny("null")
		}
	}
	return out, nil
}

// configurationSection returns the part of the initialization options a
// workspace/configuration section names. An empty section is the whole options
// value, and a dotted section walks nested objects.
func configurationSection(options protocol.LSPAny, section string) (protocol.LSPAny, bool) {
	if len(options) == 0 {
		return nil, false
	}
	if section == "" {
		return options, true
	}
	current := options
	for part := range strings.SplitSeq(section, ".") {
		var object map[string]json.RawMessage
		if json.Unmarshal(current, &object) != nil {
			return nil, false
		}
		next, ok := object[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// WorkspaceFolders reports the configured root as the only workspace folder.
func (h *clientHandler) WorkspaceFolders(context.Context) ([]protocol.WorkspaceFolder, error) {
	if h.rootURI == "" {
		return []protocol.WorkspaceFolder{}, nil
	}
	return []protocol.WorkspaceFolder{{URI: uri.URI(h.rootURI), Name: "root"}}, nil
}
