package restore

import (
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// SidebarState converts the sidebar state a session file holds to the UI state a
// model starts from. It returns nil when the session has none (a session saved by
// an older release, or by a session without an explorer): the model then keeps the
// config defaults.
func SidebarState(s session.Session) *tui.SidebarState {
	if s.Sidebar == nil {
		return nil
	}
	st := &tui.SidebarState{
		ExplorerCollapsed:    s.Sidebar.ExplorerCollapsed,
		Side:                 explorer.SideLeft,
		ExplorerSelected:     s.Sidebar.ExplorerSelected,
		ExplorerScroll:       s.Sidebar.ExplorerScroll,
		ContentsOpen:         s.Sidebar.ContentsOpen,
		ContentsCollapsed:    s.Sidebar.ContentsCollapsed,
		ContentsRows:         s.Sidebar.ContentsRows,
		ContentsScroll:       s.Sidebar.ContentsScroll,
		DiagnosticsOpen:      s.Sidebar.DiagnosticsOpen,
		DiagnosticsCollapsed: s.Sidebar.DiagnosticsCollapsed,
		DiagnosticsRows:      s.Sidebar.DiagnosticsRows,
	}
	if side, ok := explorer.ParseSide(s.Sidebar.Side); ok {
		st.Side = side
	}
	switch explorer.WidthMode(s.Sidebar.WidthMode) {
	case explorer.WidthAutomatic, explorer.WidthPercent, explorer.WidthColumns:
		st.Width = explorer.Width{Mode: explorer.WidthMode(s.Sidebar.WidthMode), Value: s.Sidebar.WidthValue}
	default:
		st.Width = explorer.Width{}
	}
	return st
}

// SaveSidebarState converts a model's sidebar state to the session form. A model
// without an explorer reports the zero state, which is still a valid, bounded
// snapshot (the session loader restores it as the defaults).
func SaveSidebarState(st tui.SidebarState) *session.SidebarState {
	return &session.SidebarState{
		ExplorerCollapsed:    st.ExplorerCollapsed,
		WidthMode:            int(st.Width.Mode),
		WidthValue:           st.Width.Value,
		Side:                 st.Side.String(),
		ExplorerSelected:     st.ExplorerSelected,
		ExplorerScroll:       st.ExplorerScroll,
		ContentsOpen:         st.ContentsOpen,
		ContentsCollapsed:    st.ContentsCollapsed,
		ContentsRows:         st.ContentsRows,
		ContentsScroll:       st.ContentsScroll,
		DiagnosticsOpen:      st.DiagnosticsOpen,
		DiagnosticsCollapsed: st.DiagnosticsCollapsed,
		DiagnosticsRows:      st.DiagnosticsRows,
	}
}
