package cmd

import (
	"testing"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/session"
)

type fakeBackend struct {
	mux.Adapter
	sessions []mux.SessionState
	killed   []string
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Spawn(mux.SpawnOptions) (*mux.SpawnResult, error) {
	return &mux.SpawnResult{}, nil
}
func (f *fakeBackend) Send(string, string) error               { return nil }
func (f *fakeBackend) Read(string) (string, error)             { return "", nil }
func (f *fakeBackend) Kill(name string) error                  { f.killed = append(f.killed, name); return nil }
func (f *fakeBackend) List() ([]mux.SessionState, error)       { return f.sessions, nil }
func (f *fakeBackend) Status(string) (mux.AgentState, error)   { return mux.StateNone, nil }
func (f *fakeBackend) ListSessions() ([]string, error)         { return listNames(f.sessions), nil }
func (f *fakeBackend) ResolveSession(q string) (string, error) { return q, nil }

func listNames(sessions []mux.SessionState) []string {
	names := make([]string, 0, len(sessions))
	for _, s := range sessions {
		names = append(names, s.Name)
	}
	return names
}

func withFakeBackend(t *testing.T, fb *fakeBackend) {
	t.Helper()
	orig := resolveMuxBackend
	resolveMuxBackend = func() (mux.Backend, error) { return fb, nil }
	t.Cleanup(func() { resolveMuxBackend = orig })
}

func TestMuxStatusOverride(t *testing.T) {
	cases := map[mux.AgentState]string{
		mux.StateWorking: session.StatusActive,
		mux.StateBlocked: session.StatusBlocked,
		mux.StateIdle:    session.StatusIdle,
		mux.StateDone:    session.StatusIdle,
		mux.StateUnknown: "",
		mux.StateNone:    "",
	}
	for state, want := range cases {
		if got := muxStatusOverride(state); got != want {
			t.Errorf("muxStatusOverride(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestResolveMuxSessionQuery(t *testing.T) {
	withFakeBackend(t, &fakeBackend{sessions: []mux.SessionState{
		{Name: "agentctl-feat-a"}, {Name: "agentctl-feat-b"}, {Name: "other"},
	}})

	if got, err := resolveMuxSessionQuery("other"); err != nil || got != "other" {
		t.Fatalf("exact match = %q, %v", got, err)
	}
	if got, err := resolveMuxSessionQuery("FEAT-B"); err != nil || got != "agentctl-feat-b" {
		t.Fatalf("substring match = %q, %v", got, err)
	}
	if _, err := resolveMuxSessionQuery("feat"); err == nil {
		t.Fatal("expected ambiguous error")
	}
	if _, err := resolveMuxSessionQuery("missing"); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestMuxStatesByNameAndCWD(t *testing.T) {
	withFakeBackend(t, &fakeBackend{sessions: []mux.SessionState{
		{Name: "Test-Herdr-A", CWD: "/a", State: mux.StateWorking},
		{Name: "zellij-only", CWD: ""},
	}})
	byName, byCWD := muxStatesByName()
	if byName["test-herdr-a"] != mux.StateWorking || byCWD["/a"] != mux.StateWorking {
		t.Fatalf("unexpected maps: %v %v", byName, byCWD)
	}
	if _, ok := byName["zellij-only"]; ok {
		t.Fatal("sessions without state must not be listed")
	}
}

func TestCleanupExitedMuxSessionOnlyKillsExited(t *testing.T) {
	fb := &fakeBackend{sessions: []mux.SessionState{
		{Name: "watchdog", Exited: true}, {Name: "manager", Exited: false},
	}}
	withFakeBackend(t, fb)
	cleanupExitedMuxSession("manager")
	cleanupExitedMuxSession("watchdog")
	if len(fb.killed) != 1 || fb.killed[0] != "watchdog" {
		t.Fatalf("killed = %v, want [watchdog]", fb.killed)
	}
	if isMuxSessionRunning("watchdog") || !isMuxSessionRunning("manager") {
		t.Fatal("isMuxSessionRunning should ignore EXITED sessions")
	}
}
