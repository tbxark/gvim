//go:build unix

package app

import (
	"errors"
	"os"
	"testing"

	"github.com/creack/pty"
)

func TestRequireTerminal(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close(); _ = tty.Close() })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	file, err := os.Create(t.TempDir() + "/out.bin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	tests := []struct {
		name string
		f    *os.File
		want error
	}{
		{"terminal", tty, nil},
		{"pipe", w, errNotTerminal},
		{"redirected to a file", file, errNotTerminal},
		{"nil", nil, errNotTerminal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := requireTerminal(tc.f); !errors.Is(got, tc.want) {
				t.Errorf("requireTerminal = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRunRefusesAStdoutThatIsNotATerminal checks that Run fails before it builds a
// session when standard output is a file, as in "gvim a.txt > out".
func TestRunRefusesAStdoutThatIsNotATerminal(t *testing.T) {
	file, err := os.Create(t.TempDir() + "/out.bin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	old := os.Stdout
	os.Stdout = file
	t.Cleanup(func() { os.Stdout = old })
	if err := Run(t.Context(), Options{}); !errors.Is(err, errNotTerminal) {
		t.Fatalf("Run = %v, want errNotTerminal", err)
	}
}
