package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// topTour returns the tour on the overlay stack, if one is open.
func topTour(m *Model) (*tourState, bool) {
	for _, v := range slices.Backward(m.overlays) {
		if o, ok := v.(boxOverlay); ok && o.v.tour != nil {
			return o.v.tour, true
		}
	}
	return nil, false
}

func TestTourHasSixSlidesThatFitAnEightyColumnFrame(t *testing.T) {
	if len(tourSlides) != 6 {
		t.Fatalf("tour has %d slides, want 6", len(tourSlides))
	}
	for i, s := range tourSlides {
		if s.title == "" || len(s.lines) == 0 {
			t.Errorf("slide %d has no title or no text", i+1)
		}
		for _, l := range s.lines {
			if w := ansi.StringWidth(l); w > 72 {
				t.Errorf("slide %d line is %d cells wide: %q", i+1, w, l)
			}
		}
	}
}

func TestTourNextOpensTheFirstSlide(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	if err := m.Run("tour_next"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	st, ok := topTour(m)
	if !ok || st.slide != 0 {
		t.Fatalf("tour open = %v at slide %d, want open at slide 0", ok, st.slide)
	}
	if !frameHas(m, "1 of 6") || !frameHas(m, tourSlides[0].title) {
		t.Fatal("the first slide is not shown")
	}
}

func TestTourPrevWithoutOpenTourOpensTheFirstSlide(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	runAction(t, m, "tour_prev")
	if st, ok := topTour(m); !ok || st.slide != 0 {
		t.Fatalf("tour open = %v, want open at slide 0", ok)
	}
}

// tourStep is one input to the tour: an action name, or a key press.
type tourStep struct {
	action string
	key    tea.KeyPressMsg
}

func TestTourStepsForwardAndBack(t *testing.T) {
	right := tourStep{key: special(tea.KeyRight)}
	left := tourStep{key: special(tea.KeyLeft)}
	enter := tourStep{key: special(tea.KeyEnter)}
	esc := tourStep{key: special(tea.KeyEscape)}
	next := tourStep{action: "tour_next"}
	prev := tourStep{action: "tour_prev"}
	const closed = -1

	tests := []struct {
		name  string
		steps []tourStep
		want  int // the slide after the steps, or closed
	}{
		{"tour_next opens, then steps", []tourStep{next, next, next}, 2},
		{"tour_prev steps back", []tourStep{next, next, next, prev}, 1},
		{"tour_prev at the first slide stays", []tourStep{next, prev, prev}, 0},
		{"tour_next at the last slide stays", []tourStep{next, right, right, right, right, right, right, right}, 5},
		{"arrows step", []tourStep{next, right, right, left}, 1},
		{"Enter steps forward mid-tour", []tourStep{next, enter, enter}, 2},
		{"Enter on the last slide closes", []tourStep{next, right, right, right, right, right, enter}, closed},
		{"Esc closes", []tourStep{next, right, esc}, closed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
			for _, s := range tt.steps {
				if s.action != "" {
					runAction(t, m, s.action)
				} else {
					press(m, s.key)
				}
			}
			st, open := topTour(m)
			if tt.want == closed {
				if open {
					t.Fatalf("the tour is still open at slide %d", st.slide)
				}
				return
			}
			if !open {
				t.Fatal("the tour is closed")
			}
			if st.slide != tt.want {
				t.Fatalf("slide = %d, want %d", st.slide, tt.want)
			}
		})
	}
}

func TestTourCtrlAltStepsWhileOpen(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	runAction(t, m, "tour_next")
	press(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl | tea.ModAlt})
	if st, _ := topTour(m); st.slide != 1 {
		t.Fatalf("ctrl+alt+n left slide %d, want 1", st.slide)
	}
	press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModAlt})
	if st, _ := topTour(m); st.slide != 0 {
		t.Fatalf("ctrl+alt+p left slide %d, want 0", st.slide)
	}
	if len(ed.actions) != 0 {
		t.Fatalf("the tour let keys reach the editor: %v", ed.actions)
	}
}

func TestTourSlideNumberIsShownInTitle(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	runAction(t, m, "tour_next")
	press(m, special(tea.KeyRight))
	if !frameHas(m, "2 of 6") || !frameHas(m, strings.TrimSpace(tourSlides[1].title)) {
		t.Fatal("slide 2 is not titled as slide 2 of 6")
	}
}
