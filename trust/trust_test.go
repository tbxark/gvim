package trust

import "testing"

func TestLevelString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level Level
		want  string
	}{
		{Trusted, "Trusted"},
		{Restricted, "Restricted"},
		{Blocked, "Blocked"},
		{Undecided, ""},
		{Level("bogus"), ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			t.Parallel()
			if got := tt.level.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in     string
		want   Level
		wantOK bool
	}{
		{"trusted", Trusted, true},
		{"Trusted", Trusted, true},
		{"  restricted ", Restricted, true},
		{"BLOCKED", Blocked, true},
		{"", Undecided, false},
		{"trust", Undecided, false},
		{"blocked!", Undecided, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseLevel(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("ParseLevel(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestValid(t *testing.T) {
	t.Parallel()
	for _, level := range []Level{Trusted, Restricted, Blocked} {
		if !level.Valid() {
			t.Errorf("%q.Valid() = false", string(level))
		}
	}
	for _, level := range []Level{Undecided, Level("trusted-ish")} {
		if level.Valid() {
			t.Errorf("%q.Valid() = true", string(level))
		}
	}
}

func TestEffectiveFallsBackToRestricted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level Level
		want  Level
	}{
		{Trusted, Trusted},
		{Restricted, Restricted},
		{Blocked, Blocked},
		{Undecided, Restricted},
		{Level("bogus"), Restricted},
	}
	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			t.Parallel()
			if got := Effective(tt.level); got != tt.want {
				t.Fatalf("Effective(%q) = %q, want %q", string(tt.level), got, tt.want)
			}
		})
	}
}

func TestStatePredicates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		state       State
		wantPrompt  bool
		wantProject bool
		wantTooling bool
	}{
		{
			name:  "undecided plain folder asks nothing and allows nothing",
			state: State{Level: Restricted},
		},
		{
			name:       "undecided folder with markers asks for a decision",
			state:      State{Level: Restricted, Markers: []string{"go.mod"}},
			wantPrompt: true,
		},
		{
			name:        "trusted allows the project config and its tooling",
			state:       State{Level: Trusted, Decided: true},
			wantProject: true,
			wantTooling: true,
		},
		{
			name:  "restricted does neither, even with markers",
			state: State{Level: Restricted, Decided: true, Markers: []string{"go.mod"}},
		},
		{
			name:  "blocked does neither",
			state: State{Level: Blocked, Decided: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.state.NeedsPrompt(); got != tt.wantPrompt {
				t.Errorf("NeedsPrompt() = %v, want %v", got, tt.wantPrompt)
			}
			if got := tt.state.AllowsProjectConfig(); got != tt.wantProject {
				t.Errorf("AllowsProjectConfig() = %v, want %v", got, tt.wantProject)
			}
			if got := tt.state.AllowsTooling(); got != tt.wantTooling {
				t.Errorf("AllowsTooling() = %v, want %v", got, tt.wantTooling)
			}
		})
	}
}
