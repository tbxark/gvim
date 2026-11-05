package fsops

import (
	"fmt"
	"os"

	trash "github.com/hymkor/trash-go"
)

// trashBackend moves the given paths to the system trash. Tests replace it.
//
// The production backend is github.com/hymkor/trash-go (v0.3.0, pure Go; the
// purity gate passes). Tests replace it.
var trashBackend = trash.Throw

// Trash moves path to the system trash. It fails with an error wrapping
// fs.ErrNotExist if path does not exist, without calling the backend. Backend
// errors are wrapped with %w.
func Trash(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return opError("trash", path, err)
	}
	if err := trashBackend(path); err != nil {
		return fmt.Errorf("fsops: trash %s: %w", path, err)
	}
	return nil
}
