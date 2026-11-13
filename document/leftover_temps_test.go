package document

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/atomicfile"
)

// A save of a file with a long name writes a shortened temporary name, so the leftover
// scan has to look for that name, not for the whole file name.
func TestLeftoverTempsOfALongName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"short.txt", strings.Repeat("n", 250) + ".txt"} {
		path := filepath.Join(dir, name)
		temp := filepath.Join(dir, atomicfile.TempPrefix(name)+"123456")
		if err := os.WriteFile(temp, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := leftoverTemps(path); len(got) != 1 || got[0] != temp {
			t.Errorf("leftoverTemps(%d-byte name) = %v, want %v", len(name), got, temp)
		}
	}
}
