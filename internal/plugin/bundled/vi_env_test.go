package bundled_test

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/plugin/bundled"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// The vi_mode tests play the keyboard on the fake host. A key reaches the plugin the way
// the ui delivers it: the input mode the plugin entered decides, a bound chord runs its
// action, a printable key goes to the mode's text function, and a key of the insert mode
// that nothing binds types into the buffer as the editor would.

const viPlugin = "vi_mode"

// viFiles reads every .lua file of the embedded vi_mode plugin, as the loader does.
func viFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	err := fs.WalkDir(bundled.FS, "plugins/"+viPlugin, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".lua") {
			return err
		}
		src, err := fs.ReadFile(bundled.FS, p)
		if err != nil {
			return err
		}
		files[viPlugin+strings.TrimPrefix(p, "plugins/"+viPlugin)] = string(src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no vi_mode files")
	}
	return files
}

// vi is a harness with the plugin loaded over one buffer.
type vi struct {
	t    *testing.T
	h    *plugintest.Harness
	host *plugintest.FakeHost
	id   pluginapi.BufferID
	want int // the wanted column of vertical moves, -1 unset
	last pluginapi.Pos
	sets int // SetCursor calls seen so far: any other one resets the wanted column, as in the editor
	// lenient: a plugin error does not fail the test (the test provokes one)
	lenient bool
	// scrolls: scroll_up and scroll_down move the window as the editor's do, and drag the
	// caret along to stay a few lines inside it
	scrolls bool
}

func newVi(t *testing.T, text string) *vi {
	t.Helper()
	return newViTimeout(t, text, 2*time.Second)
}

// newViTimeout is newVi with another budget for a slice of the plugin.
func newViTimeout(t *testing.T, text string, budget time.Duration) *vi {
	t.Helper()
	host := plugintest.NewFakeHost()
	id := host.AddBuffer("/w/a.txt", text)
	v := &vi{t: t, host: host, id: id, want: -1}
	host.Cmd().Hook = v.action
	// the heap guard measures the whole process, and a test run has many parallel sessions
	v.h = plugintest.New(t, viFiles(t), plugintest.Options{Host: host, Timeout: budget, EventBudget: budget, HeapLimit: 8 << 30})
	v.h.Start()
	if p, ok := v.h.Plugin(viPlugin); !ok || p.Status != "loaded" {
		t.Fatalf("vi_mode is %+v, log: %v", p, v.h.LogText(viPlugin))
	}
	return v
}

// at puts the caret.
func (v *vi) at(line, col int) *vi {
	v.t.Helper()
	if err := v.host.SetCursor(v.id, pluginapi.Pos{Line: line, Col: col}, false, false, false); err != nil {
		v.t.Fatal(err)
	}
	return v
}

func (v *vi) buf() *plugintest.FakeBuffer {
	for _, b := range v.host.Buffers {
		if b.Info.ID == v.id {
			return b
		}
	}
	v.t.Fatal("buffer is gone")
	return nil
}

func (v *vi) text() string { return strings.Join(v.buf().Lines, "\n") }

func (v *vi) cursor() pluginapi.Pos {
	cs, err := v.host.Cursors(v.id)
	if err != nil || len(cs) == 0 {
		v.t.Fatalf("cursors: %v", err)
	}
	return cs[0].Pos
}

func (v *vi) selection() (pluginapi.Range, bool) {
	cs, _ := v.host.Cursors(v.id)
	if len(cs) == 0 || cs[0].Anchor == nil {
		return pluginapi.Range{}, false
	}
	a, b := *cs[0].Anchor, cs[0].Pos
	if a.Line > b.Line || (a.Line == b.Line && a.Col > b.Col) {
		a, b = b, a
	}
	return pluginapi.Range{From: a, To: b}, true
}

// mode is the vi mode the editor is in, "" when none is.
func (v *vi) mode() string { return v.host.InputMode[viPlugin] }

// label is the text of the status item.
func (v *vi) label() string {
	for _, it := range v.host.Status[viPlugin] {
		if it.ID == "vi" {
			return it.Text
		}
	}
	return ""
}

func (v *vi) modeInfo() (pluginapi.ModeInfo, bool) {
	for _, m := range v.host.Modes[viPlugin] {
		if m.Name == v.mode() {
			return m, true
		}
	}
	return pluginapi.ModeInfo{}, false
}

// keys plays a key sequence. Plain characters are text. <Esc> <CR> <BS> <Tab> <Del>
// <Space> <Left> <Right> <Up> <Down> <Home> <End> <PageUp> <PageDown> <lt> and <C-x> are
// named keys. «text» answers the open prompt, <in!> dismisses it, <pick:N> chooses row N
// of the open picker.
func (v *vi) keys(s string) *vi {
	v.t.Helper()
	for len(s) > 0 {
		if strings.HasPrefix(s, "«") {
			end := strings.Index(s, "»")
			if end < 0 {
				v.t.Fatalf("unterminated « in %q", s)
			}
			text := s[len("«"):end]
			s = s[end+len("»"):]
			v.answerInput(&text)
			continue
		}
		if s[0] == '<' {
			if end := strings.IndexByte(s, '>'); end > 0 {
				name := s[1:end]
				s = s[end+1:]
				v.named(name)
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(s)
		v.press(string(r))
		s = s[n:]
	}
	return v
}

var viNames = map[string]string{
	"Esc": "escape", "CR": "enter", "BS": "backspace", "Tab": "tab", "Del": "delete",
	"Left": "left", "Right": "right", "Up": "up", "Down": "down", "Home": "home", "End": "end",
	"PageUp": "pageup", "PageDown": "pagedown",
}

func (v *vi) named(name string) {
	v.t.Helper()
	switch {
	case name == "lt":
		v.press("<")
	case name == "Space":
		v.press(" ")
	case name == "in!":
		v.answerInput(nil)
	case strings.HasPrefix(name, "pick:"):
		n, err := strconv.Atoi(strings.TrimPrefix(name, "pick:"))
		if err != nil {
			v.t.Fatalf("bad key <%s>", name)
		}
		v.answerPick(n)
	case strings.HasPrefix(name, "C-"):
		v.press("ctrl+" + strings.ToLower(strings.TrimPrefix(name, "C-")))
	default:
		chord, ok := viNames[name]
		if !ok {
			v.t.Fatalf("unknown key <%s>", name)
		}
		v.press(chord)
	}
}

// answerInput resolves the prompt the plugin opened.
func (v *vi) answerInput(text *string) {
	v.t.Helper()
	o := v.host.UI().Last("input")
	if o == nil || !o.Open {
		v.t.Fatal("no prompt is open")
	}
	o.Open = false
	var val any
	if text != nil {
		val = *text
	}
	v.h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{val}})
}

// answerPick chooses row n (0-based) of the picker the plugin opened, as ui.choose does.
func (v *vi) answerPick(n int) {
	v.t.Helper()
	o := v.host.UI().Last("picker")
	if o == nil || !o.Open {
		v.t.Fatal("no picker is open")
	}
	o.Open = false
	v.h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{n}})
}

// press delivers one key.
func (v *vi) press(k string) {
	v.t.Helper()
	if o := v.host.UI().Last("input"); o != nil && o.Open {
		v.t.Fatalf("key %q pressed while a prompt is open", k)
	}
	info, ok := v.modeInfo()
	if !ok {
		v.t.Fatalf("key %q: vi has no input mode (log: %v)", k, v.h.LogText(viPlugin))
	}
	isChar := utf8.RuneCountInString(k) == 1
	if !isChar || strings.HasPrefix(k, "ctrl+") {
		if act, ok := info.Bindings[k]; ok {
			if err := v.h.Run(act, info.Args[k]); err != nil {
				v.t.Fatalf("key %q: %v", k, err)
			}
			v.check()
			return
		}
		if !info.Fallthrough {
			return
		}
		v.base(k)
		return
	}
	switch info.Text {
	case pluginapi.TextSwallow:
	case pluginapi.TextHandler:
		if err := v.h.Run(info.TextAction, map[string]any{"text": k}); err != nil {
			v.t.Fatalf("key %q: %v", k, err)
		}
		v.check()
	default:
		v.typeText(k)
	}
}

// check fails the test when the plugin logged an error.
func (v *vi) check() {
	v.t.Helper()
	if v.lenient {
		return
	}
	for _, l := range v.h.LogText(viPlugin) {
		if strings.HasPrefix(l, "error") {
			v.t.Fatalf("plugin error: %s", l)
		}
	}
}

// base is a key that no mode binding took: the editor's own keymap. Only the ones the
// insert mode needs are played.
func (v *vi) base(k string) {
	switch k {
	case "enter":
		v.newline()
	case "backspace":
		v.backspace()
	case "delete":
		v.deleteForward()
	}
}

// ---- the editor, as far as vi_mode uses it ----

func (v *vi) edit(from, to pluginapi.Pos, text string) {
	v.t.Helper()
	err := v.host.Edit(v.id, []pluginapi.Edit{{Range: pluginapi.Range{From: from, To: to}, Text: text}}, pluginapi.EditOptions{})
	if err != nil {
		v.t.Fatal(err)
	}
}

func (v *vi) typeText(s string) {
	c := v.cursor()
	v.edit(c, c, s)
}

func (v *vi) newline() {
	c := v.cursor()
	line := v.buf().Lines[c.Line]
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	v.edit(c, c, "\n"+indent)
}

func (v *vi) backspace() {
	c := v.cursor()
	switch {
	case c.Col > 0:
		line := v.buf().Lines[c.Line]
		_, size := utf8.DecodeLastRuneInString(line[:c.Col])
		v.edit(pluginapi.Pos{Line: c.Line, Col: c.Col - size}, c, "")
	case c.Line > 0:
		v.edit(pluginapi.Pos{Line: c.Line - 1, Col: len(v.buf().Lines[c.Line-1])}, c, "")
	}
}

func (v *vi) deleteForward() {
	c := v.cursor()
	line := v.buf().Lines[c.Line]
	switch {
	case c.Col < len(line):
		_, size := utf8.DecodeRuneInString(line[c.Col:])
		v.edit(c, pluginapi.Pos{Line: c.Line, Col: c.Col + size}, "")
	case c.Line+1 < len(v.buf().Lines):
		v.edit(c, pluginapi.Pos{Line: c.Line + 1}, "")
	}
}

func (v *vi) setCursor(p pluginapi.Pos) {
	if err := v.host.SetCursor(v.id, p, false, false, false); err != nil {
		v.t.Fatal(err)
	}
	v.last = p
	v.sets = v.host.CallCount("SetCursor")
}

// scrollMargin is the scroll-off of the test editor.
const scrollMargin = 3

// scroll plays the editor's scrollView: the window moves one line, and the caret moves to
// the nearest line that is shown, a margin inside the edges that have text beyond them.
func (v *vi) scroll(c pluginapi.Pos, down bool) pluginapi.Pos {
	w := v.host.Window(v.host.CurrentWindow())
	lines := len(v.buf().Lines)
	top := w.TopLine - 1
	if down {
		top = w.TopLine + 1
	}
	top = min(max(top, 0), lines-1)
	if down && top > lines-w.Height {
		return c
	}
	w.TopLine = top
	lo, hi := top, min(top+w.Height-1, lines-1)
	if top > 0 {
		lo += scrollMargin
	}
	if top+w.Height < lines {
		hi -= scrollMargin
	}
	c.Line = min(max(c.Line, lo), max(hi, lo))
	c.Col = min(c.Col, len(v.buf().Lines[c.Line]))
	return c
}

// clusterAfter is the length in bytes of the character at col: a rune and its combining marks,
// as the editor's own moves see it.
func clusterAfter(line string, col int) int {
	_, n := utf8.DecodeRuneInString(line[col:])
	for col+n < len(line) {
		r, size := utf8.DecodeRuneInString(line[col+n:])
		if !unicode.Is(unicode.Mn, r) {
			break
		}
		n += size
	}
	return n
}

// clusterBefore is the length in bytes of the character that ends at col.
func clusterBefore(line string, col int) int {
	n := 0
	for col-n > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:col-n])
		n += size
		if !unicode.Is(unicode.Mn, r) {
			break
		}
	}
	return n
}

// action plays the editor actions vi_mode runs.
func (v *vi) action(name string, args map[string]any, count int) error {
	c := v.cursor()
	lastCol := func(l string) int {
		if l == "" {
			return 0
		}
		return len(l) - clusterBefore(l, len(l))
	}
	vertical := name == "vi_move_up" || name == "vi_move_down" || name == "move_up" || name == "move_down"
	if n := v.host.CallCount("SetCursor"); !vertical || c != v.last || n != v.sets {
		v.want = -1
		v.sets = n
	}
	if vertical && v.want < 0 {
		v.want = c.Col
	}
	for range count {
		lines := v.buf().Lines
		c = v.cursor()
		line := lines[c.Line]
		switch name {
		case "move_left_in_line", "move_left":
			if c.Col > 0 {
				c.Col -= clusterBefore(line, c.Col)
			}
		case "move_right_in_line":
			if c.Col >= len(line) {
				c.Col = lastCol(line)
			} else {
				c.Col = min(c.Col+clusterAfter(line, c.Col), lastCol(line))
			}
		case "move_right":
			if c.Col < len(line) {
				c.Col += clusterAfter(line, c.Col)
			}
		case "vi_move_down", "move_down":
			// vi does not go onto the empty line after the final line break
			if c.Line+1 < len(lines) && (name != "vi_move_down" || c.Line+2 < len(lines) || lines[c.Line+1] != "") {
				c.Line++
				c.Col = min(v.want, len(lines[c.Line]))
				if name == "vi_move_down" {
					c.Col = min(c.Col, lastCol(lines[c.Line]))
				}
			}
		case "vi_move_up", "move_up":
			if c.Line > 0 {
				c.Line--
				c.Col = min(v.want, len(lines[c.Line]))
				if name == "vi_move_up" {
					c.Col = min(c.Col, lastCol(lines[c.Line]))
				}
			}
		case "smart_home":
			c.Col = 0
		case "move_line_end":
			c.Col = len(line)
		case "toggle_case":
			if c.Col < len(line) {
				r, first := utf8.DecodeRuneInString(line[c.Col:])
				size := clusterAfter(line, c.Col)
				sw := strings.ToUpper(string(r))
				if sw == string(r) {
					sw = strings.ToLower(string(r))
				}
				sw += line[c.Col+first : c.Col+size]
				v.edit(c, pluginapi.Pos{Line: c.Line, Col: c.Col + size}, sw)
				c.Col += len(sw)
			}
		case "insert_newline":
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			v.edit(c, c, "\n"+indent)
			c = v.cursor()
		case "scroll_up", "scroll_down":
			if v.scrolls {
				c = v.scroll(c, name == "scroll_down")
			}
		case "recenter", "move_page_up", "move_page_down", "paste", "clear_search",
			"close_tab", "quit", "quit_discard", "save", "save_all", "next_buffer", "prev_buffer", "new",
			"split_horizontal", "split_vertical", "close_split", "toggle_maximize_split":
		default:
			return fmt.Errorf("the test editor does not play %q", name)
		}
		v.setCursor(c)
	}
	return nil
}
