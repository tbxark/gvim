package bundled_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// testSliceBudget is the time budget of one slice in these tests. It only bounds a runaway
// plugin; the race detector makes the Lua interpreter many times slower, and these tests
// run 150 000-line buffers and thousands of matches.
const testSliceBudget = 5 * time.Second

// flashEnv is the flash plugin on a fake host with one buffer.
type flashEnv struct {
	h   *plugintest.Harness
	buf pluginapi.BufferID
}

// newFlash loads flash from the embed FS with the settings given (nil: the defaults) over a
// buffer with the text, the caret at the start.
func newFlash(t *testing.T, text string, settings map[string]any) *flashEnv {
	t.Helper()
	src, err := fs.ReadFile(bundled.FS, "plugins/flash/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	host := plugintest.NewFakeHost()
	id := host.AddBuffer("/w/a.txt", text)
	h := plugintest.New(t, plugintest.Files{"flash/init.lua": string(src)}, plugintest.Options{Host: host, Timeout: testSliceBudget, EventBudget: testSliceBudget})
	for i := range h.Sources {
		h.Sources[i].Settings = settings
	}
	h.Start()
	return &flashEnv{h: h, buf: id}
}

// jump starts a session and returns with the plugin waiting for a key.
func (e *flashEnv) jump(t *testing.T) {
	t.Helper()
	if err := e.h.Run("flash.jump", nil); err != nil {
		t.Fatal(err)
	}
	if e.h.Host.Keys().Pending == nil {
		t.Fatalf("no key wait after Flash: Jump; log: %v", e.h.LogText("flash"))
	}
}

// press sends a key; a printable key carries its text.
func (e *flashEnv) press(key string) {
	e.h.T.Helper()
	k := pluginapi.Key{Key: key}
	if utf8.RuneCountInString(key) == 1 {
		k.Text = key
	}
	e.h.PressKey(k)
}

// labels returns the label decorations of the buffer as "line:col=text", in the order the
// plugin set them.
func (e *flashEnv) labels(buf pluginapi.BufferID) []string {
	var out []string
	for _, d := range e.h.Host.Decor().Items("flash", "label", buf) {
		switch d.Kind {
		case pluginapi.DecorReplace:
			out = append(out, itoa(d.Pos.Line)+":"+itoa(d.Pos.Col)+"="+d.Text)
		case pluginapi.DecorEOL:
			out = append(out, itoa(d.Line)+":eol="+d.Text)
		}
	}
	return out
}

// matches returns the highlighted ranges as "line:from-to".
func (e *flashEnv) matches(buf pluginapi.BufferID) []string {
	var out []string
	for _, d := range e.h.Host.Decor().Items("flash", "match", buf) {
		out = append(out, itoa(d.Range.From.Line)+":"+itoa(d.Range.From.Col)+"-"+itoa(d.Range.To.Col))
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func (e *flashEnv) cursor() pluginapi.Pos {
	cs, _ := e.h.Host.Cursors(e.buf)
	return cs[0].Pos
}

func (e *flashEnv) status() string {
	var last string
	for _, n := range e.h.Notices() {
		last = n.Msg
	}
	return last
}

func TestFlashRegistersItsCommand(t *testing.T) {
	t.Parallel()
	e := newFlash(t, "x", nil)
	cmds := e.h.Host.Commands["flash"]
	if len(cmds) != 1 || cmds[0].ID != "flash.jump" || cmds[0].Title != "Flash: Jump" {
		t.Fatalf("commands = %+v", cmds)
	}
	// flash is an opt-in plugin: the manifest turns it off by default.
	data, err := fs.ReadFile(bundled.FS, "plugins/flash/plugin.json")
	if err != nil || !strings.Contains(string(data), `"default_enabled": false`) {
		t.Fatalf("plugin.json = %q, %v: flash must be off by default", data, err)
	}
}

func TestFlashKeymapBinding(t *testing.T) {
	t.Parallel()
	env := newFlash(t, "hello world", nil)
	bindings := env.h.Host.Bindings["flash"]
	found := false
	for _, b := range bindings {
		if b.Chord == "alt+j" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bindings = %v, want alt+j bound to flash_jump", bindings)
	}
}

// TestFlashLabelsForAFixedBuffer: with nothing typed every word start is labelled, nearest
// first, with the comfortable letters, skipping the letters that start a word (typing one
// must start a search, not jump).
func TestFlashLabelsForAFixedBuffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		keys     []string
		settings map[string]any
		matches  []string
		labels   []string
	}{
		{
			name:    "word starts, skip rule removes the starting letters",
			text:    "foo bar\nbaz qux",
			matches: []string{"0:0-1", "0:4-5", "1:0-1", "1:4-5"},
			// The cursor is at 0:0. Pool asdfghjklqwertyuiopzxcvbnm minus f, b, q.
			labels: []string{"0:1=a", "0:5=s", "1:1=d", "1:5=g"},
		},
		{
			name: "the typed pattern extends and the labels keep their letters",
			text: "foo bar\nbaz qux foo",
			keys: []string{"o"},
			// Pattern o: matches at 0:1, 0:2, 1:9 and 1:10. The characters after them are o, space,
			// o, end of line: o is skipped, and the last match has no character to draw over.
			matches: []string{"0:1-2", "0:2-3", "1:9-10", "1:10-11"},
			labels:  []string{"0:2=a", "0:3=s", "1:10=d", "1:eol=f"},
		},
		{
			name:    "a two letter pattern",
			text:    "foo bar\nbaz qux foo",
			keys:    []string{"f", "o"},
			matches: []string{"0:0-2", "1:8-10"},
			labels:  []string{"0:2=a", "1:10=h"},
		},
		{
			name:    "backspace shortens the pattern",
			text:    "abab",
			keys:    []string{"a", "b", "backspace"},
			matches: []string{"0:0-1", "0:2-3"},
			labels:  []string{"0:1=s", "0:3=d"},
		},
		{
			name:     "labels setting",
			text:     "one two three",
			settings: map[string]any{"labels": "xyz"},
			matches:  []string{"0:0-1", "0:4-5", "0:8-9"},
			labels:   []string{"0:1=x", "0:5=y", "0:9=z"},
		},
		{
			name: "case sensitive by default",
			text: "Ab ab AB",
			keys: []string{"a"},
			// Typing "a" starts the pattern (the word-start frame never offered it as a label), and
			// the match keeps the label d it had.
			matches: []string{"0:3-4"},
			labels:  []string{"0:4=d"},
		},
		{
			name:     "case insensitive on request",
			text:     "Ab ab AB",
			keys:     []string{"a"},
			settings: map[string]any{"case_sensitive": false},
			matches:  []string{"0:0-1", "0:3-4", "0:6-7"},
			labels:   []string{"0:1=s", "0:4=d", "0:7=f"},
		},
		{
			name:    "multi-byte characters keep byte columns",
			text:    "日本 語 日本",
			keys:    []string{"日"},
			matches: []string{"0:0-3", "0:11-14"},
			labels:  []string{"0:3=a", "0:14=s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newFlash(t, tt.text, tt.settings)
			e.jump(t)
			for _, k := range tt.keys {
				e.press(k)
			}
			if got := e.matches(e.buf); !slices.Equal(got, tt.matches) {
				t.Errorf("matches = %v, want %v", got, tt.matches)
			}
			if got := e.labels(e.buf); !slices.Equal(got, tt.labels) {
				t.Errorf("labels = %v, want %v", got, tt.labels)
			}
		})
	}
}
