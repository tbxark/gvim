package explorer

import (
	"bufio"
	"bytes"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// gitignoreFile is the name of the per-directory ignore file.
const gitignoreFile = ".gitignore"

// readIgnore parses the .gitignore in the directory dir, given as path segments
// relative to the tree root (nil for the root itself). The patterns are scoped
// to dir, so they only match entries at or below it. A missing or unreadable
// file yields no patterns, as go-git's own reader treats it.
//
// Only the directory's own file is read. go-git's gitignore.ReadPatterns is not
// used because it walks the whole subtree on every call, which would make each
// lazy expansion re-read every directory below it.
func readIgnore(fsys billy.Filesystem, dir []string) []gitignore.Pattern {
	f, err := fsys.Open(fsys.Join(append(slices.Clone(dir), gitignoreFile)...))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var data []byte
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		data = append(data, sc.Bytes()...)
		data = append(data, '\n')
	}
	return ParseIgnore(data, dir)
}

// ParseIgnore parses .gitignore text for the directory dir, given as path segments
// relative to the tree root (nil for the root itself), into go-git patterns scoped to
// dir. Empty lines and #-comments are skipped. The remote explorer reads a remote
// .gitignore through the authority and calls this, so local and remote filtering share
// one parser (root, nested, negation and anchored patterns included).
func ParseIgnore(data []byte, dir []string) []gitignore.Pattern {
	var ps []gitignore.Pattern
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		ps = append(ps, gitignore.ParsePattern(line, dir))
	}
	return ps
}
