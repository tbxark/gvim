package plugin

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

func TestValidateDashboardResultBounds(t *testing.T) {
	if err := validateDashboardResult(map[string]any{
		"rows": []any{map[string]any{"kind": "kv", "value": "ok"}},
	}); err != nil {
		t.Fatalf("small result refused: %v", err)
	}
	rows := make([]any, 5000)
	for i := range rows {
		rows[i] = map[string]any{"kind": "kv", "label": "k", "value": "v"}
	}
	if err := validateDashboardResult(map[string]any{"rows": rows}); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("too many nodes = %v, want limit", err)
	}
	if err := validateDashboardResult(map[string]any{"text": strings.Repeat("x", maxDashboardResultString+1)}); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("oversize string = %v, want limit", err)
	}
	deep := any("leaf")
	for i := 0; i < maxDashboardResultDepth+2; i++ {
		deep = []any{deep}
	}
	if err := validateDashboardResult(deep); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("deep result = %v, want limit", err)
	}
}

func TestDashboardConsumerIdentity(t *testing.T) {
	if !isDashboardConsumer(&plugin{name: "dashboard", kindStr: SourceBundled}) {
		t.Fatal("bundled dashboard is not the authorized consumer")
	}
	if isDashboardConsumer(&plugin{name: "dashboard", kindStr: SourceUser}) {
		t.Fatal("a user plugin named dashboard was authorized")
	}
	if isDashboardConsumer(&plugin{name: "other", kindStr: SourceBundled}) {
		t.Fatal("another bundled plugin was authorized")
	}
}
