// Package gitstate reads the state of a git work tree: the work tree root,
// the checked-out branch, and the status of each changed file.
package gitstate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepo is returned when a directory is not inside a git repository.
var ErrNotRepo = errors.New("gitstate: not a git repository")

// FileStatus is the state of one changed path in a work tree. The zero value
// is not a valid status.
type FileStatus uint8

const (
	Untracked FileStatus = iota + 1
	Added
	Modified
	Deleted
	Renamed
	Conflicted
)

const shortHashLen = 7

func (s FileStatus) String() string {
	switch s {
	case Untracked:
		return "untracked"
	case Added:
		return "added"
	case Modified:
		return "modified"
	case Deleted:
		return "deleted"
	case Renamed:
		return "renamed"
	case Conflicted:
		return "conflicted"
	}
	return fmt.Sprintf("FileStatus(%d)", uint8(s))
}

// FindRoot walks upward from dir and returns the first directory that holds a
// .git entry, as an absolute path. A .git file (linked worktrees and
// submodules) counts. ok is false when no ancestor has one.
func FindRoot(dir string) (root string, ok bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Status returns the status of every changed path under root, keyed by its
// slash-separated path relative to root. Clean paths and ignored paths are
// absent. root must be the work tree root, as returned by FindRoot; any other
// directory returns ErrNotRepo.
//
// A path with an unmerged index entry is Conflicted. A staged rename is
// reported as Renamed under its new path. A path deleted in the work tree shows
// as Deleted even when it was staged as Added.
func Status(root string) (map[string]FileStatus, error) {
	if _, err := resolveGitDir(root); err != nil {
		return nil, err
	}
	cmd := exec.Command("git",
		"--no-optional-locks",
		"-c", "core.quotepath=false",
		"-c", "core.fsmonitor=false",
		"-c", "status.renames=true",
		"status", "--porcelain=v1", "-z", "-uall",
	)
	cmd.Dir = root
	cmd.Env = sanitizedGitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("gitstate: status: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("gitstate: status: %w", err)
	}
	return parsePorcelainZ(stdout.Bytes()), nil
}

// parsePorcelainZ parses `git status --porcelain=v1 -z -uall` output.
// Records have the form `XY <path>\0`; when X is 'R' or 'C', an extra
// `<orig-path>\0` field follows and is consumed.
func parsePorcelainZ(data []byte) map[string]FileStatus {
	out := make(map[string]FileStatus)
	for len(data) > 0 {
		nul := bytes.IndexByte(data, 0)
		var rec []byte
		if nul < 0 {
			rec = data
			data = nil
		} else {
			rec = data[:nul]
			data = data[nul+1:]
		}
		if len(rec) < 4 || rec[2] != ' ' {
			continue
		}
		x, y := rec[0], rec[1]
		path := string(rec[3:])
		if x == 'R' || x == 'C' {
			// Consume the trailing NUL-terminated original path.
			if origEnd := bytes.IndexByte(data, 0); origEnd >= 0 {
				data = data[origEnd+1:]
			} else {
				data = nil
			}
		}
		if s, ok := classifyXY(x, y); ok {
			out[path] = s
		}
	}
	return out
}

func classifyXY(x, y byte) (FileStatus, bool) {
	// Unmerged states in porcelain v1.
	if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
		return Conflicted, true
	}
	if x == '?' && y == '?' {
		return Untracked, true
	}
	if x == '!' && y == '!' {
		return 0, false
	}
	if x == ' ' && y == ' ' {
		return 0, false
	}
	if x == 'D' || y == 'D' {
		return Deleted, true
	}
	if x == 'R' {
		return Renamed, true
	}
	if x == 'A' {
		return Added, true
	}
	return Modified, true
}

// Branch returns the short name of the checked-out branch under root. An
// unborn branch reports its name. A detached HEAD reports the first seven hex
// digits of its commit. root must be the work tree root, as with Status.
func Branch(root string) (string, error) {
	gitDir, err := resolveGitDir(root)
	if err != nil {
		return "", err
	}
	branch, err := readHeadBranch(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", err
	}
	return branch, nil
}

// resolveGitDir checks that root holds a .git directory or valid .git pointer
// file and returns the resolved git directory path.
func resolveGitDir(root string) (string, error) {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", ErrNotRepo
	}
	if info.IsDir() {
		return dotGit, nil
	}
	if info.Size() > 4096 {
		return "", ErrNotRepo
	}
	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return "", ErrNotRepo
	}
	line := strings.TrimSpace(string(raw))
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", ErrNotRepo
	}
	target = strings.TrimSpace(target)
	if target == "" || strings.ContainsAny(target, "\r\n") {
		return "", ErrNotRepo
	}
	gitDir := filepath.Clean(target)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Clean(filepath.Join(root, target))
	}
	if st, err := os.Stat(gitDir); err != nil || !st.IsDir() {
		return "", ErrNotRepo
	}
	return gitDir, nil
}

func readHeadBranch(headPath string) (string, error) {
	f, err := os.Open(headPath)
	if err != nil {
		return "", fmt.Errorf("gitstate: read HEAD: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return "", fmt.Errorf("gitstate: read HEAD: %w", err)
	}
	s := strings.TrimSpace(string(data))
	if target, ok := strings.CutPrefix(s, "ref:"); ok {
		target = strings.TrimSpace(target)
		if target == "" || strings.ContainsAny(target, " \t\r\n") {
			return "", fmt.Errorf("gitstate: invalid HEAD ref %q", s)
		}
		return shortRefName(target), nil
	}
	if isHexHash(s) {
		return strings.ToLower(s[:shortHashLen]), nil
	}
	return "", fmt.Errorf("gitstate: unrecognized HEAD %q", s)
}

func shortRefName(ref string) string {
	for _, prefix := range []string{"refs/heads/", "refs/tags/", "refs/remotes/", "refs/notes/"} {
		if short, ok := strings.CutPrefix(ref, prefix); ok && short != "" {
			return short
		}
	}
	return ref
}

func isHexHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isHex := (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isDigit && !isHex {
			return false
		}
	}
	return true
}

func sanitizedGitEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_DIR=") ||
			strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") ||
			strings.HasPrefix(kv, "GIT_OBJECT_DIRECTORY=") ||
			strings.HasPrefix(kv, "GIT_ALTERNATE_OBJECT_DIRECTORIES=") ||
			strings.HasPrefix(kv, "GIT_COMMON_DIR=") ||
			strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GIT_TERMINAL_PROMPT=0")
}
