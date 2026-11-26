package dock

import "testing"

// noticeStarter is a TerminalStarter that also reports a start notice.
type noticeStarter struct {
	fakeStarter
	notice string
}

func (s *noticeStarter) StartNotice(dir string, cols, rows int) (Terminal, string, error) {
	t, err := s.Start(dir, cols, rows)
	return t, s.notice, err
}

func TestStartNoticeReachesTheHostFromTheStartResult(t *testing.T) {
	var got []string
	starter := &noticeStarter{fakeStarter: fakeStarter{term: &fakeTerm{}}, notice: "directory refused"}
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(s string) { got = append(got, s) },
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: starter})
	cmd := p.Show()
	if cmd == nil {
		t.Fatal("Show did not start a shell")
	}
	msg := cmd()
	if len(got) != 0 {
		t.Fatalf("the starter command reached the host before Update: %v", got)
	}
	if _, ok := p.Update(msg); !ok {
		t.Fatal("the start message was not taken")
	}
	if len(got) != 1 || got[0] != "directory refused" {
		t.Fatalf("notices = %v, want the start notice once", got)
	}
}
