package bundled_test

import (
	"strings"
	"testing"
)

// TestViFileCommands plays the : commands that open, save and list buffers.
func TestViFileCommands(t *testing.T) {
	t.Parallel()
	t.Run("e opens a file relative to the working directory", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.keys(":«e notes.txt»")
		v.h.CompleteBuffers()
		found := false
		for _, c := range v.host.Calls() {
			if c.Method == "Open" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no Open call: %v", v.host.Calls())
		}
	})
	t.Run("e of a modified buffer refuses", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.keys("x:«e»")
		if !strings.Contains(v.notices(), "E37") {
			t.Fatalf("notices = %q", v.notices())
		}
	})
	t.Run("e! reverts", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.keys("x:«e!»")
		v.h.CompleteBuffers()
		if got := v.host.CallCount("Revert"); got != 1 {
			t.Fatalf("Revert calls = %d", got)
		}
	})
	t.Run("ls lists the tabs", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.host.AddBuffer("/w/b.txt", "other")
		v.keys("x:«ls»")
		if n := v.h.NoticeTexts(); len(n) == 0 || !strings.Contains(n[len(n)-1], "1: a.txt [+]") || !strings.Contains(n[len(n)-1], "2: b.txt") {
			t.Fatalf("notices = %q", v.notices())
		}
	})
	t.Run("b switches by number and by name", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		second := v.host.AddBuffer("/w/second.txt", "other")
		v.keys(":«b 2»")
		if v.host.ActiveBuffer() != second {
			t.Fatalf("active = %d, want %d", v.host.ActiveBuffer(), second)
		}
		v.keys(":«b a.t»")
		if v.host.ActiveBuffer() != v.id {
			t.Fatalf("active = %d, want %d", v.host.ActiveBuffer(), v.id)
		}
	})
	t.Run("bd closes the tab", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.host.AddBuffer("/w/b.txt", "other")
		v.keys(":«bd»")
		if len(v.host.Buffers) != 1 {
			t.Fatalf("tabs = %d", len(v.host.Buffers))
		}
	})
	t.Run("wa saves every modified file", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.keys("x:«wa»")
		v.h.CompleteBuffers()
		if got := v.host.CallCount("Save"); got != 1 {
			t.Fatalf("Save calls = %d", got)
		}
	})
	t.Run("r reads a file under the line", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\nb")
		v.h.OS.SetFile("/work/ins.txt", "X\nY\n")
		v.keys(":«r ins.txt»")
		if got := v.text(); got != "a\nX\nY\nb" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("f shows the file", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\nb")
		v.keys(":«f»")
		if !strings.Contains(v.notices(), "/w/a.txt") || !strings.Contains(v.notices(), "2 lines") {
			t.Fatalf("notices = %q", v.notices())
		}
	})
	t.Run("marks and registers are listed", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.keys("mayy:«marks»")
		if !strings.Contains(v.notices(), "a 1:0") {
			t.Fatalf("notices = %q", v.notices())
		}
		// :reg lists the registers in a popup, a row each (a status row holds one line)
		v.keys(":«reg»")
		pop := v.host.UI().Last("popup")
		if pop == nil || !pop.Open || pop.Popup.Title != "Registers" {
			t.Fatalf("no popup: %+v", pop)
		}
		rows := strings.Join(pop.Popup.Lines, "\n")
		if !strings.Contains(rows, `"0    abc^J`) || !strings.Contains(rows, "Type  Name  Content") {
			t.Fatalf("registers popup:\n%s", rows)
		}
	})
	t.Run("k sets a mark", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\nb\nc").at(1, 0)
		v.keys(":«k a»gg'a")
		if got := v.cursor().Line; got != 1 {
			t.Fatalf("line = %d", got)
		}
	})
	t.Run("update saves only a modified buffer", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.keys(":«up»")
		if got := v.host.CallCount("Save"); got != 0 {
			t.Fatalf("Save calls = %d on a clean buffer", got)
		}
	})
	t.Run("a big file that is still indexed says so", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "abc")
		v.buf().Info.LineCount = -1
		v.keys("x")
		if got := v.text(); got != "abc" {
			t.Fatalf("text = %q", got)
		}
		if !strings.Contains(v.notices(), "too big") {
			t.Fatalf("notices = %q", v.notices())
		}
	})
}

// TestViKeysWhileASaveIsPending: a command that waits for the editor (a save) is not
// resumed by the next key, which would hand it the key as the result of the wait. The
// key is dropped, and the keys after the save finished work.
func TestViKeysWhileASaveIsPending(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abcdef")
	v.keys("x:«w»")
	if got := v.host.PendingBufferCalls(); got != 1 {
		t.Fatalf("pending buffer calls = %d, want the save", got)
	}
	v.keys("~x")
	if got := v.text(); got != "bcdef" {
		t.Fatalf("text = %q: keys typed during the save must be dropped", got)
	}
	v.h.CompleteBuffers()
	v.keys("x")
	if got := v.text(); got != "cdef" {
		t.Fatalf("text = %q after the save finished", got)
	}
	for _, l := range v.h.LogText(viPlugin) {
		if strings.HasPrefix(l, "error") {
			t.Errorf("log: %s", l)
		}
	}
}

// TestViClipboard: a yank sets the system clipboard, and "+ pastes the editor's clipboard.
func TestViClipboard(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc def")
	v.keys("yw")
	if v.host.Clipboard != "abc " {
		t.Errorf("clipboard = %q after yw", v.host.Clipboard)
	}
	v.host.Clipboard = ""
	v.keys("x")
	if v.host.Clipboard != "" {
		t.Errorf("clipboard = %q after x: a delete must not touch it", v.host.Clipboard)
	}
	v.keys("\"+yy")
	if v.host.Clipboard != "bc def\n" {
		t.Errorf("clipboard = %q after \"+yy", v.host.Clipboard)
	}
	v.keys("\"+p")
	found := false
	for _, a := range v.ran() {
		if a == "paste" {
			found = true
		}
	}
	if !found {
		t.Errorf("actions = %v, want the editor's paste", v.ran())
	}
}

// TestViReadOnlyBuffer: a read-only buffer can be moved around in but not edited.
func TestViReadOnlyBuffer(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc\ndef")
	v.buf().Info.ReadOnly = true
	v.keys("jx")
	if got := v.text(); got != "abc\ndef" {
		t.Fatalf("text = %q", got)
	}
	if !strings.Contains(v.notices(), "readonly") {
		t.Fatalf("notices = %q", v.notices())
	}
	v.keys("ggG")
	if got := v.cursor().Line; got != 1 {
		t.Fatalf("line = %d", got)
	}
}
