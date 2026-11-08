package daemon

// The workspace protocol of the daemon (#19a): the client asks for the workspace list or
// asks to select/new/close/rename one, and the server answers with a workspace list
// frame or a notice. The wire types are plain JSON payloads in the existing frames; the
// connection keeps working after an unknown or invalid workspace request (a notice is
// written, never a disconnect).

import (
	"errors"
	"strings"
)

// Workspace frame type bytes. They extend the client/server ranges of protocol.go.
const (
	TypeWorkspaceList   byte = 7
	TypeWorkspaceSelect byte = 8
	TypeWorkspaceNew    byte = 9
	TypeWorkspaceClose  byte = 10
	TypeWorkspaceRename byte = 11

	TypeWorkspaces byte = 106
)

// WorkspaceInfo is one workspace as the client sees it.
type WorkspaceInfo struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Root         string `json:"root"`
	Materialized bool   `json:"materialized"`
	Active       bool   `json:"active"`
}

// WorkspaceList is the server's answer to a workspace_list request (and the payload of a
// workspace_select/new/close/rename answer): the current list and the active id.
type WorkspaceList struct {
	Active  string          `json:"active,omitempty"`
	Entries []WorkspaceInfo `json:"entries"`
}

// WorkspaceSelect asks to switch to a workspace.
type WorkspaceSelect struct {
	ID string `json:"id"`
}

// WorkspaceNew asks to create a workspace for root and optionally select it.
type WorkspaceNew struct {
	Root  string `json:"root"`
	Label string `json:"label,omitempty"`
}

// WorkspaceClose asks to close a workspace (dormant after the switch to another one).
type WorkspaceClose struct {
	ID string `json:"id"`
}

// WorkspaceRename asks to rename a workspace.
type WorkspaceRename struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// WorkspaceRequest is one decoded workspace request for the server hook.
type WorkspaceRequest struct {
	Type   byte
	List   *struct{}
	Select *WorkspaceSelect
	New    *WorkspaceNew
	Close  *WorkspaceClose
	Rename *WorkspaceRename
}

// WorkspaceResult is what the server hook answers. A non-empty Notice is shown to the
// client; a non-nil List is sent as a workspaces frame.
type WorkspaceResult struct {
	List   *WorkspaceList
	Notice string
}

// Validate checks a select request.
func (r WorkspaceSelect) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("workspace select: empty id")
	}
	return nil
}

// Validate checks a new-workspace request.
func (r WorkspaceNew) Validate() error {
	if strings.TrimSpace(r.Root) == "" {
		return errors.New("workspace new: empty root")
	}
	return nil
}

// Validate checks a close request.
func (r WorkspaceClose) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("workspace close: empty id")
	}
	return nil
}

// Validate checks a rename request.
func (r WorkspaceRename) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("workspace rename: empty id")
	}
	if strings.TrimSpace(r.Label) == "" {
		return errors.New("workspace rename: empty label")
	}
	return nil
}

// decodeWorkspaceRequest decodes one workspace frame into a request. ok is false for a
// non-workspace frame; err is a malformed payload (the connection stays open).
func decodeWorkspaceRequest(f Frame) (WorkspaceRequest, bool, error) {
	switch f.Type {
	case TypeWorkspaceList:
		if len(f.Payload) != 0 {
			return WorkspaceRequest{}, true, errors.New("workspace list: unexpected payload")
		}
		return WorkspaceRequest{Type: f.Type, List: &struct{}{}}, true, nil
	case TypeWorkspaceSelect:
		var req WorkspaceSelect
		if err := DecodeJSON(f, &req); err != nil {
			return WorkspaceRequest{}, true, err
		}
		if err := req.Validate(); err != nil {
			return WorkspaceRequest{}, true, err
		}
		return WorkspaceRequest{Type: f.Type, Select: &req}, true, nil
	case TypeWorkspaceNew:
		var req WorkspaceNew
		if err := DecodeJSON(f, &req); err != nil {
			return WorkspaceRequest{}, true, err
		}
		if err := req.Validate(); err != nil {
			return WorkspaceRequest{}, true, err
		}
		return WorkspaceRequest{Type: f.Type, New: &req}, true, nil
	case TypeWorkspaceClose:
		var req WorkspaceClose
		if err := DecodeJSON(f, &req); err != nil {
			return WorkspaceRequest{}, true, err
		}
		if err := req.Validate(); err != nil {
			return WorkspaceRequest{}, true, err
		}
		return WorkspaceRequest{Type: f.Type, Close: &req}, true, nil
	case TypeWorkspaceRename:
		var req WorkspaceRename
		if err := DecodeJSON(f, &req); err != nil {
			return WorkspaceRequest{}, true, err
		}
		if err := req.Validate(); err != nil {
			return WorkspaceRequest{}, true, err
		}
		return WorkspaceRequest{Type: f.Type, Rename: &req}, true, nil
	}
	return WorkspaceRequest{}, false, nil
}
