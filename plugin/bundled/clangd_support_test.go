package bundled_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// clangdRig loads clangd_support with one C-family buffer and a fake OS.
func clangdRig(t *testing.T, path, text string, script func(o *plugintest.FakeOS)) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"clangd_support"},
		buffers: []rigBuffer{{path, text}},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
			if script != nil {
				script(o)
			}
		},
	})
}

func clangdVirtualBuffer(host *plugintest.FakeHost) pluginapi.BufferID {
	for _, info := range host.ListBuffers() {
		if strings.Contains(info.Name, "Clangd Project Setup") && info.Virtual {
			return info.ID
		}
	}
	return 0
}

func clangdVirtualText(t *testing.T, host *plugintest.FakeHost) string {
	t.Helper()
	id := clangdVirtualBuffer(host)
	if id == 0 {
		t.Fatalf("no Clangd Project Setup buffer (buffers %v)", host.ListBuffers())
	}
	entries, _ := host.VirtualContent(id)
	return contentText(entries)
}

func TestClangdManifestAndCommands(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/src/a.c", "int main(void) { return 0; }\n", nil)
	if info, ok := r.h.Plugin("clangd_support"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	commands := map[string]string{}
	for _, c := range r.host.Commands["clangd_support"] {
		commands[c.ID] = c.Title
	}
	want := map[string]string{
		"clangd_support.switch_source_header": "Clangd: Switch Source/Header",
		"clangd_support.project_setup":        "Clangd: Project Setup",
		"clangd_support.open_project_config":  "Clangd: Open Project Config",
	}
	for id, title := range want {
		if commands[id] != title {
			t.Errorf("command %s = %q, want %q", id, commands[id], title)
		}
	}
	if bindings := r.host.Bindings["clangd_support"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestClangdChineseCommandsAndNotices(t *testing.T) {
	t.Parallel()
	r := startRig(t, rigOpts{
		locale:  "zh-CN",
		plugins: []string{"clangd_support"},
		buffers: []rigBuffer{{"/work/src/a.c", "int main(void) { return 0; }\n"}},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/work")
		},
	})
	commands := map[string]string{}
	for _, c := range r.host.Commands["clangd_support"] {
		commands[c.ID] = c.Title
	}
	if got := commands["clangd_support.switch_source_header"]; got != "Clangd: 切换源文件/头文件" {
		t.Errorf("Chinese switch command title = %q, want Clangd: 切换源文件/头文件", got)
	}
	if got := commands["clangd_support.project_setup"]; got != "Clangd: 项目设置" {
		t.Errorf("Chinese project setup title = %q, want Clangd: 项目设置", got)
	}
	r.host.SetActive(0)
	if err := r.h.Run("clangd_support.switch_source_header", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "没有活动文件可切换") {
		t.Errorf("notice = %q, want Chinese no-active-file notice", got)
	}
}

func TestClangdSwitchSourceToHeader(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/src/a.c", "int f(void);\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/src")
		o.SetFile("/work/src/a.h", "int f(void);\n")
	})
	r.host.SetFile("/work/src/a.h", "int f(void);\n")
	if err := r.h.Run("clangd_support.switch_source_header", nil); err != nil {
		t.Fatal(err)
	}
	r.h.CompleteBuffers()
	id := r.host.FindBuffer("/work/src/a.h")
	if id == 0 || r.host.ActiveBuffer() != id {
		t.Fatalf("a.h is not open and active (buffers %v)", r.host.ListBuffers())
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "Opened /work/src/a.h") {
		t.Errorf("status = %q", got)
	}
}

func TestClangdSwitchHeaderToSourceUsesThePicker(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/src/a.h", "int f(void);\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/src")
		o.SetFile("/work/src/a.cpp", "int f(void) { return 0; }\n")
		o.SetFile("/work/src/a.cc", "int f(void) { return 0; }\n")
	})
	r.host.SetFile("/work/src/a.cpp", "int f(void) { return 0; }\n")
	r.host.SetFile("/work/src/a.cc", "int f(void) { return 0; }\n")
	if err := r.h.Run("clangd_support.switch_source_header", nil); err != nil {
		t.Fatal(err)
	}
	o := r.host.UI().Last("picker")
	if o == nil || !o.Open {
		t.Fatalf("no picker for the two source candidates (log %v)", r.h.LogText(""))
	}
	r.h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{0}})
	r.h.CompleteBuffers()
	if id := r.host.FindBuffer("/work/src/a.cc"); id == 0 {
		t.Fatalf("a.cc is not open (buffers %v, log %v)", r.host.ListBuffers(), r.h.LogText(""))
	}
}

func TestClangdSwitchNoMatchAndUnsupported(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/src/a.c", "int f(void);\n", func(o *plugintest.FakeOS) { o.SetDir("/work/src") })
	if err := r.h.Run("clangd_support.switch_source_header", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "No corresponding source/header file") {
		t.Errorf("status = %q", got)
	}
	r2 := clangdRig(t, "/work/src/a.go", "package a\n", nil)
	if err := r2.h.Run("clangd_support.switch_source_header", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r2.h); !strings.Contains(got, "needs a C-family") {
		t.Errorf("status = %q", got)
	}
}

// The lookup never leaves the workspace: the include/ and src/ next to the buffer's directory are
// only tried when that parent is inside the workspace root.
func TestClangdSwitchStaysInsideTheWorkspace(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/a.c", "int f(void);\n", func(o *plugintest.FakeOS) {
		o.SetDir("/include")
		o.SetFile("/include/a.h", "int f(void);\n")
	})
	r.host.Workspace().RootDir = "/work"
	r.host.SetFile("/include/a.h", "int f(void);\n")
	if err := r.h.Run("clangd_support.switch_source_header", nil); err != nil {
		t.Fatal(err)
	}
	r.h.CompleteBuffers()
	if id := r.host.FindBuffer("/include/a.h"); id != 0 {
		t.Errorf("opened /include/a.h outside the workspace root (notices %v)", r.h.NoticeTexts())
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "No corresponding source/header file") {
		t.Errorf("status = %q", got)
	}
}

func TestClangdProjectSetupShowsReadyProject(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/proj/src/a.cpp", "int main() {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj")
		o.SetDir("/work/proj/src")
		o.SetFile("/work/proj/compile_commands.json", "[]\n")
		o.SetFile("/work/proj/.clangd", "CompileFlags:\n  Add: [-std=c++20]\n")
		o.SetFile("/work/proj/CMakeLists.txt", "project(a)\n")
	})
	r.host.Workspace().RootDir = "/work/proj"
	if err := r.h.Run("clangd_support.project_setup", nil); err != nil {
		t.Fatal(err)
	}
	text := clangdVirtualText(t, r.host)
	for _, want := range []string{
		"Project root: /work/proj",
		"Compile commands: ready (/work/proj/compile_commands.json)",
		".clangd configuration: /work/proj/.clangd",
		"CMake project detected",
		"General tips:",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("panel = %q, want %q", text, want)
		}
	}
}

func TestClangdProjectSetupShowsMissingPieces(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/proj/src/a.cpp", "int main() {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj")
		o.SetDir("/work/proj/src")
		o.SetFile("/work/proj/Makefile", "all:\n")
	})
	r.host.Workspace().RootDir = "/work/proj"
	if err := r.h.Run("clangd_support.project_setup", nil); err != nil {
		t.Fatal(err)
	}
	text := clangdVirtualText(t, r.host)
	for _, want := range []string{
		"Compile commands: missing",
		"cmake -DCMAKE_EXPORT_COMPILE_COMMANDS=ON",
		".clangd configuration: missing",
		"Makefile detected",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("panel = %q, want %q", text, want)
		}
	}
	// Running it again refreshes the same virtual buffer instead of opening a second one.
	if err := r.h.Run("clangd_support.project_setup", nil); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, info := range r.host.ListBuffers() {
		if strings.Contains(info.Name, "Clangd Project Setup") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d Clangd Project Setup buffers, want one", count)
	}
}

func TestClangdOpenProjectConfig(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/proj/src/a.cpp", "int main() {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj/src")
		o.SetFile("/work/.clangd", "CompileFlags:\n  Add: [-std=c++20]\n")
	})
	r.host.SetFile("/work/.clangd", "CompileFlags:\n  Add: [-std=c++20]\n")
	if err := r.h.Run("clangd_support.open_project_config", nil); err != nil {
		t.Fatal(err)
	}
	r.h.CompleteBuffers()
	if id := r.host.FindBuffer("/work/.clangd"); id == 0 {
		t.Fatalf(".clangd is not open (buffers %v, log %v)", r.host.ListBuffers(), r.h.LogText(""))
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "Opened /work/.clangd") {
		t.Errorf("status = %q", got)
	}
}

func TestClangdOpenProjectConfigMissing(t *testing.T) {
	t.Parallel()
	r := clangdRig(t, "/work/proj/src/a.cpp", "int main() {}\n", func(o *plugintest.FakeOS) {
		o.SetDir("/work/proj/src")
	})
	if err := r.h.Run("clangd_support.open_project_config", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "No project .clangd file found") {
		t.Errorf("status = %q", got)
	}
}
