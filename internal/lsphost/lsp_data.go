package lsphost

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

// pulledDiagnostics is the last server-pulled diagnostics of one document, kept so
// the 300ms diagnostics poll does not pull again while the text has not changed.
type pulledDiagnostics struct {
	version int32
	items   []protocol.Diagnostic
}

// InlayHints returns the inlay hints of the document at path. Round 5 draws them at
// the end of their line (tier 1); the hint's character position is not used.
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
	out := make([]tui.InlayHint, 0, len(hints))
	for _, hint := range hints {
		text := strings.TrimSpace(hintText(hint.Label))
		if text == "" {
			continue
		}
		out = append(out, tui.InlayHint{Line: int(hint.Position.Line), Text: text, Kind: int(hint.Kind)})
	}
	return out, nil
}

// hintText renders an inlay hint label, which is a string or a list of parts.
func hintText(label protocol.InlayHintLabel) string {
	switch v := label.(type) {
	case protocol.String:
		return string(v)
	case protocol.InlayHintLabelPartSlice:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			parts = append(parts, part.Value)
		}
		return strings.Join(parts, "")
	}
	return ""
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
		out = append(out[:start], out[start+del:]...)
		if len(edit.Data) > 0 {
			rest := slices.Clone(out[start:])
			out = append(out[:start], edit.Data...)
			out = append(out, rest...)
		}
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

// WorkspaceDiagnostics returns the stored diagnostics of every file a server
// published them for, open or not, sorted by path. It is the view the diagnostics
// panel lists.
func (p *lspPort) WorkspaceDiagnostics() []tui.FileDiagnostics {
	p.mu.Lock()
	mgrs := make([]*manager.Manager, 0, len(p.managers))
	for _, mgr := range p.managers {
		mgrs = append(mgrs, mgr)
	}
	p.mu.Unlock()

	byPath := make(map[string][]protocol.Diagnostic)
	for _, mgr := range mgrs {
		for _, h := range mgr.AllHandles() {
			for docURI, ev := range h.Client().DiagnosticsAll() {
				file := uri.URI(docURI).FsPath()
				byPath[file] = append(byPath[file], ev.Diagnostics...)
			}
		}
	}
	files := make([]string, 0, len(byPath))
	for file := range byPath {
		files = append(files, file)
	}
	slices.Sort(files)
	out := make([]tui.FileDiagnostics, 0, len(files))
	for _, file := range files {
		lines := newTextLines(p.textOf(file))
		out = append(out, tui.FileDiagnostics{Path: file, Items: uiDiagnostics(lines, byPath[file])})
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
	pulled := false
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
			continue
		}
		if !report.Unchanged {
			items = append(items, report.Items...)
		}
		pulled = true
	}
	p.mu.Lock()
	if p.pulled == nil {
		p.pulled = make(map[string]pulledDiagnostics)
	}
	p.pulled[path] = pulledDiagnostics{version: rec.version, items: slices.Clone(items)}
	p.mu.Unlock()
	if !pulled {
		return tui.DiagnosticsEvent{}, false
	}
	return tui.DiagnosticsEvent{Path: path, Items: uiDiagnostics(rec.lines, items)}, true
}
