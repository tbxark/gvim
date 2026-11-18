package app

import (
	"path"
	"strings"
)

// remoteInside reports whether p is equal to or under the POSIX remote root, handling
// root == "/" without synthesizing a "//" prefix.
func remoteInside(root, p string) bool {
	if root == "" || p == "" {
		return false
	}
	cleanRoot, cleanP := path.Clean(root), path.Clean(p)
	if cleanRoot == cleanP {
		return true
	}
	if cleanRoot == "/" {
		return strings.HasPrefix(cleanP, "/")
	}
	return len(cleanP) > len(cleanRoot) && cleanP[len(cleanRoot)] == '/' && strings.HasPrefix(cleanP, cleanRoot)
}
