package uitest

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeModel draws its text, appends typed runes, and records mouse clicks. It
// shows the underlined "F" of "File" so the underline check has a target.
type fakeModel struct {
	text   string
	clicks []string
}

func (m *fakeModel) Init() tea.Cmd { return nil }

func (m *fakeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.Text != "" {
			m.text += msg.Text
		}
	case tea.MouseClickMsg:
		m.clicks = append(m.clicks, fmt.Sprintf("%d,%d", msg.X, msg.Y))
	}
	return m, nil
}

func (m *fakeModel) View() tea.View {
	body := "\x1b[4mF\x1b[24mile " + m.text
	if len(m.clicks) > 0 {
		body += " clicked " + m.clicks[len(m.clicks)-1]
	}
	return tea.NewView(body + "\n")
}

func TestFirstFrameIsOnTheScreen(t *testing.T) {
	s := Start(t, &fakeModel{}, 40, 4)
	if !s.Screen().Contains("File") {
		t.Fatalf("first frame missing:\n%s", s.Screen())
	}
}

func TestTypedKeysReachTheScreen(t *testing.T) {
	s := Start(t, &fakeModel{}, 40, 4)
	s.Type("abc")
	s.WaitFor(func(sc Screen) bool { return sc.Contains("File abc") })
}

func TestUnderlineIsReadFromTheCell(t *testing.T) {
	s := Start(t, &fakeModel{}, 40, 4)
	sc := s.Screen()
	if got := sc.Underlined(0); len(got) != 1 || got[0] != "F" {
		t.Fatalf("underlined cells on row 0 = %q, want [F]", got)
	}
	if sc.Cell(4, 0).Underline {
		t.Fatal("cell after the underline is underlined")
	}
}

func TestClickPositionReachesTheModel(t *testing.T) {
	s := Start(t, &fakeModel{}, 40, 4)
	s.Click(7, 2)
	s.WaitFor(func(sc Screen) bool { return sc.Contains("clicked 7,2") })
}

func TestFindReturnsCellPosition(t *testing.T) {
	s := Start(t, &fakeModel{}, 40, 4)
	x, y, ok := s.Screen().Find("ile")
	if !ok || x != 1 || y != 0 {
		t.Fatalf("Find(ile) = %d,%d,%v, want 1,0,true", x, y, ok)
	}
	if _, _, ok := s.Screen().Find("missing"); ok {
		t.Fatal("Find reported a missing text")
	}
}

func TestWideCharactersPreserveContinuationCellsAndDisplayColumns(t *testing.T) {
	s := Start(t, &fakeModel{text: "中文after"}, 40, 4)
	sc := s.Screen()
	if !sc.Contains("中文after") {
		t.Fatalf("Contains(中文after) = false, row0 = %q", sc.Row(0))
	}
	if got := sc.Cell(5, 0).Char; got != "中" {
		t.Fatalf("lead cell (5,0) = %q, want 中", got)
	}
	if got := sc.Cell(6, 0).Char; got != "" {
		t.Fatalf("continuation cell (6,0) = %q, want empty", got)
	}
	if x, y, ok := sc.Find("after"); !ok || x != 9 || y != 0 {
		t.Fatalf("Find(after) after File + two wide chars = %d,%d,%v, want 9,0,true", x, y, ok)
	}
}

func TestParseKey(t *testing.T) {
	cases := []struct {
		spec string
		code rune
		mod  tea.KeyMod
		text string
	}{
		{"x", 'x', 0, "x"},
		{"+", '+', 0, "+"},
		{"ctrl++", '+', tea.ModCtrl, ""},
		{"shift++", '+', tea.ModShift, "+"},
		{"enter", tea.KeyEnter, 0, ""},
		{"ctrl+s", 's', tea.ModCtrl, ""},
		{"alt+f", 'f', tea.ModAlt, ""},
		{"shift+left", tea.KeyLeft, tea.ModShift, ""},
		{"ctrl+pgdown", tea.KeyPgDown, tea.ModCtrl, ""},
		{"f10", tea.KeyF10, 0, ""},
	}
	for _, c := range cases {
		got, err := ParseKey(c.spec)
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", c.spec, err)
		}
		if got.Code != c.code || got.Mod != c.mod || got.Text != c.text {
			t.Errorf("ParseKey(%q) = {code %q mod %v text %q}, want {code %q mod %v text %q}",
				c.spec, got.Code, got.Mod, got.Text, c.code, c.mod, c.text)
		}
	}
	for _, bad := range []string{"hyper+x", "bogus", "ctrl+", "++"} {
		if _, err := ParseKey(bad); err == nil {
			t.Fatalf("ParseKey(%q) unexpectedly succeeded", bad)
		}
	}
	if _, err := ParseKey("hyper+x"); err == nil || !strings.Contains(err.Error(), "hyper") {
		t.Fatalf("unknown modifier not rejected: %v", err)
	}
}

func TestCloseReturnsTheFinalModelAndIsIdempotent(t *testing.T) {
	m := &fakeModel{}
	s := Start(t, m, 40, 4)
	s.Type("z")
	final := s.Close()
	if fm, ok := final.(*fakeModel); !ok || fm.text != "z" {
		t.Fatalf("final model = %#v, want text z", final)
	}
	s.Close()
}
