package finder

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ProbeFunc finds project files that the user named literally, before the file
// index has enumerated them. It is Fresh's fast prefix probe slice: one exact
// path, not a directory listing. The returned paths are slash-separated and
// relative to the finder root. Implementations must honor ctx and must not
// fall back to another machine or root.
type ProbeFunc func(ctx context.Context, query string) ([]string, error)

// ProbePath returns the cleaned slash-relative path query names, or ok false when
// the query cannot be a path under the root: empty, absolute, a Windows volume, a
// NUL, or an escape through "..". Spaces and colons are legal file-name bytes and
// are kept.
func ProbePath(query string) (rel string, ok bool) {
	if query == "" || strings.ContainsRune(query, 0) {
		return "", false
	}
	// Quick-open paths are slash-separated on every platform. A backslash is not a
	// separator here and, on Windows, could spell a drive or UNC path, so reject it
	// rather than let filepath interpret it later.
	if strings.ContainsRune(query, '\\') {
		return "", false
	}
	// Reject an absolute path and a Windows drive/UNC before Clean can hide it.
	if path.IsAbs(query) || filepath.IsAbs(filepath.FromSlash(query)) || filepath.VolumeName(filepath.FromSlash(query)) != "" {
		return "", false
	}
	clean := path.Clean(query)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// LocalPrefixProbe returns the local exact-path probe bound to root. It follows
// symlinks only after proving the final real path is still inside the real root,
// so a nested symlink cannot make the probe return a file outside the workspace.
// A missing path, a directory/special file, an unreadable path, a root that
// cannot be resolved, or an escape all yield no result. A canceled context is
// returned as an error.
func LocalPrefixProbe(root string) ProbeFunc {
	return func(ctx context.Context, query string) ([]string, error) {
		rel, ok := ProbePath(query)
		if !ok {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate := filepath.Join(root, filepath.FromSlash(rel))
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, nil
		}
		realCandidate, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !containedLocal(realRoot, realCandidate) {
			return nil, nil
		}
		st, err := os.Stat(realCandidate)
		if err != nil || !st.Mode().IsRegular() {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}
}

// containedLocal reports whether the resolved candidate is the root or below it.
func containedLocal(realRoot, realCandidate string) bool {
	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}
