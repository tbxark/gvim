package bundled_test

import (
	"io/fs"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const devcontainerPlugin = "devcontainer"

func devcontainerFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", devcontainerPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+devcontainerPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[devcontainerPlugin+"/"+name] = string(data)
	}
	return files
}

func devcontainerHost(t *testing.T) (*plugintest.FakeHost, *plugintest.FakeOS) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/w"
	host.AddBuffer("/w/a.go", "package a\n")
	os := plugintest.NewFakeOS()
	os.SetFile("/w/a.go", "package a\n")
	return host, os
}

func startDevcontainer(t *testing.T, host *plugintest.FakeHost, os *plugintest.FakeOS) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, devcontainerFiles(t), plugintest.Options{Host: host, OS: os, Timeout: 2 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func devcontainerConfig() string {
	return `{
		// JSONC comments and trailing commas are part of devcontainer.json
		"name": "Demo Dev",
		"image": "mcr.microsoft.com/devcontainers/go:1",
		"features": {
			"ghcr.io/devcontainers/features/node:1": { "version": "lts" },
			"ghcr.io/devcontainers/features/git:1": {},
		},
		"forwardPorts": [3000, "8080"],
		"portsAttributes": {
			"3000": { "label": "web", "protocol": "http", "onAutoForward": "openBrowser" }
		},
		"containerEnv": { "APP_ENV": "dev", "FROM_CONTAINER": "yes" },
		"remoteEnv": { "APP_ENV": "remote" },
		"postCreateCommand": "go mod download",
		"postStartCommand": ["echo", "started"],
		"customizations": { "vscode": { "extensions": ["golang.go"] } },
	}`
}

func TestDevcontainerShowInfo(t *testing.T) {
	t.Parallel()
	host, os := devcontainerHost(t)
	os.SetFile("/w/.devcontainer/devcontainer.json", devcontainerConfig())
	h := startDevcontainer(t, host, os)
	if err := h.Run(devcontainerPlugin+".show_info", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	var info *plugintest.FakeBuffer
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Dev Container: Demo Dev") {
			info = b
		}
	}
	if info == nil {
		t.Fatalf("no info panel (buffers %+v notices %v log %v)", host.Buffers, h.NoticeTexts(), h.LogText(""))
	}
	text := strings.Join(info.Lines, "\n")
	for _, want := range []string{
		"mcr.microsoft.com/devcontainers/go:1",
		"+ ghcr.io/devcontainers/features/node:1",
		"version = lts",
		"3000 -> http (web) [openBrowser]",
		"8080 -> tcp",
		"APP_ENV = remote",
		"FROM_CONTAINER = yes",
		"postCreateCommand: go mod download",
		"postStartCommand: echo started",
		"Execution, attach, rebuild and lifecycle commands are not available",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("info panel missing %q:\n%s", want, text)
		}
	}
	if runs := os.Runs(); len(runs) != 0 {
		t.Fatalf("devcontainer discovery ran processes: %+v", runs)
	}
}

func TestDevcontainerOpenConfigAndRefresh(t *testing.T) {
	t.Parallel()
	host, os := devcontainerHost(t)
	os.SetFile("/w/.devcontainer.json", `{"name":"Root Config","image":"alpine"}`)
	h := startDevcontainer(t, host, os)
	if err := h.Run(devcontainerPlugin+".open_config", nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteBuffers()
	var opened bool
	for _, c := range host.Calls() {
		if c.Method == "Open" {
			if req, ok := c.Args[1].(pluginapi.OpenRequest); ok && req.Path == "/w/.devcontainer.json" && req.Focus {
				opened = true
			}
		}
	}
	if !opened {
		t.Fatalf("config not opened (calls %v notices %v)", host.Calls(), h.NoticeTexts())
	}
	if runs := os.Runs(); len(runs) != 0 {
		t.Fatalf("open config ran processes: %+v", runs)
	}
}

func TestDevcontainerMissingAndInvalidConfig(t *testing.T) {
	t.Parallel()
	host, os := devcontainerHost(t)
	h := startDevcontainer(t, host, os)
	if err := h.Run(devcontainerPlugin+".show_info", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "no .devcontainer") {
		t.Fatalf("missing notice = %q", got)
	}
	if len(host.Buffers) != 1 { // just /w/a.go; no info panel was opened
		t.Fatalf("missing config opened a panel: %+v", host.Buffers)
	}

	host2, os2 := devcontainerHost(t)
	os2.SetFile("/w/.devcontainer/devcontainer.json", `{"name": "broken",`)
	h2 := startDevcontainer(t, host2, os2)
	if err := h2.Run(devcontainerPlugin+".show_info", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h2.NoticeTexts(), "|"); !strings.Contains(got, "cannot parse") {
		t.Fatalf("invalid notice = %q (log %v)", got, h2.LogText(""))
	}
	if len(host2.Buffers) != 1 {
		t.Fatalf("invalid config opened a panel: %+v", host2.Buffers)
	}
}

func TestDevcontainerPrefersDirectoryConfig(t *testing.T) {
	t.Parallel()
	host, os := devcontainerHost(t)
	os.SetFile("/w/.devcontainer/devcontainer.json", `{"name":"Primary","image":"primary-image"}`)
	os.SetFile("/w/.devcontainer.json", `{"name":"Secondary","image":"secondary-image"}`)
	h := startDevcontainer(t, host, os)
	if err := h.Run(devcontainerPlugin+".show_info", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	var text string
	for _, b := range host.Buffers {
		if b.Info.Virtual {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "primary-image") || strings.Contains(text, "secondary-image") {
		t.Fatalf("priority panel = %q", text)
	}
}

func TestDevcontainerCommandsAreReadOnly(t *testing.T) {
	t.Parallel()
	host, os := devcontainerHost(t)
	h := startDevcontainer(t, host, os)
	var ids []string
	for _, c := range h.Host.Commands[devcontainerPlugin] {
		if !c.Hidden {
			ids = append(ids, c.ID)
		}
	}
	want := []string{devcontainerPlugin + ".open_config", devcontainerPlugin + ".refresh", devcontainerPlugin + ".show_info"}
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %v, want %v", ids, want)
	}
	for _, id := range ids {
		for _, write := range []string{"attach", "rebuild", "run_lifecycle", "logs", "detach"} {
			if strings.Contains(id, write) {
				t.Fatalf("read-only port registered %q", id)
			}
		}
	}
}

func TestDevcontainerSubfolderAndRefresh(t *testing.T) {
	t.Parallel()
	host, os := devcontainerHost(t)
	os.SetFile("/w/.devcontainer/team/devcontainer.json", `{"name":"Team","image":"team-image"}`)
	h := startDevcontainer(t, host, os)
	if err := h.Run(devcontainerPlugin+".show_info", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	var panel *plugintest.FakeBuffer
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Dev Container: Team") {
			panel = b
		}
	}
	if panel == nil || !strings.Contains(strings.Join(panel.Lines, "\n"), "team-image") {
		t.Fatalf("subfolder panel = %+v", panel)
	}
	os.SetFile("/w/.devcontainer/team/devcontainer.json", `{"name":"Team Two","image":"new-image"}`)
	if err := h.Run(devcontainerPlugin+".refresh", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	var text string
	for _, b := range host.Buffers {
		if b.Info.Virtual && b.Info.ID != 0 {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "new-image") || strings.Contains(text, "team-image") {
		t.Fatalf("refreshed panel = %q", text)
	}
}

// An environment value of multibyte characters longer than the display cap is cut at a
// character boundary, not emptied: the clip used to drop every trailing non-ASCII byte.
func TestDevcontainerClipKeepsMultibyteValues(t *testing.T) {
	t.Parallel()
	host, os := devcontainerHost(t)
	os.SetFile("/w/.devcontainer/devcontainer.json",
		`{"name":"Demo Dev","image":"alpine","containerEnv":{"LONG":"`+strings.Repeat("容器", 200)+`"}}`)
	h := startDevcontainer(t, host, os)
	if err := h.Run(devcontainerPlugin+".show_info", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	for _, b := range host.Buffers {
		if !b.Info.Virtual || !strings.Contains(b.Info.Name, "Dev Container: Demo Dev") {
			continue
		}
		for _, line := range b.Lines {
			if strings.Contains(line, "LONG = 容器") {
				if !strings.HasSuffix(line, "…") || !utf8.ValidString(line) {
					t.Fatalf("line = %q", line)
				}
				return
			}
		}
	}
	t.Fatalf("no clipped env line (notices %v log %v)", h.NoticeTexts(), h.LogText(""))
}
