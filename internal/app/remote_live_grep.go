package app

// Remote Live Grep (the palette's Grepper over a remote workspace). It reuses the remote
// project-search adapter for the Files scope: the same authority one-shot, Fresh project-file
// candidate scope, NUL-delimited parsing, output cap and bounded/cancellable streaming. The
// Buffers and Terminals scopes stay the editor's in-memory snapshots, so they need no remote
// command and no local file read. The Ignored scope deliberately scans the remote tree with
// the tool's own widened flags through the same one-shot surface (no candidate list can name
// untracked ignored files); hidden/.gitignore matches are then part of the result. Root
// resolution, the workspace trust gate and reconnects are the authority's job; this adapter
// keeps no cache and never falls back to the local disk.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/search"
)

// remoteLiveGrep implements palette.Grepper for a remote workspace. Search is the
// non-cancellable palette path; SearchContext is the optional cancellable path the palette
// uses when it implements palette.ContextGrepper.
type remoteLiveGrep struct {
	search *remoteSearch
	// gate is the workspace trust gate; nil in tests that do not exercise trust. The
	// authority one-shot also enforces its spawn gate, so this is the early, explicit
	// refusal rather than the only protection.
	gate func(ctx context.Context, argv []string, cwd string) error
}

var _ palette.Grepper = (*remoteLiveGrep)(nil)

// newRemoteLiveGrep returns the remote Live Grep over search. gate, when not nil, runs
// before any remote command.
func newRemoteLiveGrep(search *remoteSearch, gate ...func(ctx context.Context, argv []string, cwd string) error) *remoteLiveGrep {
	g := &remoteLiveGrep{search: search}
	if len(gate) > 0 {
		g.gate = gate[0]
	}
	return g
}

// Search implements palette.Grepper. It runs the search with the background context; the
// remote project-search adapter still bounds every command (probe/list/tool) with its own
// deadlines.
func (g *remoteLiveGrep) Search(req palette.GrepRequest) ([]palette.GrepHit, error) {
	res, err := g.SearchContext(context.Background(), req)
	return res.Hits, err
}

// SearchContext is the cancellable Live Grep path: canceling ctx stops the streaming remote
// project search between records/commands and closes its batch channel. Nothing here reads
// the local file system.
func (g *remoteLiveGrep) SearchContext(ctx context.Context, req palette.GrepRequest) (palette.GrepResult, error) {
	var out palette.GrepResult
	if req.Text == "" || req.Limit <= 0 {
		return out, nil
	}
	q := search.Query{
		Text:          req.Text,
		CaseSensitive: req.Query.CaseSensitive,
		WholeWord:     req.Query.WholeWord,
		Regex:         req.Query.Regex,
	}
	if err := validateQuery(q); err != nil {
		return out, err
	}
	if req.Sources.Files || req.Sources.Ignored {
		if g.gate != nil {
			if err := g.gate(ctx, []string{"gvim-remote-live-grep", g.search.root}, g.search.root); err != nil {
				return out, err
			}
		}
	}
	if req.Sources.Ignored {
		var err error
		if out, err = g.searchIgnored(ctx, req, q, req.Limit); err != nil {
			return out, err
		}
	} else if req.Sources.Files {
		var err error
		if out, err = g.searchFiles(ctx, req, req.Limit); err != nil {
			return out, err
		}
	}
	if len(out.Hits) < req.Limit && req.Sources.Buffers {
		var err error
		if out.Hits, err = searchBuffers(req.Buffers, q, req.Limit, out.Hits); err != nil {
			return out, err
		}
	}
	if len(out.Hits) < req.Limit && req.Sources.Terminals {
		var err error
		if out.Hits, err = searchTerminals(req.Terminals, q, req.Limit, out.Hits); err != nil {
			return out, err
		}
	}
	return out, nil
}

// searchFiles runs the remote project search over the palette's file-index snapshot and maps
// its per-line matches to Live Grep hits. The project-search adapter owns the candidate
// scope, ignore rules, output cap and streaming; this function preserves its non-fatal
// Notice/Truncated signal while collecting at most the palette's row limit. It keeps draining
// the (authority-bounded) stream after the limit so a degradation/cap notice that arrives on
// the Done batch is not silently lost; cancellation still closes the stream.
func (g *remoteLiveGrep) searchFiles(ctx context.Context, req palette.GrepRequest, limit int) (palette.GrepResult, error) {
	sr := searchpanel.SearchRequest{
		Root:    req.Root,
		Query:   req.Text,
		Options: searchpanel.MatchOptions{CaseSensitive: req.Query.CaseSensitive, WholeWord: req.Query.WholeWord, Regex: req.Query.Regex},
		Scope:   searchpanel.AllFiles,
		Files:   req.Files,
		Limit:   limit,
	}
	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := palette.GrepResult{Hits: make([]palette.GrepHit, 0, limit)}
	for batch := range g.search.ProjectSearchBatches(sr, searchCtx.Done()) {
		if batch.Truncated {
			out.Truncated = true
		}
		if batch.Notice != "" {
			out.Notice = joinRemoteLiveGrepNotice(out.Notice, batch.Notice)
		}
		// Parse the batch's files BEFORE looking at its error: a tool exit>1 batch carries
		// the stdout matches and the error together (Done), and those successful hits must
		// be kept.
		for _, file := range batch.Files {
			for _, m := range file.Matches {
				if len(out.Hits) >= limit {
					continue
				}
				out.Hits = append(out.Hits, palette.GrepHit{
					Rel:    file.Name,
					Line:   m.Line,
					Col:    m.Col,
					Text:   m.Text,
					Source: palette.GrepFiles,
				})
			}
		}
		if batch.Err != "" {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			if len(out.Hits) > 0 {
				// Keep the successful hits; expose the failure as incomplete instead of
				// replacing the whole result with an error row.
				out.Truncated = true
				out.Notice = joinRemoteLiveGrepNotice(out.Notice, "remote Live Grep stopped early: "+batch.Err)
				return out, nil
			}
			return out, errors.New(batch.Err)
		}
		if batch.Done {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// searchIgnored uses the remote tool's widened scan for Live Grep's Ignored scope: no
// candidate list can name untracked ignored files, so the tool scans the whole remote tree
// with --no-ignore/--hidden (rg) through the same authority one-shot, output cap and parser.
// The result is filtered to safe, project-relative paths; .git content is never returned.
// A tool output-cap hit is carried as an incomplete result notice instead of being dropped.
func (g *remoteLiveGrep) searchIgnored(ctx context.Context, req palette.GrepRequest, q search.Query, limit int) (palette.GrepResult, error) {
	var out palette.GrepResult
	tool, err := g.search.probeTool(ctx)
	if err != nil {
		return out, err
	}
	runCtx, stop := context.WithTimeout(ctx, g.search.timeout)
	defer stop()
	sr := searchpanel.SearchRequest{
		Root:    req.Root,
		Query:   req.Text,
		Options: searchpanel.MatchOptions{CaseSensitive: req.Query.CaseSensitive, WholeWord: req.Query.WholeWord, Regex: req.Query.Regex},
		Scope:   searchpanel.AllFiles,
		Limit:   limit,
	}
	res, err := g.search.run(runCtx, g.search.root, remoteSearchArgv(tool, sr, true, nil))
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if parsed := ignoredLiveGrepHits(tool, res.Stdout, limit); len(parsed) > 0 {
			out.Hits = parsed
			out.Truncated = true
			out.Notice = appText("gvim.notice.remote_live_grep_stopped", "remote Live Grep stopped early: the widened ignored-file scan failed on the remote host; the shown matches are incomplete")
			return out, nil
		}
		return out, fmt.Errorf("remote Live Grep ignored scan failed: %w", err)
	}
	if res.Code > 1 {
		if parsed := ignoredLiveGrepHits(tool, res.Stdout, limit); len(parsed) > 0 {
			// Keep the matches the tool printed before its non-zero exit; the partial
			// result is explicitly incomplete rather than silently dropped.
			out.Hits = parsed
			out.Truncated = true
			out.Notice = appText("gvim.notice.remote_live_grep_stopped", "remote Live Grep stopped early: the widened ignored-file scan failed on the remote host; the shown matches are incomplete")
			return out, nil
		}
		return out, fmt.Errorf("%s failed on the remote host (exit %d)", tool, res.Code)
	}
	if res.Code == 1 {
		return out, nil
	}
	out.Hits = ignoredLiveGrepHits(tool, res.Stdout, limit)
	if res.Truncated {
		out.Truncated = true
		out.Notice = appText("gvim.notice.remote_live_grep_cap", "remote Live Grep result is incomplete: the widened ignored-file scan hit the remote output cap; narrow the scope or refine the query")
	}
	return out, nil
}

// ignoredLiveGrepHits parses the widened tool's output into at most limit project-relative
// hits, dropping .git/skip directories and escaping paths. It is shared by the success and
// partial-failure paths so a capped run keeps its successful hits.
func ignoredLiveGrepHits(tool string, stdout []byte, limit int) []palette.GrepHit {
	hits := make([]palette.GrepHit, 0, limit)
	for _, rec := range parseRemoteSearchOutput(tool, stdout) {
		if ignoredRemotePath(rec.rel) {
			continue
		}
		col := 1
		if rec.byteCol > 1 && rec.byteCol-1 <= len(rec.text) {
			col = utf8.RuneCountInString(rec.text[:rec.byteCol-1]) + 1
		}
		hits = append(hits, palette.GrepHit{
			Rel:    rec.rel,
			Line:   rec.line,
			Col:    col,
			Text:   rec.text,
			Source: palette.GrepFiles,
		})
		if len(hits) >= limit {
			break
		}
	}
	return hits
}

// joinRemoteLiveGrepNotice appends one adapter notice to another without duplicating it.
func joinRemoteLiveGrepNotice(a, b string) string {
	if a == "" || a == b {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

// ignoredRemotePath reports a path under .git or a tool-hardcoded skip directory. The
// widened rg argv already excludes .git; this is the defensive filter for grep/older tools.
func ignoredRemotePath(rel string) bool {
	for _, segment := range strings.Split(rel, "/") {
		if segment == ".git" {
			return true
		}
		for _, skip := range remoteGrepSkipDirs {
			if segment == skip {
				return true
			}
		}
	}
	return false
}
