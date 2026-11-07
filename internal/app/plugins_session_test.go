package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui"
)

// pluginSession drives the real runtime of an app (buildRuntime: the real editor, the real plugin
// runtime and OS, temp directories) the way Bubble Tea would: the test goroutine is the
// update loop, and the commands the model returns run on goroutines whose messages come back
// through a channel.
type pluginSession struct {
	t    *testing.T
	rt   *runtime
	m    *ui.Model
	plug pluginapi.Runtime // the plugin runtime the model holds; nil when plugins are off
	in   chan tea.Msg
	stop chan struct{}
}

var cmdType = reflect.TypeFor[tea.Cmd]()

// pluginConfigDir is where isolateConfig points the user's gvim config directory.
func pluginConfigDir() string { return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "gvim") }

// writeUserFile writes a file under the user's config directory.
func writeUserFile(t *testing.T, rel, text string) string {
	t.Helper()
	path := filepath.Join(pluginConfigDir(), filepath.FromSlash(rel))
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(text), 0o644))
	return path
}

// newSession builds the app with opts and starts the model: the window size, then Init.
// The caller has called isolateConfig and written the plugins and the config it wants.
func newSession(t *testing.T, opts Options) *pluginSession {
	t.Helper()
	// The plugin runtime panics when it is called from a goroutine other than the one that
	// started it, so every test here also checks the single-goroutine rule.
	t.Setenv("GVIM_PLUGIN_DEBUG", "1")
	s := &pluginSession{t: t, in: make(chan tea.Msg, 4096), stop: make(chan struct{})}
	prev := newUI
	t.Cleanup(func() { newUI = prev })
	newUI = func(d ui.Deps) *ui.Model {
		s.plug = d.Root.Plugins
		return prev(d)
	}
	if opts.Root == "" {
		opts.Root = t.TempDir()
	}
	if opts.Keymap == "" {
		opts.Keymap = "macos"
	}
	rt, err := buildRuntime(t.Context(), opts, false)
	must(t, err)
	s.rt, s.m = rt, rt.model
	t.Cleanup(func() {
		close(s.stop)
		rt.close()
	})
	// Bubble Tea calls Init before it delivers the first window size.
	s.run(s.m.Init())
	s.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	return s
}

// send handles msg in the model and starts the command it returns.
func (s *pluginSession) send(msg tea.Msg) {
	s.t.Helper()
	_, cmd := s.m.Update(msg)
	s.run(cmd)
}

// run starts cmd on a goroutine. Its message, and the messages of a batch or a sequence,
// go to the channel.
func (s *pluginSession) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() { s.route(cmd()) }()
}

func (s *pluginSession) route(msg tea.Msg) {
	switch v := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range v {
			s.run(c)
		}
		return
	}
	if rv := reflect.ValueOf(msg); rv.Kind() == reflect.Slice && rv.Type().Elem() == cmdType {
		for i := range rv.Len() {
			if c, _ := rv.Index(i).Interface().(tea.Cmd); c != nil {
				s.route(c())
			}
		}
		return
	}
	select {
	case s.in <- msg:
	case <-s.stop:
	}
}

// until handles the messages that arrive until cond holds, and fails after 10 seconds.
func (s *pluginSession) until(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %s\n%s", what, strings.Join(s.rows(), "\n"))
		}
		select {
		case msg := <-s.in:
			s.send(msg)
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// settle handles messages until none has come for quiet, or for a second at most: a plugin
// timer keeps messages coming for as long as it runs.
func (s *pluginSession) settle(quiet time.Duration) {
	s.t.Helper()
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		select {
		case msg := <-s.in:
			s.send(msg)
		case <-time.After(quiet):
			return
		}
	}
}

// pump handles the messages that arrive during d.
func (s *pluginSession) pump(d time.Duration) {
	s.t.Helper()
	end := time.After(d)
	for {
		select {
		case msg := <-s.in:
			s.send(msg)
		case <-end:
			return
		}
	}
}

// rows returns the painted rows without styles.
func (s *pluginSession) rows() []string {
	return strings.Split(ansi.Strip(s.m.View().Content), "\n")
}

// screenHas reports whether some painted row contains sub.
func (s *pluginSession) screenHas(sub string) bool {
	for _, r := range s.rows() {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

// statusRow is the last painted row, which holds the notice.
func (s *pluginSession) statusRow() string {
	rows := s.rows()
	return strings.TrimSpace(rows[len(rows)-1])
}

func (s *pluginSession) press(k tea.KeyPressMsg) { s.t.Helper(); s.send(k) }

func (s *pluginSession) typeText(text string) {
	s.t.Helper()
	for _, r := range text {
		s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// loaded waits until the plugin has the status in the runtime's plugin list.
func (s *pluginSession) waitStatus(name, status string) {
	s.t.Helper()
	s.until("plugin "+name+" to be "+status, func() bool {
		for _, p := range s.plug.Plugins() {
			if p.Name == name {
				return p.Status == status
			}
		}
		return false
	})
}

func (s *pluginSession) status(name string) string {
	for _, p := range s.plug.Plugins() {
		if p.Name == name {
			return p.Status
		}
	}
	return ""
}

// runCommand runs a palette command by its title: it opens the palette, types the query and
// presses Enter on the first row.
func (s *pluginSession) runCommand(title string) {
	s.t.Helper()
	s.press(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}) // macos: command palette
	s.typeText(title)
	s.until("the palette row "+title, func() bool { return s.screenHas(title) })
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.settle(20 * time.Millisecond)
}

// openFile opens a file with the given text in a new tab and returns its path.
func (s *pluginSession) openFile(name, text string) string {
	s.t.Helper()
	path := filepath.Join(s.t.TempDir(), name)
	must(s.t, os.WriteFile(path, []byte(text), 0o644))
	must(s.t, s.rt.ed.OpenFile(path))
	return path
}

func (s *pluginSession) text() string { return s.rt.ed.Active().Buffer.Text() }
