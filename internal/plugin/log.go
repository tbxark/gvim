package plugin

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// Log levels of a LogEntry.
const (
	levelDebug = "debug"
	levelInfo  = "info"
	levelWarn  = "warn"
	levelError = "error"
)

// logRing keeps the last maxLogRing log entries. It is the only structure of this
// package guarded by a mutex: the plugin manager may read it while the UI goroutine
// appends.
type logRing struct {
	mu      sync.Mutex
	entries []pluginapi.LogEntry
	start   int
	sink    func(pluginapi.LogEntry) // Options.LogSink
}

func (l *logRing) add(e pluginapi.LogEntry) {
	if l.sink != nil {
		l.sink(e)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) < maxLogRing {
		l.entries = append(l.entries, e)
		return
	}
	l.entries[l.start] = e
	l.start = (l.start + 1) % maxLogRing
}

func (l *logRing) snapshot() []pluginapi.LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]pluginapi.LogEntry, 0, len(l.entries))
	out = append(out, l.entries[l.start:]...)
	out = append(out, l.entries[:l.start]...)
	return out
}

// clipMsg cuts msg to max bytes on a rune boundary and marks the cut.
func clipMsg(msg string, max int) string {
	if len(msg) <= max {
		return msg
	}
	cut := max - 3
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut] + "..."
}

// logf appends one entry to the log ring. plugin may be "" for runtime messages.
func (r *Runtime) logf(plugin, level, msg string) {
	r.log.add(pluginapi.LogEntry{Time: r.now(), Plugin: plugin, Level: level, Msg: clipMsg(msg, maxLogLine)})
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
