# Binary files and the hex view

A file that is not text opens as a small read-only summary. The bytes stay in the
file, so opening a large blob does not load it into memory and never builds a line
index that no reader could use.

```
blob.bin

Binary file
Size: 3.0 KB

Binary file: read-only.

Run Toggle Hex View to inspect the bytes.
```

The summary is also what a terminal without the image protocol shows for a
picture; pictures themselves are covered in [images.md](images.md).

## Toggle Hex View

The palette's **Toggle Hex View** row switches an open binary file between its
summary and a read-only hex dump. It is available from the editor, the file
explorer and a terminal. On a text or image tab it explains that the row is for
binary files only. The command has no default key binding.

The status row also carries the switch: a binary summary shows a clickable
`[Hex]` chip on its right side, and the dump shows `[Summary]` in the same place.
The chip is part of gvim's status row rather than an `editor.status_bar`
element, so it stays available when a custom status-bar configuration omits
`{read_only}`. Hovering it underlines it like the other clickable status
segments.

The dump uses 16 bytes per row:

```
00000000  89 50 4E 47 0D 0A 1A 0A  00 00 00 0D 49 48 44 52  |.PNG........IHDR|
```

| Field | Meaning |
|---|---|
| `00000000` | byte offset in hexadecimal |
| `89 50 ...` | the bytes; a space after every byte and an extra gap after eight |
| `.PNG...` | printable ASCII (`0x20`–`0x7e`); every other byte is `.` |

The hex view is read-only. Editing, search, replace and copy actions are refused
with a notice; the terminal cursor is hidden. Up/Down, PageUp/PageDown, Home/End,
the mouse wheel, a scrollbar-track click and the usual `j`/`k`/`g`/`G` fallbacks
navigate the dump. **Toggle Hex View** or the status row's `[Summary]` chip
returns to the summary.

## Memory and I/O

The viewer is a bounded window, not a buffer copy:

- The first read fetches at most 64 KiB from the first visible row.
- Scrolling asks only for the window that covers the new visible rows. A read
  that is still in flight is single-flight; a result from an older top row is
  discarded rather than painted.
- Local files use `os.File.ReadAt`; a remote file uses the authority's
  `ReadRange`. A remote access without range reads reports
  `ErrBinaryRangeUnsupported` instead of transferring the whole file or reading
  the local filesystem.
- The file is never written. The summary and dump are views of the same file, and
  closing the tab drops both.

## Opening a binary file

The open path probes at most the first 64 KiB before it reads a whole file. A
binary head becomes the summary immediately; a picture head becomes an image
document. A small file is still checked whole before text decoding, so a NUL or a
control byte after the probe keeps the old binary result. A binary file never
enters large-file mode, whatever its size; its summary is the whole document.

## Files

| File | Role |
|---|---|
| `document/binary.go` | `binaryDocument`, `Binary`, `BinaryStat`, `ReadBinaryRange` |
| `document/document.go` | the bounded head probe and the binary branch |
| `document/large.go`, `document/remote_large.go` | a large binary opens as the summary, not a windowed text source |
| `internal/tui/hexview.go` | per-path state, the read window, navigation and painting |
| `commands/binary_commands.go`, `commands/palette.go` | the **Toggle Hex View** palette row |
| `internal/uiadapter/documentadapter.go` | forwards `Binary`, `IsImage`, `BinaryStat` and `ReadBinaryRange` |
| `internal/tui/ports.go` | the optional `BinaryDoc` and `BinaryRangeDoc` interfaces |
