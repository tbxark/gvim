package daemon

import "testing"

// TestFrameTypesAreDistinct checks no two frame types share a byte: a client must never
// read one reply as another.
func TestFrameTypesAreDistinct(t *testing.T) {
	seen := map[byte]string{}
	for name, b := range map[string]byte{
		"Hello": TypeHello, "Input": TypeInput, "Resize": TypeResize, "Detach": TypeDetach,
		"OpenFile": TypeOpenFile, "Ping": TypePing, "VersionRejected": TypeVersionRejected,
		"Output": TypeOutput, "Notice": TypeNotice, "Detached": TypeDetached, "Pong": TypePong,
		"Exit": TypeExit, "WorkspaceList": TypeWorkspaceList, "WorkspaceSelect": TypeWorkspaceSelect,
		"WorkspaceNew": TypeWorkspaceNew, "WorkspaceClose": TypeWorkspaceClose,
		"WorkspaceRename": TypeWorkspaceRename, "Workspaces": TypeWorkspaces,
	} {
		if other, ok := seen[b]; ok {
			t.Errorf("%s and %s share frame type %d", name, other, b)
		}
		seen[b] = name
	}
}
