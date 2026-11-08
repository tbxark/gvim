package authority

// Exclusive file creation: the file port's optional create-new surface for callers that
// must never overwrite an existing entry (the remote explorer's New File). It is separate
// from WriteFile, whose atomic path replaces the target by design. A backend that cannot
// make the create atomic must not implement it: the caller then refuses explicitly instead
// of falling back to an existence check that races a concurrent creator.

import (
	"context"
	"fmt"

	"github.com/tbxark/gvim/plugin/hostos"
)

// FileCreator is the optional part of a FileSystem that creates a new file without
// replacing anything. CreateNew is atomic (O_EXCL-like): a path that already exists as a
// file, directory or symbolic link fails with an error wrapping fs.ErrExist, and the
// existing entry is untouched. Only hostos.WriteOptions.Mode is honored; Append and
// Parents are refused with ErrUnsupportedFileIO because a create-new does not append and
// does not create ancestors. Missing parent directories fail like a plain open.
type FileCreator interface {
	CreateNew(ctx context.Context, path string, o hostos.WriteOptions) error
}

// Creators returns the exclusive-create surface of fs, or nil when the backend has none.
func Creators(fs FileSystem) FileCreator {
	c, _ := fs.(FileCreator)
	return c
}

// validateCreateOptions rejects the write options a create-new cannot honor.
func validateCreateOptions(o hostos.WriteOptions) error {
	if o.Append {
		return fmt.Errorf("%w: a create-new cannot append", ErrUnsupportedFileIO)
	}
	if o.Parents {
		return fmt.Errorf("%w: a create-new does not create parent directories", ErrUnsupportedFileIO)
	}
	return nil
}

var _ FileCreator = (*sshFiles)(nil)
var _ FileCreator = (*local)(nil)
