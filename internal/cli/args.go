package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/app"
)

// Targets is the result of parsing the positional arguments.
type Targets struct {
	// Root is the directory argument, or "" when none was given.
	Root string
	// Files are the file arguments in the order they were given.
	Files []app.FileTarget
	// Remote is the parsed remote target of a user@host:/path or ssh:// argument,
	// or nil. A remote launch opens the SSH terminal for its workspace and does not
	// restore or save the local session.
	Remote *authority.RemoteSpec
}

// StatFunc reports on a path the way os.Stat does. ParseTargets takes it as
// an argument so that tests do not touch the file system.
type StatFunc func(name string) (fs.FileInfo, error)

// ParseTargets sorts the positional arguments into a directory and files.
//
// An argument that names an existing directory is the root. At most one
// directory may be given. An argument that names an existing path of any other
// kind is a file. Any other argument is a file that does not exist yet, which
// opens as an empty buffer and is created on save.
//
// An argument may end in ":LINE" or ":LINE:COL", with both numbers 1-based. The
// suffix is cut off only when the whole argument is not an existing path, so a
// file literally named "a.txt:3" still opens as that file. A position on a
// directory is an error.
func ParseTargets(args []string, stat StatFunc) (Targets, error) {
	var t Targets
	dirs := 0
	for _, arg := range args {
		// A remote target is never stat'ed: its path lives on the remote host. The
		// ssh:// scheme is exclusive, so a malformed URL is a startup error, not a
		// local file name.
		if spec, remote, err := authority.ParseTarget(arg); remote {
			if err != nil {
				return Targets{}, err
			}
			if t.Remote != nil {
				return Targets{}, errors.New("only one remote target may be given")
			}
			t.Remote = &spec
			continue
		}
		info, err := stat(arg)
		if err == nil {
			if !info.IsDir() {
				t.Files = append(t.Files, app.FileTarget{Path: arg})
				continue
			}
			dirs++
			if dirs > 1 {
				return Targets{}, errors.New("only one directory may be given")
			}
			t.Root = arg
			continue
		}
		path, line, col, ok := splitPosition(arg)
		if !ok {
			t.Files = append(t.Files, app.FileTarget{Path: arg})
			continue
		}
		if info, err := stat(path); err == nil && info.IsDir() {
			return Targets{}, fmt.Errorf("%s: a line or column needs a file, not a directory", arg)
		}
		t.Files = append(t.Files, app.FileTarget{Path: path, Line: line, Col: col})
	}
	if t.Remote != nil {
		// One workspace is one backend: a remote launch opens no local directory
		// and no local files in this slice.
		if len(t.Files) > 0 || t.Root != "" {
			return Targets{}, errors.New("a remote target cannot be combined with local directories or files")
		}
	}
	return t, nil
}

// splitPosition cuts a trailing ":LINE" or ":LINE:COL" from arg. ok is false
// when arg has no such suffix. Both numbers must be at least 1, and the path
// before the suffix must not be empty.
func splitPosition(arg string) (path string, line, col int, ok bool) {
	i := strings.LastIndex(arg, ":")
	if i <= 0 {
		return "", 0, 0, false
	}
	last, ok := parsePosition(arg[i+1:])
	if !ok {
		return "", 0, 0, false
	}
	head := arg[:i]
	if j := strings.LastIndex(head, ":"); j > 0 {
		if first, ok := parsePosition(head[j+1:]); ok {
			return head[:j], first, last, true
		}
	}
	return head, last, 0, true
}

// parsePosition reads a positive decimal number that has only digits.
func parsePosition(s string) (int, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
