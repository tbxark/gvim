package tui

import "errors"

var errWorkspaceRootEmpty = errors.New("workspace: the root path is empty")

// The workspace prompt tags; the answer dispatches by them (see workspace_ui.go).
type (
	workspaceNewRootTag  struct{}
	workspaceNewLabelTag struct{ root string }
	workspaceRenameTag   struct{ id string }
)
