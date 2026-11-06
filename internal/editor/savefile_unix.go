//go:build unix

package editor

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// fchownFn changes the owner of a file. Tests replace it to make the change fail.
var fchownFn = func(f *os.File, uid, gid int) error { return f.Chown(uid, gid) }

// inheritFile gives the new file f, which is about to replace the file old, what a save
// should keep of old besides its content: the owner and group, the set-user-id,
// set-group-id and sticky bits, and the extended attributes (which hold the ACLs on
// some systems), copied from the file at path. It returns the sentence that says what could not be kept, or "" when
// all was. A replacement by rename also drops the other names of a file, which it
// cannot undo, so a file with more than one name is said too.
func inheritFile(f *os.File, path string, old os.FileInfo) string {
	if old == nil {
		return ""
	}
	var lost []string
	st, isUnix := old.Sys().(*syscall.Stat_t)
	if isUnix {
		lost = keepOwner(f, st)
	}
	copyXattrs(path, f)
	// Changing the owner clears the set-id bits, and an ACL can change the mode.
	const kept = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky
	if err := f.Chmod(old.Mode() & kept); err != nil {
		lost = append(lost, "the mode")
	}
	var note string
	if len(lost) > 0 {
		note = "could not keep " + strings.Join(lost, " and ")
	}
	if isUnix && uint64(st.Nlink) > 1 {
		if note != "" {
			note += "; "
		}
		note += fmt.Sprintf("the file had %d links, the other names keep the old text", st.Nlink)
	}
	return note
}

// keepOwner makes f belong to the user and group that st has, as far as the system
// lets the user. It returns what is different afterwards.
func keepOwner(f *os.File, st *syscall.Stat_t) (lost []string) {
	cur, err := f.Stat()
	if err != nil {
		return nil
	}
	now, ok := cur.Sys().(*syscall.Stat_t)
	if !ok || now.Uid == st.Uid && now.Gid == st.Gid {
		return nil
	}
	if err := fchownFn(f, int(st.Uid), int(st.Gid)); err != nil && now.Gid != st.Gid {
		// Only the owner of a file may give it away; the group is often still allowed.
		_ = fchownFn(f, -1, int(st.Gid))
	}
	if cur, err = f.Stat(); err != nil {
		return nil
	}
	now, ok = cur.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if now.Uid != st.Uid {
		lost = append(lost, "the owner")
	}
	if now.Gid != st.Gid {
		lost = append(lost, "the group")
	}
	return lost
}
