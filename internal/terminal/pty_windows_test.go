//go:build windows

package terminal

import (
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// The environment block is the pure part of the ConPTY spawn: these tests run
// under GOOS=windows (compile) and on a real Windows machine.
func TestWindowsEnvBlock(t *testing.T) {
	t.Parallel()
	block := windowsEnvBlock([]string{"A=1", "BAD", "=broken", "B=2"})
	var want []uint16
	for _, s := range []string{"A=1", "B=2"} {
		u, err := windows.UTF16FromString(s)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, u...)
	}
	want = append(want, 0)
	if len(block) < 2 || block[len(block)-2] != 0 || block[len(block)-1] != 0 {
		t.Fatalf("block = %v, want the double NUL terminator", block)
	}
	// utf16.Decode of the block up to the double NUL is the name=value list.
	end := len(block) - 2
	var got []string
	start := 0
	for i := 0; i < end; i++ {
		if block[i] == 0 {
			got = append(got, string(utf16.Decode(block[start:i])))
			start = i + 1
		}
	}
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Fatalf("env block entries = %q, want A=1 and B=2 with invalid entries dropped", got)
	}
	dup := windowsEnvBlock([]string{"Path=old", "TERM=dumb", "PATH=new"})
	if got := string(utf16.Decode(dup[:len(dup)-2])); got != "PATH=new\x00TERM=dumb" {
		t.Fatalf("deduplicated block = %q, want the last PATH and the names sorted", got)
	}
	empty := windowsEnvBlock(nil)
	if len(empty) != 2 || empty[0] != 0 || empty[1] != 0 {
		t.Fatalf("empty block = %v, want two NULs", empty)
	}
}

func TestWindowsExitError(t *testing.T) {
	t.Parallel()
	err := &windowsExitError{code: 7}
	if got, want := err.Error(), "exit status 7"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got := err.ExitCode(); got != 7 {
		t.Fatalf("ExitCode() = %d, want 7", got)
	}
}
