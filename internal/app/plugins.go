package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	goruntime "runtime"

	"github.com/tbxark/gvim/internal/editor"
	palmodel "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/ui"
	uipalette "github.com/tbxark/gvim/internal/ui/palette"
)

var (
	_ uipalette.PluginRunner = (*pluginRunner)(nil)
	_ plugin.Editor          = (*pluginEditor)(nil)
)

// pluginDir returns the folder that holds the plugin files: gvim/plugins under
// the user config directory.
func pluginDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("plugin dir: %w", err)
	}
	return filepath.Join(dir, "gvim", "plugins"), nil
}

// loadPlugins starts the plugin host over the plugin folder and loads every plugin
// in it. It returns the runner that the palette and the root use, and the notices
// for the status row: one for each plugin that failed to load or did not load within
// the host's load budget, and any text the plugins showed while loading. A missing
// folder means no plugins, and gives no notice. timeout bounds each plugin call; zero
// means the host default.
//
// The editor adapter remembers the goroutine that calls loadPlugins, and every
// adapter method checks it. The caller must be the UI goroutine. app.Run builds
// the runtime on the goroutine that then runs the program, so this holds there.
func loadPlugins(ed *editor.Editor, state ui.EditorState, timeout time.Duration) (*pluginRunner, []string) {
	pe := &pluginEditor{ed: ed, state: state, owner: goroutineID()}
	r := &pluginRunner{editor: pe}
	dir, err := pluginDir()
	if err != nil {
		return r, []string{"plugins: " + err.Error()}
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	host, err := plugin.New(plugin.Options{Dir: dir, Editor: pe, Timeout: timeout})
	if err != nil {
		return r, []string{"plugins: " + err.Error()}
	}
	r.host, r.dir = host, dir
	var notices []string
	for _, err := range host.Load() {
		notices = append(notices, r.short(err))
	}
	if text := pe.take(); text != "" {
		notices = append(notices, text)
	}
	return r, notices
}

// pluginRunner is the plugin host of a session, as the palette and the root see
// it. host is nil when no plugin folder could be used, and then there are no
// commands.
type pluginRunner struct {
	editor *pluginEditor
	host   *plugin.Host
	dir    string
}

// Commands returns one palette entry per plugin command. A command with an empty
// label is shown by its name.
func (r *pluginRunner) Commands() []palmodel.Entry {
	if r.host == nil {
		return nil
	}
	cmds := r.host.Commands()
	out := make([]palmodel.Entry, 0, len(cmds))
	for _, c := range cmds {
		label := c.Label
		if label == "" {
			label = c.Name
		}
		out = append(out, palmodel.PluginEntry(c.Name, label))
	}
	return out
}

// Run runs the command called name on the calling goroutine, which is the UI
// goroutine, bounded by the host's timeout. It returns the notices the command
// left and the error that stopped it. Notices from an earlier call are dropped
// first.
func (r *pluginRunner) Run(name string) (string, error) {
	if r.host == nil {
		return "", errors.New("plugin: no plugins are loaded")
	}
	r.editor.take()
	err := r.host.Run(name)
	notice := r.editor.take()
	if err != nil {
		return notice, errors.New(r.short(err))
	}
	return notice, nil
}

// short returns the text of err with the plugin folder cut from it, so that the
// file name comes first in the status row. It is only called when r.host is set,
// so r.dir is not empty.
func (r *pluginRunner) short(err error) string {
	return strings.ReplaceAll(err.Error(), r.dir+string(filepath.Separator), "")
}

// pluginEditor is the plugin.Editor of a session. The host calls its methods from
// Load and Run, and both run on the UI goroutine. A call from any other goroutine
// panics, because the editor is not safe to use there.
type pluginEditor struct {
	ed    *editor.Editor // reads the active document
	state ui.EditorState // inserts text, so that the syntax caches see the edit
	owner uint64         // the goroutine that built the adapter
	notes []string       // the Notify and SetStatus texts since the last take
}

// onUI panics when the caller is not the goroutine that built p.
func (p *pluginEditor) onUI() {
	if goroutineID() != p.owner {
		panic("plugin: editor call from a goroutine other than the UI goroutine")
	}
}

// Notify keeps msg as a notice for the status row.
func (p *pluginEditor) Notify(msg string) {
	p.onUI()
	p.notes = append(p.notes, msg)
}

// SetStatus keeps text as a notice for the status row.
func (p *pluginEditor) SetStatus(text string) {
	p.onUI()
	p.notes = append(p.notes, text)
}

// InsertText types text at the caret of the active document, as one insert_char
// action. It fails the way typing does: with no document open, or with a read-only
// one.
func (p *pluginEditor) InsertText(text string) error {
	p.onUI()
	return p.state.Do(editor.Action("insert_char"), map[string]any{"text": text}).Err
}

// ActivePath returns the file path of the active document. It is "" when no
// document is open or the document has no file yet.
func (p *pluginEditor) ActivePath() string {
	p.onUI()
	if doc := p.ed.Active(); doc != nil {
		return doc.Path
	}
	return ""
}

// take returns the notes joined by "; " and clears them. Newlines become spaces,
// so the notice stays on one row.
func (p *pluginEditor) take() string {
	text := strings.ReplaceAll(strings.Join(p.notes, "; "), "\n", " ")
	p.notes = nil
	return text
}

// goroutineID returns the id of the calling goroutine. The runtime does not export
// it, so it is read from the first line of the stack trace, "goroutine N [state]:".
func goroutineID() uint64 {
	var buf [64]byte
	n := goruntime.Stack(buf[:], false)
	fields := strings.Fields(string(buf[:n]))
	if len(fields) < 2 {
		return 0
	}
	id, _ := strconv.ParseUint(fields[1], 10, 64)
	return id
}
