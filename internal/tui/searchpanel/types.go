// Package searchpanel is the project-wide Search and Replace panel and the Git Grep
// results panel of gvim. It is a sheet: one card docked to the bottom rows of the frame,
// with the search and replacement fields, the match-option toggles, the grouped file and
// match rows and the key hints on the bottom border.
//
// The panel does no IO. A search is a tea.Cmd over the plain ports declared here, which
// the app adapters implement, and the result comes back as a message that this package
// applies. The panel never imports internal/search: the match and edit types below are
// plain, and the app does the search and the replacement expansion.
//
// The root model stacks the panel as an overlay and keeps it after it closes, so the
// next and previous match actions can walk the last result set from the editor.
package searchpanel

import (
	"context"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// DefaultLimit is the most matches a project search returns when Options.Limit is zero:
// the same cap Fresh's project search uses.
const DefaultLimit = 10000

// MatchOptions are how a project search query is interpreted.
type MatchOptions struct {
	CaseSensitive bool
	WholeWord     bool
	Regex         bool
}

// Scope is what a project search searches.
type Scope int

const (
	// AllFiles searches the project's files, with the open buffers' unsaved text in
	// place of their files on disk.
	AllFiles Scope = iota
	// OpenBuffers searches only the open buffers, unsaved text included: the project's
	// files on disk are not walked.
	OpenBuffers
	// CurrentFile searches only the active buffer, unsaved text included.
	CurrentFile
)

// SourceFile is an open buffer's text, so a search sees unsaved edits.
type SourceFile struct {
	Path  string // absolute path, or "" for an unnamed buffer
	Name  string // the tab label, for display
	Text  string // the buffer text, LF-normalised
	Dirty bool   // the buffer is open, so its text may differ from the disk
}

// SearchRequest is one project-wide search.
type SearchRequest struct {
	Root    string
	Query   string
	Options MatchOptions
	Scope   Scope
	// Files is the file index snapshot, for AllFiles. The panel fills it in the
	// command, never in Update (already filtered by Glob).
	Files []string
	// Glob is the raw file-filter value. The panel uses it to filter Files for the
	// local searcher and carries it so a port that enumerates files itself (the remote
	// project search) can apply the same filter when the index snapshot is empty or
	// stale.
	Glob string
	// FilesKnown reports that Files is the panel's file-index snapshot (possibly still
	// loading) rather than an absent/legacy filter. FilesReady is true when that index is
	// fully loaded: a ready snapshot that filters to zero files means a genuinely empty
	// result, while a loading/unknown snapshot permits a bounded whole-tree fallback.
	FilesKnown bool
	FilesReady bool
	// The optional detailed finder state behind Files: an index can be not ready because
	// it is loading, failed, truncated or is finder.Idle's placeholder; Ready alone is
	// true only when the caller's capability confirmed a complete, error-free snapshot.
	FilesUnknown   bool
	FilesIdle      bool
	FilesTruncated bool
	FilesClosed    bool
	FilesError     string
	FilesNotice    string
	// Current is the active buffer, for CurrentFile.
	Current *SourceFile
	// OpenFiles are the open buffers' text; a file here is searched from the snapshot
	// instead of the disk, so unsaved edits are seen.
	OpenFiles []SourceFile
	// Limit is the most matches to return.
	Limit int
}

// Match is one matching line of a file.
type Match struct {
	Line, Col int    // 1-based start of the match
	Len       int    // the match's length in runes on its first line
	Text      string // the match's first line, trailing blanks removed
	// EndLine and EndCol are the 1-based exclusive end of the match, so a match
	// that spans lines shows its whole range. For a single-line match EndLine is
	// Line and EndCol is Col+Len.
	EndLine, EndCol int
}

// MultiLine reports whether the match spans more than one line.
func (m Match) MultiLine() bool { return m.EndLine > m.Line }

// FileMatches is one file's matches.
type FileMatches struct {
	Path    string // absolute path
	Name    string // the path to show, relative to the root
	Hash    uint64 // hash of the text searched: the version a replacement verifies
	Dirty   bool   // searched from an open buffer's snapshot
	Matches []Match
}

// SearchResult is the result of one project search.
type SearchResult struct {
	Files     []FileMatches
	Truncated bool // the match or file cap was hit
	// Skipped counts the files left out of the search, and the summaries name the
	// groups: for example "12 large files", "3 binary files".
	SkippedCount int
	Skipped      []string
	// Notice is a non-fatal warning about the result (for example a degraded remote
	// ignore scope). It is shown without failing the search; "" when there is none.
	Notice string
	Err    string // the search failed
}

// ReplaceFile is one file a replacement applies to. Hash is the version the match
// offsets were planned from; Dirty marks a file searched from its open buffer, whose
// text the request carries so the replacement sees the unsaved edits.
type ReplaceFile struct {
	Path  string
	Name  string
	Hash  uint64
	Dirty bool
	Text  string // the open buffer's text, for a Dirty file
	// Selected are the 0-based start positions of the matches to replace, in the order
	// the result showed them. An empty list means every match of the file.
	Selected []Position
}

// Position is a 0-based position in a file.
type Position struct{ Line, Col int }

// ReplaceEdit replaces From..To of the old text with Text. Text is already expanded, so
// the root can hand the edit to the editor as it is.
type ReplaceEdit struct {
	From, To Position
	Text     string
}

// FileEdits is one file's planned replacement.
type FileEdits struct {
	Path  string
	Name  string
	Dirty bool
	Hash  uint64
	Edits []ReplaceEdit
	Err   string // the file could not be read or searched
	Stale bool   // the text changed since the search, so the file is not replaced
}

// ReplaceRequest is one project-wide replacement.
type ReplaceRequest struct {
	Root        string
	Query       string
	Replacement string
	Options     MatchOptions
	Files       []ReplaceFile
	// Matches is how many matches the replacement touches, for the confirmation.
	Matches int
	Limit   int
}

// ReplaceResult is the plan of one project-wide replacement.
type ReplaceResult struct {
	Files []FileEdits
	Err   string
}

// GitRequest is one git grep.
type GitRequest struct {
	Root          string
	Query         string
	CaseSensitive bool
	// Pathspecs narrow the search to the tracked paths they name. They are git pathspecs
	// (git's wildcard matching: '*' and '?' cross directories), passed to git after "--",
	// not the project-search glob syntax; the panel validates their brackets before it
	// sends them. Empty searches every tracked file.
	Pathspecs []string
	Limit     int
}

// GitResult is one git grep's matches, in the repository-root-relative paths that git
// prints. Path and Name both hold that path.
type GitResult struct {
	Files []FileMatches
	Err   string
}

// Searcher runs a project-wide search. The app implements it, and the panel calls it
// from a tea.Cmd.
type Searcher interface {
	ProjectSearch(SearchRequest) (SearchResult, error)
}

// SearchBatch is one incremental piece of a project search, reported by a BatchSearcher.
// A batch holds whole files in result order, so appending batches to the result keeps the
// finder's order: a file's matches are never split across two batches.
type SearchBatch struct {
	Files []FileMatches // the files this batch found, in result order
	// Scanned is how many files the search has examined so far, and Total how many it
	// will examine (0 when the port cannot say).
	Scanned, Total int
	Truncated      bool     // the match or file cap was hit
	SkippedCount   int      // files left out so far
	Skipped        []string // the skip summaries so far
	// Notice is a non-fatal warning carried with the batches (for example a degraded
	// remote ignore scope). The last non-empty Notice is shown with the result.
	Notice string
	// Done marks the last batch of a completed search. Err, when set, says why the
	// search stopped instead of returning a result.
	Done bool
	Err  string
}

// BatchSearcher is the optional streaming half of Searcher. The app implements it by
// starting one goroutine per request that sends whole-file batches on the returned channel,
// in result order, and closes the channel after the final batch (or when cancel closes): a
// completed search always ends with a Done batch, while a cancelled one just closes the
// channel. The caller must read the channel and owns the cancel channel: closing cancel
// asks the search to stop, and the search never sends once it has observed the close. The
// batches are bounded (a file count or a match count), so a reader sees progress instead
// of one large result.
type BatchSearcher interface {
	ProjectSearchBatches(SearchRequest, <-chan struct{}) <-chan SearchBatch
}

// Replacer plans a project-wide replacement. The app implements it, and the root calls
// it from a tea.Cmd after the user confirms the replacement.
type Replacer interface {
	ProjectReplace(ReplaceRequest) (ReplaceResult, error)
}

// GitGrepper runs git grep. The app implements it, and the panel calls it from a tea.Cmd.
type GitGrepper interface {
	GitGrep(GitRequest) (GitResult, error)
}

// PreviewRequest asks for one match's file context, for the preview column.
type PreviewRequest struct {
	Path  string
	Name  string // the display path of the file
	Dirty bool   // the text came from an open buffer
	// Text is the open buffer's text the panel already holds; empty means the preview
	// reads the file from disk (and applies the search read caps).
	Text string
	// Line, Col, EndLine and EndCol are the 1-based match range, End exclusive.
	Line, Col, EndLine, EndCol int
	// Start is the first file line the preview wants, 0-based. A negative Start asks
	// for the window centred on the match instead.
	Start int
	// Lines is the most context lines the preview wants.
	Lines int
}

// PreviewSpan is one syntax-coloured run of a preview line.
type PreviewSpan struct {
	Line       int    // 0-based file line
	Start, End int    // half-open rune columns on that line
	Kind       string // the syntax kind name, such as "keyword"; "plain" is uncoloured
}

// PreviewResult is one match's context: a window of the file's lines, with the syntax
// runs of those lines when the port can highlight them.
type PreviewResult struct {
	Start int      // 0-based file line of Lines[0]
	Lines []string // the context lines, tabs unexpanded
	// Total is the file's line count after the read caps, so the panel can show the
	// window's place and clamp scrolling. Zero means unknown.
	Total int
	// Spans are the syntax runs of the returned lines; empty means plain text (the port
	// cannot highlight, or the file is too large or has no grammar).
	Spans []PreviewSpan
	// Truncated says the preview is an honest partial view of a larger file: the port
	// read a bounded window and Lines/Total describe only what it could read. The header
	// marks it; Window says the bounded read was a window around the selected match
	// rather than a file head.
	Truncated bool
	Window    bool
	Err       string // why the file could not be read or previewed
}

// Previewer reads a match's context for the panel's preview column. The app
// implements it; the panel calls it from a tea.Cmd, never from Update. An empty
// PreviewResult.Err and no lines means the file has no lines to show.
type Previewer interface {
	Preview(PreviewRequest) (PreviewResult, error)
}

// WindowPreviewer is the optional bounded-window seam: instead of a file head it reads
// around the selected match (or the requested Start line). The remote adapter implements
// it through the authority's ReadRange with a scan budget; the panel prefers it over
// Preview when available and keeps the plain seam for local/test previewers.
type WindowPreviewer interface {
	PreviewWindow(PreviewRequest) (PreviewResult, error)
}

// ContextWindowPreviewer is WindowPreviewer with the panel's cancellation context, so a
// selection change/panel dismissal stops the authority read. A previewer implementing it
// is used before the context-only seam.
type ContextWindowPreviewer interface {
	PreviewWindowContext(context.Context, PreviewRequest) (PreviewResult, error)
}

// ContextPreviewer is the cancellable preview seam. The panel calls PreviewContext with a
// context that it cancels when the selected match/query/size changes, the column no longer
// wants the preview, or the panel closes, so a provider can stop an in-flight file or
// authority read instead of doing stale work. A Previewer that cannot take a context keeps
// working through Preview (the panel then only drops the stale answer by sequence).
type ContextPreviewer interface {
	PreviewContext(context.Context, PreviewRequest) (PreviewResult, error)
}

// Files is the file index snapshot the panel searches, for AllFiles. The finder index
// passed by the root implements it.
type Files interface {
	Snapshot() (files []string, loading bool)
}

// FileIndexState is the optional finder state behind a Files implementation. Known is
// false only when the caller supplied no Files capability at all; Ready means the
// snapshot is authoritative (complete, error-free, untruncated, not Idle and not closed).
type FileIndexState struct {
	Known     bool
	Ready     bool
	Idle      bool
	Truncated bool
	Closed    bool
	Unknown   bool
	Error     string
	Notice    string
}

// FileIndexHealth is the optional capability of a Files implementation that can report
// the snapshot and all of its state in one consistent observation (the finder locks one
// generation). The plain Files interface stays unchanged; a Files without this
// capability is treated as non-authoritative (a bounded fallback may run) rather than
// assuming that Snapshot's loading=false means complete.
type FileIndexHealth interface {
	IndexSnapshot() (files []string, loading, ready, idle, truncated, closed bool, notice string, err error)
}

// SnapshotWithState reads the caller's file-index snapshot and, when the optional
// FileIndexHealth capability exists, its completeness state from the same observation.
// Ready is never inferred from Snapshot's loading flag alone, and a closed index is never
// ready even if its stored state looked complete.
func SnapshotWithState(files Files) ([]string, FileIndexState) {
	if files == nil {
		return nil, FileIndexState{}
	}
	if health, ok := files.(FileIndexHealth); ok {
		snap, loading, ready, idle, truncated, closed, notice, err := health.IndexSnapshot()
		state := FileIndexState{
			Known: true, Closed: closed, Idle: idle, Truncated: truncated, Notice: notice,
		}
		state.Ready = ready && !loading && !closed
		if err != nil {
			state.Error = err.Error()
		}
		return snap, state
	}
	snap, _ := files.Snapshot()
	return snap, FileIndexState{Known: true, Unknown: true}
}

// ReplaceRequestMsg asks the root to confirm and apply a replacement. The panel sends it
// when the user presses Alt+Enter.
type ReplaceRequestMsg struct {
	Request ReplaceRequest
	Owner   palette.Owner
}

func (m ReplaceRequestMsg) PaletteOwner() palette.Owner { return m.Owner }

// ExportMsg asks the root to show the current result set as a navigable list: the
// quickfix-style export of a project search. The panel sends it when the user presses
// Alt+Q.
type ExportMsg struct{ Owner palette.Owner }

func (m ExportMsg) PaletteOwner() palette.Owner { return m.Owner }

// ExportItem is one row of an exported result set: a file header or one match. The
// root turns the items into its list overlay; the panel itself does not import it.
type ExportItem struct {
	Header bool
	// Name is the file's display path on a header row.
	Name string
	// Path, Line and Col locate a match row's file and its 1-based position.
	Path      string
	Line, Col int
	// Text is the match row's text: "line:col  the line".
	Text string
}

// OpenMsg asks the root to open (or switch to) a file at a position, or to move in the
// active buffer when Path is empty. It is what Enter on a match row sends.
type OpenMsg struct {
	Path      string
	Line, Col int
	Owner     palette.Owner
}

func (m OpenMsg) PaletteOwner() palette.Owner { return m.Owner }
