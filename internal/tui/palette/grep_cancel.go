package palette

import "context"

// ContextGrepper is the optional cancellable Live Grep port. A Grepper that implements it
// receives a context that the palette cancels when the query or scopes are superseded, when
// the palette is dismissed, and when a row is opened; the remote workspace adapter uses it
// to stop the streaming authority search and close the remote command. The plain Grepper
// seam stays supported for local/test ports.
type ContextGrepper interface {
	SearchContext(ctx context.Context, req GrepRequest) ([]GrepHit, error)
}

// cancelGrepSearch cancels the in-flight contextual Live Grep search, if any.
func (p *Palette) cancelGrepSearch() {
	if p.grep.cancel != nil {
		p.grep.cancel()
		p.grep.cancel = nil
	}
}
