package prompt

import (
	"strings"
	"testing"
)

func choiceOptions() []ChoiceOption {
	return []ChoiceOption{
		{Label: "Overwrite", Key: "o"},
		{Label: "Overwrite All", Key: "O"},
		{Label: "Skip", Key: "s"},
		{Label: "Skip All", Key: "S"},
		{Label: "Cancel", Key: "c"},
	}
}

// TestChoiceMnemonicKeys checks every answer's exact (case-sensitive) key selects it.
func TestChoiceMnemonicKeys(t *testing.T) {
	t.Parallel()
	for i, mnemonic := range []string{"o", "O", "s", "S", "c"} {
		c := NewChoice(testStyles(), "Conflict", "tag", choiceOptions())
		_, cmd := c.Update(key(mnemonic))
		d, ok := emitted(cmd)
		if !ok || d.Choice != i || d.Tag != "tag" {
			t.Fatalf("key %q: done = %+v %v, want choice %d", mnemonic, d, ok, i)
		}
		if c.Active() {
			t.Fatalf("key %q left the choice open", mnemonic)
		}
	}
}

// TestChoiceIgnoresUnknownKeysAndEscCloses checks a typo keeps the dialog (a paste queue
// must not be dropped by an unknown key) and Esc closes without an answer.
func TestChoiceIgnoresUnknownKeysAndEscCloses(t *testing.T) {
	t.Parallel()
	c := NewChoice(testStyles(), "Conflict", nil, choiceOptions())
	_, cmd := c.Update(key("x"))
	if _, ok := emitted(cmd); ok || !c.Active() {
		t.Fatal("an unknown key answered or closed the choice")
	}
	_, cmd = c.Update(key("esc"))
	if _, ok := emitted(cmd); ok || c.Active() {
		t.Fatal("Esc should close without Done")
	}
}

// TestChoiceArrowsAndEnter checks the focused option can be answered with Enter and that
// the focus moves with the arrows.
func TestChoiceArrowsAndEnter(t *testing.T) {
	t.Parallel()
	c := NewChoice(testStyles(), "Conflict", nil, choiceOptions())
	c.Update(key("right"))
	c.Update(key("right"))
	_, cmd := c.Update(key("enter"))
	d, ok := emitted(cmd)
	if !ok || d.Choice != 2 {
		t.Fatalf("done = %+v %v, want choice 2", d, ok)
	}
}

// TestChoiceDrawShowsEveryAnswer checks the bottom row names each option with its key.
func TestChoiceDrawShowsEveryAnswer(t *testing.T) {
	t.Parallel()
	c := NewChoice(testStyles(), "Conflict:", nil, choiceOptions())
	rows := draw(c, 120, 3)
	row := strings.TrimRight(rows[2], " ")
	for _, want := range []string{"Conflict:", "o Overwrite", "O Overwrite All", "s Skip", "S Skip All", "c Cancel"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row = %q, missing %q", row, want)
		}
	}
	if _, _, ok := c.Cursor(120, 3); ok {
		t.Fatal("a choice reported a text caret")
	}
}
