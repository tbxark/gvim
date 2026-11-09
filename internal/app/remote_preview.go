package app

// Remote file preview for the quick-open palette (task-116): the head of a remote file is
// read through the workspace authority (bounded, no local fallback), so the palette's
// existing LRU, stale-sequence and "reader is called from a command" machinery works
// unchanged.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/tui/palette"
)

const (
	// remotePreviewMaxBytes caps one remote preview read regardless of what the caller
	// asks for (the palette asks 16 KiB today).
	remotePreviewMaxBytes = 64 << 10
	// remotePreviewMinBytes is the smallest head a too-large file is retried at before the
	// read gives up with an error (never pretending a partial head is complete).
	remotePreviewMinBytes = 1 << 10
	// remotePreviewTimeout bounds one remote preview read.
	remotePreviewTimeout = 6 * time.Second
)

// remotePreviewer is the palette's Previewer over a remote workspace: it reads the head of
// the remote file through authority.Files() and reports truncation honestly. A disconnected
// authority is an error, never a local read.
type remotePreviewer struct {
	slot authority.Slot
}

// newRemotePreviewer returns the remote previewer over slot.
func newRemotePreviewer(slot authority.Slot) *remotePreviewer {
	return &remotePreviewer{slot: slot}
}

// ReadHead returns at most maxBytes of the remote file at path. truncated reports that the
// file continues past the returned bytes. The authority's windowed ReadRange returns a short
// head of a large file (a short answer is io.EOF), so the palette shows the real head with an
// honest truncation flag; a backend without ReadRange (ErrUnsupportedFileIO) falls back to a
// bounded whole-file ReadFile, which can only return a complete small file and reports the
// too-large error for a larger one instead of pretending a head is the whole file.
func (p *remotePreviewer) ReadHead(path string, maxBytes int) ([]byte, bool, error) {
	if p.slot == nil {
		return nil, false, errors.New("remote workspace is disconnected")
	}
	if !p.slot.Connected() {
		return nil, false, errors.New("remote workspace is disconnected")
	}
	files := p.slot.Files()
	if files == nil {
		return nil, false, errors.New("remote workspace has no file system")
	}
	if maxBytes <= 0 {
		maxBytes = remotePreviewMinBytes
	}
	if maxBytes > remotePreviewMaxBytes {
		maxBytes = remotePreviewMaxBytes
	}
	ctx, cancel := context.WithTimeout(context.Background(), remotePreviewTimeout)
	defer cancel()
	// One byte past the window tells whether the file continues; the extra byte is
	// dropped and only reported as truncated.
	data, err := files.ReadRange(ctx, path, 0, int64(maxBytes)+1)
	if err == nil || errors.Is(err, io.EOF) {
		if len(data) > maxBytes {
			return data[:maxBytes], true, nil
		}
		return data, false, nil
	}
	if !errors.Is(err, authority.ErrUnsupportedFileIO) {
		return nil, false, err
	}
	// A backend without the windowed primitive: read the bounded whole file. A file that
	// fits is complete; a larger one is an error, never a silently truncated head.
	data, err = files.ReadFile(ctx, path, int64(maxBytes))
	if err != nil {
		return nil, false, err
	}
	return data, false, nil
}

// ReadWindow is the remote Live Grep preview: it reads the remote file through the
// authority in bounded ReadRange chunks, stops at a scan budget, and returns the complete
// lines around the 1-based match line (five lines before, twelve lines maximum). It is
// cancellable through ctx (the palette cancels it when the selection moves), never reads the
// local disk, and falls back to a bounded whole-file ReadFile only when the backend has no
// windowed primitive.
func (p *remotePreviewer) ReadWindow(ctx context.Context, path string, line, maxBytes int) ([]byte, int, bool, error) {
	if p.slot == nil {
		return nil, 0, false, errors.New("remote workspace is disconnected")
	}
	if !p.slot.Connected() {
		return nil, 0, false, errors.New("remote workspace is disconnected")
	}
	files := p.slot.Files()
	if files == nil {
		return nil, 0, false, errors.New("remote workspace has no file system")
	}
	if line < 1 {
		line = 1
	}
	if maxBytes <= 0 {
		maxBytes = remotePreviewMinBytes
	}
	if maxBytes > remotePreviewMaxBytes {
		maxBytes = remotePreviewMaxBytes
	}
	const (
		chunk      = 64 << 10
		scanBudget = 8 << 20
		before     = 5
		maxLines   = 12
	)
	runCtx, cancel := context.WithTimeout(ctx, remotePreviewTimeout)
	defer cancel()
	var data []byte
	eof := false
	for len(data) < scanBudget {
		if err := runCtx.Err(); err != nil {
			return nil, 0, false, err
		}
		part, err := files.ReadRange(runCtx, path, int64(len(data)), chunk)
		if len(part) > 0 {
			data = append(data, part...)
		}
		if err == nil {
			if len(part) == 0 {
				eof = true
				break
			}
			if strings.Count(string(data), "\n") >= line+maxLines {
				break
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			eof = true
			break
		}
		if errors.Is(err, authority.ErrUnsupportedFileIO) {
			data, err = files.ReadFile(runCtx, path, scanBudget)
			if err != nil {
				return nil, 0, false, err
			}
			eof = true
			break
		}
		return nil, 0, false, err
	}
	if len(data) == 0 {
		return nil, 0, false, nil
	}
	lines := strings.Split(string(data), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	} else if !eof && len(lines) > 0 {
		// The scan stopped on a budget/line bound, not EOF: the last line may be partial.
		lines = lines[:len(lines)-1]
	}
	first := line - before
	if first < 1 {
		first = 1
	}
	if first > len(lines) {
		return nil, 0, false, nil
	}
	last := min(first+maxLines-1, len(lines))
	if last < first {
		return nil, 0, false, nil
	}
	out := strings.Join(lines[first-1:last], "\n") + "\n"
	truncated := last < len(lines) || len(data) >= scanBudget
	if len(out) > maxBytes {
		cut := maxBytes
		if i := strings.LastIndexByte(out[:maxBytes], '\n'); i >= 0 {
			cut = i + 1
		}
		out = out[:cut]
		truncated = true
	}
	return []byte(out), first, truncated, nil
}

var _ palette.WindowPreviewer = (*remotePreviewer)(nil)

// Bounds of the search-panel window preview. It reads through the authority's chunked
// ReadRange, never the local disk: one line is capped, the whole scan is capped, and the
// amount returned to the panel is capped. A window that could not reach the hit before
// the scan budget is reported as incomplete instead of silently showing the file head.
const (
	remotePreviewWindowChunk     = 64 << 10
	remotePreviewWindowScanBytes = 8 << 20
	remotePreviewWindowMaxLine   = 256 << 10
	remotePreviewWindowMaxOutput = remoteSearchPreviewHeadBytes
)

var (
	errRemotePreviewScanBudget = errors.New("remote preview window is past the scan budget")
	errRemotePreviewBinary     = errors.New("preview: the remote file is binary")
	errRemotePreviewNotUTF8    = errors.New("preview: the remote file is not UTF-8 text")
)

// remotePreviewWindow is a bounded line window: Lines (complete lines), the 1-based file
// line of Lines[0], the known total line count when EOF was reached (zero when the scan
// stopped at the window), truncated when the window/byte budget cut the preview, and
// budget when the scan budget stopped before the requested window (partial lines may be
// present).
type remotePreviewWindow struct {
	lines     []string
	first     int
	total     int
	truncated bool
	budget    bool
}

// remoteRangeReader is an io.Reader over authority ReadRange chunks. It stops with
// errRemotePreviewScanBudget after the byte budget, so a deep match cannot turn the
// preview into an unbounded read.
type remoteRangeReader struct {
	ctx     context.Context
	files   authority.FileSystem
	path    string
	off     int64
	scanned int
	eof     bool
	carry   []byte // an incomplete UTF-8 rune at a chunk boundary
}

// validate checks every byte the scan reads for NUL/control binary markers and invalid
// UTF-8. A window is not a place to silently sanitize a binary/legacy-encoded remote file
// into a plausible-looking excerpt; the caller gets the explicit reason instead. A
// multi-byte rune split at a chunk boundary is carried to the next read.
func (r *remoteRangeReader) validate(data []byte) error {
	buf := append(r.carry, data...)
	r.carry = r.carry[:0]
	for len(buf) > 0 {
		if buf[0] == 0 {
			return errRemotePreviewBinary
		}
		rn, size := utf8.DecodeRune(buf)
		if rn == utf8.RuneError && size == 1 {
			if !utf8.FullRune(buf) {
				r.carry = append(r.carry[:0], buf...)
				return nil
			}
			return errRemotePreviewNotUTF8
		}
		buf = buf[size:]
	}
	return nil
}

// Read implements io.Reader over the authority's windowed read.
func (r *remoteRangeReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.eof {
		return 0, io.EOF
	}
	if r.scanned >= remotePreviewWindowScanBytes {
		return 0, errRemotePreviewScanBudget
	}
	if len(p) > remotePreviewWindowChunk {
		p = p[:remotePreviewWindowChunk]
	}
	data, err := r.files.ReadRange(r.ctx, r.path, r.off, int64(len(p)))
	if len(data) > 0 {
		if verr := r.validate(data); verr != nil {
			return 0, verr
		}
		copy(p, data)
		r.off += int64(len(data))
		r.scanned += len(data)
	}
	// Only io.EOF ends the scan. Any other error with data is returned to the scanner
	// (which surfaces it after the already-delivered tokens) rather than being mistaken
	// for a clean end of file; the caller then reports the partial/failed read.
	if err != nil {
		if errors.Is(err, io.EOF) {
			r.eof = true
			if len(r.carry) > 0 {
				return 0, errRemotePreviewNotUTF8
			}
			if len(data) == 0 {
				return 0, io.EOF
			}
			return len(data), nil
		}
		if len(data) > 0 {
			return len(data), err
		}
		return 0, err
	}
	if len(data) == 0 {
		return 0, io.EOF
	}
	return len(data), nil
}

// readRemotePreviewWindow scans the remote file from its start (authority ReadRange has
// no line index) until firstLine and lineCount complete lines are collected. The scan is
// bounded by remotePreviewWindowScanBytes and the context; a hit beyond that budget is
// returned with budget=true rather than pretending the file starts there.
func readRemotePreviewWindow(ctx context.Context, files authority.FileSystem, path string, firstLine, lineCount int) (remotePreviewWindow, error) {
	var out remotePreviewWindow
	if firstLine < 1 {
		firstLine = 1
	}
	if lineCount < 1 {
		lineCount = 1
	}
	rd := &remoteRangeReader{ctx: ctx, files: files, path: path}
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, remotePreviewWindowChunk), remotePreviewWindowMaxLine)
	lineNo := 0
	bytesOut := 0
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		lineNo++
		if lineNo < firstLine {
			continue
		}
		if len(out.lines) >= lineCount {
			// One more line after the window: the file continues and the total is
			// unknown. This is an honest window-only preview.
			out.truncated = true
			out.total = 0
			return out, nil
		}
		line := strings.TrimRight(sc.Text(), "\r")
		if len(out.lines) == 0 {
			out.first = lineNo
		}
		if len(out.lines) == 0 && len(line) > remotePreviewWindowMaxOutput {
			line = line[:remotePreviewWindowMaxOutput]
			out.truncated = true
		}
		if bytesOut+len(line)+1 > remotePreviewWindowMaxOutput {
			out.truncated = true
			return out, nil
		}
		out.lines = append(out.lines, line)
		bytesOut += len(line) + 1
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, errRemotePreviewScanBudget) {
			out.truncated = true
			out.budget = true
			return out, errRemotePreviewScanBudget
		}
		return out, err
	}
	// EOF: the total line count is known when no earlier window stop happened.
	if out.total == 0 {
		out.total = lineNo
	}
	return out, nil
}
