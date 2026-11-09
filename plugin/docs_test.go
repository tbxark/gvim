package plugin_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The documentation tests (FINAL section 11, the "docs" suite): the plugin author docs under
// docs/plugins are checked against the runtime instead of by eye, so they cannot drift.
//
//   - TestStubsMatchRuntime: gvim.d.lua declares exactly the functions and values a live plugin
//     state has.
//   - TestDocsCoverTheAPI: every function of a live state is mentioned in the Markdown, and every
//     gvim.x.y( the Markdown mentions exists.
//   - TestDocsLuaFencesCompile: every ```lua block of the Markdown is valid Lua.
//   - TestDocsLinksResolve: every relative link and anchor of the Markdown exists.
//   - TestDocsExamples: every file of docs/plugins/examples loads and runs on plugintest.

const docsDir = "../docs/plugins"

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(docsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// docFiles returns the names of the Markdown files of docs/plugins.
func docFiles(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("no Markdown files in docs/plugins")
	}
	return out
}

// liveInventory loads a plugin that touches gvim.git and returns what its state exposes.
func liveInventory(t *testing.T) plugin.Inventory {
	t.Helper()
	h := plugintest.New(t, plugintest.Files{"inv.lua": "local gvim = require 'gvim'\nlocal _ = gvim.git"}, plugintest.Options{})
	h.Start()
	inv, ok := plugin.APIInventory(h.RT, "inv")
	if !ok {
		t.Fatalf("plugin did not load: %v", h.LogText(""))
	}
	return inv
}

var (
	stubFuncRE  = regexp.MustCompile(`(?m)^function ([A-Za-z_][A-Za-z0-9_.]*)([.:])([A-Za-z_][A-Za-z0-9_]*)\(`)
	stubValueRE = regexp.MustCompile(`(?m)^(gvim(?:\.[A-Za-z_][A-Za-z0-9_]*)+) = (.+)$`)
)

// diffSets returns the sorted members of a that are not in b.
func diffSets(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func TestStubsMatchRuntime(t *testing.T) {
	src := readDoc(t, "gvim.d.lua")
	var funcs, values, methods []string
	for _, m := range stubFuncRE.FindAllStringSubmatch(src, -1) {
		switch m[2] {
		case ":":
			methods = append(methods, m[1]+":"+m[3])
		default:
			funcs = append(funcs, m[1]+"."+m[3])
		}
	}
	for _, m := range stubValueRE.FindAllStringSubmatch(src, -1) {
		if strings.TrimSpace(m[2]) != "{}" { // a namespace table, not a value
			values = append(values, m[1])
		}
	}
	inv := liveInventory(t)
	for _, row := range []struct {
		what           string
		stubs, runtime []string
	}{
		{"functions", funcs, inv.Funcs},
		{"values", values, inv.Values},
		{"handle methods", methods, inv.Methods},
	} {
		if len(row.stubs) == 0 {
			t.Errorf("gvim.d.lua declares no %s: the parser found nothing", row.what)
		}
		if extra := diffSets(row.stubs, row.runtime); len(extra) > 0 {
			t.Errorf("gvim.d.lua declares %s the runtime does not have:\n  %s", row.what, strings.Join(extra, "\n  "))
		}
		if missing := diffSets(row.runtime, row.stubs); len(missing) > 0 {
			t.Errorf("the runtime has %s that gvim.d.lua does not declare:\n  %s", row.what, strings.Join(missing, "\n  "))
		}
	}
	// the global aliases the docs promise
	for _, g := range []string{"json = gvim.json", "re = gvim.re", "utf8 = gvim.utf8", "bit = gvim.bit"} {
		if !strings.Contains(src, "\n"+g+"\n") {
			t.Errorf("gvim.d.lua lacks the line %q", g)
		}
	}
}

// globalLibs are the areas that are also globals; the docs write json.encode, not gvim.json.encode.
var globalLibs = map[string]bool{"json": true, "re": true, "utf8": true, "bit": true}

func TestDocsCoverTheAPI(t *testing.T) {
	var all strings.Builder
	for _, name := range docFiles(t) {
		all.WriteString(readDoc(t, name))
		all.WriteString("\n")
	}
	docs := all.String()
	inv := liveInventory(t)

	mentioned := func(path string) bool {
		if strings.Contains(docs, path) {
			return true
		}
		rest := strings.TrimPrefix(path, "gvim.")
		if first, _, ok := strings.Cut(rest, "."); ok && globalLibs[first] {
			return strings.Contains(docs, rest)
		}
		return false
	}
	for _, f := range append(append([]string(nil), inv.Funcs...), inv.Values...) {
		if !mentioned(f) {
			t.Errorf("%s is not mentioned in docs/plugins/*.md", f)
		}
	}
	for _, m := range inv.Methods {
		kind, name, _ := strings.Cut(m, ":")
		if !regexp.MustCompile(`:` + regexp.QuoteMeta(name) + `\b`).MatchString(docs) {
			t.Errorf("the method %s (:%s) is not mentioned in docs/plugins/*.md", m, kind+"/"+name)
		}
	}

	// the other direction: no gvim.x.y( of the docs is made up
	known := map[string]bool{}
	for _, f := range inv.Funcs {
		known[f] = true
	}
	ref := regexp.MustCompile(`\bgvim((?:\.[a-z_][a-z0-9_]*)+)\(`)
	for _, m := range ref.FindAllStringSubmatch(docs, -1) {
		if path := "gvim" + m[1]; !known[path] {
			t.Errorf("the docs call %s(, which the runtime does not have", path)
		}
	}
}

var fenceRE = regexp.MustCompile("(?s)```lua\n(.*?)```")

func TestDocsLuaFencesCompile(t *testing.T) {
	n := 0
	for _, name := range docFiles(t) {
		for i, m := range fenceRE.FindAllStringSubmatch(readDoc(t, name), -1) {
			n++
			if _, err := plugin.CompileSource(fmt.Sprintf("%s#%d", name, i+1), m[1]); err != nil {
				t.Errorf("%s, lua block %d does not compile: %v\n%s", name, i+1, err, m[1])
			}
		}
	}
	if n == 0 {
		t.Fatal("no lua blocks found")
	}
}

var (
	linkRE    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	headingRE = regexp.MustCompile(`(?m)^#{1,6} +(.+?) *$`)
)

// slug is the anchor GitHub makes of a heading: lower case, punctuation dropped, spaces to hyphens.
func slug(h string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(h) {
		switch {
		case r == ' ' || r == '-':
			sb.WriteByte('-')
		case r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r > 127 && r != '⏳':
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func TestDocsLinksResolve(t *testing.T) {
	anchors := map[string]map[string]bool{}
	for _, name := range docFiles(t) {
		set := map[string]bool{}
		for _, m := range headingRE.FindAllStringSubmatch(readDoc(t, name), -1) {
			set[slug(strings.ReplaceAll(m[1], "`", ""))] = true
		}
		anchors[name] = set
	}
	for _, name := range docFiles(t) {
		for _, m := range linkRE.FindAllStringSubmatch(readDoc(t, name), -1) {
			target := m[1]
			if strings.Contains(target, "://") {
				continue
			}
			file, frag, _ := strings.Cut(target, "#")
			if file == "" {
				file = name
			}
			if strings.HasSuffix(file, "/") {
				if _, err := os.Stat(filepath.Join(docsDir, file)); err != nil {
					t.Errorf("%s links to %s, which does not exist", name, target)
				}
				continue
			}
			if _, err := os.Stat(filepath.Join(docsDir, file)); err != nil {
				t.Errorf("%s links to %s, which does not exist", name, target)
				continue
			}
			if frag != "" && strings.HasSuffix(file, ".md") && !anchors[file][frag] {
				t.Errorf("%s links to %s, but %s has no heading with that anchor", name, target, file)
			}
		}
	}
}

// exampleRow checks one example plugin: setup prepares the host, check drives the plugin and asserts.
type exampleRow struct {
	setup func(h *plugintest.FakeHost)
	check func(t *testing.T, h *plugintest.Harness)
}

func hostText(h *plugintest.FakeHost, i int) string { return strings.Join(h.Buffers[i].Lines, "\n") }

var docExamples = map[string]exampleRow{
	"hello": {
		setup: func(h *plugintest.FakeHost) { h.AddBuffer("/w/main.go", "package main") },
		check: func(t *testing.T, h *plugintest.Harness) {
			if err := h.Run("hello.hello", nil); err != nil {
				t.Fatal(err)
			}
			if got := h.NoticeTexts(); len(got) != 1 || got[0] != "Hello from main.go" {
				t.Fatalf("notices = %q, want the greeting", got)
			}
		},
	},
	"dashboard_section": {
		setup: func(*plugintest.FakeHost) {},
		check: func(t *testing.T, h *plugintest.Harness) {
			if err := h.Run("dashboard_section.report", nil); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "demo 0/1") {
				t.Fatalf("notices = %q, want the registered section report", got)
			}
		},
	},
	"bookmarks": {
		setup: func(h *plugintest.FakeHost) { h.AddBuffer("/w/a.go", "package a\n\tfunc f() {}\nvar x = 1") },
		check: func(t *testing.T, h *plugintest.Harness) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(h.Host.SetCursor(1, pluginapi.Pos{Line: 1, Col: 3}, false, false, false))
			must(h.Run("bookmarks.toggle", nil))
			prompt := h.Host.UI().Last("input")
			if prompt == nil || prompt.Input.Default != "func f() {}" {
				t.Fatalf("the name prompt = %+v, want the trimmed line as the default", prompt)
			}
			h.Resolve(pluginapi.Completion{Token: prompt.Token, Values: []any{"the function"}})
			items := h.Host.Decor().Items("bookmarks", "bookmarks", 1)
			if len(items) != 1 || items[0].Kind != pluginapi.DecorGutter || items[0].Line != 1 {
				t.Fatalf("gutter items = %+v, want one on line 1", items)
			}
			// the list command offers it, and choosing it moves the cursor
			must(h.Host.SetCursor(1, pluginapi.Pos{}, false, false, false))
			must(h.Run("bookmarks.go", nil))
			pick := h.Host.UI().Last("picker")
			if pick == nil || len(pick.Picker.Items) != 1 || pick.Picker.Items[0].Text != "the function  (line 2)" {
				t.Fatalf("picker = %+v, want one row with the name and line", pick)
			}
			h.Resolve(pluginapi.Completion{Token: pick.Token, Values: []any{0}})
			cs, _ := h.Host.Cursors(1)
			if cs[0].Pos.Line != 1 {
				t.Fatalf("cursor = %+v, want line 1", cs[0].Pos)
			}
			// toggling the same line removes the bookmark and its mark
			must(h.Run("bookmarks.toggle", nil))
			if got := h.Host.Decor().Items("bookmarks", "bookmarks", 1); len(got) != 0 {
				t.Fatalf("items after removing = %+v", got)
			}
		},
	},
	"todo_highlight": {
		setup: func(h *plugintest.FakeHost) {
			h.AddBuffer("/w/a.go", "// TODO: one\nx := 1\n// FIXME two\n// NOTE")
		},
		check: func(t *testing.T, h *plugintest.Harness) {
			// the plugin scans the open buffers when it loads
			items := h.Host.Decor().Items("todo_highlight", "todo", 1)
			if len(items) != 3 {
				t.Fatalf("highlights = %+v, want TODO, FIXME and NOTE", items)
			}
			lines := map[int]bool{}
			for _, it := range items {
				lines[it.Range.From.Line] = true
				if it.Kind != pluginapi.DecorHighlight {
					t.Fatalf("item %+v is not a highlight", it)
				}
			}
			if !lines[0] || !lines[2] || !lines[3] {
				t.Fatalf("highlighted lines = %v, want 0, 2 and 3", lines)
			}
		},
	},
	"trim_on_save": {
		setup: func(h *plugintest.FakeHost) { h.AddBuffer("/w/a.go", "a  \nb\nc \t\n") },
		check: func(t *testing.T, h *plugintest.Harness) {
			cancel, _ := h.RT.Veto(pluginapi.Event{Name: "before_file_save", Data: map[string]any{
				"buf": pluginapi.BufferID(1), "path": "/w/a.go", "source": "manual"}})
			if cancel {
				t.Fatal("the plugin vetoed the save")
			}
			if got, want := hostText(h.Host, 0), "a\nb\nc\n"; got != want {
				t.Fatalf("text after the save hook = %q, want %q", got, want)
			}
			// a second save has nothing to trim and makes no edit
			before := h.Host.CallCount("Edit")
			h.RT.Veto(pluginapi.Event{Name: "before_file_save", Data: map[string]any{"buf": pluginapi.BufferID(1), "path": "/w/a.go", "source": "autosave"}})
			if h.Host.CallCount("Edit") != before {
				t.Fatal("a clean buffer was edited")
			}
		},
	},
}

func TestDocsExamples(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(docsDir, "examples", "*.lua"))
	if err != nil || len(files) == 0 {
		t.Fatalf("examples: %v, %d files", err, len(files))
	}
	seen := map[string]bool{}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".lua")
		seen[name] = true
		row, ok := docExamples[name]
		if !ok {
			t.Errorf("examples/%s.lua has no row in docExamples: add one that runs it", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			host := plugintest.NewFakeHost()
			row.setup(host)
			h := plugintest.New(t, plugintest.Files{name + ".lua": string(src)}, plugintest.Options{Host: host, Timeout: 5 * time.Second})
			h.Start()
			if p, ok := h.Plugin(name); !ok || p.Status != "loaded" {
				t.Fatalf("plugin status = %+v, log %v", p, h.LogText(""))
			}
			row.check(t, h)
			for _, line := range h.LogText(name) {
				if strings.Contains(line, "error") {
					t.Errorf("the example logged an error: %s", line)
				}
			}
		})
	}
	for name := range docExamples {
		if !seen[name] {
			t.Errorf("docExamples has a row for %s, but examples/%s.lua does not exist", name, name)
		}
	}
}

// TestDocsQuotasMatchLimits keeps the quota table of limits.md in step with the constants.
func TestDocsQuotasMatchLimits(t *testing.T) {
	doc := readDoc(t, "limits.md")
	rows := []struct {
		resource string
		value    int
	}{
		{"event subscriptions", plugin.QuotaSubscriptions},
		{"commands", plugin.QuotaCommands},
		{"timers", plugin.QuotaTimers},
		{"live processes", plugin.QuotaProcesses},
		{"decoration namespaces", plugin.QuotaNamespaces},
		{"key bindings", plugin.QuotaBindings},
		{"status items", plugin.QuotaStatusItems},
		{"in-flight background jobs", plugin.QuotaWork},
		{"parked tasks", plugin.QuotaParkedTasks},
		{"panels mounted", plugin.QuotaPanels},
		{"menu bar rows", plugin.QuotaMenuItems},
	}
	for _, r := range rows {
		want := fmt.Sprintf("| %s", r.resource)
		i := strings.Index(doc, want)
		if i < 0 {
			t.Errorf("limits.md has no quota row for %q", r.resource)
			continue
		}
		line := doc[i : i+strings.IndexByte(doc[i:], '\n')]
		if !strings.Contains(line, fmt.Sprintf("| %d", r.value)) {
			t.Errorf("limits.md row %q = %q, want the value %d of the constant", r.resource, line, r.value)
		}
	}
	for _, c := range []struct {
		phrase    string // as written in limits.md or api-data.md
		constant  int    // the constant of limits.go
		documents int    // the value the phrase stands for
	}{
		{"64 MiB", plugin.MaxString, 64 << 20},
		{"1 MiB", plugin.MaxFormat, 1 << 20},
		{"1 000 000 nodes", plugin.MaxJSONNodes, 1_000_000},
		{"depth 200", plugin.MaxJSONDepth, 200},
		{"500 000", plugin.MaxSortN, 500_000},
		{"8192", plugin.MaxPadWidth, 8192},
		{"1024 bytes", plugin.MaxDateFormat, 1024},
		{"1 000 000 pieces", plugin.MaxSplitParts, 1_000_000},
		{"256 KiB each", plugin.MaxLuaFile, 256 << 10},
	} {
		if c.constant != c.documents {
			t.Errorf("the cap %q is now %d: update limits.md and api-data.md", c.phrase, c.constant)
		}
		if !strings.Contains(doc, c.phrase) && !strings.Contains(readDoc(t, "api-data.md"), c.phrase) {
			t.Errorf("limits.md does not mention the cap %q", c.phrase)
		}
	}
}

// TestDocsStateKnownBehaviour pins statements of the docs that plugin authors and vi users
// ran into: what the runtime does differs from what one would assume. The behaviour itself
// is tested where it lives: TestUnloadAndShutdownHooksCannotWait, TestSelectIndexIsOneBased,
// TestBufVirtualReadOnlyAndMode, TestGrepScopeIsTheFileIndex (internal/app) and
// TestViOwnsOnlyDocumentedCtrlKeys (plugin/bundled). This keeps the docs from losing the note.
// Whitespace is collapsed, so a line break in the Markdown does not matter.
func TestDocsStateKnownBehaviour(t *testing.T) {
	tests := []struct {
		file    string
		must    []string
		mustNot []string
	}{
		{
			file:    "api-core.md",
			must:    []string{"no status-bar counter", "cannot touch files"},
			mustNot: []string{"`[warn N]`", "a file you wrote"},
		},
		{
			file:    "api-ui.md",
			must:    []string{"every item, strings included"},
			mustNot: []string{"(what to show for an item that is not a string)"},
		},
		{
			file: "api-buffers.md",
			must: []string{
				"resolved against the workspace root",
				"opens a new empty buffer",
				"set_read_only(false)",
				"`bash`",
			},
		},
		{
			file: "api-system.md",
			must: []string{
				"relative to the workspace root",
				"no `truncated` flag",
				"`spawn` takes the same options",
				"`nil, err`",
			},
		},
		{
			file:    "api-commands.md",
			must:    []string{"⌃S"},
			mustNot: []string{`("Ctrl+S")`},
		},
		{
			file:    "README.md",
			must:    []string{"--log-file PATH` appends the notices", "the file is current while gvim runs", "binds `Esc`"},
			mustNot: []string{"is not written", "not implemented"},
		},
		{
			file: "../../plugin/bundled/plugins/vi_mode/README.md",
			must: []string{"Vim's insert-mode ctrl keys", "`ctrl+w`"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			doc := strings.Join(strings.Fields(readDoc(t, tt.file)), " ")
			for _, s := range tt.must {
				if !strings.Contains(doc, s) {
					t.Errorf("%s does not say %q", tt.file, s)
				}
			}
			for _, s := range tt.mustNot {
				if strings.Contains(doc, s) {
					t.Errorf("%s still says %q", tt.file, s)
				}
			}
		})
	}
}

// gvim.mode.leave does not end a buffer-local mode, so the docs say how to leave one (RV-06).
func TestDocsSayBufferLocalModesLeaveWithSetMode(t *testing.T) {
	doc := readDoc(t, "api-commands.md")
	for _, want := range []string{"`gvim.mode.leave()` does not end a buffer-local mode", "`b:set_mode(nil)`"} {
		if !strings.Contains(doc, want) {
			t.Errorf("api-commands.md lacks %q", want)
		}
	}
}
