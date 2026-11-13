package lsphost

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/manager"
)

// semanticCache is the semantic-token data last received for one document: the
// document version it describes, the server's result id for the delta request, and
// the raw token data. A version mismatch invalidates it, so an edit never paints
// tokens over a text they were not computed for.
type semanticCache struct {
	version  int32
	resultID string
	data     []uint32
}

// workspacePull is the last workspace/diagnostic answer and the state it was taken
// under: the time, the servers' capability revision and the number of open
// documents. A change in any of them means the next poll asks again.
type workspacePull struct {
	files    map[string][]protocol.Diagnostic
	at       time.Time
	capsRev  uint64
	docCount int
	ok       bool
}

// pulledDiagnostics is the last server-pulled diagnostics of one document, kept so
// the 300ms diagnostics poll does not pull again while the text has not changed.
type pulledDiagnostics struct {
	version int32
	items   []protocol.Diagnostic
}

// InlayHints returns the inlay hints of the document at path. Each keeps the line and
// rune column the pane inserts its cells at (tier 2), plus the joined label the
// end-of-line fallback form uses.
func (p *lspPort) InlayHints(path string) ([]tui.InlayHint, error) {
	hs, rec, err := p.handles(path, manager.FeatureInlayHints)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	last := len(rec.lines) - 1
	if last < 0 {
		last = 0
	}
	hints, err := hs[0].Client().InlayHints(ctx, lsp.PathURI(path), protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: uint32(last), Character: uint32(len(rec.lines.line(last)))},
	})
	if err != nil {
		return nil, err
	}
	return inlayHintsFrom(rec.lines, hints), nil
}

// inlayHintsFrom maps the protocol hints to the editor's form: the UTF-16 character is
// converted to a rune column of its line (clamped to the line), the label's parts are
// kept, and the padding flags are carried. A hint whose line is outside the document or
// whose label is empty is dropped.
func inlayHintsFrom(lines textLines, hints []protocol.InlayHint) []tui.InlayHint {
	out := make([]tui.InlayHint, 0, len(hints))
	for _, hint := range hints {
		line := int(hint.Position.Line)
		if line < 0 || line >= len(lines) {
			continue
		}
		text := strings.TrimSpace(hintText(hint.Label))
		if text == "" {
			continue
		}
		out = append(out, tui.InlayHint{
			Line:         line,
			Col:          lsp.UTF16ToRuneCol(lines.line(line), hint.Position.Character),
			Text:         text,
			Parts:        hintParts(hint.Label),
			PaddingLeft:  hint.PaddingLeft != nil && *hint.PaddingLeft,
			PaddingRight: hint.PaddingRight != nil && *hint.PaddingRight,
			Kind:         int(hint.Kind),
		})
	}
	return out
}

// hintParts returns the label's parts, one entry per protocol label part; a string label
// is one part.
func hintParts(label protocol.InlayHintLabel) []string {
	switch v := label.(type) {
	case protocol.String:
		return []string{string(v)}
	case protocol.InlayHintLabelPartSlice:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			parts = append(parts, part.Value)
		}
		return parts
	}
	return nil
}

// hintText renders an inlay hint label, which is a string or a list of parts.
func hintText(label protocol.InlayHintLabel) string {
	return strings.Join(hintParts(label), "")
}

// SemanticTokens returns the semantic tokens of the document's lines
// [firstLine, lastLine]. It asks for the whole document and filters to the visible
// lines, so the answer is correct with or without the server's range support; a
// server that advertises deltas is asked for one after the first full answer.
func (p *lspPort) SemanticTokens(path string, firstLine, lastLine int) ([]tui.SemanticToken, error) {
	hs, rec, err := p.handles(path, manager.FeatureSemanticTokens)
	if err != nil {
		return nil, err
	}
	h := hs[0]
	caps := h.Capabilities()
	legend := caps.SemanticTokensLegend
	if legend == nil || len(legend.TokenTypes) == 0 {
		return nil, fmt.Errorf("lsp: %s does not send a semantic token legend", h.Name())
	}

	p.mu.Lock()
	cache, ok := p.sem[path]
	p.mu.Unlock()
	version := rec.version
	if ok && cache.version == version && len(cache.data) > 0 {
		return decodeSemanticTokens(cache.data, legend, rec.lines, firstLine, lastLine), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	docURI := lsp.PathURI(path)
	var data []uint32
	var resultID string
	if ok && cache.resultID != "" && caps.SemanticTokensDelta {
		result, err := h.Client().SemanticTokensDelta(ctx, docURI, cache.resultID)
		if err != nil {
			return nil, err
		}
		switch v := result.(type) {
		case *protocol.SemanticTokens:
			if v != nil {
				data, resultID = v.Data, stringValue(v.ResultID)
			}
		case *protocol.SemanticTokensDelta:
			if v != nil {
				data = applySemanticTokenEdits(cache.data, v.Edits)
				resultID = cache.resultID
				if v.ResultID != nil {
					resultID = *v.ResultID
				}
			}
		}
	} else {
		tokens, err := h.Client().SemanticTokensFull(ctx, docURI)
		if err != nil {
			return nil, err
		}
		if tokens != nil {
			data, resultID = tokens.Data, stringValue(tokens.ResultID)
		}
	}

	p.mu.Lock()
	if p.sem == nil {
		p.sem = make(map[string]semanticCache)
	}
	p.sem[path] = semanticCache{version: version, resultID: resultID, data: slices.Clone(data)}
	p.mu.Unlock()
	return decodeSemanticTokens(data, legend, rec.lines, firstLine, lastLine), nil
}

// stringValue reads a protocol optional string.
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// applySemanticTokenEdits applies a semantic token delta to the previous data.
func applySemanticTokenEdits(data []uint32, edits []protocol.SemanticTokensEdit) []uint32 {
	out := slices.Clone(data)
	for _, edit := range edits {
		start := min(int(edit.Start), len(out))
		del := min(int(edit.DeleteCount), len(out)-start)
		out = slices.Replace(out, start, start+del, edit.Data...)
	}
	return out
}

// decodeSemanticTokens turns the protocol's quintuples into rune-column spans of
// the lines [firstLine, lastLine], both zero-based and inclusive. A span is one
// line; its type and modifiers come from the server's legend.
func decodeSemanticTokens(data []uint32, legend *protocol.SemanticTokensLegend, lines textLines, firstLine, lastLine int) []tui.SemanticToken {
	var out []tui.SemanticToken
	line, char := 0, 0
	for i := 0; i+4 < len(data); i += 5 {
		deltaLine, deltaChar := int(data[i]), int(data[i+1])
		length, tokenType, mods := int(data[i+2]), int(data[i+3]), data[i+4]
		if deltaLine == 0 {
			char += deltaChar
		} else {
			line += deltaLine
			char = deltaChar
		}
		if tokenType < 0 || tokenType >= len(legend.TokenTypes) {
			continue
		}
		if line < firstLine || line > lastLine {
			continue
		}
		start := lsp.UTF16ToRuneCol(lines.line(line), uint32(char))
		end := lsp.UTF16ToRuneCol(lines.line(line), uint32(char+length))
		if end <= start {
			continue
		}
		out = append(out, tui.SemanticToken{
			Line:      line,
			Start:     start,
			End:       end,
			Type:      legend.TokenTypes[tokenType],
			Modifiers: semanticModifiers(mods, legend.TokenModifiers),
		})
	}
	return out
}

// semanticModifiers names the modifier bits set in mods.
func semanticModifiers(mods uint32, names []string) []string {
	if mods == 0 || len(names) == 0 {
		return nil
	}
	var out []string
	for bit, name := range names {
		if bit >= 32 {
			break
		}
		if mods&(1<<uint(bit)) != 0 {
			out = append(out, name)
		}
	}
	return out
}

// FoldingRanges returns the server's folding ranges for the document, clamped to
// its lines. The editor merges them with its indentation folds.
func (p *lspPort) FoldingRanges(path string) ([]tui.FoldRange, error) {
	hs, rec, err := p.handles(path, manager.FeatureFoldingRange)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	ranges, err := hs[0].Client().FoldingRanges(ctx, lsp.PathURI(path))
	if err != nil {
		return nil, err
	}
	return clampFoldRanges(ranges, len(rec.lines)-1), nil
}

// clampFoldRanges keeps the server's folding ranges that fit the document's lines
// (last is its last line, zero-based) and cuts the ones that run past it.
func clampFoldRanges(ranges []protocol.FoldingRange, last int) []tui.FoldRange {
	out := make([]tui.FoldRange, 0, len(ranges))
	for _, r := range ranges {
		start, end := int(r.StartLine), int(r.EndLine)
		if start < 0 || end < start || start > last || last < 0 {
			continue
		}
		out = append(out, tui.FoldRange{StartLine: start, EndLine: min(end, last)})
	}
	return out
}

// lspProgressCap is the most progress entries the editor shows at once, across all
// servers. A server reports at most eight of its own; the workspace list is capped
// again so the panel's header stays one line.
const lspProgressCap = 8

// Progress returns the live servers' unfinished progress entries, in server order
// and capped. The entries are cleared when the server ends them, and stopping or
// restarting the servers drops them with the clients.
func (p *lspPort) Progress() []tui.LSPProgress {
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()
	out := make([]tui.LSPProgress, 0, lspProgressCap)
	for _, mgr := range mgrs {
		for _, h := range mgr.AllHandles() {
			for _, entry := range h.Client().Progress() {
				out = append(out, tui.LSPProgress{
					Server: h.Name(), Title: entry.Title, Message: entry.Message, Percentage: entry.Percentage,
				})
				if len(out) == lspProgressCap {
					return out
				}
			}
		}
	}
	return out
}

// lspWorkspacePullEvery is how often the port re-asks the servers for the whole
// workspace's diagnostics. The workspace/diagnostic pull can be expensive, so it is
// throttled; a capability change or the set of open documents changing asks sooner.
// It is a variable so tests can shorten it.
var lspWorkspacePullEvery = 5 * time.Second

// workspaceDiagPerFileCap bounds the workspace-pulled diagnostics one file adds to
// the store. A file with more keeps its most severe first, so a noisy server cannot
// flood the panel or the fix list.
const workspaceDiagPerFileCap = 50

// pullSet tracks the cancellable workspace/diagnostic pulls in flight. A newer pull
// supersedes the ones already running (they are cancelled and their answers dropped), and
// stopping or restarting the servers cancels every pull, so a pull never outlives the
// server it asked. The zero value is ready to use.
type pullSet struct {
	mu      sync.Mutex
	next    uint64
	cancels map[uint64]context.CancelFunc
}

// begin cancels the pulls already running and registers cancel for a new pull. It returns
// the id end needs.
func (s *pullSet) begin(cancel context.CancelFunc) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancels == nil {
		s.cancels = make(map[uint64]context.CancelFunc)
	}
	for _, c := range s.cancels {
		c()
	}
	clear(s.cancels)
	s.next++
	s.cancels[s.next] = cancel
	return s.next
}

// end forgets a pull that returned. The pull was already cancelled by a newer one if its
// id is gone.
func (s *pullSet) end(id uint64) {
	s.mu.Lock()
	delete(s.cancels, id)
	s.mu.Unlock()
}

// cancelAll cancels every pull in flight and forgets them.
func (s *pullSet) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.cancels {
		c()
	}
	clear(s.cancels)
}

// workspacePulled returns the workspace/diagnostic answer, asking the capability
// servers when the cached one is stale. The result is keyed by file path. A server
// that does not advertise workspace diagnostics is not asked; a failed pull is
// cached as empty until the next interval or capability change, so the poll does
// not hammer it.
func (p *lspPort) workspacePulled() map[string][]protocol.Diagnostic {
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	docCount := len(p.docs)
	cache := p.wsPull
	p.mu.Unlock()

	rev := uint64(0)
	for _, mgr := range mgrs {
		for _, h := range mgr.AllHandles() {
			if r := h.Client().CapabilitiesRevision(); r > rev {
				rev = r
			}
		}
	}
	if cache.ok && time.Since(cache.at) < lspWorkspacePullEvery && cache.capsRev == rev && cache.docCount == docCount {
		return cache.files
	}

	files := make(map[string][]protocol.Diagnostic)
	canceled := false
	for _, mgr := range mgrs {
		for _, h := range mgr.AllHandles() {
			if !h.Capabilities().DiagnosticWorkspace {
				continue
			}
			// A pull registers here before it asks: a newer pull, or a stop/restart,
			// cancels it, the client sends the $/cancelRequest for its request id, and
			// the timeout below is the fallback for a server that never answers.
			parent, cancel := context.WithCancel(context.Background())
			ctx, cancelTimeout := context.WithTimeout(parent, lspRequestTimeout)
			id := p.wsPulls.begin(cancel)
			items, err := h.Client().WorkspaceDiagnostics(ctx, nil)
			p.wsPulls.end(id)
			cancelTimeout()
			cancel()
			if err != nil {
				if errors.Is(err, context.Canceled) {
					canceled = true
				}
				continue
			}
			for _, item := range items {
				if item.Unchanged || len(item.Items) == 0 {
					continue
				}
				file := uri.URI(item.URI).FsPath()
				files[file] = capWorkspaceDiagnostics(append(files[file], item.Items...))
			}
		}
	}
	if canceled {
		// A superseded or stopped pull answered nothing to merge: keep the last good
		// workspace answer (and its throttle state) so a cancellation never drops rows
		// or writes a stale set. The next poll asks again.
		p.mu.Lock()
		last := p.wsPull
		p.mu.Unlock()
		if last.ok {
			return last.files
		}
		return nil
	}

	p.mu.Lock()
	p.wsPull = workspacePull{files: files, at: time.Now(), capsRev: rev, docCount: docCount, ok: true}
	p.mu.Unlock()
	return files
}

// capWorkspaceDiagnostics sorts the workspace-pulled diagnostics of one file most
// severe first, removes exact duplicates and keeps the first
// workspaceDiagPerFileCap.
func capWorkspaceDiagnostics(items []protocol.Diagnostic) []protocol.Diagnostic {
	if len(items) == 0 {
		return nil
	}
	out := make([]protocol.Diagnostic, 0, len(items))
	for _, d := range items {
		duplicate := false
		for _, kept := range out {
			if kept.Range == d.Range && kept.Severity == d.Severity &&
				diagnosticText(kept.Message) == diagnosticText(d.Message) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b protocol.Diagnostic) int {
		if a.Severity != b.Severity {
			return int(a.Severity) - int(b.Severity)
		}
		if a.Range.Start.Line != b.Range.Start.Line {
			return int(a.Range.Start.Line) - int(b.Range.Start.Line)
		}
		return int(a.Range.Start.Character) - int(b.Range.Start.Character)
	})
	if len(out) > workspaceDiagPerFileCap {
		out = out[:workspaceDiagPerFileCap]
	}
	return out
}

// mergeWorkspaceDiagnostics merges the workspace-pulled files into the published
// sets. A file the servers published for keeps its published set: the published
// diagnostics are the live ones, and merging both would report the same problem
// twice. The second map is the source of each file's set.
func mergeWorkspaceDiagnostics(published, pulled map[string][]protocol.Diagnostic) (map[string][]protocol.Diagnostic, map[string]string) {
	merged := make(map[string][]protocol.Diagnostic, len(published)+len(pulled))
	source := make(map[string]string, len(merged))
	for file, items := range published {
		merged[file] = items
		source[file] = "publish"
	}
	for file, items := range pulled {
		if _, has := merged[file]; has {
			continue
		}
		merged[file] = items
		source[file] = "workspace"
	}
	return merged, source
}

// WorkspaceDiagnostics returns the stored diagnostics of every file a server
// reported on, open or not, sorted by path. It is the view the diagnostics panel
// lists: the files the servers published for, plus the files a workspace/diagnostic
// pull reported (the source says which). A file with both keeps its published set.
func (p *lspPort) WorkspaceDiagnostics() []tui.FileDiagnostics {
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()

	published := make(map[string][]protocol.Diagnostic)
	for _, mgr := range mgrs {
		for _, h := range mgr.AllHandles() {
			for docURI, ev := range h.Client().DiagnosticsAll() {
				file := uri.URI(docURI).FsPath()
				published[file] = append(published[file], ev.Diagnostics...)
			}
		}
	}
	byPath, source := mergeWorkspaceDiagnostics(published, p.workspacePulled())
	files := make([]string, 0, len(byPath))
	for file := range byPath {
		files = append(files, file)
	}
	slices.Sort(files)
	out := make([]tui.FileDiagnostics, 0, len(files))
	for _, file := range files {
		lines := newTextLines(p.textOf(file))
		out = append(out, tui.FileDiagnostics{
			Path:   file,
			Items:  uiDiagnostics(lines, byPath[file]),
			Source: source[file],
		})
	}
	return out
}

// pullDiagnostics asks the pull-capable servers for the document's diagnostics
// when none of them published any. A server that published is not pulled from, so
// a problem is never reported twice. ok is false when no server can pull.
func (p *lspPort) pullDiagnostics(path string, rec lspDocRec, mgr *manager.Manager) (tui.DiagnosticsEvent, bool) {
	p.mu.Lock()
	cached, haveCache := p.pulled[path]
	p.mu.Unlock()
	if haveCache && cached.version == rec.version {
		if len(cached.items) == 0 {
			return tui.DiagnosticsEvent{}, false
		}
		return tui.DiagnosticsEvent{Path: path, Items: uiDiagnostics(rec.lines, cached.items)}, true
	}

	docURI := lsp.PathURI(path)
	var items []protocol.Diagnostic
	pulled, failed := false, false
	for _, h := range mgr.Handles(rec.lang) {
		caps := h.Capabilities()
		if !caps.DiagnosticPull {
			continue
		}
		if _, published := h.Client().DiagnosticsFor(docURI); published {
			// This server publishes; its set is already in Diagnostics, so pulling
			// here would report the same problems twice.
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
		report, err := h.Client().PullDiagnostics(ctx, docURI, "")
		cancel()
		if err != nil {
			// A server still indexing answers ContentModified: ask again next time
			// rather than remember "no diagnostics" for this version.
			failed = true
			continue
		}
		if !report.Unchanged {
			items = append(items, report.Items...)
		}
		pulled = true
	}
	if !failed {
		p.mu.Lock()
		if p.pulled == nil {
			p.pulled = make(map[string]pulledDiagnostics)
		}
		p.pulled[path] = pulledDiagnostics{version: rec.version, items: slices.Clone(items)}
		p.mu.Unlock()
	}
	if !pulled {
		return tui.DiagnosticsEvent{}, false
	}
	return tui.DiagnosticsEvent{Path: path, Items: uiDiagnostics(rec.lines, items)}, true
}
