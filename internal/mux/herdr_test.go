package mux

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestHerdrAgentName(t *testing.T) {
	cases := []struct {
		in    string
		taken map[string]bool
		want  string
	}{
		{"agentctl-feat-x", nil, "agentctl-feat-x"},
		{"Repo/Branch Name", nil, "repo-branch-name"},
		{"123-session", nil, "a123-session"},
		{"", nil, "agentctl"},
		{"a-very-long-session-name-that-exceeds-thirty-two-characters", nil, "a-very-long-session-name-that-ex"},
		{"dup", map[string]bool{"dup": true}, "dup-2"},
		{"dup", map[string]bool{"dup": true, "dup-2": true}, "dup-3"},
		{"a-very-long-session-name-that-ex", map[string]bool{"a-very-long-session-name-that-ex": true}, "a-very-long-session-name-that-2"},
	}
	for _, c := range cases {
		got := herdrAgentName(c.in, c.taken)
		if got != c.want {
			t.Errorf("herdrAgentName(%q) = %q, want %q", c.in, got, c.want)
		}
		if !herdrAgentNameRe.MatchString(got) {
			t.Errorf("herdrAgentName(%q) = %q does not satisfy herdr's name rule", c.in, got)
		}
	}
}

func TestMainPanePrefersSingleAgentPane(t *testing.T) {
	panes := []herdrPane{
		{PaneID: "w1:p3", Agent: ""},
		{PaneID: "w1:p2", Agent: "claude", AgentStatus: "idle"},
		{PaneID: "w1:p1", Agent: ""},
	}
	got, ok := mainPane(panes)
	if !ok || got.PaneID != "w1:p2" {
		t.Fatalf("mainPane = %+v (%v), want w1:p2", got, ok)
	}
}

func TestMainPaneFallsBackToLowestPane(t *testing.T) {
	panes := []herdrPane{
		{PaneID: "w1:p10", Agent: "codex"},
		{PaneID: "w1:p2", Agent: "claude"},
		{PaneID: "w1:p1"},
	}
	got, ok := mainPane(panes)
	if !ok || got.PaneID != "w1:p1" {
		t.Fatalf("mainPane = %+v (%v), want w1:p1", got, ok)
	}
	if _, ok := mainPane(nil); ok {
		t.Fatal("mainPane(nil) should report no pane")
	}
}

func TestHerdrErrorCode(t *testing.T) {
	var env herdrEnvelope
	raw := `{"id":"cli:agent:get","error":{"code":"agent_not_found","message":"agent target x not found"}}`
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil {
		t.Fatal("expected error in envelope")
	}
	wrapped := errors.Join(errors.New("ctx"), env.Error)
	if got := herdrErrorCode(wrapped); got != "agent_not_found" {
		t.Fatalf("herdrErrorCode = %q", got)
	}
	if got := herdrErrorCode(errors.New("plain")); got != "" {
		t.Fatalf("herdrErrorCode(plain) = %q", got)
	}
}

func TestHerdrListMapsWorkspacesToSessions(t *testing.T) {
	workspaces := []herdrWorkspace{{WorkspaceID: "w1", Label: "test-herdr-a"}, {WorkspaceID: "w2", Label: ""}}
	panes := []herdrPane{
		{PaneID: "w1:p1", WorkspaceID: "w1", CWD: "/repo", Agent: "claude", AgentStatus: "working"},
		{PaneID: "w2:p1", WorkspaceID: "w2", CWD: "/tmp"},
	}
	byWorkspace := map[string][]herdrPane{}
	for _, p := range panes {
		byWorkspace[p.WorkspaceID] = append(byWorkspace[p.WorkspaceID], p)
	}
	var sessions []SessionState
	for _, ws := range workspaces {
		if ws.Label == "" {
			continue
		}
		state := SessionState{Name: ws.Label}
		if p, ok := mainPane(byWorkspace[ws.WorkspaceID]); ok {
			state.CWD, state.Agent, state.Target = p.CWD, p.Agent, p.PaneID
			if p.Agent != "" {
				state.State = AgentState(p.AgentStatus)
			}
		}
		sessions = append(sessions, state)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 labelled session, got %d", len(sessions))
	}
	if sessions[0].State != StateWorking || sessions[0].CWD != "/repo" || sessions[0].Target != "w1:p1" {
		t.Fatalf("unexpected session mapping: %+v", sessions[0])
	}
}

func TestResolveBackendByNameRejectsUnknown(t *testing.T) {
	if _, err := resolveBackendByName("screen"); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestBackendNameDefaultsToZellij(t *testing.T) {
	t.Setenv(EnvMux, "")
	if got := BackendName(); got != "zellij" {
		t.Fatalf("BackendName() = %q, want zellij", got)
	}
	t.Setenv(EnvMux, "Herdr")
	if got := BackendName(); got != "herdr" {
		t.Fatalf("BackendName() = %q, want herdr", got)
	}
}
