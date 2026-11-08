package app

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/pluginapi"
)

// fileLog is --log-file PATH: the notices of the status row and the plugin log, one line each,
// appended to a file. The update loop must not do IO, so lines go through a channel to a
// goroutine that writes and flushes; a line that does not fit the channel is counted, not waited
// for. An unwritable path is the caller's notice, never a failure of the editor.
type fileLog struct {
	f       *os.File
	lines   chan string
	done    chan struct{}
	dropped atomic.Int64
	now     func() time.Time
	mu      sync.Mutex              // guards closed and the sends against the close of lines
	echo    [2]map[string]time.Time // the texts each channel wrote lately (echoed)
	closed  bool
}

// logQueue is how many lines wait for the writer before new ones are dropped.
const logQueue = 1024

// openLogFile opens path for appending, creating it. The error says why it could not.
func openLogFile(path string) (*fileLog, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	l := &fileLog{f: f, lines: make(chan string, logQueue), done: make(chan struct{}), now: time.Now}
	go l.run()
	l.write("log", fmt.Sprintf("gvim %s started", gvimVersion()))
	return l, nil
}

// write queues one line.
func (l *fileLog) write(kind, text string) {
	if l == nil {
		return
	}
	line := l.now().Format("2006-01-02T15:04:05.000Z07:00") + " " + kind + " " + oneLine(text)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	select {
	case l.lines <- line:
	default:
		l.dropped.Add(1)
	}
}

// Notice is the sink of the status row's notices (ui.Deps.NoticeSink).
func (l *fileLog) Notice(text string) {
	if l == nil {
		return
	}
	keys := []string{text}
	if i := strings.Index(text, ": "); i >= 0 {
		keys = append(keys, text[i+2:])
	}
	if l.echoed(chanNotice, keys...) {
		return
	}
	l.write("notice", text)
}

// Plugin is the sink of the plugin log (plugin.Options.LogSink).
func (l *fileLog) Plugin(e pluginapi.LogEntry) {
	who := "plugin"
	if e.Plugin != "" {
		who += "(" + e.Plugin + ")"
	}
	if l == nil || l.echoed(chanLog, e.Msg, e.Plugin+": "+e.Msg) {
		return
	}
	l.write(who+" "+e.Level, e.Msg)
}

// The two channels that write the file: the plugin log, and the notices of the status row.
const (
	chanLog = iota
	chanNotice
)

// echoWindow is how long a text one channel wrote hides the same text from the other: a plugin's
// warning reaches the status row as a notice and into the plugin log, and the file keeps it once.
const echoWindow = 2 * time.Second

// echoed reports whether one of keys was written by the other channel lately, and notes keys as
// written by ch.
func (l *fileLog) echoed(ch int, keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range keys {
		if at, ok := l.echo[1-ch][k]; ok && now.Sub(at) < echoWindow {
			return true
		}
	}
	if l.echo[ch] == nil || len(l.echo[ch]) > 256 {
		l.echo[ch] = map[string]time.Time{}
	}
	for _, k := range keys {
		l.echo[ch][k] = now
	}
	return false
}

// run is the writer: it writes what is queued and flushes whenever the queue is empty, so the
// file is current within a moment and a crash loses at most the last few lines.
func (l *fileLog) run() {
	defer close(l.done)
	w := bufio.NewWriter(l.f)
	for line := range l.lines {
		_, _ = w.WriteString(line)
		_ = w.WriteByte('\n')
		if len(l.lines) == 0 {
			_ = w.Flush()
		}
	}
	if n := l.dropped.Load(); n > 0 {
		_, _ = fmt.Fprintf(w, "%s log %d lines were dropped: the writer fell behind\n", l.now().Format(time.RFC3339), n)
	}
	_ = w.Flush()
}

// Close writes what is queued and closes the file. It waits a little for a slow disk, and
// gives up after that rather than hold the exit.
func (l *fileLog) Close() {
	if l == nil {
		return
	}
	l.write("log", "gvim exited")
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.lines)
	}
	l.mu.Unlock()
	select {
	case <-l.done:
	case <-time.After(2 * time.Second):
	}
	_ = l.f.Close()
}

// oneLine puts a message on one line: a newline would make one entry look like several.
func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " | ", "\n", " | ", "\r", " ").Replace(s)
}

// startLogFile opens the --log-file at path, if there is one, and logs the config problems that
// were found before it could. It returns the log, or nil with the notice that says why there is
// none (the editor runs without a log rather than refuse to start).
func startLogFile(path string, diags []config.Diagnostic) (*fileLog, string) {
	if path == "" {
		return nil, ""
	}
	l, err := openLogFile(path)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: log file:", err)
		return nil, "cannot write the log file: " + err.Error()
	}
	for _, d := range diags {
		l.write("config", d.String())
	}
	return l, ""
}
