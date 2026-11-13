package bundled_test

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const pkgPlugin = "pkg"

func pkgFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{
		"alpha/plugin.json": `{"name":"alpha","version":"1.2.3","description":"Alpha package","requires":["beta"]}`,
		"alpha/init.lua":    `local gvim = require "gvim"`,
		"beta/plugin.json":  `{"name":"beta","version":"0.4.0","description":"Beta package"}`,
		"beta/init.lua":     `local gvim = require "gvim"`,
	}
	for _, name := range []string{"init.lua", pkgPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+pkgPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[pkgPlugin+"/"+name] = string(data)
	}
	return files
}

func startPkg(t *testing.T) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, pkgFiles(t), plugintest.Options{Timeout: 2 * time.Second, EventBudget: 5 * time.Second})
	h.Host.FakeTheme().Themes = []string{"dark", "light"}
	h.Start()
	return h
}

func TestPkgListsAndShowsDetails(t *testing.T) {
	t.Parallel()
	h := startPkg(t)
	if err := h.Run(pkgPlugin+".list", nil); err != nil {
		t.Fatal(err)
	}
	picker := h.Host.UI().Last("picker")
	if picker == nil || !picker.Open {
		t.Fatalf("no package picker (log %v)", h.LogText(""))
	}
	idx := -1
	var texts []string
	for i, it := range picker.Picker.Items {
		texts = append(texts, it.Text)
		if strings.Contains(it.Text, "alpha") {
			idx = i
		}
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "INSTALLED") || !strings.Contains(joined, "THEMES") {
		t.Fatalf("picker groups = %q", joined)
	}
	if idx < 0 {
		t.Fatalf("no alpha row: %q", joined)
	}
	if !strings.Contains(texts[idx], "[P] alpha") || !strings.Contains(texts[idx], "v1.2.3") || !strings.Contains(texts[idx], "enabled") {
		t.Fatalf("alpha row = %q", texts[idx])
	}

	// Enter opens the read-only detail panel for alpha's manifest metadata.
	h.Resolve(pluginapi.Completion{Token: picker.Token, Values: []any{idx}})
	var detail *plugintest.FakeBuffer
	for _, b := range h.Host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Package: alpha") {
			detail = b
		}
	}
	if detail == nil {
		t.Fatalf("no detail panel: buffers %+v notices %v", h.Host.Buffers, h.NoticeTexts())
	}
	text := strings.Join(detail.Lines, "\n")
	for _, want := range []string{
		"Name: alpha", "Type: plugin", "Version: 1.2.3", "Status: enabled", "Source: user",
		"Path: /plugins/alpha/init.lua", "Alpha package", "Dependencies:", "- beta",
		"Read-only package inspection",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("detail missing %q:\n%s", want, text)
		}
	}
}

func TestPkgShowsBundledPackage(t *testing.T) {
	t.Parallel()
	h := startPkg(t)
	compiled := plugin.Compile("bundled_demo", map[string]string{"init.lua": `local gvim = require "gvim"`})
	if err := h.RT.LoadPlugin(plugin.Source{Name: "bundled_demo", Version: "9.9.9", Description: "Bundled demo", Kind: plugin.SourceBundled, Compiled: compiled}); err != nil {
		t.Fatal(err)
	}
	h.Settle()
	if err := h.Run(pkgPlugin+".list", nil); err != nil {
		t.Fatal(err)
	}
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatal("no picker")
	}
	idx := -1
	for i, it := range picker.Picker.Items {
		if strings.Contains(it.Text, "bundled_demo") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no bundled row: %+v", picker.Picker.Items)
	}
	h.Resolve(pluginapi.Completion{Token: picker.Token, Values: []any{idx}})
	var detail *plugintest.FakeBuffer
	for _, b := range h.Host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Package: bundled_demo") {
			detail = b
		}
	}
	if detail == nil || !strings.Contains(strings.Join(detail.Lines, "\n"), "(embedded bundled plugin)") {
		t.Fatalf("bundled detail = %+v", detail)
	}
}

func TestPkgReadOnlyCommandAndNoProcesses(t *testing.T) {
	t.Parallel()
	h := startPkg(t)
	var ids []string
	for _, c := range h.Host.Commands[pkgPlugin] {
		if !c.Hidden {
			ids = append(ids, c.ID)
		}
	}
	if strings.Join(ids, "|") != pkgPlugin+".list" {
		t.Fatalf("commands = %v, want only %s.list", ids, pkgPlugin)
	}
	if runs := h.OS.Runs(); len(runs) != 0 {
		t.Fatalf("package browser ran processes: %+v", runs)
	}
}

func TestPkgShowsNativeThemeDetails(t *testing.T) {
	t.Parallel()
	h := startPkg(t)
	if err := h.Run(pkgPlugin+".list", nil); err != nil {
		t.Fatal(err)
	}
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatal("no picker")
	}
	idx := -1
	for i, it := range picker.Picker.Items {
		if strings.Contains(it.Text, "[T] dark") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no theme row: %+v", picker.Picker.Items)
	}
	h.Resolve(pluginapi.Completion{Token: picker.Token, Values: []any{idx}})
	var detail *plugintest.FakeBuffer
	for _, b := range h.Host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Package: dark") {
			detail = b
		}
	}
	if detail == nil {
		t.Fatal("no theme detail panel")
	}
	text := strings.Join(detail.Lines, "\n")
	for _, want := range []string{"Name: dark", "Type: theme", "Version: native", "Status: current", "Native gvim theme", "Dependencies:", "(none)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("theme detail missing %q:\n%s", want, text)
		}
	}
}

// A description that is all multibyte characters must be cut at a character boundary, not
// emptied: the clip used to drop every trailing non-ASCII byte.
func TestPkgClipKeepsMultibyteDescriptions(t *testing.T) {
	t.Parallel()
	files := pkgFiles(t)
	files["gamma/plugin.json"] = `{"name":"gamma","version":"1.0.0","description":"` + strings.Repeat("插件", 100) + `"}`
	files["gamma/init.lua"] = `local gvim = require "gvim"`
	h := plugintest.New(t, files, plugintest.Options{Timeout: 2 * time.Second, EventBudget: 5 * time.Second})
	h.Host.FakeTheme().Themes = []string{"dark"}
	h.Start()
	if err := h.Run(pkgPlugin+".list", nil); err != nil {
		t.Fatal(err)
	}
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatalf("no package picker (log %v)", h.LogText(""))
	}
	for _, it := range picker.Picker.Items {
		if !strings.Contains(it.Text, "gamma") {
			continue
		}
		if !strings.HasPrefix(it.Detail, "插件插件") || !strings.HasSuffix(it.Detail, "…") || len(it.Detail) < 290 {
			t.Fatalf("gamma detail = %q", it.Detail)
		}
		return
	}
	t.Fatal("no gamma row")
}
