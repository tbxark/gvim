package bundled_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// welcomeRig loads the welcome plugin with one file buffer.
func welcomeRig(t *testing.T, path, text string) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"welcome"},
		buffers: []rigBuffer{{path, text}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
}

func welcomeBuffer(r *rig, path string) pluginapi.BufferID {
	return r.ids[path]
}

func TestWelcomeManifestAndCommands(t *testing.T) {
	t.Parallel()
	r := welcomeRig(t, "/work/a.go", "package a\n")
	if info, ok := r.h.Plugin("welcome"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	want := map[string]string{
		"welcome.open_help":      "Plugin Demo: Open Help",
		"welcome.save_file":      "Plugin Demo: Save File",
		"welcome.say_hello":      "Plugin Demo: Say Hello",
		"welcome.insert_time":    "Plugin Demo: Insert Time",
		"welcome.insert_comment": "Plugin Demo: Insert Comment",
	}
	got := map[string]string{}
	for _, c := range r.host.Commands["welcome"] {
		got[c.ID] = c.Title
	}
	for id, title := range want {
		if got[id] != title {
			t.Errorf("command %s = %q, want %q", id, got[id], title)
		}
	}
	if bindings := r.host.Bindings["welcome"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestWelcomeBuiltInActions(t *testing.T) {
	t.Parallel()
	r := welcomeRig(t, "/work/a.go", "package a\n")
	if err := r.h.Run("welcome.open_help", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("welcome.save_file", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"show_help", "save"}
	if got := r.host.Cmd().Ran(); !slices.Equal(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func TestWelcomeInsertsGreetingAndTime(t *testing.T) {
	t.Parallel()
	r := welcomeRig(t, "/work/a.go", "package a\n")
	id := welcomeBuffer(r, "/work/a.go")
	if err := r.h.Run("welcome.say_hello", nil); err != nil {
		t.Fatal(err)
	}
	text := fullText(t, r.h, id)
	if !strings.HasPrefix(text, "Hello from a Lua plugin! The plugin system works.\n") {
		t.Fatalf("buffer = %q", text)
	}
	if got := lastNoticeText(r.h); got != "Greeting inserted at the cursor" {
		t.Errorf("status = %q", got)
	}
	if err := r.h.Run("welcome.insert_time", nil); err != nil {
		t.Fatal(err)
	}
	text = fullText(t, r.h, id)
	if !strings.Contains(text, "Current time: ") {
		t.Fatalf("buffer after insert_time = %q", text)
	}
	if got := lastNoticeText(r.h); got != "Current time inserted at the cursor" {
		t.Errorf("status = %q", got)
	}
}

func TestWelcomeCommentFollowsTheBufferLanguage(t *testing.T) {
	t.Parallel()
	r := welcomeRig(t, "/work/a.lua", "return 1\n")
	r.host.Buffers[0].Info.Language = "lua"
	id := welcomeBuffer(r, "/work/a.lua")
	if err := r.h.Run("welcome.insert_comment", nil); err != nil {
		t.Fatal(err)
	}
	text := fullText(t, r.h, id)
	if !strings.HasPrefix(text, "-- This comment was inserted by a plugin!\n") {
		t.Fatalf("lua buffer = %q", text)
	}
	if got := lastNoticeText(r.h); got != "The plugin inserted a comment" {
		t.Errorf("status = %q", got)
	}

	// A C-like language keeps the "//" prefix Fresh hard-codes.
	r2 := welcomeRig(t, "/work/a.go", "package a\n")
	id2 := welcomeBuffer(r2, "/work/a.go")
	if err := r2.h.Run("welcome.insert_comment", nil); err != nil {
		t.Fatal(err)
	}
	if text := fullText(t, r2.h, id2); !strings.HasPrefix(text, "// This comment was inserted by a plugin!\n") {
		t.Fatalf("go buffer = %q", text)
	}
}

func TestWelcomeInsertFailureIsReported(t *testing.T) {
	t.Parallel()
	r := welcomeRig(t, "/work/a.go", "package a\n")
	r.host.Buffers[0].Info.ReadOnly = true
	if err := r.h.Run("welcome.say_hello", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "Could not insert the text") {
		t.Fatalf("status = %q", got)
	}
}
