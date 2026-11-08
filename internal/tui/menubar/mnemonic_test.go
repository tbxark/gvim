package menubar

import (
	"testing"
	"unicode"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// mnemonicPorts is fakePorts with the keymap's menu accelerators: the letter each
// menu label underlines.
type mnemonicPorts struct {
	*fakePorts
	letters map[string]rune
}

func (p *mnemonicPorts) MenuMnemonic(name string) (rune, bool) {
	r, ok := p.letters[name]
	return r, ok
}

// mnemonicBar returns a bar over all-on ports and the given accelerators, with
// mnemonics on or off.
func mnemonicBar(on bool, letters map[string]rune) Model {
	p := &mnemonicPorts{fakePorts: allOn(), letters: letters}
	return newBar(p, 100, 24).WithMnemonics(on)
}

// barCells paints the bar and returns the grid.
func barCells(m Model) *grid.Grid {
	g := grid.New(100, 1)
	m.Paint(g)
	return g
}

// labelAt returns the place of the bar label called title.
func labelAt(t *testing.T, m Model, title string) labelRun {
	t.Helper()
	for _, r := range m.geometry(100, 24).labels {
		if r.title == title {
			return r
		}
	}
	t.Fatalf("no label %q on the bar", title)
	return labelRun{}
}

// TestMenuLabelsUnderlineTheKeymapMnemonic checks the underline: each root menu's
// label marks the letter of its menu_open binding, at the first occurrence in the
// label, and nothing else on the row is underlined.
func TestMenuLabelsUnderlineTheKeymapMnemonic(t *testing.T) {
	letters := map[string]rune{
		"File": 'f', "Edit": 'e', "View": 'v', "Selection": 's',
		"Go": 'g', "LSP": 'l', "Help": 'h',
	}
	m := mnemonicBar(true, letters)
	g := barCells(m)
	for title, letter := range letters {
		r := labelAt(t, m, title)
		want := -1
		for i, c := range []rune(r.title) {
			if unicode.ToLower(c) == letter {
				want = i
				break
			}
		}
		if want < 0 {
			t.Fatalf("test letter %q is not in label %q", letter, title)
		}
		if got := g.At(r.x+want, 0).Text; got != string([]rune(r.title)[want]) {
			t.Fatalf("%s: cell %d holds %q", title, r.x+want, got)
		}
		if !g.At(r.x+want, 0).Style.Underline {
			t.Errorf("%s: the mnemonic letter %q is not underlined", title, letter)
		}
		for i := range []rune(r.title) {
			if i != want && g.At(r.x+i, 0).Style.Underline {
				t.Errorf("%s: character %d is underlined too", title, i)
			}
		}
	}
}

// TestMnemonicsOffLeavesNoUnderline checks editor.menu_bar_mnemonics off: the labels
// paint plain even when the keymap knows the accelerators.
func TestMnemonicsOffLeavesNoUnderline(t *testing.T) {
	letters := map[string]rune{"File": 'f', "Edit": 'e', "View": 'v', "Selection": 's', "Go": 'g', "LSP": 'l', "Help": 'h'}
	m := mnemonicBar(false, letters)
	g := barCells(m)
	for x := 0; x < 100; x++ {
		if g.At(x, 0).Style.Underline {
			t.Fatalf("column %d underlines a letter with mnemonics off", x)
		}
	}
}

// TestPortWithoutMnemonicerUnderlinesNothing checks a ports implementation that
// cannot answer the accelerators: the labels paint plain.
func TestPortWithoutMnemonicerUnderlinesNothing(t *testing.T) {
	m := newBar(allOn(), 100, 24).WithMnemonics(true)
	g := barCells(m)
	for x := 0; x < 100; x++ {
		if g.At(x, 0).Style.Underline {
			t.Fatalf("column %d underlines a letter without a mnemonic port", x)
		}
	}
}

// TestMnemonicLetterMissingFromTheLabelUnderlinesNothing checks the letter the keymap
// returns but the label does not contain: nothing is underlined, as in Fresh.
func TestMnemonicLetterMissingFromTheLabelUnderlinesNothing(t *testing.T) {
	m := mnemonicBar(true, map[string]rune{"File": 'z'})
	g := barCells(m)
	r := labelAt(t, m, "File")
	for i := range []rune(r.title) {
		if g.At(r.x+i, 0).Style.Underline {
			t.Fatalf("column %d underlines a letter although the mnemonic is not in the label", r.x+i)
		}
	}
}

// TestUnderlineComposesWithTheActiveLabel checks the open title: it keeps the active
// style and still underlines its mnemonic.
func TestUnderlineComposesWithTheActiveLabel(t *testing.T) {
	m := mnemonicBar(true, map[string]rune{"File": 'f'})
	labels := Labels(false)
	index := -1
	for i, title := range labels {
		if title == "File" {
			index = i
		}
	}
	m = m.Open(index)
	g := barCells(m)
	r := labelAt(t, m, "File")
	cell := g.At(r.x, 0)
	if !cell.Style.Underline {
		t.Error("the open label does not underline its mnemonic")
	}
	if cell.Style == barCells(mnemonicBar(true, nil)).At(r.x, 0).Style {
		t.Error("the open label lost its active style")
	}
}

// TestMnemonicLookupIsCaseInsensitive checks a keymap that reports the upper-case
// letter, as Fresh's resolver lower-cases both sides.
func TestMnemonicLookupIsCaseInsensitive(t *testing.T) {
	m := mnemonicBar(true, map[string]rune{"Edit": 'E'})
	g := barCells(m)
	r := labelAt(t, m, "Edit")
	if !g.At(r.x, 0).Style.Underline {
		t.Error("the upper-case mnemonic letter is not underlined")
	}
}
