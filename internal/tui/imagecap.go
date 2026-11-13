package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// The values of image_protocol. Auto asks the host terminal when the first image
// tab opens, kitty assumes it supports the graphics protocol, and off never draws
// an image.
const (
	imageProtocolAuto  = "auto"
	imageProtocolKitty = "kitty"
	imageProtocolOff   = "off"
)

// imageProbeID is the image id of the capability query. It is above the ids the
// viewer hands out (which start at 1), so a query's reply is never mistaken for a
// placed image's, and a reply whose id failed to parse (id 0) is not taken as the
// probe's.
const imageProbeID = 4294967295

// imageProbeTimeout bounds the wait for the terminal's reply to the capability
// query. A terminal that supports the protocol answers at once; one that does
// not stays silent, and the viewer falls back to the summary text.
const imageProbeTimeout = 400 * time.Millisecond

// imageState is the root's view of the host terminal's graphics support. The zero
// value is "not asked yet", which draws no image.
type imageState struct {
	// kitty is true once the terminal answered the query, so the viewer may send
	// image data.
	kitty bool
	// probed is true when the question has an answer, either way. Before it, the
	// viewer draws the summary text, so no frame shows a stray APC.
	probed bool
	// off is set by image_protocol "off": no query is sent and no image is drawn.
	off bool
	// forced is set by image_protocol "kitty": the terminal is trusted without a
	// query, for a terminal whose reply does not reach the program.
	forced bool
	// sent maps an image path to the id it was transmitted under, so the pixels go
	// to the terminal once and later frames only place the placeholders. The
	// picture is not refreshed when the file changes; closing and reopening the tab
	// reads the file again under a fresh id.
	sent map[string]sentImage
	// nextID is the id the next transmission takes. It starts at 1: 0 is the probe's
	// id, and a placeholder's foreground color cannot be 0.
	nextID int
	// cellW and cellH are the pixel size of one cell, from the terminal's reply to
	// CSI 16 t, or the default when it did not answer.
	cellW, cellH int
	// pending marks the ids whose upload is in flight, so a picture is read once.
	pending map[int]bool
	// arming is set while the capability query is in flight, so the probe is sent
	// once and not on every update until its timeout.
	arming bool
	// localOnly is set for a hosted (daemon) workspace: images are shown as the
	// text summary because the daemon has no single terminal to draw them in.
	localOnly bool
}

// sentImage records one transmission: the id, the size and modification time it
// was sent for, and whether the terminal has acknowledged the upload.
type sentImage struct {
	id    int
	size  int64
	mtime int64
	// state is where this transmission is: queued, uploaded, or failed. Only an
	// uploaded picture is drawn; a queued or failed one keeps the summary text.
	state imageSendState
}

// imageSendState is the progress of one picture's transmission.
type imageSendState int

const (
	// The zero value is an entry whose read or upload is still in flight.
	_ imageSendState = iota
	// imageUploaded state means the terminal has the picture.
	imageUploaded
	// imageFailed means this file cannot be shown with its current stat. The
	// summary stays and the same file is not read again.
	imageFailed
)

// imageEnabled reports whether the viewer may send image data now.
func (s imageState) imageEnabled() bool {
	return !s.off && !s.localOnly && (s.kitty || s.forced)
}

// cellSize returns the pixel size of one cell, falling back to the default.
func (s imageState) cellSize() (w, h int) {
	if s.cellW > 0 && s.cellH > 0 {
		return s.cellW, s.cellH
	}
	return imageCellSize.w, imageCellSize.h
}

// imageID returns the id for path, allocating one the first time. send reports
// whether the picture must be transmitted: true for a new path, and for the same
// path with a different stat after its tab was closed and reopened. A paint of an
// unchanged open tab does not reach that branch, and a picture is drawn only after
// its upload was recorded.
func (s *imageState) imageID(path string, size, mtime int64) (id int, send bool) {
	if s.sent == nil {
		s.sent = make(map[string]sentImage)
	}
	if got, ok := s.sent[path]; ok && got.size == size && got.mtime == mtime {
		return got.id, false
	}
	if old, ok := s.sent[path]; ok {
		// A new stat for the same path (a close and reopen after the file changed)
		// gets a new id. An upload still in flight for the old id must not stay
		// marked as pending.
		delete(s.pending, old.id)
	}
	if s.nextID < 1 {
		s.nextID = 1
	}
	id = s.nextID
	if id > maxImageID {
		// The id space is exhausted; wrap so a long session keeps working. The old
		// placements are dropped by the terminal's own quota long before this.
		id = 1
	}
	s.nextID = id + 1
	if s.nextID > maxImageID {
		s.nextID = 1
	}
	s.sent[path] = sentImage{id: id, size: size, mtime: mtime}
	return id, true
}

// imageUploaded reports the id and whether the picture at path has reached the
// terminal, so the pane may draw its placeholders. It reads the state and never
// allocates an id, so a paint on a background tab (which is not the transmission
// owner) cannot shift the ids.
func (s imageState) imageUploaded(path string, size, mtime int64) (id int, uploaded bool) {
	got, ok := s.sent[path]
	if !ok || got.size != size || got.mtime != mtime {
		return 0, false
	}
	return got.id, got.state == imageUploaded
}

// markUploaded records that path's picture reached the terminal. It records only
// while the entry is still the one the upload was for, so a tab closed or reopened
// while the read was in flight is not marked by the stale result.
func (s *imageState) markUploaded(path string, id int) {
	got, ok := s.sent[path]
	if !ok || got.id != id {
		return
	}
	got.state = imageUploaded
	s.sent[path] = got
}

// markFailed records that path's picture cannot be shown for the transmission id.
// It is still an entry with the file's stat, so imageID does not send the same
// file again on every frame. A close and a reopen make a new entry and retry the
// read.
func (s *imageState) markFailed(path string, id int) {
	got, ok := s.sent[path]
	if !ok || got.id != id {
		return
	}
	got.state = imageFailed
	s.sent[path] = got
}

// forget drops path's transmission, so closing an image tab frees the entry. The
// returned id names the image whose placements the caller should delete.
func (s *imageState) forget(path string) (id int, ok bool) {
	got, found := s.sent[path]
	if !found {
		return 0, false
	}
	delete(s.sent, path)
	delete(s.pending, got.id)
	return got.id, true
}

// uploadCurrent reports whether path still names the transmission id. A result
// that arrives after the tab closed or reopened is dropped.
func (s imageState) uploadCurrent(path string, id int) bool {
	got, ok := s.sent[path]
	return ok && got.id == id
}

// maxImageID bounds the ids the viewer hands out, so the placeholder's foreground
// color always fits a true color.
const maxImageID = 1<<24 - 1

// imageProtocolOf resolves image_protocol to the state it starts in.
func imageProtocolOf(value string) imageState {
	switch value {
	case imageProtocolOff:
		return imageState{off: true, probed: true}
	case imageProtocolKitty:
		return imageState{kitty: true, probed: true, forced: true}
	}
	return imageState{}
}

// applyImageProtocol applies a changed image_protocol value live. Turning the
// viewer off releases the terminal resources it uploaded; turning it back on
// starts over and reads and uploads the open image tabs again.
func (m *Model) applyImageProtocol(value string) tea.Cmd {
	next := imageProtocolOf(value)
	if next.off {
		// An off terminal should not keep pictures it cannot show: release the
		// uploads, and turning the mode back on uploads them again.
		cmd := m.releaseImages()
		next.cellW, next.cellH, next.localOnly = m.img.cellW, m.img.cellH, m.img.localOnly
		m.img = next
		return cmd
	}
	if m.img.off {
		next.cellW, next.cellH, next.localOnly = m.img.cellW, m.img.cellH, m.img.localOnly
		m.img = next
		return m.imageQueue()
	}
	// Auto and kitty share the terminal's resources: the pictures already sent
	// stay under their ids. Switching to auto still asks the terminal when it has
	// not answered before, so an unsupported terminal falls back to the summary.
	wasEnabled := m.img.imageEnabled()
	next.sent, next.nextID, next.pending = m.img.sent, m.img.nextID, m.img.pending
	next.cellW, next.cellH, next.localOnly = m.img.cellW, m.img.cellH, m.img.localOnly
	if !wasEnabled && next.imageEnabled() {
		// A read that failed while images could not be drawn is not permanent:
		// turning the protocol back on retries it. Uploaded pictures keep their
		// ids and terminal resources.
		for path, entry := range next.sent {
			if entry.state == imageFailed {
				delete(next.sent, path)
			}
		}
	}
	m.img = next
	if !next.probed || !wasEnabled {
		return m.imageQueue()
	}
	return nil
}

// imageDeleteSequence removes every placement of id and the uploaded image
// resource itself. Closing an image tab uses it, so the terminal's image quota
// does not fill with pictures that are no longer shown.
func imageDeleteSequence(id int) string {
	return ansi.KittyGraphics(nil, fmt.Sprintf("a=d,d=I,i=%d,q=2", id))
}

// releaseImages forgets every transmission and returns one raw command that
// deletes its terminal resource.
func (m *Model) releaseImages() tea.Cmd {
	ids := make([]int, 0, len(m.img.sent))
	for path := range m.img.sent {
		if id, ok := m.img.forget(path); ok {
			ids = append(ids, id)
		}
	}
	m.img.nextID = 0
	return imageDeleteCmd(ids)
}

// imageReleaseClosed removes the image entries whose files are no longer open as
// images and returns one raw command that deletes their terminal resources.
func (m *Model) imageReleaseClosed() tea.Cmd {
	if len(m.img.sent) == 0 {
		return nil
	}
	open := make(map[string]bool)
	for _, f := range m.openTabFiles() {
		if f.image {
			open[filepath.Clean(f.path)] = true
		}
	}
	var ids []int
	for path := range m.img.sent {
		if open[filepath.Clean(path)] {
			continue
		}
		if id, ok := m.img.forget(path); ok {
			ids = append(ids, id)
		}
	}
	return imageDeleteCmd(ids)
}

// imageDeleteCmd builds the raw command that deletes the terminal resources of
// ids. No ids means no command.
func imageDeleteCmd(ids []int) tea.Cmd {
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(imageDeleteSequence(id))
	}
	return tea.Raw(b.String())
}

// probeSequence is the APC that asks the terminal whether it supports the
// graphics protocol: a 1x1 RGB query with an id the reply carries back. The quiet
// key is deliberately absent: a quiet query is not answered even by a terminal that
// supports the protocol, which would make every probe time out. A terminal that
// does not know the sequence prints nothing (APC is ignored), so the unquiet query
// is safe. The payload is the 3 bytes of one black pixel, base64 "AAAA", which the
// protocol's own example uses; whether the terminal says OK or reports an error,
// an answer means it understands the protocol.
func probeSequence() string {
	return ansi.KittyGraphics([]byte("AAAA"), "a=q", "t=d", "f=24", "s=1", "v=1", fmt.Sprintf("i=%d", imageProbeID))
}

// imageProbeArmMsg asks Update to send the query and start the timeout.
type imageProbeArmMsg struct{}

// imageProbeTimeoutMsg ends the wait for the query's reply.
type imageProbeTimeoutMsg struct{}

// probeTimeoutCmd waits for the reply, then reports the timeout.
func probeTimeoutCmd() tea.Cmd {
	return tea.Tick(imageProbeTimeout, func(time.Time) tea.Msg {
		return imageProbeTimeoutMsg{}
	})
}

// kittyGraphicsReply returns the options of a Kitty graphics reply event, and
// whether msg is one. A reply to the capability query has the probe id.
func kittyGraphicsReply(msg tea.Msg) (id int, ok bool) {
	ev, isEvent := msg.(uv.KittyGraphicsEvent)
	if !isEvent {
		return 0, false
	}
	return ev.Options.ID, true
}

// imageMessage handles the messages of the image viewer. It reports whether it
// took msg, so Update stops before the rest of the root sees it.
func (m *Model) imageMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case imageProbeArmMsg:
		// Send the query and start the wait for its reply. The reply itself arrives as
		// a uv.KittyGraphicsEvent, handled below. CSI 16 t asks for the cell's pixel
		// size, which the fit uses to keep the aspect ratio.
		return tea.Batch(tea.Raw(probeSequence()+ansi.WindowOp(16)), probeTimeoutCmd()), true
	case imageProbeTimeoutMsg:
		if m.img.probed {
			return nil, true // the reply beat the timeout
		}
		m.img.probed = true
		return nil, true
	case imageSendMsg:
		// The picture was read: hand the upload to the terminal. It goes through Raw,
		// not the frame, so the pixel data is not repeated on every repaint. The entry
		// is marked uploaded only after the Raw message reaches the program, so no
		// frame can draw placeholders before the terminal has the image. An empty
		// sequence means the picture could not be decoded or is over the render limit,
		// and the summary text stays. A result whose tab closed, or whose file was
		// replaced while the read was in flight, is dropped.
		delete(m.img.pending, msg.id)
		if !m.img.uploadCurrent(msg.path, msg.id) {
			return nil, true
		}
		if msg.err != nil {
			m.img.markFailed(msg.path, msg.id)
			return nil, true
		}
		seq := imageUploadSequence(msg.data, msg.id)
		if seq == "" {
			m.img.markFailed(msg.path, msg.id)
			return nil, true
		}
		return tea.Sequence(
			tea.Raw(seq),
			func() tea.Msg { return imageUploadedMsg{path: msg.path, id: msg.id} },
		), true
	case imageUploadedMsg:
		m.img.markUploaded(msg.path, msg.id)
		return nil, true
	}
	if id, ok := kittyGraphicsReply(msg); ok {
		if id == imageProbeID {
			m.img.kitty, m.img.probed = true, true
			return m.imageQueue(), true
		}
		// A reply to an upload or a placement: nothing to do, the terminal's own
		// acknowledgement is silent for the queries this viewer sends (q=2).
		return nil, true
	}
	if ev, ok := msg.(uv.CellSizeEvent); ok {
		if ev.Width > 0 && ev.Height > 0 {
			m.img.cellW, m.img.cellH = ev.Width, ev.Height
		}
		return nil, true
	}
	return nil, false
}
