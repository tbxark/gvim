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
	return g.SearchContext(context.Background(), req)
}

// SearchContext is the cancellable Live Grep path: canceling ctx stops the streaming remote
// project search between records/commands and closes its batch channel. Nothing here reads
// the local file system.
func (g *remoteLiveGrep) SearchContext(ctx context.Context, req palette.GrepRequest) ([]palette.GrepHit, error) {
	if req.Text == "" || req.Limit <= 0 {
		return nil, nil
	}
	q := search.Query{
		Text:          req.Text,
		CaseSensitive: req.Query.CaseSensitive,
		WholeWord:     req.Query.WholeWord,
		Regex:         req.Query.Regex,
	}
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	if req.Sources.Files || req.Sources.Ignored {
		if g.gate != nil {
			if err := g.gate(ctx, []string{"gvim-remote-live-grep", g.search.root}, g.search.root); err != nil {
				return nil, err
			}
		}
	}
	var hits []palette.GrepHit
	if req.Sources.Ignored {
		var err error
		if hits, err = g.searchIgnored(ctx, req, q, req.Limit); err != nil {
			return nil, err
		}
	} else if req.Sources.Files {
		var err error
		if hits, err = g.searchFiles(ctx, req, req.Limit); err != nil {
			return nil, err
		}
	}
	if len(hits) < req.Limit && req.Sources.Buffers {
		var err error
		if hits, err = searchBuffers(req.Buffers, q, req.Limit, hits); err != nil {
			return nil, err
		}
	}
	if len(hits) < req.Limit && req.Sources.Terminals {
		var err error
		if hits, err = searchTerminals(req.Terminals, q, req.Limit, hits); err != nil {
			return nil, err
		}
	}
	return hits, nil
}

// searchFiles runs the remote project search over the palette's file-index snapshot and maps
// its per-line matches to Live Grep hits. The project-search adapter owns the candidate
// scope, ignore rules, output cap and streaming; this function only converts the batches and
// stops as soon as the palette's row limit is reached (canceling the remaining stream).
func (g *remoteLiveGrep) searchFiles(ctx context.Context, req palette.GrepRequest, limit int) ([]palette.GrepHit, error) {
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
	hits := make([]palette.GrepHit, 0, limit)
	for batch := range g.search.ProjectSearchBatches(sr, searchCtx.Done()) {
		if batch.Err != "" {
			return nil, errors.New(batch.Err)
		}
		for _, file := range batch.Files {
			for _, m := range file.Matches {
				hits = append(hits, palette.GrepHit{
					Rel:    file.Name,
					Line:   m.Line,
					Col:    m.Col,
					Text:   m.Text,
					Source: palette.GrepFiles,
				})
				if len(hits) >= limit {
					return hits, nil
				}
			}
		}
		if batch.Done {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// searchIgnored uses the remote tool's widened scan for Live Grep's Ignored scope: no
// candidate list can name untracked ignored files, so the tool scans the whole remote tree
// with --no-ignore/--hidden (rg) through the same authority one-shot, output cap and parser.
// The result is filtered to safe, project-relative paths; .git content is never returned.
func (g *remoteLiveGrep) searchIgnored(ctx context.Context, req palette.GrepRequest, q search.Query, limit int) ([]palette.GrepHit, error) {
	tool, err := g.search.probeTool(ctx)
	if err != nil {
		return nil, err
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
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("remote Live Grep ignored scan failed: %w", err)
	}
	if res.Code > 1 {
		return nil, fmt.Errorf("%s failed on the remote host (exit %d)", tool, res.Code)
	}
	if res.Code == 1 {
		return nil, nil
	}
	hits := make([]palette.GrepHit, 0, limit)
	for _, rec := range parseRemoteSearchOutput(tool, res.Stdout) {
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
	return hits, nil
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
