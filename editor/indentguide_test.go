package editor

import "testing"

func TestResolveIndentGuide(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		Name     string
		global   string
		override *bool
		allows   bool
		Want     string
	}{
		{"global none", GuidesNone, nil, true, GuidesNone},
		{"empty reads as none", "", nil, true, GuidesNone},
		{"unknown reads as none", "rainbow", nil, true, GuidesNone},
		{"global all", GuidesAll, nil, true, GuidesAll},
		{"global active", GuidesActive, nil, true, GuidesActive},
		{"plain text draws none", GuidesAll, nil, false, GuidesNone},
		{"buffer on turns none into all", GuidesNone, &yes, true, GuidesAll},
		{"buffer on keeps active", GuidesActive, &yes, true, GuidesActive},
		{"buffer on beats the language", GuidesAll, &yes, false, GuidesAll},
		{"buffer off", GuidesAll, &no, true, GuidesNone},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			if got := ResolveIndentGuide(tc.global, tc.override, tc.allows); got != tc.Want {
				t.Errorf("ResolveIndentGuide(%q, %v, %v) = %q, want %q", tc.global, tc.override, tc.allows, got, tc.Want)
			}
		})
	}
}
