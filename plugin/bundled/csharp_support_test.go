package bundled_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// csRig loads csharp_support with one buffer and a fake OS. Events are emitted by the
// tests: the harness loads plugins directly and does not replay after_file_open.
func csRig(t *testing.T, path, text string, script func(o *plugintest.FakeOS)) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"csharp_support"},
		buffers: []rigBuffer{{path, text}},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
			o.Bin["dotnet"] = "/usr/bin/dotnet"
			if script != nil {
				script(o)
			}
		},
	})
}

func csOpen(r *rig, path string) {
	r.h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"buf": r.ids[path], "path": path}})
}

func csRestoreCalls(r *rig, root string) int {
	n := 0
	for _, run := range r.os.Runs() {
		if len(run.Argv) == 3 && run.Argv[0] == "dotnet" && run.Argv[1] == "restore" && run.Argv[2] == root {
			n++
		}
	}
	return n
}

func TestCSharpManifestAndCommands(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/a.cs", "class A {}\n", nil)
	if info, ok := r.h.Plugin("csharp_support"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	commands := map[string]string{}
	for _, c := range r.host.Commands["csharp_support"] {
		commands[c.ID] = c.Title
	}
	want := map[string]string{
		"csharp_support.restore_project": "C#: Restore Project",
		"csharp_support.show_help":       "C#: Show LSP Help",
	}
	for id, title := range want {
		if commands[id] != title {
			t.Errorf("command %s = %q, want %q", id, commands[id], title)
		}
	}
	if bindings := r.host.Bindings["csharp_support"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestCSharpRestoreUsesChineseTranslations(t *testing.T) {
	t.Parallel()
	r := startRig(t, rigOpts{
		locale:  "zh-CN",
		plugins: []string{"csharp_support"},
		buffers: []rigBuffer{{"/work/proj/src/a.cs", "class A {}\n"}},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
			o.SetDir("/work/proj")
			o.SetDir("/work/proj/src")
			o.Bin["dotnet"] = "/usr/bin/dotnet"
			o.SetFile("/work/proj/App.csproj", "<Project/>\n")
			o.Script([]string{"dotnet", "restore", "/work/proj"}, plugintest.ProcScript{Stdout: "restored\n"})
		},
	})
	r.host.EnvTrust = "trusted"
	csOpen(r, "/work/proj/src/a.cs")
	if got := csRestoreCalls(r, "/work/proj"); got != 1 {
		t.Fatalf("restore calls = %d (runs %v)", got, r.os.Runs())
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "NuGet恢复已完成") {
		t.Errorf("status = %q, want Chinese restore completion notice", got)
	}
}

func TestCSharpRestoresNearestProjectOnOpen(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/src/a.cs", "class A {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj")
		o.SetDir("/work/proj/src")
		o.SetFile("/work/proj/App.csproj", "<Project/>\n")
		o.Script([]string{"dotnet", "restore", "/work/proj"}, plugintest.ProcScript{Stdout: "restored\n"})
	})
	r.host.EnvTrust = "trusted"
	csOpen(r, "/work/proj/src/a.cs")
	if got := csRestoreCalls(r, "/work/proj"); got != 1 {
		t.Fatalf("restore calls = %d (runs %v)", got, r.os.Runs())
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "NuGet restore completed for /work/proj") {
		t.Errorf("status = %q", got)
	}
	// Once per project per session.
	csOpen(r, "/work/proj/src/a.cs")
	if got := csRestoreCalls(r, "/work/proj"); got != 1 {
		t.Fatalf("restore ran again for the same project (%d calls)", got)
	}
}

func TestCSharpRestoreDefersUntilTrusted(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/a.cs", "class A {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj")
		o.SetFile("/work/proj/App.csproj", "<Project/>\n")
		o.Script([]string{"dotnet", "restore", "/work/proj"}, plugintest.ProcScript{})
	})
	// The fake host is restricted unless a test says otherwise.
	csOpen(r, "/work/proj/a.cs")
	if got := csRestoreCalls(r, "/work/proj"); got != 0 {
		t.Fatalf("restore ran in an untrusted workspace (%d calls)", got)
	}
	r.host.EnvTrust = "trusted"
	r.h.Emit(pluginapi.Event{Name: "trust_changed", Data: map[string]any{"level": "trusted"}})
	if got := csRestoreCalls(r, "/work/proj"); got != 1 {
		t.Fatalf("trust_changed did not run the deferred restore (%d calls)", got)
	}
}

func TestCSharpRestoreWithoutProjectFileUsesTheFileDirectory(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/single/a.cs", "class A {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/single")
		o.Script([]string{"dotnet", "restore", "/work/single"}, plugintest.ProcScript{})
	})
	r.host.EnvTrust = "trusted"
	csOpen(r, "/work/single/a.cs")
	if got := csRestoreCalls(r, "/work/single"); got != 1 {
		t.Fatalf("restore calls = %d, want the file's directory", got)
	}
}

func TestCSharpRestoreFailureIsReported(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/a.cs", "class A {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj")
		o.SetFile("/work/proj/App.csproj", "<Project/>\n")
		o.Script([]string{"dotnet", "restore", "/work/proj"}, plugintest.ProcScript{Code: 1, Stderr: "MSB1001: boom\nsecond line"})
	})
	r.host.EnvTrust = "trusted"
	csOpen(r, "/work/proj/a.cs")
	if got := lastNoticeText(r.h); !strings.Contains(got, "NuGet restore failed: MSB1001: boom") {
		t.Errorf("status = %q", got)
	}
}

func TestCSharpMissingDotnetIsReported(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/a.cs", "class A {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj")
		o.SetFile("/work/proj/App.csproj", "<Project/>\n")
		o.Script([]string{"dotnet", "restore", "/work/proj"}, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeIO, "exec: dotnet not found")})
	})
	r.host.EnvTrust = "trusted"
	csOpen(r, "/work/proj/a.cs")
	if got := lastNoticeText(r.h); !strings.Contains(got, "NuGet restore error") {
		t.Errorf("status = %q", got)
	}
}

func TestCSharpRestoreCommand(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/a.cs", "class A {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj")
		o.SetFile("/work/proj/App.csproj", "<Project/>\n")
		o.Script([]string{"dotnet", "restore", "/work/proj"}, plugintest.ProcScript{})
	})
	r.host.EnvTrust = "trusted"
	if err := r.h.Run("csharp_support.restore_project", nil); err != nil {
		t.Fatal(err)
	}
	if got := csRestoreCalls(r, "/work/proj"); got != 1 {
		t.Fatalf("restore calls = %d", got)
	}
}

func TestCSharpInstallHelpAndActions(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/a.cs", "class A {}\n", nil)
	r.host.EnvTrust = "trusted"
	r.host.LSP().Enabled = true
	for _, b := range r.host.Buffers {
		if b.Info.Path == "/work/proj/a.cs" {
			b.Info.Language = "csharp"
		}
	}
	r.h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "csharp-ls: server not found"}})
	if got := lastNoticeText(r.h); !strings.Contains(got, "C# LSP server not found") {
		t.Fatalf("status = %q", got)
	}
	r.h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov := r.host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	ids := map[string]bool{}
	for _, a := range ov.ActionPopup.Actions {
		ids[a.ID] = true
	}
	for _, want := range []string{"copy:install", "act:lsp_restart", "act:show_lsp_status", "act:lsp_disable_language", "dismiss"} {
		if !ids[want] {
			t.Errorf("help popup lacks %q (actions %v)", want, ids)
		}
	}
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"copy:install"}})
	if r.host.Clipboard != "dotnet tool install --global csharp-ls" {
		t.Errorf("clipboard = %q", r.host.Clipboard)
	}
	if got := lastNoticeText(r.h); got != "Copied: dotnet tool install --global csharp-ls" {
		t.Errorf("copy status = %q", got)
	}
	r.h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_restart"}})
	found := false
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_restart" {
			found = true
		}
	}
	if !found {
		t.Errorf("commands = %v, want lsp_restart", r.host.Cmd().Ran())
	}
	// Switch away after the popup opens: the controlled disable must refuse the wrong tab.
	r.h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov = r.host.UI().Last("action_popup")
	notes := r.host.AddBuffer("/work/proj/notes.md", "# notes\n")
	r.host.SetActive(notes)
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_disable_language"}})
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_disable_language_target" {
			t.Fatalf("disable dispatched for a non-C# active buffer: %v", r.host.Cmd().Ran())
		}
	}
	if got := lastNoticeText(r.h); got != "Open a C# file first (.cs)" {
		t.Fatalf("switched-buffer status = %q", got)
	}
	// Back on the C# buffer the target action receives path/language; success is native-owned.
	r.host.SetActive(r.ids["/work/proj/a.cs"])
	r.h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov = r.host.UI().Last("action_popup")
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_disable_language"}})
	found = false
	for _, call := range r.host.Cmd().Calls {
		if call.Action == "lsp_disable_language_target" {
			found = true
			if call.Args["path"] != "/work/proj/a.cs" || call.Args["language"] != "csharp" {
				t.Fatalf("disable target args = %+v", call.Args)
			}
		}
	}
	if !found {
		t.Fatalf("commands = %v, want the controlled disable target", r.host.Cmd().Ran())
	}
	if strings.Contains(strings.Join(r.h.NoticeTexts(), "\n"), "disabled") {
		t.Fatalf("plugin claimed async disable success: %v", r.h.NoticeTexts())
	}
}

func TestCSharpDisableWithoutLSPDoesNotDispatch(t *testing.T) {
	t.Parallel()
	r := csRig(t, "/work/proj/a.cs", "class A {}\n", nil)
	r.host.EnvTrust = "trusted"
	r.h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "csharp-ls: server not found"}})
	r.h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov := r.host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_disable_language"}})
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_disable_language_target" {
			t.Fatalf("disable dispatched without LSP: %v", r.host.Cmd().Ran())
		}
	}
	if got := lastNoticeText(r.h); got != "No language server is configured" {
		t.Fatalf("no-LSP status = %q", got)
	}
}

func TestCSharpIgnoresNonCSharpBuffers(t *testing.T) {
	t.Parallel()
	r := startRig(t, rigOpts{
		plugins: []string{"csharp_support"},
		buffers: []rigBuffer{{"/work/notes.md", "# notes\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.host.EnvTrust = "trusted"
	csOpen(r, "/work/notes.md")
	if runs := r.os.Runs(); len(runs) != 0 {
		t.Fatalf("restore ran for a markdown buffer: %v", runs)
	}
	r.h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "csharp-ls: not found"}})
	if got := lastNoticeText(r.h); got != "" {
		t.Fatalf("claimed a C# error on a markdown buffer: %q", got)
	}
}
