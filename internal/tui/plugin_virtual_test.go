package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// virtualFakeEditor adds CreateBuffer to the fake plugin editor: a new virtual tab at the end.
type virtualFakeEditor struct {
	*fakePluginEditor
	next    pluginapi.BufferID
	failure error
	made    []pluginapi.CreateBuffer
}

func (v *virtualFakeEditor) CreateBuffer(o pluginapi.CreateBuffer) (pluginapi.BufferID, error) {
	if v.failure != nil {
		return 0, v.failure
	}
	v.made = append(v.made, o)
	v.next++
	id := 100 + v.next
	name := "*" + o.Name + "*"
	v.bufs = append(v.bufs, pluginapi.BufferInfo{ID: id, Name: name, Virtual: true})
	v.ed.tabs = append(v.ed.tabs, TabInfo{Name: name})
	return id, nil
}

// newVirtualModel is a model over the fake plugin editor of three file buffers, with
// CreateBuffer.
func newVirtualModel(t *testing.T) (*Model, *virtualFakeEditor) {
	t.Helper()
	ed := newFakeEditor("hello", "world")
	ed.quitAction = "quit"
	ed.tabs = []TabInfo{{Name: "a.txt", Path: "/w/a.txt", Active: true}, {Name: "b.txt", Path: "/w/b.txt"}}
	pe := &virtualFakeEditor{fakePluginEditor: &fakePluginEditor{ed: ed, active: 1, bufs: []pluginapi.BufferInfo{
		{ID: 1, Path: "/w/a.txt", Name: "a.txt", Active: true},
		{ID: 2, Path: "/w/b.txt", Name: "b.txt"},
	}}}
	rt := newFakeRuntime(t)
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	return m, pe
}

func TestOwnerGoneClosesTheVirtualBuffersOfThePlugin(t *testing.T) {
	m, pe := newVirtualModel(t)
	host := m.plug.host
	a1, err := host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "alpha", Name: "one"})
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "alpha", Name: "two"})
	b1, _ := host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "beta", Name: "three"})
	if n := len(pe.bufs); n != 5 {
		t.Fatalf("%d buffers, want 5", n)
	}

	host.OwnerGone("alpha", 1)
	if len(pe.closed) != 2 || pe.closed[0]+pe.closed[1] != a1+a2 {
		t.Fatalf("closed = %v, want alpha's buffers %d and %d", pe.closed, a1, a2)
	}
	if _, ok := pe.Buffer(b1); !ok {
		t.Fatal("beta's buffer was closed by alpha's unload")
	}
	if owners := m.plug.virtual.owner; len(owners) != 1 || owners[b1] != "beta" {
		t.Fatalf("owners = %v, want only beta's buffer", owners)
	}
	// the file buffers are untouched, and the tab bookkeeping followed the closes
	if n := len(pe.ed.tabs); n != 3 {
		t.Fatalf("%d tabs, want the 2 files and beta's buffer", n)
	}

	host.OwnerGone("alpha", 2) // idempotent
	host.OwnerGone("beta", 1)
	if len(pe.closed) != 3 || len(m.plug.virtual.owner) != 0 {
		t.Fatalf("closed = %v owners = %v after both unloads", pe.closed, m.plug.virtual.owner)
	}
}

// A buffer the user closed is forgotten the next time a plugin makes one, and a plugin
// that goes later does not close an id that is gone.
func TestVirtualOwnersForgetClosedBuffers(t *testing.T) {
	m, pe := newVirtualModel(t)
	host := m.plug.host
	first, _ := host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "alpha", Name: "one"})
	if err := pe.Close(first, true); err != nil { // the user's close, not the host's
		t.Fatal(err)
	}
	second, _ := host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "alpha", Name: "two"})
	if _, still := m.plug.virtual.owner[first]; still {
		t.Fatal("the owner of a closed buffer is still recorded")
	}
	pe.closed = nil
	host.OwnerGone("alpha", 1)
	if len(pe.closed) != 1 || pe.closed[0] != second {
		t.Fatalf("closed = %v, want only the open buffer %d", pe.closed, second)
	}
}

func TestCreateBufferFailureRecordsNothing(t *testing.T) {
	m, pe := newVirtualModel(t)
	pe.failure = errors.New("boom")
	if _, err := m.plug.host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "alpha", Name: "x"}); err == nil {
		t.Fatal("CreateBuffer did not fail")
	}
	if len(m.plug.virtual.owner) != 0 {
		t.Fatalf("owners = %v", m.plug.virtual.owner)
	}
}

func TestCreateBufferFocusGivesTheKeyboardToTheEditor(t *testing.T) {
	tests := []struct {
		name  string
		focus bool
		want  bool // explorerFocus afterwards
	}{
		{"without focus the keyboard stays", false, true},
		{"with focus the editor takes it", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, pe := newVirtualModel(t)
			m.explorerFocus = true
			if _, err := m.plug.host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "alpha", Name: "x", Focus: tt.focus}); err != nil {
				t.Fatal(err)
			}
			if m.explorerFocus != tt.want {
				t.Fatalf("explorerFocus = %v, want %v", m.explorerFocus, tt.want)
			}
			if len(pe.made) != 1 || pe.made[0].Plugin != "alpha" || pe.made[0].Focus != tt.focus {
				t.Fatalf("the port got %+v", pe.made)
			}
		})
	}
}

// ---------- exclusion: the language-server sync ----------

func TestLSPLeavesVirtualDocumentsAlone(t *testing.T) {
	tests := []struct {
		name      string
		virtual   bool
		wantOpens int
	}{
		{"a file is opened", false, 1},
		// the tab has a path here so only the virtual check can keep it out
		{"a virtual document is not", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newFakeLSP("go")
			lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}, virtual: tt.virtual})
			lt.open()
			lt.touch()
			if got := len(port.opens); got != tt.wantOpens {
				t.Fatalf("opens = %v, want %d", port.opens, tt.wantOpens)
			}
			if tracked := len(lt.m.lsp.tracks); tracked != tt.wantOpens {
				t.Fatalf("%d documents tracked, want %d", tracked, tt.wantOpens)
			}
		})
	}
}
