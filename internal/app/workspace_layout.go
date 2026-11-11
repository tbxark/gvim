package app

import (
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/session"
)

// restoreRegistryLayout reopens the files of a daemon workspace that has no session store
// because editor.save_session is off. The registry layout is workspace metadata (the tabs
// the workspace had), not the project session file: without reopening it a switch to a
// dormant workspace would land on an empty editor and look like lost work. The unsaved
// stash text is not restored, because the store that holds it is disabled; the files
// themselves still open at their saved cursors. A remote workspace is left alone: its
// paths are not local, and its files only open through the authority.
func restoreRegistryLayout(rt *runtime, ws daemon.Workspace) {
	if rt == nil || rt.ed == nil || rt.sessionDir != "" || ws.File.AuthoritySpec != "" {
		return
	}
	layout := ws.File.Layout
	if layout == nil || len(layout.Files) == 0 {
		return
	}
	files := make([]session.FileState, 0, len(layout.Files))
	active := -1
	for i, f := range layout.Files {
		if f.Path == "" {
			// An untitled buffer or shared-conflict copy keeps its text in the session
			// stash, which this workspace does not have.
			continue
		}
		if i == layout.Active {
			active = len(files)
		}
		files = append(files, session.FileState{
			Path: f.Path, Line: f.Line, Col: f.Col, Top: f.Top,
			PageView: f.PageView, PageWidth: f.PageWidth,
		})
	}
	if len(files) == 0 {
		return
	}
	restore.RestoreSession(rt.ed, "", session.Session{Version: session.Version, Files: files, Active: active})
}
