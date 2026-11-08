package document

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tbxark/gvim/buffer"
)

// OverlayVersion is the only overlay stash format version this package reads and
// writes.
const OverlayVersion = 1

// The overlay stash file names an overlay is stored under (see StashOverlay).
const (
	overlayPrefix = "overlay-"
	overlaySuffix = ".json"
)

// OverlayStash is the unsaved edits of a large file, kept without the file's
// text: the edited lines and which lines of the file stay as they are. It is
// valid only over the file it was taken from, which the identity fields name.
// A session keeps it beside the session state, and a crash recovery copy keeps
// it as it is.
type OverlayStash struct {
	Version int `json:"version"`
	// Path is the file the edits belong to.
	Path string `json:"path"`
	// Size and MTimeUnixNano are the size and modification time of the file the
	// document read, Skip the bytes of byte-order mark before its text, and EOL the
	// line ending of the document. The overlay is put back only onto a file that
	// has the same values.
	Size          int64  `json:"size"`
	MTimeUnixNano int64  `json:"mtimeUnixNano"`
	Skip          int64  `json:"skip,omitzero"`
	EOL           string `json:"eol,omitempty"`
	// Overlay is the edits.
	Overlay buffer.OverlayState `json:"overlay"`
}

// StashOverlay stores the overlay of a large file in a new stash file in dir and
// returns the file's name. Put the name in the session's FileState.OverlayFile so
// that a session save keeps the file. The file is JSON, so the edited lines can be
// read from it by hand.
func StashOverlay(dir string, o OverlayStash) (string, error) {
	o.Version = OverlayVersion
	data, err := json.Marshal(o, jsontext.Multiline(true))
	if err != nil {
		return "", fmt.Errorf("session: encode overlay: %w", err)
	}
	return writeStash(dir, overlayPrefix+"*"+overlaySuffix, data)
}

// ReadOverlay returns the overlay stash file name in dir. name must be an overlay
// stash name returned by StashOverlay.
func ReadOverlay(dir, name string) (OverlayStash, error) {
	if !isOverlayName(name) {
		return OverlayStash{}, fmt.Errorf("session: invalid overlay stash name %q", name)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return OverlayStash{}, err
	}
	var o OverlayStash
	if err := json.Unmarshal(data, &o); err != nil {
		return OverlayStash{}, fmt.Errorf("session: decode %s: %w", name, err)
	}
	if o.Version != OverlayVersion {
		return OverlayStash{}, fmt.Errorf("session: unsupported overlay version %d, want %d", o.Version, OverlayVersion)
	}
	return o, nil
}

// IsOverlayName reports whether name is an overlay stash file name.
func IsOverlayName(name string) bool {
	return isOverlayName(name)
}

func isOverlayName(name string) bool {
	return strings.HasPrefix(name, overlayPrefix) &&
		strings.HasSuffix(name, overlaySuffix) &&
		filepath.Base(name) == name
}

// writeStash writes data to a new file in dir, named by pattern, and syncs it.
func writeStash(dir, pattern string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return filepath.Base(f.Name()), nil
}
