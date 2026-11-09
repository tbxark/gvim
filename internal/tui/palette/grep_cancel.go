package palette

import "context"

// GrepResult is one Live Grep result: the hits plus the adapter's non-fatal completeness
// signal. Notice is display-ready (a degradation/cap explanation); Truncated says the
// result is incomplete because a remote limit was hit. A plain Grepper has neither.
type GrepResult struct {
	Hits      []GrepHit
	Notice    string
	Truncated bool
}

// ContextGrepper is the optional cancellable Live Grep port. A Grepper that implements it
// receives a context that the palette cancels when the query or scopes are superseded, when
// the palette is dismissed, and when a row is opened; the remote workspace adapter uses it
// to stop the streaming authority search and close the remote command. It returns the
// non-fatal notice/truncation alongside the successful hits. The plain Grepper seam stays
// supported for local/test ports.
type ContextGrepper interface {
	SearchContext(ctx context.Context, req GrepRequest) (GrepResult, error)
}

// cancelGrepSearch cancels the in-flight contextual Live Grep search, if any.
func (p *Palette) cancelGrepSearch() {
	if p.grep.cancel != nil {
		p.grep.cancel()
		p.grep.cancel = nil
	}
}
