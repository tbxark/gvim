package editor

// SetRoot names the workspace root that copy_relative_file_path measures from. The root
// calls it when a project opens, so the relative path follows the folder that is open now.
func (e *Editor) SetRoot(root string) {
	e.opts.Root = root
}

// copyPath copies the file path of d to the clipboard, and hands it back in Result.Text.
// With relative set, the path is given below Options.Root when the file lies under it,
// and as the absolute path otherwise, including when no root is known. An untitled
// buffer has no path, and the copy fails with ErrNoPath.
func copyPath(e *Editor, d *Document, relative bool) error {
	if d.Path == "" {
		return ErrNoPath
	}
	p := d.Path
	if rest, ok := within(p, e.opts.Root); relative && ok {
		p = rest
	}
	e.clipboard = p
	e.resultText = p
	e.setStatus("Copied path: " + p)
	return nil
}
