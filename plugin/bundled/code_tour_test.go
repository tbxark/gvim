package bundled_test

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const codeTourPlugin = "code_tour"

func codeTourFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", codeTourPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+codeTourPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read the bundled plugin file %s: %v", name, err)
		}
		files[codeTourPlugin+"/"+name] = string(data)
	}
	return files
}

// startCodeTour starts the plugin over a host whose workspace root is /w.
func startCodeTour(t *testing.T, host *plugintest.FakeHost, os *plugintest.FakeOS) *plugintest.Harness {
	t.Helper()
	if host.Workspace().RootDir == "" {
		host.Workspace().RootDir = "/w"
	}
	h := plugintest.New(t, codeTourFiles(t), plugintest.Options{
		Host: host, OS: os, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second,
	})
	h.Start()
	return h
}

// codeTourHost has /w/src/a.go open and /w/src/b.go on disk not open.
func codeTourHost(t *testing.T) (*plugintest.FakeHost, *plugintest.FakeOS) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/w"
	host.AddBuffer("/w/src/a.go", "line one\nline two\nline three\n")
	os := plugintest.NewFakeOS()
	os.Bin["git"] = "/usr/bin/git"
	os.SetFile("/w/src/a.go", "line one\nline two\nline three\n")
	return host, os
}

// codeTourPanels returns the mounted panels.
func codeTourPanels(h *plugintest.Harness) []*plugintest.FakePanel {
	return h.Host.Panels().Mounted()
}

// widgetText collects the strings of a widget tree (labels, values, titles, items).
func widgetText(w pluginapi.Widget) string {
	var b strings.Builder
	if w.Label != "" {
		b.WriteString(w.Label + "\n")
	}
	if w.Text != "" {
		b.WriteString(w.Text + "\n")
	}
	if w.Title != "" {
		b.WriteString(w.Title + "\n")
	}
	for _, item := range w.Items {
		b.WriteString(item.Text + "\n")
	}
	for _, run := range w.Runs {
		b.WriteString(run.Text + "\n")
	}
	for _, child := range w.Children {
		b.WriteString(widgetText(child))
	}
	return b.String()
}

// resolvePrompt answers the current plugin prompt with value.
func resolvePrompt(t *testing.T, h *plugintest.Harness, value string) {
	t.Helper()
	overlay := h.Host.UI().Last("input")
	if overlay == nil || !overlay.Open {
		t.Fatalf("no input prompt open (notices %v log %v)", h.NoticeTexts(), h.LogText(""))
	}
	h.Resolve(pluginapi.Completion{Token: overlay.Token, Values: []any{value}})
}

// codeTourJSON is a two-step Fresh-schema manifest.
func codeTourJSON() string {
	return `{"title":"Demo Tour","description":"","schema_version":"1.0","steps":[
	  {"step_id":1,"title":"first step","file_path":"src/a.go","lines":[1,1],"explanation":"Look at line one"},
	  {"step_id":2,"title":"second step","file_path":"src/a.go","lines":[2,3],"explanation":"Now line two and three"}
	]}`
}

// vscodeTourJSON is a two-step VS Code CodeTour manifest (0-based line).
func vscodeTourJSON() string {
	return `{"title":"VS Tour","steps":[
	  {"title":"vs one","file":"src/a.go","line":0,"description":"first"},
	  {"title":"vs two","file":"src/a.go","line":2,"description":"second"}
	]}`
}

func TestCodeTourManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/code_tour/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name           string `json:"name"`
		API            int    `json:"api"`
		DefaultEnabled *bool  `json:"default_enabled"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != codeTourPlugin || m.API != 2 || m.DefaultEnabled == nil || *m.DefaultEnabled {
		t.Errorf("manifest = %+v", m)
	}
	host, os := codeTourHost(t)
	h := startCodeTour(t, host, os)
	for _, l := range h.LogText("") {
		if strings.HasPrefix(l, "warn:") || strings.HasPrefix(l, "error:") {
			t.Errorf("load logged %s", l)
		}
	}
	if bindings := h.Host.Bindings[codeTourPlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
	want := map[string]string{
		codeTourPlugin + ".load":     "Tour: Load Definition...",
		codeTourPlugin + ".discover": "Tour: Open Workspace Tour...",
		codeTourPlugin + ".next":     "Tour: Next Step",
		codeTourPlugin + ".prev":     "Tour: Previous Step",
		codeTourPlugin + ".exit":     "Tour: Exit",
		codeTourPlugin + ".create":   "Tour: Create from Current Location...",
	}
	for _, c := range h.Host.Commands[codeTourPlugin] {
		if c.Hidden {
			continue
		}
		if title, ok := want[c.ID]; ok {
			if c.Title != title {
				t.Errorf("command %s title = %q, want %q", c.ID, c.Title, title)
			}
			delete(want, c.ID)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing commands: %v", want)
	}
}

func TestCodeTourLoadsAndWalksSteps(t *testing.T) {
	t.Parallel()
	host, os := codeTourHost(t)
	os.SetFile("/w/demo.fresh-tour.json", codeTourJSON())
	h := startCodeTour(t, host, os)
	if err := h.Run(codeTourPlugin+".load", nil); err != nil {
		t.Fatal(err)
	}
	resolvePrompt(t, h, "demo.fresh-tour.json")
	h.CompleteBuffers()
	h.Advance(20 * time.Millisecond)

	panels := codeTourPanels(h)
	if len(panels) != 1 {
		t.Fatalf("panels = %d, want the tour panel (notices %v log %v)", len(panels), h.NoticeTexts(), h.LogText(""))
	}
	panel := panels[0]
	if panel.Mount.Placement != pluginapi.PlacementFloat || panel.Mount.Title != "Tour: Demo Tour" {
		t.Errorf("panel mount = %+v", panel.Mount)
	}
	text := widgetText(panel.Panel.Root)
	for _, want := range []string{"Demo Tour", "first step", "second step", "Look at line one", "Step 1 of 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("panel misses %q:\n%s", want, text)
		}
	}
	// The first step opened src/a.go at line 0 and highlighted it.
	a := host.FindBuffer("/w/src/a.go")
	if a == 0 {
		t.Fatal("src/a.go is not open")
	}
	buf := host.ActiveBuffer()
	if buf != a {
		t.Errorf("active buffer = %d, want src/a.go %d", buf, a)
	}
	decor := host.Decor().Items(codeTourPlugin, "code_tour", a)
	if len(decor) != 1 || decor[0].Kind != pluginapi.DecorHighlight || decor[0].Range.From.Line != 0 || decor[0].Range.To.Line != 0 {
		t.Errorf("step 1 highlight = %+v, want line 0", decor)
	}
	// Next goes to step 2 and highlights lines 1..2.
	if err := h.Run(codeTourPlugin+".next", nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteBuffers()
	h.Advance(20 * time.Millisecond)
	text = widgetText(codeTourPanels(h)[0].Panel.Root)
	if !strings.Contains(text, "Step 2 of 2") || !strings.Contains(text, "Now line two and three") {
		t.Errorf("panel after next:\n%s", text)
	}
	decor = host.Decor().Items(codeTourPlugin, "code_tour", a)
	if len(decor) != 2 || decor[0].Range.From.Line != 1 || decor[1].Range.From.Line != 2 {
		t.Errorf("step 2 highlight = %+v, want lines 1 and 2", decor)
	}
	// Exit unmounts the panel and clears the highlight.
	if err := h.Run(codeTourPlugin+".exit", nil); err != nil {
		t.Fatal(err)
	}
	if len(codeTourPanels(h)) != 0 {
		t.Errorf("panel still mounted after exit")
	}
	if got := host.Decor().Items(codeTourPlugin, "code_tour", a); len(got) != 0 {
		t.Errorf("highlight after exit = %+v", got)
	}
}

// The title row styles its runs: a run's colours and flags sit on the run table itself, a
// nested style table would be ignored.
func TestCodeTourTitleRunIsStyled(t *testing.T) {
	t.Parallel()
	host, os := codeTourHost(t)
	os.SetFile("/w/demo.fresh-tour.json", codeTourJSON())
	h := startCodeTour(t, host, os)
	if err := h.Run(codeTourPlugin+".load", nil); err != nil {
		t.Fatal(err)
	}
	resolvePrompt(t, h, "demo.fresh-tour.json")
	h.CompleteBuffers()
	h.Advance(20 * time.Millisecond)
	panels := codeTourPanels(h)
	if len(panels) != 1 {
		t.Fatalf("panels = %d, want the tour panel", len(panels))
	}
	root := panels[0].Panel.Root
	if len(root.Children) == 0 || len(root.Children[0].Runs) == 0 {
		t.Fatalf("no title runs in %+v", root)
	}
	run := root.Children[0].Runs[0]
	if run.Text != "Demo Tour" || !run.Style.Bold || run.Style.FG.Key != "syntax.keyword" {
		t.Errorf("title run = %+v, want bold syntax.keyword", run)
	}
}

func TestCodeTourReadsVSCodeTours(t *testing.T) {
	t.Parallel()
	host, os := codeTourHost(t)
	os.SetFile("/w/demo.tour", vscodeTourJSON())
	h := startCodeTour(t, host, os)
	if err := h.Run(codeTourPlugin+".load", nil); err != nil {
		t.Fatal(err)
	}
	resolvePrompt(t, h, "/w/demo.tour")
	h.CompleteBuffers()
	h.Advance(20 * time.Millisecond)
	panels := codeTourPanels(h)
	if len(panels) != 1 {
		t.Fatalf("panels = %d (notices %v)", len(panels), h.NoticeTexts())
	}
	text := widgetText(panels[0].Panel.Root)
	if !strings.Contains(text, "VS Tour") || !strings.Contains(text, "vs one") || !strings.Contains(text, "vs two") {
		t.Errorf("VS Code tour panel:\n%s", text)
	}
	decor := host.Decor().Items(codeTourPlugin, "code_tour", host.FindBuffer("/w/src/a.go"))
	if len(decor) != 1 || decor[0].Range.From.Line != 0 {
		t.Errorf("VS Code first step highlight = %+v, want line 0", decor)
	}
}

func TestCodeTourErrors(t *testing.T) {
	t.Parallel()
	host, os := codeTourHost(t)
	os.SetFile("/w/bad.tour", "{ not json")
	os.SetFile("/w/empty.tour", `{"title":"none","steps":[]}`)
	os.SetFile("/w/version.fresh-tour.json", `{"title":"x","schema_version":"9.0","steps":[{"title":"s"}]}`)
	h := startCodeTour(t, host, os)
	cases := []struct {
		path string
		want string
	}{
		{"missing.tour", "Failed to read tour file"},
		{"bad.tour", "Failed to parse"},
		{"empty.tour", "Tour has no steps"},
		{"version.fresh-tour.json", "Unsupported tour schema version"},
	}
	for _, tc := range cases {
		if err := h.Run(codeTourPlugin+".load", nil); err != nil {
			t.Fatal(err)
		}
		resolvePrompt(t, h, tc.path)
		notices := h.NoticeTexts()
		if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], tc.want) {
			t.Errorf("%s notice = %v, want %q", tc.path, notices, tc.want)
		}
	}
	if panels := codeTourPanels(h); len(panels) != 0 {
		t.Errorf("a failed load mounted a panel")
	}
}

func TestCodeTourCreatesATour(t *testing.T) {
	t.Parallel()
	host, os := codeTourHost(t)
	h := startCodeTour(t, host, os)
	host.SetActive(host.FindBuffer("/w/src/a.go"))
	h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": host.ActiveBuffer()}})
	if err := h.Run(codeTourPlugin+".create", nil); err != nil {
		t.Fatal(err)
	}
	resolvePrompt(t, h, "My Tour")
	resolvePrompt(t, h, "/w/.tours/my-tour.tour")
	h.Advance(20 * time.Millisecond)
	data, ok := os.File("/w/.tours/my-tour.tour")
	if !ok || data == "" {
		t.Fatalf("created tour = %q, %v", data, ok)
	}
	if !strings.Contains(data, "My Tour") || !strings.Contains(data, "src/a.go") {
		t.Errorf("created manifest = %s", data)
	}
	if panels := codeTourPanels(h); len(panels) != 1 {
		t.Errorf("created tour did not open its panel")
	}
	if notices := h.NoticeTexts(); !slices.ContainsFunc(notices, func(s string) bool { return strings.Contains(s, "Tour written to") }) {
		t.Errorf("notices = %v, want the created notice", notices)
	}
}

var _ = slices.Equal[[]string]

func TestCodeTourWarnsAboutCommitDrift(t *testing.T) {
	t.Parallel()
	host, os := codeTourHost(t)
	tour := strings.Replace(vscodeTourJSON(), `"title":"VS Tour"`, `"title":"VS Tour","ref":"abc1234"`, 1)
	os.SetFile("/w/drift.tour", tour)
	os.Script(gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD"), plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
	os.Script(gitArgv("rev-parse", "--short", "HEAD"), plugintest.ProcScript{Stdout: "def5678\n"})
	h := startCodeTour(t, host, os)
	if err := h.Run(codeTourPlugin+".load", nil); err != nil {
		t.Fatal(err)
	}
	resolvePrompt(t, h, "/w/drift.tour")
	h.CompleteBuffers()
	h.Advance(20 * time.Millisecond)
	panels := codeTourPanels(h)
	if len(panels) != 1 {
		t.Fatalf("panels = %d (notices %v log %v)", len(panels), h.NoticeTexts(), h.LogText(""))
	}
	if text := widgetText(panels[0].Panel.Root); !strings.Contains(text, "recorded at abc1234 · you are on def5678") {
		t.Fatalf("panel text = %q, want the commit drift warning", text)
	}
}

func TestCodeTourCommitDriftMatchesHEADPrefix(t *testing.T) {
	t.Parallel()
	host, os := codeTourHost(t)
	tour := strings.Replace(vscodeTourJSON(), `"title":"VS Tour"`, `"title":"VS Tour","ref":"def5678"`, 1)
	os.SetFile("/w/same.tour", tour)
	os.Script(gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD"), plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
	os.Script(gitArgv("rev-parse", "--short", "HEAD"), plugintest.ProcScript{Stdout: "def5678\n"})
	h := startCodeTour(t, host, os)
	if err := h.Run(codeTourPlugin+".load", nil); err != nil {
		t.Fatal(err)
	}
	resolvePrompt(t, h, "/w/same.tour")
	h.CompleteBuffers()
	h.Advance(20 * time.Millisecond)
	panels := codeTourPanels(h)
	if len(panels) != 1 {
		t.Fatalf("panels = %d (notices %v log %v)", len(panels), h.NoticeTexts(), h.LogText(""))
	}
	if text := widgetText(panels[0].Panel.Root); strings.Contains(text, "recorded at") {
		t.Fatalf("panel text = %q, want no drift warning for a matching HEAD", text)
	}
}
