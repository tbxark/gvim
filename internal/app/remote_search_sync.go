package app

// Remote project-search scopes and preview column (task-139). The current-file and
// open-buffers scopes search the editor's remote document snapshots: the authority loaded
// that text and unsaved edits are included, so no local disk is read and no remote process
// is started. The preview column reads a bounded head of a remote file through
// authority.FileSystem.ReadRange (a non-open-buffer file); an open buffer's text comes from
// the panel request, exactly as locally. Everything here runs in a tea.Cmd, never on the
// Update loop.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/search"
)

const (
	// remoteSearchPreviewHeadBytes is the remote preview's bounded display/line cap. It
	// used to bound a head read; the window scan now uses it as the output cap so a long
	// context line or a dense window cannot grow without a bound (and it still matches the
	// local preview's syntax-colour byte cap).
	remoteSearchPreviewHeadBytes = 256 << 10
	// remoteSearchPreviewTimeout bounds one preview read.
	remoteSearchPreviewTimeout = 10 * time.Second
)

// searchSnapshots runs the current-file/open-buffers scopes over the editor snapshots in
// req, using the same match caps, batching and query semantics as the local search. It
// never runs a remote command and never reads the local disk.
func (s *remoteSearch) searchSnapshots(req searchpanel.SearchRequest, q search.Query, limit int, cancelled func() bool, send func(searchpanel.SearchBatch) bool) {
	var sources []searchpanel.SourceFile
	switch req.Scope {
	case searchpanel.CurrentFile:
		if req.Current == nil {
			send(searchpanel.SearchBatch{Done: true})
			return
		}
		sources = []searchpanel.SourceFile{*req.Current}
	case searchpanel.OpenBuffers:
		sources = dedupeSources(req.OpenFiles)
	}
	total := len(sources)
	if total == 0 {
		send(searchpanel.SearchBatch{Done: true})
		return
	}
	var pending []searchpanel.FileMatches
	pendingMatches, totalMatches, scanned := 0, 0, 0
	truncated := false
	flush := func(done bool, errText string) bool {
		batch := searchpanel.SearchBatch{
			Files: pending, Scanned: scanned, Total: total,
			Truncated: truncated, Done: done, Err: errText,
		}
		if !send(batch) {
			return false
		}
		pending, pendingMatches = nil, 0
		return true
	}
	for _, src := range sources {
		if cancelled() {
			return
		}
		name := src.Name
		if name == "" {
			name = "[No Name]"
		}
		b := buffer.New(src.Text)
		matches, err := search.Find(b, q)
		if err != nil {
			flush(true, err.Error())
			return
		}
		scanned++
		if len(matches) > 0 {
			fm := searchpanel.FileMatches{
				Path: src.Path, Name: name, Hash: hashText(src.Text), Dirty: true,
			}
			for _, m := range matches {
				fm.Matches = append(fm.Matches, searchpanel.Match{
					Line:    m.Start.Line + 1,
					Col:     m.Start.Col + 1,
					Len:     matchRunes(b, m),
					Text:    strings.TrimRight(b.Line(m.Start.Line), "\r"),
					EndLine: m.End.Line + 1,
					EndCol:  m.End.Col + 1,
				})
				if totalMatches+len(fm.Matches) >= limit {
					truncated = true
					break
				}
			}
			totalMatches += len(fm.Matches)
			pending = append(pending, fm)
			pendingMatches += len(fm.Matches)
		}
		if totalMatches >= limit {
			truncated = true
			flush(true, "")
			return
		}
		if len(pending) >= searchBatchFiles || pendingMatches >= searchBatchMatches {
			if !flush(false, "") {
				return
			}
		}
	}
	flush(true, "")
}

// Preview implements searchpanel.Previewer for the remote project-search panel. An open
// buffer's text (req.Text) is used as is; otherwise the file's head is read through the
// authority's bounded windowed read — never the local disk. A head shorter than the whole
// file sets Truncated and keeps complete lines only, so the panel can say so honestly.
func (s *remoteSearch) Preview(req searchpanel.PreviewRequest) (searchpanel.PreviewResult, error) {
	return s.PreviewContext(context.Background(), req)
}

// PreviewContext is the cancellable remote preview seam: the caller's context (cancelled
// when the panel's selection changes or the panel closes) bounds the authority ReadRange
// together with the fixed preview timeout. No local disk is ever consulted.
// PreviewWindow implements searchpanel.WindowPreviewer: a closed remote file is read
// around the selected match through authority ReadRange chunks (bounded scan) instead of
// only at its head. The open-buffer snapshot path is unchanged: it is already in memory
// and is used without any authority read.
func (s *remoteSearch) PreviewWindow(req searchpanel.PreviewRequest) (searchpanel.PreviewResult, error) {
	return s.PreviewWindowContext(context.Background(), req)
}

// PreviewContext keeps the context-capable seam; it now reads the same window.
func (s *remoteSearch) PreviewContext(ctx context.Context, req searchpanel.PreviewRequest) (searchpanel.PreviewResult, error) {
	return s.PreviewWindowContext(ctx, req)
}

// PreviewWindowContext is the cancellable window preview: selection changes/dismissal
// stop the authority scan. The scan has a fixed byte/scan budget; a hit beyond it is
// reported as a partial/window preview (or an explicit error when no line was reached),
// never by silently falling back to the file head or the local disk.
func (s *remoteSearch) PreviewWindowContext(ctx context.Context, req searchpanel.PreviewRequest) (searchpanel.PreviewResult, error) {
	var out searchpanel.PreviewResult
	if req.Lines <= 0 {
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if req.Text != "" {
		// The panel already holds the open buffer's text; window it locally with the
		// same centering/scroll rules as before, with full-text syntax spans.
		return previewStoredText(req, &s.previewCache), nil
	}
	if s.files == nil {
		out.Err = "remote preview is not available"
		return out, nil
	}
	first := req.Start + 1
	if req.Start < 0 {
		start := req.Line - 1
		end := req.EndLine - 1
		if end < start {
			end = start
		}
		span := end - start + 1
		first = start + 1
		if span < req.Lines {
			first -= (req.Lines - span) / 2
		}
		if first < 1 {
			first = 1
		}
	}
	readCtx, cancel := context.WithTimeout(ctx, remoteSearchPreviewTimeout)
	win, err := readRemotePreviewWindow(readCtx, s.files, req.Path, first, req.Lines)
	cancelled := readCtx.Err()
	cancel()
	if err != nil {
		if cancelled != nil {
			return out, cancelled
		}
		if errors.Is(err, errRemotePreviewScanBudget) {
			out.Window = true
			out.Truncated = true
			if len(win.lines) == 0 {
				out.Err = "preview window is past the remote scan budget"
				return out, nil
			}
			out.Start = win.first - 1
			out.Lines = append(out.Lines, win.lines...)
			return out, nil
		}
		out.Err = err.Error()
		return out, nil
	}
	out.Window = true
	out.Start = win.first - 1
	out.Lines = append(out.Lines, win.lines...)
	out.Total = win.total
	out.Truncated = win.truncated
	// Syntax colour is CPU-only but bounded per file. A window whose total line count is
	// unknown cannot prove it is under the highlighter's line cap, so it stays plain
	// (the panel still shows the text); a known-small file keeps the shared spans.
	if out.Total > 0 && out.Total <= previewHighlightLines && len(out.Lines) > 0 {
		text := strings.Join(out.Lines, "\n") + "\n"
		spans := s.previewCache.spans(req.Path, text, out.Lines, 0, len(out.Lines))
		for i := range spans {
			spans[i].Line += out.Start
		}
		out.Spans = spans
	}
	return out, nil
}

// previewStoredText windows text the panel already holds (an open buffer) around the
// request's match, with the same centering and scroll rules as the closed-file path.
func previewStoredText(req searchpanel.PreviewRequest, cache *previewHighlighterCache) searchpanel.PreviewResult {
	var out searchpanel.PreviewResult
	lines := strings.Split(req.Text, "\n")
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	total := len(lines)
	out.Total = total
	start := clampInt(req.Line-1, 0, max(total-1, 0))
	end := req.EndLine - 1
	if end < start {
		end = start
	}
	first := 0
	if req.Start >= 0 {
		first = clampInt(req.Start, 0, max(total-req.Lines, 0))
	} else {
		span := end - start + 1
		first = start
		if span < req.Lines {
			first = start - (req.Lines-span)/2
		}
		first = clampInt(first, 0, max(total-req.Lines, 0))
	}
	out.Start = first
	for k := first; k < total && k < first+req.Lines; k++ {
		out.Lines = append(out.Lines, strings.TrimRight(lines[k], "\r"))
	}
	out.Spans = cache.spans(req.Path, req.Text, lines, first, len(out.Lines))
	return out
}
