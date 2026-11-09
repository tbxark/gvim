package bundled

import (
	"encoding/json"
	"os"
	"testing"
)

// TestDashboardWeatherI18nEnglish checks the plugin-owned weather catalogue: the en
// table is byte-identical to the previous literals (so behavior/tests do not change) and
// the dashboard owns these strings rather than the editor Go catalogue. Unknown locales
// fall back to en through the plugin i18n mechanism (plugin/i18n_test.go).
func TestDashboardWeatherI18nEnglish(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugins/dashboard/dashboard.i18n.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"weather.heading":          "Weather",
		"weather.status_label":     "weather",
		"weather.loading":          "loading…",
		"weather.now":              "now",
		"weather.evening":          "5pm",
		"weather.tomorrow":         "tomorrow",
		"weather.unavailable":      "unavailable",
		"weather.invalid_response": "invalid response",
		"weather.network_disabled": "network disabled (set network = true)",
		"external.loading":         "loading…",
		"external.unavailable":     "unavailable",
		"external.invalid":         "invalid response",
		"external.stale":           "stale",
		"external.no_data":         "no data",
		"disk.unavailable":         "unavailable",
	}
	en := catalog["en"]
	if len(en) != len(want) {
		t.Fatalf("en keys = %d, want %d", len(en), len(want))
	}
	for k, v := range want {
		if en[k] != v {
			t.Errorf("en[%s] = %q, want %q", k, en[k], v)
		}
	}
}
