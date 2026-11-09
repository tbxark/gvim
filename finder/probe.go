package finder

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ProbeFunc finds project files that the user named literally, before the file
// index has enumerated them. This is Fresh's fast prefix probe: the exact path,
// a directory's files, or the files in the parent whose names start with the
// query's basename. The returned paths are slash-separated and relative to the
// finder root. Implementations must honor ctx and must not fall back to another
// machine or root.
type ProbeFunc func(ctx context.Context, query string) ([]string, error)

const (
	// ProbeLimit caps the paths a probe returns, Fresh's truncate(50) bound.
	ProbeLimit = 50
	// ProbeScanLimit caps directory entries inspected for one probe, so a huge
	// directory cannot make one keystroke unbounded. The cap is a resource bound,
	// not a claim that a result is complete for an enormous directory.
	ProbeScanLimit = 4096
)

// ProbePath returns the cleaned slash-relative path query names, or ok false when
// the query cannot be a path under the root: empty, absolute, a Windows volume, a
// NUL, a backslash, or an escape through "..". Spaces and colons are legal
// file-name bytes and are kept.
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

// LocalPrefixProbe returns the local probe bound to root with the explorer's hidden
// files hidden. LocalPrefixProbeOptions takes the Show Hidden Files setting, as
// Fresh's FileProvider does.
func LocalPrefixProbe(root string) ProbeFunc {
	return LocalPrefixProbeOptions(root, false)
}

// LocalPrefixProbeOptions is LocalPrefixProbe with showHidden selecting whether a
// directory listing (and the parent-prefix branch) includes dot entries. A typed
// basename starting with "." includes its dot matches regardless, as shell
// completion does; Fresh's probe has the same exception.
func LocalPrefixProbeOptions(root string, showHidden bool) ProbeFunc {
	return func(ctx context.Context, query string) ([]string, error) {
		rel, ok := ProbePath(query)
		if !ok {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, nil
		}
		// A query naming an existing directory lists its files, Fresh's directory
		// branch. The directory itself must resolve inside the root.
		candidate := filepath.Join(root, filepath.FromSlash(rel))
		if realCandidate, err := filepath.EvalSymlinks(candidate); err == nil {
			if containedLocal(realRoot, realCandidate) {
				if st, err := os.Stat(realCandidate); err == nil && st.IsDir() {
					return listLocalProbeDir(ctx, realRoot, rel, realCandidate, showHidden)
				}
			}
		}
		return listLocalProbePrefix(ctx, realRoot, rel, showHidden)
	}
}

// listLocalProbeDir lists the regular files directly inside dir, bounded by
// ProbeScanLimit and ProbeLimit. dirRel is the slash-relative directory.
func listLocalProbeDir(ctx context.Context, realRoot, dirRel, dirReal string, showHidden bool) ([]string, error) {
	entries, err := readLocalProbeDir(ctx, dirReal)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := make([]string, 0, ProbeLimit)
	for _, entry := range entries {
		if len(out) >= ProbeLimit {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		rel := path.Join(dirRel, entry.Name())
		if _, ok := localProbeFile(ctx, realRoot, dirReal, entry.Name()); ok {
			out = append(out, rel)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// listLocalProbePrefix lists the regular files in rel's parent whose names start
// with rel's basename. Hidden entries are included when showHidden is set or the
// basename itself starts with a dot, as Fresh's include_hidden rule does.
func listLocalProbePrefix(ctx context.Context, realRoot, rel string, showHidden bool) ([]string, error) {
	parentRel := path.Dir(rel)
	base := path.Base(rel)
	parentAbs := filepath.Join(realRoot, filepath.FromSlash(parentRel))
	realParent, err := filepath.EvalSymlinks(parentAbs)
	if err != nil {
		return nil, nil
	}
	if !containedLocal(realRoot, realParent) {
		return nil, nil
	}
	st, err := os.Stat(realParent)
	if err != nil || !st.IsDir() {
		return nil, nil
	}
	entries, err := readLocalProbeDir(ctx, realParent)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	includeHidden := showHidden || strings.HasPrefix(base, ".")
	out := make([]string, 0, ProbeLimit)
	for _, entry := range entries {
		if len(out) >= ProbeLimit {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !includeHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(name, base) {
			continue
		}
		if _, ok := localProbeFile(ctx, realRoot, realParent, name); ok {
			out = append(out, path.Join(parentRel, name))
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// readLocalProbeDir reads at most ProbeScanLimit entries, in batches, checking ctx
// between batches. It never reads a directory unboundedly.
func readLocalProbeDir(ctx context.Context, dir string) ([]os.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []os.DirEntry
	for len(out) < ProbeScanLimit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Request only what the bound still allows: a fixed ReadDir(128) would
		// overshoot ProbeScanLimit by up to a batch on the final read.
		remaining := ProbeScanLimit - len(out)
		n := 128
		if remaining < n {
			n = remaining
		}
		batch, err := f.ReadDir(n)
		out = append(out, batch...)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// localProbeFile reports whether the named entry in realDir is a regular file
// whose resolved real path is still inside realRoot.
func localProbeFile(ctx context.Context, realRoot, realDir, name string) (string, bool) {
	if err := ctx.Err(); err != nil {
		return "", false
	}
	realCandidate, err := filepath.EvalSymlinks(filepath.Join(realDir, name))
	if err != nil || !containedLocal(realRoot, realCandidate) {
		return "", false
	}
	st, err := os.Stat(realCandidate)
	return realCandidate, err == nil && st.Mode().IsRegular()
}

// containedLocal reports whether the resolved candidate is below the root.
func containedLocal(realRoot, realCandidate string) bool {
	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
