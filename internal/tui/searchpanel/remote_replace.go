package searchpanel

import "context"

// RemoteReplacer is a Replacer whose files live on another host: the adapter reads and
// writes them through the workspace authority itself (one atomic write per file) and
// reports how many it changed. The root calls ProjectReplaceRemote from a tea.Cmd after
// the confirmation, with a cancellable context, and applies NO local edits for it.
type RemoteReplacer interface {
	Replacer
	// ProjectReplaceRemote applies the replacement on the backend. Files whose text
	// changed since the search (or while the run was reading) are skipped and counted,
	// never written; a read/write failure aborts with the files already written counted
	// in the result (there is no cross-file rollback).
	ProjectReplaceRemote(ctx context.Context, req ReplaceRequest) (RemoteReplaceResult, error)
	// RemoteTarget names the backend for the confirmation, for example
	// "SSH:user@host:/srv/proj".
	RemoteTarget() string
}

// RemoteReplaceResult is what a RemoteReplacer wrote and skipped.
type RemoteReplaceResult struct {
	Files   int // files atomically written
	Skipped int // files left alone (stale, changed, binary, too large, open buffer)
}
