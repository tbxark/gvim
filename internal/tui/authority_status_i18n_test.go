package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestAuthoritySegmentUsesCataloguedStatusKeys checks the extracted remote-authority
// status text: the segment renders the catalogue key with %{userhost} for connected
// and disconnected SSH labels, and a non-SSH label keeps its own words. Not parallel:
// the de fallback case changes and restores the process locale.
func TestAuthoritySegmentUsesCataloguedStatusKeys(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale("en") })
	tests := []struct {
		name      string
		text      string
		connected bool
		want      string
	}{
		{"connected", "SSH:user@host", true, "SSH:user@host"},
		{"disconnected", "SSH:user@host", false, "SSH:user@host · disconnected"},
		{"not ssh", "daemon:user@host", true, "daemon:user@host"},
		{"not ssh disconnected", "daemon:user@host", false, "daemon:user@host · disconnected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{}
			m.SetAuthorityHooks(AuthorityHooks{Status: func() AuthorityStatus {
				return AuthorityStatus{Text: tt.text, Connected: tt.connected}
			}})
			seg, ok := m.authoritySegment()
			if !ok || seg.Text != tt.want {
				t.Fatalf("segment = %+v (ok %v), want %q", seg, ok, tt.want)
			}
		})
	}
	// German has no gvim.status.* translation: the English catalogue value is the
	// explicit fallback, never the bare key.
	i18n.SetLocale("de")
	m := &Model{}
	m.SetAuthorityHooks(AuthorityHooks{Status: func() AuthorityStatus {
		return AuthorityStatus{Text: "SSH:user@host", Connected: false}
	}})
	seg, ok := m.authoritySegment()
	if !ok || seg.Text != "SSH:user@host · disconnected" {
		t.Fatalf("de fallback segment = %+v (ok %v), want the English text", seg, ok)
	}
	// No hooks means no segment.
	empty := &Model{}
	if _, ok := empty.authoritySegment(); ok {
		t.Fatal("a model without hooks has an authority segment")
	}
}
