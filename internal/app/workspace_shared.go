package app

import "github.com/tbxark/gvim/editor"

// sharedBuffersFor returns the daemon's cross-workspace buffer registry, or nil for a
// direct or remote launch. A daemon's manager creates it once (see newWorkspaceManager)
// and every workspace build copies the same pointer into its editor options; remote
// workspaces keep independent buffers because their files live on another host.
func sharedBuffersFor(opts Options) *editor.SharedBuffers {
	if opts.Daemon == nil || opts.Remote != nil {
		return nil
	}
	return opts.sharedBuffers
}
