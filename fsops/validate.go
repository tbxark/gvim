package fsops

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Validate reports whether name is a legal single-element name for an entry
// directly inside root. It fails with an error wrapping ErrInvalidName when
// name is empty or only whitespace, is "." or "..", contains '/', the platform
// path separator, or NUL, or would resolve to a path outside root.
//
// Validate does not look at the filesystem. It does not check whether an entry
// with that name already exists.
func Validate(root, name string) error {
	if strings.TrimSpace(name) == "" {
		return invalidName(name, "name is empty")
	}
	if name == "." || name == ".." {
		return invalidName(name, "reserved name")
	}
	for _, r := range name {
		if r == '/' || r == filepath.Separator {
			return invalidName(name, "contains a path separator")
		}
		if r == 0 {
			return invalidName(name, "contains NUL")
		}
	}
	rel, err := filepath.Rel(root, filepath.Join(root, name))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return invalidName(name, "resolves outside root")
	}
	return nil
}

func invalidName(name, reason string) error {
	return fmt.Errorf("fsops: name %q: %s: %w", name, reason, ErrInvalidName)
}
