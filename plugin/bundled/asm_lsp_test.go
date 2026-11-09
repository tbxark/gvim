package bundled_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// asmRig loads asm_lsp with one assembly buffer and a project root at /work. script adds
// the fake-OS files/dirs the test needs.
func asmRig(t *testing.T, path, text string, script func(o *plugintest.FakeOS)) *rig {
	t.Helper()
	r := startRig(t, rigOpts{
		plugins: []string{"asm_lsp"},
		buffers: []rigBuffer{{path, text}},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
			if script != nil {
				script(o)
			}
		},
	})
	// The test harness loads plugins directly; the real loader's startup flush raises
	// plugins_loaded and replays the open files. Ask for that sweep explicitly.
	asmSweep(r.h)
	return r
}

// asmSweepEmits plugins_loaded: the test harness loads plugins directly and does not run
// the real loader's startup flush, so a test asks for the already-open-buffer sweep.
func asmSweep(h *plugintest.Harness) {
	h.Emit(pluginapi.Event{Name: "plugins_loaded", Data: map[string]any{}})
}

// asmPopup returns the last action popup, or nil.
func asmPopup(h *plugintest.Harness) *plugintest.FakeOverlay {
	return h.Host.UI().Last("action_popup")
}

func asmPopupCount(h *plugintest.Harness) int {
	n := 0
	for _, o := range h.Host.UI().Overlays {
		if o.Kind == "action_popup" {
			n++
		}
	}
	return n
}

func asmActionIDs(o *plugintest.FakeOverlay) []string {
	var ids []string
	for _, a := range o.ActionPopup.Actions {
		ids = append(ids, a.ID)
	}
	return ids
}

func asmHasAction(o *plugintest.FakeOverlay, id string) bool {
	for _, a := range o.ActionPopup.Actions {
		if a.ID == id {
			return true
		}
	}
	return false
}

func TestAsmLspManifestAndCommands(t *testing.T) {
	t.Parallel()
	r := asmRig(t, "/work/a.asm", "section .text\n", nil)
	if info, ok := r.h.Plugin("asm_lsp"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	commands := map[string]bool{}
	for _, c := range r.host.Commands["asm_lsp"] {
		commands[c.ID] = true
	}
	for _, want := range []string{"asm_lsp.create_config", "asm_lsp.show_help"} {
		if !commands[want] {
			t.Errorf("missing command %q (have %v)", want, commands)
		}
	}
	if bindings := r.host.Bindings["asm_lsp"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestAsmLspOffersAndCreatesConfig(t *testing.T) {
	t.Parallel()
	r := asmRig(t, "/work/a.asm", "section .text\nglobal _start\n", nil)
	ov := asmPopup(r.h)
	if ov == nil {
		t.Fatalf("no config offer popup (log %v)", r.h.LogText(""))
	}
	if !strings.Contains(ov.ActionPopup.Title, ".asm-lsp.toml") {
		t.Errorf("popup title = %q", ov.ActionPopup.Title)
	}
	if !asmHasAction(ov, "create:nasm") || !asmHasAction(ov, "create:gas") || !asmHasAction(ov, "create:masm") ||
		!asmHasAction(ov, "dismiss") || !asmHasAction(ov, "never") {
		t.Fatalf("popup actions = %v", asmActionIDs(ov))
	}
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"create:nasm"}})
	text, ok := r.os.File("/work/.asm-lsp.toml")
	if !ok {
		t.Fatalf("the offer did not write .asm-lsp.toml (files: %v)", r.os.Calls())
	}
	for _, want := range []string{"assembler = \"nasm\"", "instruction_set = \"x86/x86-64\"", "default_diagnostics = false"} {
		if !strings.Contains(text, want) {
			t.Errorf(".asm-lsp.toml = %q, want %q", text, want)
		}
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "Created .asm-lsp.toml (nasm, x86/x86-64)") {
		t.Errorf("status = %v", r.h.NoticeTexts())
	}
	found := false
	for _, action := range r.host.Cmd().Ran() {
		if action == "lsp_restart" {
			found = true
		}
	}
	if !found {
		t.Errorf("commands = %v, want an lsp_restart", r.host.Cmd().Ran())
	}
}

func TestAsmLspDetectsMasmAndArm(t *testing.T) {
	t.Parallel()
	text := "myproc PROC\n  stp x29, x30, [sp, #-16]!\nmyproc ENDP\n"
	r := asmRig(t, "/work/m.asm", text, nil)
	asmSweep(r.h)
	ov := asmPopup(r.h)
	if ov == nil {
		t.Fatal("no config offer popup")
	}
	if !asmHasAction(ov, "create:masm") {
		t.Fatalf("popup actions = %v, want a MASM offer", asmActionIDs(ov))
	}
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"create:masm"}})
	config, _ := r.os.File("/work/.asm-lsp.toml")
	for _, want := range []string{"assembler = \"masm\"", "instruction_set = \"arm64\"", "default_diagnostics = false"} {
		if !strings.Contains(config, want) {
			t.Errorf(".asm-lsp.toml = %q, want %q", config, want)
		}
	}
}

func TestAsmLspGasSource(t *testing.T) {
	t.Parallel()
	r := asmRig(t, "/work/a.s", ".globl _start\n_start:\n  mov $1, %eax\n", nil)
	ov := asmPopup(r.h)
	if ov == nil {
		t.Fatal("no config offer popup")
	}
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"create:gas"}})
	config, _ := r.os.File("/work/.asm-lsp.toml")
	if !strings.Contains(config, "assembler = \"gas\"") || strings.Contains(config, "default_diagnostics") {
		t.Errorf("GAS config = %q, want gas with built-in diagnostics", config)
	}
}

func TestAsmLspSkipsWhenConfigExists(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		script func(o *plugintest.FakeOS)
	}{
		{"project config", func(o *plugintest.FakeOS) {
			o.SetFile("/work/.asm-lsp.toml", "[default_config]\nassembler = \"gas\"\n")
		}},
		{"xdg global config", func(o *plugintest.FakeOS) {
			o.Env["XDG_CONFIG_HOME"] = "/home/u/.config"
			o.SetFile("/home/u/.config/asm-lsp/.asm-lsp.toml", "assembler = \"gas\"\n")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := asmRig(t, "/work/a.asm", "section .text\n", tt.script)
			if got := asmPopupCount(r.h); got != 0 {
				t.Fatalf("offered a config although one exists (%d popups)", got)
			}
		})
	}
}

func TestAsmLspNeverAsksAgainForTheProject(t *testing.T) {
	t.Parallel()
	r := asmRig(t, "/work/a.asm", "section .text\n", nil)
	ov := asmPopup(r.h)
	if ov == nil {
		t.Fatal("no config offer popup")
	}
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"never"}})
	if got := lastNoticeText(r.h); !strings.Contains(got, "Won't offer") {
		t.Errorf("status = %v", r.h.NoticeTexts())
	}
	r.h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"buf": r.ids["/work/a.asm"], "path": "/work/a.asm"}})
	if got := asmPopupCount(r.h); got != 1 {
		t.Fatalf("a second offer was made on the same project (%d popups)", got)
	}
}

func TestAsmLspInstallHelpAndActions(t *testing.T) {
	t.Parallel()
	r := asmRig(t, "/work/a.asm", "section .text\n", func(o *plugintest.FakeOS) {
		o.SetFile("/work/.asm-lsp.toml", "assembler = \"nasm\"\n")
	})
	r.h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "asm-lsp: server not found"}})
	if got := lastNoticeText(r.h); !strings.Contains(got, "Assembly LSP server not found") {
		t.Fatalf("status = %q", got)
	}
	r.h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov := asmPopup(r.h)
	if ov == nil {
		t.Fatalf("no help popup (log %v)", r.h.LogText(""))
	}
	for _, want := range []string{"copy:cargo", "copy:binstall", "act:lsp_restart", "act:show_lsp_status", "dismiss"} {
		if !asmHasAction(ov, want) {
			t.Errorf("help popup lacks %q (actions %v)", want, asmActionIDs(ov))
		}
	}
	r.h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"copy:cargo"}})
	if r.host.Clipboard != "cargo install asm-lsp" {
		t.Errorf("clipboard = %q", r.host.Clipboard)
	}
	if got := lastNoticeText(r.h); got != "Copied: cargo install asm-lsp" {
		t.Errorf("copy status = %q", got)
	}
	// A second status click opens help again; choosing the native restart dispatches it.
	r.h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov = asmPopup(r.h)
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
}

func TestAsmLspIgnoresNonAsmBuffers(t *testing.T) {
	t.Parallel()
	r := startRig(t, rigOpts{
		plugins: []string{"asm_lsp"},
		buffers: []rigBuffer{{"/work/notes.md", "# notes\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	if got := asmPopupCount(r.h); got != 0 {
		t.Fatalf("offered a config for a markdown buffer (%d popups)", got)
	}
	r.h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "asm-lsp: not found"}})
	if got := lastNoticeText(r.h); got != "" {
		t.Fatalf("claimed an asm error on a markdown buffer: %q", got)
	}
}
