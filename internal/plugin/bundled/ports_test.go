package bundled_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/bundled"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// load starts a harness around the bundled plugin name, read from the embedded files, as
// the loader reads them. The host has the buffers of names, the first active.
func load(t *testing.T, name string, names ...string) *plugintest.Harness {
	t.Helper()
	src, err := fs.ReadFile(bundled.FS, "plugins/"+name+"/init.lua")
	if err != nil {
		t.Fatalf("read the bundled plugin %s: %v", name, err)
	}
	host := plugintest.NewFakeHost()
	for _, n := range names {
		host.AddBuffer("/w/"+n, "text of "+n)
	}
	h := plugintest.New(t, plugintest.Files{name + "/init.lua": string(src)}, plugintest.Options{Host: host, Timeout: 200 * time.Millisecond, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func tabNames(h *plugintest.Harness) []string {
	var out []string
	for _, b := range h.Host.ListBuffers() {
		out = append(out, b.Name)
	}
	return out
}

func TestBundledPluginsHaveManifests(t *testing.T) {
	t.Parallel()
	entries, err := fs.ReadDir(bundled.FS, "plugins")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		seen++
		for _, f := range []string{"plugin.json", "init.lua"} {
			if _, err := fs.Stat(bundled.FS, "plugins/"+e.Name()+"/"+f); err != nil {
				t.Errorf("plugin %s: %v", e.Name(), err)
			}
		}
	}
	if seen < 2 {
		t.Errorf("bundled plugins = %d, want at least goto_with_selection and tab_actions", seen)
	}
}

func TestGotoWithSelection(t *testing.T) {
	t.Parallel()
	h := load(t, "goto_with_selection", "a.txt")
	cmds := h.Host.Commands["goto_with_selection"]
	if len(cmds) != 1 || cmds[0].ID != "goto_with_selection.goto_line_with_selection" || cmds[0].Title != "Go to Line with Selection" {
		t.Fatalf("commands = %+v", cmds)
	}
	if err := h.Run("goto_with_selection.goto_line_with_selection", nil); err != nil {
		t.Fatal(err)
	}
	calls := h.Host.Cmd().Calls
	if len(calls) != 2 || calls[0].Action != "set_mark" || calls[1].Action != "goto_line" || calls[0].Group != calls[1].Group || calls[0].Group == 0 {
		t.Fatalf("actions = %+v, want set_mark then goto_line in one run_all group", calls)
	}
	if len(h.Host.Notices) != 0 {
		t.Errorf("notices = %v, want none", h.Host.Notices)
	}
}

func TestGotoWithSelectionReportsAFailedStep(t *testing.T) {
	t.Parallel()
	h := load(t, "goto_with_selection", "a.txt")
	h.Host.Cmd().Errors["goto_line"] = pluginapi.Errf(pluginapi.CodeUnsupported, "no prompt here")
	if err := h.Run("goto_with_selection.goto_line_with_selection", nil); err != nil {
		t.Fatal(err)
	}
	texts := h.NoticeTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "no prompt here") {
		t.Fatalf("notices = %v, want one that names the failure", texts)
	}
}

func TestTabActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		active   string
		modified []string
		command  string
		want     []string
		activeAt string
		notice   string
	}{
		{"close others", "c.txt", nil, "close_others", []string{"c.txt"}, "c.txt", "Closed 4 tabs"},
		{"close others keeps modified", "c.txt", []string{"b.txt"}, "close_others", []string{"b.txt", "c.txt"}, "c.txt", "Closed 3 tabs; kept 1 tab with unsaved changes"},
		{"close all", "c.txt", nil, "close_all", nil, "", "Closed 5 tabs"},
		{"close all keeps modified", "c.txt", []string{"c.txt", "e.txt"}, "close_all", []string{"c.txt", "e.txt"}, "c.txt", "Closed 3 tabs; kept 2 tabs with unsaved changes"},
		{"close to right", "b.txt", nil, "close_to_right", []string{"a.txt", "b.txt"}, "b.txt", "Closed 3 tabs"},
		{"close to left", "d.txt", nil, "close_to_left", []string{"d.txt", "e.txt"}, "d.txt", "Closed 3 tabs"},
		{"close to left at the first tab", "a.txt", nil, "close_to_left", []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"}, "a.txt", "Closed 0 tabs"},
		{"close to right keeps one modified", "a.txt", []string{"d.txt"}, "close_to_right", []string{"a.txt", "d.txt"}, "a.txt", "Closed 3 tabs; kept 1 tab with unsaved changes"},
		{"move left", "c.txt", nil, "move_to_left", []string{"a.txt", "c.txt", "b.txt", "d.txt", "e.txt"}, "c.txt", ""},
		{"move right", "c.txt", nil, "move_to_right", []string{"a.txt", "b.txt", "d.txt", "c.txt", "e.txt"}, "c.txt", ""},
		{"move left at the first tab", "a.txt", nil, "move_to_left", []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"}, "a.txt", ""},
		{"move right at the last tab", "e.txt", nil, "move_to_right", []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"}, "e.txt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := load(t, "tab_actions", "a.txt", "b.txt", "c.txt", "d.txt", "e.txt")
			for _, b := range h.Host.Buffers {
				b.Info.Modified = slices.Contains(tt.modified, b.Info.Name)
				if b.Info.Name == tt.active {
					h.Host.SetActive(b.Info.ID)
				}
			}
			if err := h.Run("tab_actions."+tt.command, nil); err != nil {
				t.Fatal(err)
			}
			if got := tabNames(h); !slices.Equal(got, tt.want) {
				t.Errorf("tabs = %v, want %v", got, tt.want)
			}
			active := ""
			if info, ok := h.Host.Buffer(h.Host.ActiveBuffer()); ok {
				active = info.Name
			}
			if active != tt.activeAt {
				t.Errorf("active tab = %q, want %q", active, tt.activeAt)
			}
			texts := h.NoticeTexts()
			switch {
			case tt.notice == "" && len(texts) != 0:
				t.Errorf("notices = %v, want none", texts)
			case tt.notice != "" && !slices.Equal(texts, []string{tt.notice}):
				t.Errorf("notices = %v, want %q", texts, tt.notice)
			}
		})
	}
}

func TestTabActionsRegistersItsCommands(t *testing.T) {
	t.Parallel()
	h := load(t, "tab_actions", "a.txt")
	var got []string
	for _, c := range h.Host.Commands["tab_actions"] {
		got = append(got, c.ID+"="+c.Title)
	}
	// The runtime publishes a plugin's commands sorted by name.
	want := []string{
		"tab_actions.close_all=Close All Tabs",
		"tab_actions.close_others=Close Other Tabs",
		"tab_actions.close_to_left=Close Tabs To Left",
		"tab_actions.close_to_right=Close Tabs To Right",
		"tab_actions.move_to_left=Move Tab To Left",
		"tab_actions.move_to_right=Move Tab To Right",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}
