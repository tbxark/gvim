package restore

import "github.com/tbxark/gvim/internal/terminal"

// activeSaved is the saved tab the dock state marks active, or the zero tab when it
// holds none.
func activeSaved(st terminal.SavedState) terminal.SavedTab {
	if len(st.Tabs) == 0 {
		return terminal.SavedTab{}
	}
	return st.Tabs[min(max(st.ActiveTab, 0), len(st.Tabs)-1)]
}
