package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestMenuBarShowsTranslatedLabels checks the end-to-end bar: with the Chinese locale the
// bar paints the catalogue titles while the menu identity (used for opening and hit
// testing) stays the English label. Not parallel: locale.
func TestMenuBarShowsTranslatedLabels(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(2), 120, 30)
	frame := strings.Join(frameText(m), "\n")
	for _, want := range []string{"文件", "编辑"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the menu bar lacks the Chinese label %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "File Edit View") {
		t.Fatalf("the menu bar still shows the English bar:\n%s", frame)
	}
}
