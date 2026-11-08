package keymap_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// keyToken matches a backticked key a document names: a chord with a modifier, or a function
// key. A bare letter is not taken for a key.
var keyToken = regexp.MustCompile(`(?i)^(?:(?:ctrl|alt|shift|cmd|super|meta)\+)+[a-z0-9]+$|^(?:(?:ctrl|alt|shift)\+)*f\d{1,2}$`)

// TestDocsDefaultKeysAreBound walks the Markdown files of docs/ for the paragraphs that say a
// key is in "the default keymap" and checks each key they name against the default preset, so
// that pruning a binding cannot leave a document claiming it. The plugin API docs and the
// generated keybindings.md are not read.
func TestDocsDefaultKeysAreBound(t *testing.T) {
	m := mustPreset(t, "default")
	files, err := filepath.Glob(filepath.Join("..", "docs", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no docs found: %v", err)
	}
	backticked := regexp.MustCompile("`([^`\n]+)`")
	for _, file := range files {
		if filepath.Base(file) == "keybindings.md" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, para := range strings.Split(string(data), "\n\n") {
			flat := strings.Join(strings.Fields(para), " ")
			if !strings.Contains(flat, "default keymap") && !strings.Contains(flat, "default key") {
				continue
			}
			for _, tok := range backticked.FindAllStringSubmatch(flat, -1) {
				if !keyToken.MatchString(tok[1]) {
					continue
				}
				chord, err := keymap.NormalizeChord(tok[1])
				if err != nil {
					t.Errorf("%s: %q is not a key: %v", filepath.Base(file), tok[1], err)
					continue
				}
				if _, ok := m.Resolve(chord, keymap.ContextNormal); !ok {
					t.Errorf("%s says %s is in the default keymap, but it is not bound", filepath.Base(file), tok[1])
				}
			}
		}
	}
}
