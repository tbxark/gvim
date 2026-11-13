# Images

gvim shows a picture in the editor pane when you open an image file, using the
[Kitty graphics protocol](https://sw.kovidgoyal.net/kitty/graphics-protocol/)
through Unicode placeholders. A terminal that does not support the protocol shows
a text summary instead, and nothing is ever printed to the screen as a stray
escape sequence.

## What opens as an image

`png`, `jpeg`, `gif` and `webp` files are image documents (see
`document/image.go`). The header alone is read, through `image.DecodeConfig`, so
opening a very large picture does not decode it. A JPEG whose metadata (EXIF, XMP, ICC
profiles) pushes the frame header past the first 64 KiB is still recognised: the probe
walks the marker segments and reads more of the file, up to 4 MiB in all; a JPEG whose frame
header lies beyond that is not shown as a picture. The document is read-only and its
text is a short summary, so the tab strip, the status row and the pane geometry
need no special case:

```
pic.png

PNG 2.3 MB (1920x1080) · RGBA

Image file: read-only.
```

Every other file keeps its old path: text opens as text, and a non-image binary
opens as a read-only file summary. **Toggle Hex View** switches that summary to a
read-only hex dump; see [binary.md](binary.md).

## How the picture is drawn

The viewer does not write the pixel data into the frame. Instead:

1. **The capability query.** The first time an image tab is opened, gvim sends the
   protocol's query (`a=q`, a 1x1 RGB image with id `4294967295`) together with
   `CSI 16 t` for the cell's pixel size. A terminal that supports the protocol
   answers with that id, which arrives as an ultraviolet `KittyGraphicsEvent`. The
   query deliberately omits the quiet key (`q=2`): a quiet query is not answered
   even by a terminal that supports the protocol. A terminal that does not know
   the sequence ignores the APC, so the unquiet query leaks nothing. A terminal
   that stays silent for 400 ms is recorded as "no support".

2. **The upload, once.** When the terminal can draw and the picture is not there
   yet, a command reads the file and sends it with `a=t,f=100,i=<id>,q=2`, chunked
   into the protocol's 4 KiB base64 pieces. A JPEG, GIF or WEBP is re-encoded to PNG
   first, because the protocol's compressed format is PNG. The transmission goes
   through `tea.Raw`, not the frame, so the pixels are not repeated on every
   repaint. The picture is marked drawable only after the `tea.Raw` message has
   been delivered, so no frame draws placeholders before the terminal has the
   image.

3. **Placeholders, every frame.** The pane paints cells of `U+10EEEE` with the row
   and column diacritics from `kitty.Diacritic`, and sets the cell's foreground
   color to the image id. The first cell of the picture also carries the virtual
   placement
   (`a=p,U=1,q=2,i=<image-id>,p=<placement-id>,c=<cols>,r=<rows>`), created again
   each frame so a resize re-fits the picture. The placement id is stable per
   pane, so each frame replaces the previous placement instead of stacking a new
   one; each pane showing the same picture gets its own placement. The foreground
   color carries the image id and the underline color (SGR 58) carries the
   placement id. SGR 4 is deliberately not enabled: that would draw a visible
   underline under every placeholder row. Because a placeholder is ordinary text,
   the host renderer moves the picture with the text: scrolling, splitting,
   switching tabs and resizing all need no post-render hook. The placeholder's
   cell width is 1 (verified in
   `internal/tui/imagecap_test.go`), so the pane's column model holds.

The picture is fitted into the pane keeping its aspect ratio: it is scaled down to
fit, never up, and centered. The cell pixel size comes from the `CSI 16 t` reply,
or from a 16x32 default when the terminal does not answer.

## The setting

`image_protocol` (Interface section, user and project layers, applied live):

| Value | Meaning |
|---|---|
| `auto` (default) | Ask the terminal the first time an image tab opens. |
| `kitty` | Trust the terminal without asking. Use it behind a multiplexer or a wrapper that swallows the reply. |
| `off` | Never draw an image; the summary text is shown. |

Turning the setting `off` releases the uploaded pictures: their terminal resources
are deleted and the local `sent`/`pending` bookkeeping is cleared. Turning it back
on reads and uploads the open image tabs again. A failed read is retried when the
protocol changes from unavailable to available.

## Limits

- Only `png`, `jpeg`, `gif` and `webp` are image documents. Another format the
  `image` package knows opens as a binary file. WEBP decoding comes from
  `golang.org/x/image/webp`, which the module already requires; it decodes lossy and
  lossless stills, not an animation's later frames.
- A GIF or an animated WEBP shows its first frame: animation is not used.
- A file over 32 MiB, a picture over 64 megapixels, an unreadable file and one
  whose header cannot be decoded all keep the text summary. The limits are checked
  before the pixels are decoded, so an oversized picture cannot stall the update
  loop.
- tmux needs `allow-passthrough on` to forward APC sequences. Without it the
  picture does not show; the query goes unanswered, so `auto` falls back to text.
  `image_protocol: "kitty"` does not help there, because the sequence never reaches
  the terminal.
- Remote images and daemon-hosted workspaces keep the text summary: a remote
  picture is not read through the local filesystem, and a shared multi-client
  program has no single terminal to attach a placement to.
- A file changed outside the editor is not refreshed automatically. Image tabs are
  skipped by the file watcher, the disk poll and auto-revert, and **Revert File**
  refuses an image with `ErrImage`: close and reopen the tab to read a new version.
- Closing the last tab that shows an image deletes its terminal resource. Reopening
  it reads and uploads the file again under a fresh id.

## Files

| File | Role |
|---|---|
| `document/image.go` | `ImageInfo`, `imageInfoOf`, `newImageDocument`, `Image`, `ImageStat` |
| `document/binary.go` | the non-image binary summary and its bounded `ReadBinaryRange` |
| `document/document.go` | the image branch of `readDocument`, before the binary one |
| `document/large.go` | a large image stays an image, not a windowed binary |
| `commands/binary_commands.go` | the **Toggle Hex View** palette row |
| `internal/tui/hexview.go` | the read-only hex pane, its window cache and navigation |
| `internal/tui/imagecap.go` | the capability state, the probe, the id allocator |
| `internal/tui/imageview.go` | fitting, placeholder cells, the upload and placement sequences |
| `internal/tui/split.go` | `paintPanes` draws an image pane instead of the text pane |
| `internal/tui/model.go` | `imageQueue` (probe and read), `Update` hooks, `ImageProtocol` in `ViewOptions` |
| `internal/tui/root_watch.go`, `internal/tui/root_revert_fix.go` | image tabs are skipped by the watcher and auto-revert |
| `internal/tui/grid/grid.go` | `grid.Style.UnderlineColor` (the placement id's SGR 58 color) |
| `internal/uiadapter/documentadapter.go` | forwards `Image`, `ImageStat` and `LocalImage` |
| `editor/doc_revert.go`, `editor/files_diskcheck.go` | **Revert File** and the disk poll refuse/skip images |
| `document/doc_revert.go` | `AdoptFile` carries the image facts across a reload |
| `document/large.go`, `document/remote_large.go` | an image over the large-file threshold and a remote image stay image documents |
| `config/config.go`, `config/settings_schema.go` | the `image_protocol` key and its row |
