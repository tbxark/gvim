package tui

// URL opening for plugins (gvim.url.open): strict scheme validation, a mandatory
// keyboard-reachable confirmation dialog, and an argv-only opener port. The opener itself
// is a tea.Cmd, so a slow browser launch never blocks Update.

import (
	"net/url"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/pluginapi"
)

// maxShownURL bounds the URL shown in the confirmation text; the full URL is what the
// opener receives after validation.
const maxShownURL = 120

// urlOpenTag is the confirm dialog tag for one pending URL open.
type urlOpenTag struct{ url string }

// urlOpenedMsg is the outcome of the platform opener, delivered back to the update loop.
type urlOpenedMsg struct {
	url string
	err error
}

// validateOpenURL is the scheme/format policy for gvim.url.open. Only absolute http(s)
// URLs with a host are accepted; every other scheme (javascript, file, data, ssh, mailto,
// ...), credentials, whitespace/control characters, opaque forms and over-long URLs are
// refused before any prompt.
func validateOpenURL(raw string) error {
	invalid := func(why string) error {
		return pluginapi.Errf(pluginapi.CodeInvalid, "url: %s", why)
	}
	if raw == "" {
		return invalid("empty")
	}
	if len(raw) > pluginapi.MaxOpenURLLen {
		return pluginapi.Errf(pluginapi.CodeLimit, "url longer than %d bytes", pluginapi.MaxOpenURLLen)
	}
	if strings.TrimSpace(raw) != raw {
		return invalid("leading or trailing whitespace")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			return invalid("whitespace or control character")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" {
		return invalid("not an absolute URL with a host")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return invalid("scheme must be http or https, not " + u.Scheme)
	}
	if u.User != nil {
		return invalid("credentials are not allowed")
	}
	if u.Hostname() == "" {
		return invalid("missing host")
	}
	return nil
}

// OpenURL is the plugin Host part behind gvim.url.open: validate, then queue the
// confirmation dialog. It returns before anything is opened; the user must answer Yes.
func (h *pluginHost) OpenURL(raw string) error {
	m := h.model()
	if m == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "opening URLs is not available")
	}
	if err := validateOpenURL(raw); err != nil {
		return err
	}
	if m.root.ports.OpenURL == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "opening URLs is not available in this environment")
	}
	m.confirmOpenURL(raw)
	return nil
}

// confirmOpenURL pushes the Yes/No dialog that gates one URL open. Yes starts the opener,
// No/Esc closes it; the dialog is a normal prompt overlay, reachable from the keyboard.
func (m *Model) confirmOpenURL(raw string) {
	q := uiTextVars("gvim.url.open_confirm", "Open %{url} in the system browser?", map[string]any{"url": displayURL(raw)})
	m.pushOverlay(prompt.NewConfirm(m.styles, q, urlOpenTag{url: raw}))
}

// openURLCmd starts the platform opener off the update loop. The validated URL is passed
// as one argv element by the port; no shell or string concatenation is involved. A start
// failure is delivered as urlOpenedMsg and shown as a notice.
func (m *Model) openURLCmd(raw string) tea.Cmd {
	opener := m.root.ports.OpenURL
	if opener == nil {
		m.notice = uiText("gvim.url.unavailable", "Opening URLs is not available in this environment")
		return nil
	}
	return func() tea.Msg {
		return urlOpenedMsg{url: raw, err: opener(raw)}
	}
}

// displayURL shortens a URL for the confirmation text without changing what the opener
// receives.
func displayURL(raw string) string {
	r := []rune(raw)
	if len(r) <= maxShownURL {
		return raw
	}
	return string(r[:maxShownURL-1]) + "\u2026"
}

// openURLAnswer handles the confirmation answer.
func (m *Model) openURLAnswer(tag urlOpenTag, d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Yes {
		m.quietNotice(uiText("gvim.url.cancelled", "URL opening cancelled"))
		return nil
	}
	return m.openURLCmd(tag.url)
}

// openURLDone reports the opener outcome: success says what was opened, a failure says
// why, so a missing/broken platform opener is never silent.
func (m *Model) openURLDone(msg urlOpenedMsg) {
	if msg.err != nil {
		m.notice = uiTextVars("gvim.url.error", "Open URL: %{error}", map[string]any{"error": msg.err.Error()})
		return
	}
	m.quietNotice(uiText("gvim.url.opened", "Opened URL"))
}

var _ pluginapi.URLOpenHost = (*pluginHost)(nil)
