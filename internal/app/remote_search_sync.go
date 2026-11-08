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
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/search"
)

const (
	// remoteSearchPreviewHeadBytes bounds the remote preview's read: the whole file when it is
	// smaller, otherwise an honest head with Truncated set. It matches the local preview's
	// syntax-colour cap.
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
	var out searchpanel.PreviewResult
	if req.Lines <= 0 {
		return out, nil
	}
	text := req.Text
	if text == "" {
		if s.files == nil {
			out.Err = "remote preview is not available"
			return out, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), remoteSearchPreviewTimeout)
		data, err := s.files.ReadRange(ctx, req.Path, 0, remoteSearchPreviewHeadBytes+1)
		cancel()
		if err != nil && len(data) == 0 && !errors.Is(err, io.EOF) {
			out.Err = err.Error()
			return out, nil
		}
		if len(data) > remoteSearchPreviewHeadBytes {
			data = data[:remoteSearchPreviewHeadBytes]
			out.Truncated = true
			if i := strings.LastIndexByte(string(data), '\n'); i >= 0 {
				data = data[:i+1]
			}
		} else if errors.Is(err, io.EOF) || err != nil {
			// A short window: the read reached the end (or the fake/port's short-read
			// shape); this is the whole file.
			out.Truncated = false
		}
		if !utf8.Valid(data) {
			out.Err = "preview: the remote file is not UTF-8 text"
			return out, nil
		}
		text = string(data)
	}
	lines := strings.Split(text, "\n")
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
	return out, nil
}
