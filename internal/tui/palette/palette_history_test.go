package palette

import (
	"testing"

	"github.com/tbxark/gvim/commands"
)

// TestCommandHistoryOrdersRows: the command mode uses the root's usage history to put
// recently run commands first, then names, while keeping the old fuzzy match ranking.
func TestCommandHistoryOrdersRows(t *testing.T) {
	entries := []commands.Entry{
		{Name: "Alpha", Action: "alpha"},
		{Name: "Beta", Action: "beta"},
		{Name: "Gamma", Action: "gamma"},
	}
	p := New(Options{Commands: entries, History: []string{"gamma"}})
	rows := p.rowsFor(">")
	if len(rows) != 3 || rows[0].action != "gamma" || rows[1].action != "alpha" || rows[2].action != "beta" {
		t.Fatalf("history rows = %+v, want gamma, alpha, beta", rows)
	}
}
