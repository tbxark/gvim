package bundled_test

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The path_complete port: Fresh's completion engine on the one prompt surface gvim has
// (gvim.ui.input + on_change -> set_prompt_suggestions), plus the exported engine for other
// plugins. The tests drive the fake host prompt and a scripted FakeOS tree.

const pathCompletePlugin = "path_complete"

func pathCompleteFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", pathCompletePlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+pathCompletePlugin+"/"+name)
		if err != nil {
			t.Fatalf("read the bundled plugin file %s: %v", name, err)
		}
		files[pathCompletePlugin+"/"+name] = string(data)
	}
	return files
}

// pathHost is a fake host over a fake OS with the Fresh test tree under /work (the cwd).
func pathHost(t *testing.T) (*plugintest.FakeHost, *plugintest.FakeOS) {
	t.Helper()
	host := plugintest.NewFakeHost()
	os := plugintest.NewFakeOS()
	for path, content := range map[string]string{
		"/work/alpha.go":     "package a\n",
		"/work/beta.txt":     "beta\n",
		"/work/Charlie.md":   "# charlie\n",
		"/work/cmd/one.go":   "package one\n",
		"/work/cmd/two.txt":  "two\n",
		"/work/.hiddenfile":  "hidden\n",
		"/work/.hiddendir/x": "x\n",
	} {
		os.SetFile(path, content)
		host.SetFile(path, content)
	}
	return host, os
}

func startPathComplete(t *testing.T, host *plugintest.FakeHost, os *plugintest.FakeOS) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, pathCompleteFiles(t), plugintest.Options{
		Host: host, OS: os, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second,
	})
	h.Start()
	return h
}

// suggestionTexts reduces the fake suggestion rows to "text" values.
func suggestionTexts(h *plugintest.Harness) []string {
	out := []string{}
	for _, s := range h.Host.UI().Suggestions {
		out = append(out, s.Text)
	}
	return out
}

func runPrompt(t *testing.T, h *plugintest.Harness) *plugintest.FakeOverlay {
	t.Helper()
	if err := h.Run(pathCompletePlugin+".complete_path", nil); err != nil {
		t.Fatal(err)
	}
	in := h.Host.UI().Last("input")
	if in == nil {
		t.Fatalf("no path prompt was opened (log %v)", h.LogText(""))
	}
	return in
}

func TestPathCompleteManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/"+pathCompletePlugin+"/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name           string `json:"name"`
		Version        string `json:"version"`
		Description    string `json:"description"`
		API            int    `json:"api"`
		DefaultEnabled *bool  `json:"default_enabled"`
	}
	if err := jsonUnmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != pathCompletePlugin || m.Version == "" || strings.TrimSpace(m.Description) == "" || m.API != 2 {
		t.Errorf("manifest = %+v", m)
	}
	if m.DefaultEnabled == nil || !*m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want true (a small suggestion provider)", m.DefaultEnabled)
	}
	host, os := pathHost(t)
	h := startPathComplete(t, host, os)
	if info, ok := h.Plugin(pathCompletePlugin); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, h.LogText(""))
	}
	for _, l := range h.LogText("") {
		if strings.HasPrefix(l, "warn:") || strings.HasPrefix(l, "error:") {
			t.Errorf("load logged %s", l)
		}
	}
	cmds := h.Host.Commands[pathCompletePlugin]
	if len(cmds) != 1 || cmds[0].ID != pathCompletePlugin+".complete_path" ||
		cmds[0].Title != "Path Completion: Complete Path" {
		t.Errorf("commands = %+v, want the one path-completion command", cmds)
	}
	if bindings := h.Host.Bindings[pathCompletePlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none (Fresh binds none)", bindings)
	}
}

func TestPathCompleteSuggestions(t *testing.T) {
	t.Parallel()
	host, os := pathHost(t)
	h := startPathComplete(t, host, os)
	in := runPrompt(t, h)

	cases := []struct {
		input string
		want  []string
	}{
		// Directories first, then files alphabetical; hidden entries are out.
		{"", []string{"cmd/", "alpha.go", "beta.txt", "Charlie.md"}},
		// Case-insensitive prefix.
		{"AL", []string{"alpha.go", "New file: AL"}},
		{"ch", []string{"Charlie.md", "New file: ch"}},
		// A directory with a trailing slash lists that directory (its path exists, so no
		// new-file row).
		{"cmd/", []string{"one.go", "two.txt"}},
		{"cmd/t", []string{"two.txt", "New file: cmd/t"}},
		// Absolute input.
		{"/work/cmd/", []string{"one.go", "two.txt"}},
		// The hidden rule: dotfiles show only when the pattern starts with a dot.
		{".h", []string{".hiddendir/", ".hiddenfile", "New file: .h"}},
		// A missing directory lists nothing (no new-file row without a readable dir).
		{"nope/", nil},
		// A missing file path still offers the new-file row.
		{"brandnew.txt", []string{"New file: brandnew.txt"}},
	}
	for _, tc := range cases {
		in.Input.OnChange(tc.input)
		h.Settle()
		got := suggestionTexts(h)
		if !slicesEqual(got, tc.want) {
			t.Errorf("OnChange(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
	// The directory flag is in the row's description.
	in.Input.OnChange("cmd")
	h.Settle()
	found := false
	for _, s := range h.Host.UI().Suggestions {
		if s.Text == "cmd/" && s.Description == "directory" && s.Value == "cmd/" {
			found = true
		}
	}
	if !found {
		t.Errorf("directory suggestion = %+v", h.Host.UI().Suggestions)
	}
}

func TestPathCompleteCapAndHidden(t *testing.T) {
	t.Parallel()
	host, os := pathHost(t)
	for i := 0; i < 150; i++ {
		os.SetFile("/work/many/file"+threeDigits(i)+".txt", "x\n")
	}
	h := startPathComplete(t, host, os)
	in := runPrompt(t, h)
	in.Input.OnChange("many/")
	h.Settle()
	if got := len(h.Host.UI().Suggestions); got != 100 {
		t.Errorf("suggestions = %d, want the 100-row cap", got)
	}
	// The cap keeps the first rows after Fresh's sort: file000, file001, ...
	if got := h.Host.UI().Suggestions[0].Text; got != "file000.txt" {
		t.Errorf("first suggestion = %q", got)
	}
}

func TestPathCompletePromptDescendsAndOpens(t *testing.T) {
	t.Parallel()
	host, os := pathHost(t)
	h := startPathComplete(t, host, os)
	in := runPrompt(t, h)
	in.Input.OnChange("cmd")
	h.Settle()
	// Enter on a directory opens a new prompt pre-filled with the directory and a slash.
	h.Resolve(pluginapi.Completion{Token: in.Token, Values: []any{"cmd"}})
	h.Settle()
	next := h.Host.UI().Last("input")
	if next == nil || next.Open == false {
		t.Fatalf("no second prompt after the directory answer (overlays %+v)", h.Host.UI().Overlays)
	}
	if next.Input.Default != "cmd/" {
		t.Errorf("second prompt default = %q, want cmd/", next.Input.Default)
	}
	next.Input.OnChange("cmd/one")
	h.Settle()
	if got := suggestionTexts(h); len(got) != 2 || got[0] != "one.go" || got[1] != "New file: cmd/one" {
		t.Errorf("suggestions inside cmd/ = %v", got)
	}
	// Enter on a file opens it (the fake host completes the async open).
	h.Resolve(pluginapi.Completion{Token: next.Token, Values: []any{"cmd/one.go"}})
	h.CompleteBuffers()
	if id := host.FindBuffer("/work/cmd/one.go"); id == 0 {
		t.Errorf("the file was not opened: %v", host.ListBuffers())
	}
	if got := lastNoticeText(h); got != "Opened cmd/one.go" {
		t.Errorf("open notice = %q", got)
	}
}

func TestPathCompleteCancel(t *testing.T) {
	t.Parallel()
	host, os := pathHost(t)
	h := startPathComplete(t, host, os)
	in := runPrompt(t, h)
	before := len(host.ListBuffers())
	h.Resolve(pluginapi.Completion{Token: in.Token, Values: []any{nil}})
	if got := len(host.ListBuffers()); got != before {
		t.Errorf("Esc opened a buffer (%d -> %d)", before, got)
	}
	if got := lastNoticeText(h); got != "Path completion cancelled" {
		t.Errorf("cancel notice = %q", got)
	}
}

func TestPathCompleteExport(t *testing.T) {
	t.Parallel()
	host, os := pathHost(t)
	files := pathCompleteFiles(t)
	files["probe/init.lua"] = `local gvim = require "gvim"
gvim.command.register{ name = "probe", title = "Probe: Paths", run = function()
  local pc = gvim.plugins.get("path_complete")
  if not pc then
    gvim.notify("no export")
    return
  end
  local items = pc.complete("al")
  local first = items[1]
  gvim.notify("count:" .. tostring(#items) .. " value:" .. tostring(first and first.value) .. " desc:" .. tostring(first and first.description))
end }`
	h := plugintest.New(t, files, plugintest.Options{Host: host, OS: os, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second})
	h.Start()
	if err := h.Run("probe.probe", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(h); got != "count:2 value:alpha.go desc:nil" {
		t.Errorf("exporter notice = %q", got)
	}
}

// slicesEqual compares two string slices without importing slices (the package tests keep
// their helpers local).
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// threeDigits formats 0..999 with leading zeros (the fixture names sort like the numbers).
func threeDigits(n int) string {
	return string([]byte{byte('0' + n/100), byte('0' + (n/10)%10), byte('0' + n%10)})
}
