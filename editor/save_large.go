package editor

import (
	"github.com/tbxark/gvim/buffer"
	"os"
)

// applyLargeSave records the result of the write of a large document on the editor's
// goroutine. A document that was edited while the file was written stays modified: the
// file holds the text of the snapshot, and the next save writes the newer text. A
// document that has not changed moves onto the file that was written. If the buffer
// cannot, the file is saved anyway, the buffer keeps reading the file it has, and the
// status says the document should be reloaded.
func (e *Editor) applyLargeSave(r SaveResult) {
	d, lg := r.Job.Doc, r.Job.LargeSave()
	if d.LargeState() == nil || d.Buffer != lg.Buf {
		// The document was reverted or closed while the file was written.
		return
	}
	l := d.LargeState()
	if r.Skipped {
		l.SaveNote = "Nothing to save"
		return
	}
	d.Disk = r.Disk
	d.SetEncodingLabel(r.Job.Label)
	l.SaveNote = r.Note
	if d.Buffer.Version() != lg.Ver {
		// The buffer goes on reading the old file, which is not the file at the path now.
		l.Written = writtenOf(r)
		return
	}
	hadHistory := d.Buffer.Stats().HistoryGroups > 0
	d.MetaDirty = false
	if !e.rebaseOnSaved(d, r) {
		d.Buffer.MarkSaved()
		if r.Tmp != nil {
			// The file at the path is not the one that was written, or could not be
			// told to be: record the file that was written, so that the next save sees
			// the difference.
			d.Disk = diskState{Exists: true, Size: r.Tmp.Size(), Mtime: r.Tmp.ModTime()}
		}
		l.RebaseFailed = true
		l.Written = writtenOf(r)
		l.SaveNote = "Saved; reload to continue editing"
		return
	}
	l.RebaseFailed, l.Written = false, nil
	d.ResetHistoryState()
	if hadHistory && l.SaveNote == "" {
		l.SaveNote = "Saved (undo history cleared)"
	}
}

// rebaseOnSaved makes the buffer of d read the file that was just written, after it
// checks that the file at the path is the temporary file that was renamed there: the
// same file, of the size that was written. It reports whether the buffer moved. On a
// false nothing has changed.
func (e *Editor) rebaseOnSaved(d *Document, r SaveResult) bool {
	lg := r.Job.LargeSave()
	if r.Source != nil {
		// A remote windowed large save opened the new source in its command. Adopt it
		// here; no remote read happens on the update goroutine.
		if r.Source.Size() != r.Res.Bytes {
			_ = r.Source.Close()
			return false
		}
		if err := d.Buffer.Rebase(r.Source, r.Res, lg.Ver, int64(len(lg.Prefix))); err != nil {
			_ = r.Source.Close()
			return false
		}
		d.LargeState().Size = r.Source.Size()
		d.LargeState().Mtime = mtimeNano(r.Disk)
		d.LargeState().Skip = int64(len(lg.Prefix))
		return true
	}
	open := openSavedSource
	if e.opts.openSrc != nil {
		open = func(path string) (buffer.Source, os.FileInfo, error) {
			src, err := e.opts.openSrc.Open(path)
			return src, nil, err
		}
	}
	src, info, err := open(r.Job.Path)
	if err != nil {
		return false
	}
	if info != nil && (r.Tmp == nil || !os.SameFile(info, r.Tmp) || info.Size() != r.Res.Bytes) {
		_ = src.Close()
		return false
	}
	if err := d.Buffer.Rebase(src, r.Res, lg.Ver, int64(len(lg.Prefix))); err != nil {
		_ = src.Close()
		return false
	}
	d.LargeState().Size = src.Size()
	d.LargeState().Mtime = mtimeNano(r.Disk)
	d.LargeState().Skip = int64(len(lg.Prefix))
	return true
}
