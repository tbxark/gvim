package frame

import (
	"strings"
	"testing"
)

// TestStatusUpdateElement paints Fresh's {update} indicator only when the release
// check found a newer version.
func TestStatusUpdateElement(t *testing.T) {
	row := StatusRow{Left: []StatusElement{{Kind: StatusElementUpdate}}}
	paint := func(update string) string {
		var b strings.Builder
		for _, r := range StatusRuns(100, StatusView{Row: row, Update: update}) {
			b.WriteString(r.Text)
		}
		return b.String()
	}
	if got := paint(""); got != "" {
		t.Fatalf("an empty update painted %q", got)
	}
	if got := paint("Update: v0.4.2"); got != " Update: v0.4.2 " {
		t.Fatalf("the update element painted %q, want %q", got, " Update: v0.4.2 ")
	}
}
