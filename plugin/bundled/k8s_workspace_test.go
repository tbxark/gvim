package bundled_test

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const k8sPlugin = "k8s_workspace"

func k8sFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", k8sPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+k8sPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[k8sPlugin+"/"+name] = string(data)
	}
	return files
}

func k8sHost(t *testing.T) (*plugintest.FakeHost, *plugintest.FakeOS) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/w"
	host.AddBuffer("/w/a.go", "package a\n")
	os := plugintest.NewFakeOS()
	os.SetFile("/w/a.go", "package a\n")
	return host, os
}

func startK8s(t *testing.T, host *plugintest.FakeHost, os *plugintest.FakeOS) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, k8sFiles(t), plugintest.Options{Host: host, OS: os, Timeout: 2 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func k8sValidConfig() string {
	return `{
		// team-shared targets
		"defaultTarget": "dev",
		"targets": {
			"dev": {
				"provider": { "kind": "attach-existing", "context": "kind-dev", "namespace": "team" },
				"workspace": "/workspace",
				"vars": { "USER_ALIAS": "me" },
				"confirmCreate": false,
			},
			"build": {
				"provider": { "kind": "run", "name": "dev-${user}", "image": "alpine:3", "namespace": "team", "context": "kind-dev", "waitTimeoutSec": 60 },
				"workspace": "/src",
			},
			"custom": {
				"provider": {
					"kind": "command",
					"up": { "command": "terraform", "args": ["apply", "-auto-approve"], "cwd": "infra" },
					"down": { "command": "terraform", "args": ["destroy"] },
				},
			},
		},
	}`
}

func k8sPanel(t *testing.T, host *plugintest.FakeHost) *plugintest.FakeBuffer {
	t.Helper()
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "K8s Workspace") {
			return b
		}
	}
	return nil
}

func TestK8sWorkspaceShowsValidConfig(t *testing.T) {
	t.Parallel()
	host, os := k8sHost(t)
	os.SetFile("/w/.fresh/k8s.json", k8sValidConfig())
	h := startK8s(t, host, os)
	if err := h.Run(k8sPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	panel := k8sPanel(t, host)
	if panel == nil {
		t.Fatalf("no panel (notices %v log %v)", h.NoticeTexts(), h.LogText(""))
	}
	text := strings.Join(panel.Lines, "\n")
	for _, want := range []string{
		"Config: /w/.fresh/k8s.json",
		"Default target: dev",
		"provider: attach-existing",
		"context: kind-dev",
		"namespace: team",
		"workspace: /workspace",
		"vars: USER_ALIAS = me",
		"confirmCreate: false",
		"provider: run",
		"run: dev-${user} · image alpine:3",
		"wait timeout: 60s",
		"provider: command",
		"command: terraform apply -auto-approve",
		"cwd: infra",
		"down: terraform destroy",
		"Configuration is valid",
		"does not run kubectl",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("panel missing %q:\n%s", want, text)
		}
	}
	if runs := os.Runs(); len(runs) != 0 {
		t.Fatalf("k8s config inspection ran processes: %+v", runs)
	}

	// Refresh re-reads the config and replaces an open panel's content.
	os.SetFile("/w/.fresh/k8s.json", strings.Replace(k8sValidConfig(), `"defaultTarget": "dev"`, `"defaultTarget": "build"`, 1))
	if err := h.Run(k8sPlugin+".refresh", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	panel = k8sPanel(t, host)
	if panel == nil || !strings.Contains(strings.Join(panel.Lines, "\n"), "Default target: build") {
		t.Fatalf("refreshed panel = %+v", panel)
	}
}

func TestK8sWorkspaceValidationAndInvalidJSON(t *testing.T) {
	t.Parallel()
	host, os := k8sHost(t)
	os.SetFile("/w/.fresh/k8s.json", `{
		"defaultTarget": "missing",
		"targets": {
			"broken": { "provider": { "kind": "manifest" } },
			"unknown": { "provider": { "kind": "nope" } },
		},
	}`)
	h := startK8s(t, host, os)
	if err := h.Run(k8sPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	panel := k8sPanel(t, host)
	if panel == nil {
		t.Fatal("no validation panel")
	}
	text := strings.Join(panel.Lines, "\n")
	for _, want := range []string{
		"Configuration errors",
		"defaultTarget 'missing' is not a target",
		"provider.template is required",
		"provider.namespace is required",
		"unknown provider.kind nope",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("validation panel missing %q:\n%s", want, text)
		}
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "configuration error") {
		t.Fatalf("validation notice = %q", got)
	}

	host2, os2 := k8sHost(t)
	os2.SetFile("/w/.fresh/k8s.json", `{"targets": {`)
	h2 := startK8s(t, host2, os2)
	if err := h2.Run(k8sPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h2.Advance(0)
	panel2 := k8sPanel(t, host2)
	if panel2 == nil || !strings.Contains(strings.Join(panel2.Lines, "\n"), "cannot parse JSONC") {
		t.Fatalf("invalid JSON panel = %+v notices %v", panel2, h2.NoticeTexts())
	}
	if got := strings.Join(h2.NoticeTexts(), "|"); !strings.Contains(got, "failed to parse") {
		t.Fatalf("invalid JSON notice = %q", got)
	}
}

func TestK8sWorkspaceOpenConfigAndMissing(t *testing.T) {
	t.Parallel()
	host, os := k8sHost(t)
	os.SetFile("/w/.fresh/k8s.json", `{"targets":{"x":{"provider":{"kind":"attach-existing"}}}}`)
	h := startK8s(t, host, os)
	if err := h.Run(k8sPlugin+".open", nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteBuffers()
	opened := false
	for _, c := range host.Calls() {
		if c.Method == "Open" {
			if req, ok := c.Args[1].(pluginapi.OpenRequest); ok && req.Path == "/w/.fresh/k8s.json" && req.Focus {
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

	host2, os2 := k8sHost(t)
	h2 := startK8s(t, host2, os2)
	if err := h2.Run(k8sPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h2.NoticeTexts(), "|"); !strings.Contains(got, "no .fresh/k8s.json") {
		t.Fatalf("missing notice = %q", got)
	}
	if k8sPanel(t, host2) != nil {
		t.Fatal("missing config opened a panel")
	}
}

func TestK8sWorkspaceCommandsAreReadOnly(t *testing.T) {
	t.Parallel()
	host, os := k8sHost(t)
	h := startK8s(t, host, os)
	var ids []string
	for _, c := range h.Host.Commands[k8sPlugin] {
		if !c.Hidden {
			ids = append(ids, c.ID)
		}
	}
	want := []string{k8sPlugin + ".open", k8sPlugin + ".refresh", k8sPlugin + ".show"}
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %v, want %v", ids, want)
	}
	for _, id := range ids {
		for _, write := range []string{"connect", "disconnect", "attach", "kubectl", "run"} {
			if strings.Contains(id, write) {
				t.Fatalf("read-only port registered %q", id)
			}
		}
	}
}

func k8sConfigWithVars(n int) string {
	var b strings.Builder
	b.WriteString(`{"targets":{"t":{"provider":{"kind":"attach-existing"},"vars":{`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"K%d":"v%d"`, i, i)
	}
	b.WriteString(`}}}}`)
	return b.String()
}

func TestK8sWorkspaceVarsLimitIsInclusive(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n       int
		wantBad bool
	}{
		{100, false},
		{101, true},
	} {
		t.Run(fmt.Sprintf("%d vars", tc.n), func(t *testing.T) {
			host, os := k8sHost(t)
			os.SetFile("/w/.fresh/k8s.json", k8sConfigWithVars(tc.n))
			h := startK8s(t, host, os)
			if err := h.Run(k8sPlugin+".show", nil); err != nil {
				t.Fatal(err)
			}
			h.Advance(0)
			panel := k8sPanel(t, host)
			if panel == nil {
				t.Fatal("no panel")
			}
			text := strings.Join(panel.Lines, "\n")
			if got := strings.Contains(text, "vars is too large"); got != tc.wantBad {
				t.Fatalf("vars-too-large reported = %v, want %v:\n%s", got, tc.wantBad, text)
			}
			if !tc.wantBad && !strings.Contains(text, "Configuration is valid") {
				t.Fatalf("100 vars panel not valid:\n%s", text)
			}
		})
	}
}

func k8sConfigWithTargets(n int) string {
	var b strings.Builder
	b.WriteString(`{"targets":{`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"t%d":{"provider":{"kind":"attach-existing"}}`, i)
	}
	b.WriteString(`}}`)
	return b.String()
}

func TestK8sWorkspaceTargetsLimitIsInclusive(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n       int
		wantBad bool
	}{
		{100, false},
		{101, true},
	} {
		t.Run(fmt.Sprintf("%d targets", tc.n), func(t *testing.T) {
			host, os := k8sHost(t)
			os.SetFile("/w/.fresh/k8s.json", k8sConfigWithTargets(tc.n))
			h := startK8s(t, host, os)
			if err := h.Run(k8sPlugin+".show", nil); err != nil {
				t.Fatal(err)
			}
			h.Advance(0)
			panel := k8sPanel(t, host)
			if panel == nil {
				t.Fatal("no panel")
			}
			text := strings.Join(panel.Lines, "\n")
			if got := strings.Contains(text, "too many targets"); got != tc.wantBad {
				t.Fatalf("too-many-targets reported = %v, want %v:\n%s", got, tc.wantBad, text)
			}
		})
	}
}
