package editor

import (
	"fmt"
	"os"
)

// loadSpec returns the load settings of the editor's options.
func (e *Editor) loadSpec() loadSpec {
	spec := loadSpec{Eol: e.defaultEOL(), AutoRO: e.opts.AutoReadOnly, ReadOnly: e.opts.ReadOnly, Access: e.opts.FileAccess}
	spec.LargeConf.FullMax = orDefault(e.opts.fullLoadMax, maxFullLoad)
	if e.opts.LargeFileMode && e.opts.LargeFileThresholdBytes > 0 {
		spec.LargeConf.Threshold = int64(e.opts.LargeFileThresholdBytes)
		spec.LargeConf.Geom = e.opts.largeGeom
		if e.opts.openSrc != nil {
			spec.LargeConf.Open = e.opts.openSrc.Open
		}
		spec.LargeConf.EstLine = e.opts.EstimatedLineLength
		spec.LargeConf.Batch = orDefault(e.opts.indexBatch, indexBatchBytes)
		spec.LargeConf.StashDir = e.opts.SessionDir
	}
	return spec
}

// isLargePath reports whether the file at path would open in large file mode.
// It reads no file content, only the file's size, so the update loop may call
// it.
func (e *Editor) isLargePath(path string) bool {
	if e.opts.FileAccess != nil {
		// A remote document is loaded whole through its access in a command; there is no
		// large-file mode for it, and no local stat may run for it here (Update calls
		// this). Reporting true routes the TUI's open through its asynchronous load.
		return true
	}
	spec := e.loadSpec().LargeConf
	if spec.Threshold == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && spec.IsLarge(info.Size())
}

// IsLargePath reports whether the file at path would open in large file mode. It only
// looks at the file's size, so the caller may ask before it reads the file.
func (e *Editor) IsLargePath(path string) bool {
	return e.isLargePath(path)
}

// LoadFullFunc returns the read of path that loads the file whole, as a small file is
// loaded, even when it is large. It is for the answer "load" to the question that
// ErrLargeNeedsFullLoad asks. A file over 256 MiB is refused with an error that wraps
// ErrTooLargeToTranscode. Like LoadFunc, the function may run on another goroutine.
func (e *Editor) LoadFullFunc(path string) func() (*Document, error) {
	spec := e.loadSpec()
	limit := spec.LargeConf.FullMax
	spec.LargeConf = largeSpec{}
	return func() (*Document, error) {
		if path == "" {
			return nil, fmt.Errorf("open: %w", ErrEmptyPath)
		}
		abs, err := absPath(path)
		if err != nil {
			return nil, err
		}
		if info, err := os.Stat(abs); err == nil && info.Size() > limit {
			return nil, fmt.Errorf("open %s: %w", abs, ErrTooLargeToTranscode)
		}
		return loadDocument(abs, spec)
	}
}
