package dialog

import "testing"

func TestErrorDialogAnswersOKByEnterOKKeyAndEsc(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{"enter after the grace window", "enter"},
		{"lower o", "o"},
		{"upper O", "O"},
		{"esc", "esc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			d := NewErrorDialog(c.Now, "Save failed: permission denied", "tag", testOv)
			c.afterGrace()
			mustFind(t, draw(d, 80, 24), "Save failed: permission denied")
			_, cmd := d.Update(key(tt.key))
			r, ok := answer(cmd)
			if !ok || r.Choice != ChoiceOK || r.Tag != "tag" {
				t.Fatalf("%q answered %+v, want OK with the tag", tt.key, r)
			}
		})
	}
}

func TestErrorDialogClickOK(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewErrorDialog(c.Now, "boom", nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "OK")
	_, cmd := d.Update(click(x, y))
	mustClose(t, d, cmd, ChoiceOK, "")
}

func TestErrorDialogWrapsLongMessage(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	long := "open /very/long/path/that/keeps/going/and/going/until/it/must/wrap/somewhere/near/the/end/tail-word"
	d := NewErrorDialog(c.Now, long, nil, testOv)
	g := draw(d, 80, 30)
	mustFind(t, g, "tail-word")
	if got := len(g.Plain()); got < 10 {
		t.Fatalf("grid has %d rows", got)
	}
}
