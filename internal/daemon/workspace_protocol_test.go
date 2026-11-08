package daemon

import (
	"bytes"
	"testing"
)

// TestWorkspaceWireRoundTrip checks the workspace request frames survive the wire and
// decode into the same request; malformed payloads are errors, never panics.
func TestWorkspaceWireRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		typ   byte
		value any
		check func(t *testing.T, req WorkspaceRequest)
	}{
		{"list", TypeWorkspaceList, nil, func(t *testing.T, req WorkspaceRequest) {
			if req.List == nil {
				t.Fatal("no list request")
			}
		}},
		{"select", TypeWorkspaceSelect, WorkspaceSelect{ID: "abc12345"}, func(t *testing.T, req WorkspaceRequest) {
			if req.Select == nil || req.Select.ID != "abc12345" {
				t.Fatalf("select = %+v", req.Select)
			}
		}},
		{"new", TypeWorkspaceNew, WorkspaceNew{Root: "/tmp/a", Label: "a"}, func(t *testing.T, req WorkspaceRequest) {
			if req.New == nil || req.New.Root != "/tmp/a" || req.New.Label != "a" {
				t.Fatalf("new = %+v", req.New)
			}
		}},
		{"close", TypeWorkspaceClose, WorkspaceClose{ID: "abc12345"}, func(t *testing.T, req WorkspaceRequest) {
			if req.Close == nil || req.Close.ID != "abc12345" {
				t.Fatalf("close = %+v", req.Close)
			}
		}},
		{"rename", TypeWorkspaceRename, WorkspaceRename{ID: "abc12345", Label: "renamed"}, func(t *testing.T, req WorkspaceRequest) {
			if req.Rename == nil || req.Rename.Label != "renamed" {
				t.Fatalf("rename = %+v", req.Rename)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			fw := NewFrameWriter(&buf)
			if tt.value == nil {
				if err := fw.WriteFrame(tt.typ, nil); err != nil {
					t.Fatal(err)
				}
			} else if err := fw.WriteJSON(tt.typ, tt.value); err != nil {
				t.Fatal(err)
			}
			f, err := ReadFrame(&buf)
			if err != nil {
				t.Fatal(err)
			}
			req, ok, err := decodeWorkspaceRequest(f)
			if err != nil || !ok {
				t.Fatalf("decode = %+v, %v, %v", req, ok, err)
			}
			tt.check(t, req)
		})
	}
}

// TestWorkspaceRequestValidation checks the empty-field rejections are typed errors.
func TestWorkspaceRequestValidation(t *testing.T) {
	bad := []Frame{
		{Type: TypeWorkspaceSelect, Payload: []byte("{}")},
		{Type: TypeWorkspaceNew, Payload: []byte("{}")},
		{Type: TypeWorkspaceClose, Payload: []byte("{}")},
		{Type: TypeWorkspaceRename, Payload: []byte("{}")},
		{Type: TypeWorkspaceRename, Payload: []byte("{\"id\":\"abc12345\",\"label\":\" \"}")},
	}
	for _, f := range bad {
		if _, ok, err := decodeWorkspaceRequest(f); !ok || err == nil {
			t.Errorf("decode(%d, %q) = ok %v, err %v; want a validation error", f.Type, f.Payload, ok, err)
		}
	}
	if err := (WorkspaceSelect{ID: "abc"}).Validate(); err != nil {
		t.Errorf("valid select: %v", err)
	}
}

// TestWorkspaceListFrame checks the server answer shape.
func TestWorkspaceListFrame(t *testing.T) {
	var buf bytes.Buffer
	fw := NewFrameWriter(&buf)
	want := WorkspaceList{Active: "a", Entries: []WorkspaceInfo{{ID: "a", Label: "one", Root: "/x", Materialized: true, Active: true}, {ID: "b", Label: "two", Root: "/y"}}}
	if err := fw.WriteJSON(TypeWorkspaces, want); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(&buf)
	if err != nil || f.Type != TypeWorkspaces {
		t.Fatalf("frame = %#v, %v", f, err)
	}
	var got WorkspaceList
	if err := DecodeJSON(f, &got); err != nil {
		t.Fatal(err)
	}
	if got.Active != want.Active || len(got.Entries) != 2 || got.Entries[0].Label != "one" || !got.Entries[0].Materialized || got.Entries[1].Root != "/y" {
		t.Fatalf("list = %+v", got)
	}
}
