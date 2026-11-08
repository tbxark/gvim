package document

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/encoding"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrLargeFile is returned by an action that cannot work on a document in
// large file mode.
var ErrLargeFile = errors.New("not available in large file mode")

// ErrIndexing is returned by an action that needs the whole line index of a
// large document before the index is complete.
var ErrIndexing = errors.New("line index is still being built")

// ErrTooLargeToTranscode is returned when a file in an encoding other than UTF-8
// is too large to load whole, which is the only way to open it.
var ErrTooLargeToTranscode = errors.New("too large to transcode")

// ErrLargeNeedsFullLoad is returned when a large file is not UTF-8. Such a file
// cannot be read lazily: it has to be decoded as a whole. The caller asks the
// user, and LoadFullFunc loads the file if the answer is yes.
type ErrLargeNeedsFullLoad struct {
	// Path is the file, Size its size in bytes, and Codec the name of the
	// encoding that was detected, as encoding names it.
	Path  string
	Size  int64
	Codec string
}

// Error describes the file as "<encoding> (<size>) requires full load".
func (e ErrLargeNeedsFullLoad) Error() string {
	name := labels[e.Codec]
	if name == "" {
		name = e.Codec
	}
	return fmt.Sprintf("%s (%s) requires full load", name, formatSize(e.Size))
}

const (
	// largeSampleSize is how much of the head of a large file decides its
	// encoding, its line ending and whether it is binary.
	largeSampleSize = 64 << 10
	// maxFullLoad is the largest file that LoadFullFunc loads whole.
	maxFullLoad = 256 << 20
)

// sourceOpener opens the source of a large document. Tests set it to read from a
// generated source instead of a file.
type sourceOpener struct {
	Open func(path string) (buffer.Source, error)
}

// largeSpec is how a load decides whether a file opens in large file mode.
type largeSpec struct {
	// threshold is the file size from which a file is large. Zero means large
	// file mode is off.
	Threshold int64
	Geom      buffer.LargeOptions
	Open      func(path string) (buffer.Source, error)
	EstLine   int
	// fullMax is the largest file LoadFullFunc loads.
	FullMax int64
	// batch is the bytes one index job reads.
	Batch int64
	// stashDir is where unsaved edits are stashed when the file changes under a document.
	StashDir string
}

// loadSpec is what a load of a file needs from the editor's options. It is
// copied out of the editor, so the load can run in a command.
type loadSpec struct {
	Eol       string
	AutoRO    bool
	LargeConf largeSpec
	// Access is the file I/O seam of the load: nil means the local filesystem, non-nil
	// means every read and stat of the new document goes through it (a remote
	// authority). The loaded document keeps it for its saves.
	Access FileAccess
}

// orDefault returns v, or def when v is zero.
func orDefault(v, def int64) int64 {
	if v == 0 {
		return def
	}
	return v
}

// isLarge reports whether a file of size bytes opens in large file mode.
func (s largeSpec) IsLarge(size int64) bool {
	return s.Threshold > 0 && size >= s.Threshold
}

// sampleInfo is what the head of a large file says about the whole file.
type sampleInfo struct {
	Codec  string // utf-8 or utf-8-bom; for a binary file iso-8859-1
	Skip   int64  // bytes of byte-order mark before the text
	Eol    string
	binary bool
	Label  string
}

// utf8BOM is the byte-order mark of UTF-8 text.
var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// sampleOf reads the head of src. truncated is true when src is longer.
func sampleOf(src buffer.Source) (sample []byte, truncated bool, err error) {
	size := src.Size()
	sample = make([]byte, min(size, largeSampleSize))
	n, err := src.ReadAt(sample, 0)
	switch {
	case n == len(sample):
	case err == nil || errors.Is(err, io.EOF):
		// The file is shorter than it was when it was opened.
		return nil, false, buffer.ErrSourceChanged
	default:
		return nil, false, err
	}
	return sample, size > int64(len(sample)), nil
}

// validUTF8Head reports whether head is valid UTF-8. When head was cut out of a longer
// file, it may end inside a character.
func validUTF8Head(head []byte, truncated bool) bool {
	if utf8.Valid(head) {
		return true
	}
	if !truncated {
		return false
	}
	for cut := 1; cut <= utf8.UTFMax-1 && cut < len(head); cut++ {
		tail := head[len(head)-cut:]
		if utf8.Valid(head[:len(head)-cut]) && utf8.RuneStart(tail[0]) && !utf8.FullRune(tail) {
			return true
		}
	}
	return false
}

// detectEOL is the line ending of data by the rule of splitLineEnding: CRLF
// only when its breaks outnumber the bare LF breaks.
func detectEOL(data []byte) string {
	crlf := bytes.Count(data, []byte("\r\n"))
	if crlf > bytes.Count(data, []byte("\n"))-crlf {
		return eolCRLF
	}
	return eolLF
}

// readSample decides from the head of a large file how to open it. A file whose
// head is not UTF-8 text returns an ErrLargeNeedsFullLoad, because only UTF-8 is read
// lazily.
func readSample(path string, src buffer.Source) (sampleInfo, error) {
	sample, truncated, err := sampleOf(src)
	if err != nil {
		return sampleInfo{}, fmt.Errorf("open %s: %w", path, err)
	}
	info := sampleInfo{Codec: defaultCodec}
	body := sample
	switch {
	case bytes.HasPrefix(sample, utf8BOM):
		info.Codec, info.Skip, body = "utf-8-bom", int64(len(utf8BOM)), sample[len(utf8BOM):]
	case bytes.HasPrefix(sample, []byte{0xff, 0xfe}), bytes.HasPrefix(sample, []byte{0xfe, 0xff}):
		return sampleInfo{}, ErrLargeNeedsFullLoad{Path: path, Size: src.Size(), Codec: encoding.Detect(sample)}
	}
	valid := validUTF8Head(body, truncated)
	if encoding.IsBinary(body) || (valid && bytes.IndexByte(body, 0) >= 0) {
		return sampleInfo{Codec: "iso-8859-1", Eol: detectEOL(sample), binary: true, Label: "Binary"}, nil
	}
	if !valid {
		return sampleInfo{}, ErrLargeNeedsFullLoad{Path: path, Size: src.Size(), Codec: encoding.Detect(body)}
	}
	info.Eol = detectEOL(body)
	info.Label = encodingLabel(info.Codec, string(body))
	return info, nil
}

// readLargeDocument opens the file at path as a large document. The text stays in the
// file: the buffer reads lines from it as they are needed. It reads the head of the
// file and indexes the first few MiB, never the whole file. disk is the state of the
// file taken before the read. It returns ErrLargeNeedsFullLoad for a file that is not
// UTF-8.
func readLargeDocument(path string, disk diskState, spec largeSpec) (*Document, error) {
	openSrc := spec.Open
	if openSrc == nil {
		openSrc = buffer.OpenFileSource
	}
	src, err := openSrc(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := readSample(path, src)
	if err != nil {
		_ = src.Close()
		var need ErrLargeNeedsFullLoad
		if errors.As(err, &need) && spec.FullMax > 0 && need.Size > spec.FullMax {
			// Answering yes could not help: the file is over what a full load reads.
			err = fmt.Errorf("open %s: %w", path, ErrTooLargeToTranscode)
		}
		return nil, err
	}
	geom := spec.Geom
	geom.Skip = info.Skip
	buf, err := buffer.OpenLarge(src, geom)
	if err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	d := &Document{Path: path, Buffer: buf, Want: -1, Eol: info.Eol}
	d.Disk = disk
	d.Codec = info.Codec
	d.encoding = info.Label
	d.large = &largeDoc{
		Size: src.Size(), EstLine: spec.EstLine, Batch: spec.Batch, Notes: leftoverTemps(path),
		Mtime: mtimeNano(disk), Skip: info.Skip, StashDir: spec.StashDir,
	}
	if info.binary {
		d.readOnly, d.binary = true, true
	}
	return d, nil
}

// leftoverTemps names the temporary files that an interrupted save of path left
// next to it. A save writes ".NAME.gvim-*" in the directory of the file and renames
// it over the file only when it is complete.
func leftoverTemps(path string) []string {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return nil
	}
	prefix := "." + filepath.Base(path) + ".gvim-"
	var out []string
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), prefix) {
			out = append(out, filepath.Join(filepath.Dir(path), ent.Name()))
		}
	}
	return out
}

// formatSize writes a byte count for the status row, such as "2.1 GB".
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	size, suffix := float64(n), ""
	for _, s := range []string{"KB", "MB", "GB", "TB"} {
		size /= unit
		suffix = s
		if size < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", size, suffix)
}

// largeLimitsHint is the start of the status of a large document that was just opened:
// what the document cannot do. It is short enough to survive the cut of the status row.
const largeLimitsHint = "LARGE: folds, wrap, LSP, auto-save off."

// largeOpenStatus is the status row text for a large document that was just opened.
func (d *Document) largeOpenStatus() string {
	var sb strings.Builder
	// The status row cuts the message to about 40 cells on a terminal of 120 columns,
	// so what the user must know comes first, and fits.
	sb.WriteString(largeLimitsHint + " ")
	if d.binary {
		sb.WriteString("Binary file: read-only. ")
	}
	fmt.Fprintf(&sb, "Large file (%s)", formatSize(d.large.Size))
	if d.large.RemoteWindowed {
		sb.WriteString(" remote large file (read-only, windowed): the bytes stay on the remote host and load as you scroll")
	} else if d.readOnly && !d.binary {
		sb.WriteString(" (read-only)")
	}
	if info := d.Index(); info.Complete {
		noun := "lines"
		if info.Lines == 1 {
			noun = "line"
		}
		fmt.Fprintf(&sb, ": %d %s.", info.Lines, noun)
	} else {
		sb.WriteString(": line index loading in the background.")
	}
	sb.WriteString(" Syntax is limited to the visible lines; change marks are off.")
	for _, p := range d.large.Notes {
		fmt.Fprintf(&sb, " Leftover temp file %s from an interrupted save.", p)
	}
	return sb.String()
}

// Large reports whether the document is in large file mode.
func (d *Document) Large() bool { return d.large != nil }

// Index reports how much of the line index of the document exists. A document that
// is not large has its whole index, with no source behind it.
func (d *Document) Index() buffer.IndexInfo {
	est := 0
	if d.large != nil {
		est = d.large.EstLine
	}
	return d.Buffer.Index(est)
}

// CheckSource compares the file of a large document with its state when it was opened.
// A file that shrank or was rewritten in place freezes the document: the lines that
// are cached keep showing, and the document turns read-only, because nothing more can
// be read or saved safely. A file that only grew keeps working, but a save over it is
// refused. The caller runs it from the disk poll. It returns the buffer's error, and
// nil for a document that is not large.
func (d *Document) CheckSource() error {
	if d.large == nil {
		return nil
	}
	err := d.Buffer.CheckSource()
	if err == nil || err == d.large.Noted {
		// The poll calls this again and again; the status says each thing once.
		return err
	}
	d.large.Noted = err
	switch {
	case errors.Is(err, buffer.ErrSourceChanged):
		d.readOnly = true
		d.Fail(errFileChanged)
	case errors.Is(err, buffer.ErrSourceGrew):
		d.Fail(errFileGrew)
	default:
		return err
	}
	// The edits cannot be saved over this file, so they are stashed at once, and the
	// status says where.
	if note := d.StashUnsaved(); note != "" {
		d.Status += ". " + note
		d.StatusErr = d.Status
	}
	return err
}

// frozen reports whether the file of a large document changed under it, so that the
// document can read no more of it. Such a document is read-only until it is reverted.
func (d *Document) Frozen() bool {
	return d.large != nil && d.Buffer.SourceErr() != nil
}
