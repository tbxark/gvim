package buffer

import "errors"

// Limits shared by the buffer, the editor guards and UI messages. Large file
// mode is not wired in yet; the constants live here so later API signatures
// compile from the start.
const (
	// MaxMaterialize is the largest text Text and Range return in large file
	// mode. Anything bigger is refused (ErrOverCap).
	MaxMaterialize = 16 << 20
	// DefaultChunk is the byte size of one index chunk.
	DefaultChunk = 256 << 10
	// DefaultMaxLine is the longest line, in bytes, that large file mode reads
	// and edits.
	DefaultMaxLine = 1 << 20
	// InlineOpMax is the largest edit text a history operation stores inline.
	InlineOpMax = 256 << 10
	// DefaultMaxOverlayBytes caps the estimated memory of unsaved edits.
	DefaultMaxOverlayBytes = 256 << 20
	// DefaultMaxHistoryBytes caps the estimated memory the undo history keeps.
	DefaultMaxHistoryBytes = 512 << 20
	// MaxHistoryGroups caps the undo groups a large buffer keeps.
	MaxHistoryGroups = 10_000
)

// Errors reported by large file mode. Memory buffers never return them.
var (
	ErrOverCap       = errors.New("buffer: text too large to read in large file mode")
	ErrSourceChanged = errors.New("buffer: file changed on disk")
	// ErrSourcePending means an asynchronous window source has not loaded the
	// window a read needs. It is not a failure: the read returns at once and the
	// caller loads the window in a command, then repaints.
	ErrSourcePending = errors.New("buffer: source window not loaded")
	// ErrNotWindowed means a window operation was asked of a source that loads
	// synchronously (a local file, a memory buffer).
	ErrNotWindowed         = errors.New("buffer: source is not a windowed source")
	ErrSourceGrew          = errors.New("buffer: file grew on disk")
	ErrLineTooLong         = errors.New("buffer: line too long to edit")
	ErrInvalidUTF8         = errors.New("buffer: line is not valid UTF-8; not edited")
	ErrOverlayFull         = errors.New("buffer: too many unsaved bytes; save first")
	ErrLineOverBudget      = errors.New("buffer: line too big to edit: its runes do not fit the unsaved-edit budget")
	ErrIndexPending        = errors.New("buffer: line index is not complete")
	ErrUnsupportedPlatform = errors.New("buffer: large file mode needs a 64-bit platform")
)
