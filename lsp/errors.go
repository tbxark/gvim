package lsp

import (
	"fmt"
	"strings"
	"sync"
)

// NotInstalledError says the program of a language server is not on PATH, so nothing
// was started. Start returns it with only Command set, and the Manager adds the language
// and the install hint it knows.
type NotInstalledError struct {
	Language string // the language the server is for, when known
	Command  string // the program that was looked up
	Install  string // how to get it, or empty
}

// Error says what is missing and how to get it.
func (e *NotInstalledError) Error() string {
	var b strings.Builder
	b.WriteString(e.Command)
	b.WriteString(" is not installed")
	if e.Language != "" {
		b.WriteString(" (the language server for " + e.Language + ")")
	}
	if e.Install != "" {
		b.WriteString(": " + e.Install)
	} else {
		b.WriteString(": install it and put it on PATH, or point lsp." + e.langKey() + ".command at it in config.json")
	}
	return b.String()
}

func (e *NotInstalledError) langKey() string {
	if e.Language == "" {
		return "<language>"
	}
	return e.Language
}

// StartError is a language server that was launched but did not complete its handshake.
// It names the server, the command line and the last of what the server wrote to its
// error output, which says why far more often than the "EOF" of a closed pipe.
type StartError struct {
	Language string
	Name     string // the server's name: its program unless the config names it
	Command  string // the command line
	Stderr   string // the tail of the server's error output, or empty
	Err      error
}

// Error reads "gopls (language server for go) could not start: lsp: initialize: EOF
// [command: gopls serve] [it said: ...]".
func (e *StartError) Error() string {
	var b strings.Builder
	b.WriteString(e.Name)
	if e.Language != "" {
		b.WriteString(" (language server for " + e.Language + ")")
	}
	fmt.Fprintf(&b, " failed to start: %v [command: %s]", e.Err, e.Command)
	if e.Stderr != "" {
		fmt.Fprintf(&b, " [it said: %s]", e.Stderr)
	}
	return b.String()
}

// Unwrap returns the failure of the handshake.
func (e *StartError) Unwrap() error { return e.Err }

// tailBuffer keeps the last bytes written to it. A server's error output goes there, so a
// chatty server cannot grow the editor's memory.
type tailBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if len(t.data) > t.max {
		t.data = t.data[len(t.data)-t.max:]
	}
	return len(p), nil
}

// tail returns the last few non-empty lines written, joined with " | " and cut to limit
// runes from the end, or "" when nothing was written.
func (t *tailBuffer) tail(lines, limit int) string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	text := string(t.data)
	t.mu.Unlock()
	var keep []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			keep = append(keep, l)
		}
	}
	if len(keep) > lines {
		keep = keep[len(keep)-lines:]
	}
	out := strings.Join(keep, " | ")
	if r := []rune(out); len(r) > limit {
		out = "..." + string(r[len(r)-limit:])
	}
	return out
}
