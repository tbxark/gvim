package bundled_test

import (
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The rig of the git ports: bundled plugins and a probe plugin over a fake host and a fake OS
// whose git binary answers scripted output. Nothing here runs git.

// gitHead is the part of every git command line the lib adds.
var gitHead = []string{
	"git", "-c", "core.fsmonitor=false", "-c", "core.pager=cat", "-c", "diff.external=",
	"-c", "core.attributesFile=/dev/null", "-c", "protocol.ext.allow=never",
}

// gitArgv is the full command line the lib builds for git's own arguments.
func gitArgv(args ...string) []string { return append(slices.Clone(gitHead), args...) }

// filtersArgv is the command that lists the filter drivers of a repository.
var filtersArgv = gitArgv("config", "-z", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)

// showArgv is the command line that reads file from HEAD.
func showArgv(file string) []string {
	return gitArgv("show", "--no-ext-diff", "--no-textconv", "HEAD:./"+file)
}

type rigBuffer struct{ path, text string }

type rigOpts struct {
	locale   string             // plugin translation locale
	noGit    bool               // no git binary on the PATH of the fake OS
	services pluginapi.Services // nil: the fake services
	timeout  time.Duration      // plugin_timeout_ms; 0: 200 ms
	buffers  []rigBuffer        // open before the plugins load (the first is active)
	script   func(o *plugintest.FakeOS)
	plugins  []string          // bundled plugins to load
	probe    string            // Lua source of an extra plugin "probe"
	extra    map[string]string // extra plugin files by path, e.g. "live_diff/init.lua"
	// fileLaunch is the fake host's gvim.workspace.file_launch() value (the
	// immutable startup invocation property).
	fileLaunch bool
	encoding   string // the encoding of the first buffer (default utf-8)
	// settings are the plugins.<name>.settings layers, by plugin name, applied to the
	// matching source before Start.
	settings map[string]plugin.PluginSettings
	// httpClient is the runtime's gvim.http client (nil: the production safe client).
	httpClient *http.Client
	// osWrap wraps the fake OS before the harness uses it (race tests add a gated
	// Run); nil uses the fake OS as-is.
	osWrap func(*plugintest.FakeOS) hostos.OS
}

type rig struct {
	t    *testing.T
	h    *plugintest.Harness
	os   *plugintest.FakeOS
	host *plugintest.FakeHost
	ids  map[string]pluginapi.BufferID
}

func startRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range o.plugins {
		src, err := fs.ReadFile(bundled.FS, "plugins/"+name+"/init.lua")
		if err != nil {
			t.Fatalf("read the bundled plugin %s: %v", name, err)
		}
		files[name+"/init.lua"] = string(src)
		// A port with its own i18n table gets it too (the rig also loads plugins that
		// hard-code their messages).
		if i18n, err := fs.ReadFile(bundled.FS, "plugins/"+name+"/"+name+".i18n.json"); err == nil {
			files[name+"/"+name+".i18n.json"] = string(i18n)
		}
	}
	if o.probe != "" {
		files["probe/init.lua"] = o.probe
	}
	for name, src := range o.extra {
		files[name] = src
	}
	r := &rig{t: t, host: plugintest.NewFakeHost(), os: plugintest.NewFakeOS(), ids: map[string]pluginapi.BufferID{}}
	r.host.Workspace().FileLaunch = o.fileLaunch
	if !o.noGit {
		r.os.Bin["git"] = "/usr/bin/git"
	}
	if o.script != nil {
		o.script(r.os)
	}
	// A repository without filter drivers, unless the script above answered otherwise.
	r.os.Script(filtersArgv, plugintest.ProcScript{Code: 1})
	for _, b := range o.buffers {
		r.ids[b.path] = r.host.AddBuffer(b.path, b.text)
	}
	if o.encoding != "" && len(r.host.Buffers) > 0 {
		r.host.Buffers[0].Info.Encoding = o.encoding
	}
	timeout := o.timeout
	if timeout == 0 {
		timeout = 200 * time.Millisecond
	}
	osImpl := hostos.OS(r.os)
	if o.osWrap != nil {
		osImpl = o.osWrap(r.os)
	}
	r.h = plugintest.New(t, files, plugintest.Options{Host: r.host, OS: osImpl, Services: o.services, Timeout: timeout, EventBudget: 5 * time.Second, HTTPClient: o.httpClient, Locale: o.locale})
	bundledNames := map[string]bool{}
	for _, name := range o.plugins {
		bundledNames[name] = true
	}
	for i := range r.h.Sources {
		if bundledNames[r.h.Sources[i].Name] {
			r.h.Sources[i].Kind = plugin.SourceBundled
		}
		if st, ok := o.settings[r.h.Sources[i].Name]; ok {
			r.h.Sources[i].Settings = st.User
			r.h.Sources[i].ProjectSettings = st.Project
		}
	}
	r.h.Start()
	return r
}

// emit sends a lifecycle event about the buffer at path.
func (r *rig) emit(name, path string, extra map[string]any) {
	r.t.Helper()
	data := map[string]any{"buf": r.ids[path], "path": path}
	for k, v := range extra {
		data[k] = v
	}
	r.h.Emit(pluginapi.Event{Name: name, Data: data})
}

// edit replaces the whole text of the buffer at path, as typing would.
func (r *rig) edit(path, text string) {
	r.t.Helper()
	b := r.host.Buffers[r.index(path)]
	last := len(b.Lines) - 1
	err := r.host.Edit(r.ids[path], []pluginapi.Edit{{
		Range: pluginapi.Range{To: pluginapi.Pos{Line: last, Col: len(b.Lines[last])}}, Text: text,
	}}, pluginapi.EditOptions{})
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) index(path string) int {
	for i, b := range r.host.Buffers {
		if b.Info.Path == path {
			return i
		}
	}
	r.t.Fatalf("no buffer %s", path)
	return -1
}

// runs counts the git commands run whose arguments after the fixed prefix start with sub.
func (r *rig) runs(sub string) int {
	n := 0
	for _, run := range r.os.Runs() {
		if len(run.Argv) > len(gitHead) && run.Argv[len(gitHead)] == sub {
			n++
		}
	}
	return n
}

// logs returns the plugin log lines at warn or error level.
func (r *rig) problems() []string {
	var out []string
	for _, l := range r.h.Log() {
		if l.Level == "warn" || l.Level == "error" {
			out = append(out, l.Plugin+": "+l.Msg)
		}
	}
	return out
}

func (r *rig) decor(plugin, ns, path string) []pluginapi.Decoration {
	return r.host.Decor().Items(plugin, ns, r.ids[path])
}

// marks reduces gutter decorations to "line:text:color" strings.
func marks(items []pluginapi.Decoration) []string {
	var out []string
	for _, it := range items {
		out = append(out, fmt.Sprintf("%d:%s:%s", it.Line, it.Text, it.Style.FG.Key))
	}
	slices.Sort(out)
	return out
}

func lines(s ...string) string { return strings.Join(s, "\n") }
