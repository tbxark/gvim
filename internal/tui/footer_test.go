package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/style"
)

// fakeHints implements HintSource. It returns the hints of each context.
type fakeHints map[KeyContext][]Hint

func (f fakeHints) Hints(ctx KeyContext) []Hint { return f[ctx] }

func TestFooterShowsTheFocusedContextOnly(t *testing.T) {
	f := newFooter(fakeHints{
		KeyContextNormal:       {{Keys: "Ctrl+S", Desc: "save"}},
		KeyContextFileExplorer: {{Keys: "Enter", Desc: "open"}, {Keys: "Down", Desc: "next"}},
	}, style.Styles{})

	got := f.View(KeyContextFileExplorer, 80)
	for _, want := range []string{"Enter", "open", "Down", "next"} {
		if !strings.Contains(got, want) {
			t.Errorf("explorer footer %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "save") {
		t.Errorf("explorer footer shows the normal context: %q", got)
	}
	if normal := ansi.Strip(f.View(KeyContextNormal, 80)); !strings.Contains(normal, "Ctrl+S save") {
		t.Errorf("normal footer = %q, want the short form \"Ctrl+S save\"", normal)
	}
}

func TestFooterBindingsCarryHelp(t *testing.T) {
	f := newFooter(fakeHints{KeyContextNormal: {{Keys: "Ctrl+Q", Desc: "quit"}, {Keys: "", Desc: "skipped"}}}, style.Styles{})
	bs := f.bindings(KeyContextNormal)
	if len(bs) != 1 {
		t.Fatalf("bindings = %d, want the empty-chord hint skipped", len(bs))
	}
	if h := bs[0].Help(); h.Key != "Ctrl+Q" || h.Desc != "quit" {
		t.Fatalf("help = %+v", h)
	}
	if !bs[0].Enabled() {
		t.Fatal("a footer binding is disabled")
	}
}

func TestFooterCutsToWidth(t *testing.T) {
	hints := make([]Hint, 0, 10)
	for range 10 {
		hints = append(hints, Hint{Keys: "Ctrl+Shift+Alt+X", Desc: "a long action name"})
	}
	f := newFooter(fakeHints{KeyContextNormal: hints}, style.Styles{})
	got := f.View(KeyContextNormal, 40)
	if w := ansi.StringWidth(got); w > 40 {
		t.Fatalf("footer is %d cells, want at most 40: %q", w, got)
	}
	if got == "" {
		t.Fatal("no hint fits a 40-cell footer")
	}
}

func TestFooterWithoutSourceIsEmpty(t *testing.T) {
	f := newFooter(nil, style.Styles{})
	if got := f.View(KeyContextNormal, 80); got != "" {
		t.Fatalf("nil source footer = %q", got)
	}
}

func TestFooterWithNoHintsIsEmpty(t *testing.T) {
	f := newFooter(fakeHints{}, style.Styles{})
	if got := f.View(KeyContextFileExplorer, 80); got != "" {
		t.Fatalf("empty context footer = %q", got)
	}
}
