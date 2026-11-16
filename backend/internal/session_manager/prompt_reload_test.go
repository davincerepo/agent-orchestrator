package sessionmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// reloadLauncher records the recomputed standing prompt a reload delivers.
type reloadLauncher struct {
	*recordingLauncher

	prompts []string
	err     error
}

func newReloadLauncher() *reloadLauncher {
	return &reloadLauncher{recordingLauncher: &recordingLauncher{}}
}

func (l *reloadLauncher) ReloadChatPrompt(_ context.Context, _ domain.SessionID, systemPrompt string) (ports.ChatPromptReloadResult, error) {
	l.prompts = append(l.prompts, systemPrompt)
	if l.err != nil {
		return ports.ChatPromptReloadResult{}, l.err
	}
	return ports.ChatPromptReloadResult{ProviderConversationID: "thread-2", BranchID: "branch-2"}, nil
}

func reloadTestSession(mode domain.SessionMode) domain.SessionRecord {
	return domain.SessionRecord{
		ID: "s-reload", ProjectID: chatTestProject, Kind: domain.KindWorker,
		Harness: domain.HarnessCodex, Mode: mode,
	}
}

func TestReloadSessionPromptRecomputesRulesAndDelegates(t *testing.T) {
	launcher := newReloadLauncher()
	m, st, _ := newChatManager(launcher)
	project := st.projects[string(chatTestProject)]
	project.Config.AgentRules = "RULE-MARKER-9f2c"
	st.projects[string(chatTestProject)] = project
	st.sessions["s-reload"] = reloadTestSession(domain.SessionModeChat)

	result, err := m.ReloadSessionPrompt(context.Background(), "s-reload")
	if err != nil {
		t.Fatalf("ReloadSessionPrompt: %v", err)
	}
	if result.ProviderConversationID != "thread-2" || result.BranchID != "branch-2" {
		t.Errorf("result = %#v, want the launcher's replacement identity", result)
	}
	if len(launcher.prompts) != 1 {
		t.Fatalf("launcher reloads = %d, want 1", len(launcher.prompts))
	}
	if !strings.Contains(launcher.prompts[0], "RULE-MARKER-9f2c") {
		t.Errorf("recomputed prompt lacks the current project rules:\n%s", launcher.prompts[0])
	}
}

func TestReloadSessionPromptRefusesNonChatModes(t *testing.T) {
	launcher := newReloadLauncher()
	m, st, _ := newChatManager(launcher)
	st.sessions["s-reload"] = reloadTestSession(domain.SessionModeTUI)

	if _, err := m.ReloadSessionPrompt(context.Background(), "s-reload"); !errors.Is(err, ErrPromptReloadUnavailable) {
		t.Fatalf("TUI reload err = %v, want ErrPromptReloadUnavailable", err)
	}
	if len(launcher.prompts) != 0 {
		t.Errorf("TUI reload reached the launcher with %q", launcher.prompts[0])
	}
}

func TestReloadSessionPromptRefusesTerminatedSession(t *testing.T) {
	launcher := newReloadLauncher()
	m, st, _ := newChatManager(launcher)
	record := reloadTestSession(domain.SessionModeChat)
	record.IsTerminated = true
	st.sessions["s-reload"] = record

	if _, err := m.ReloadSessionPrompt(context.Background(), "s-reload"); !errors.Is(err, ErrTerminated) {
		t.Fatalf("terminated reload err = %v, want ErrTerminated", err)
	}
	if len(launcher.prompts) != 0 {
		t.Errorf("terminated reload reached the launcher")
	}
}

func TestReloadSessionPromptRequiresAKnownSession(t *testing.T) {
	launcher := newReloadLauncher()
	m, _, _ := newChatManager(launcher)

	if _, err := m.ReloadSessionPrompt(context.Background(), "s-missing"); err == nil {
		t.Fatal("reload of an unknown session succeeded")
	}
}

func TestReloadSessionPromptRequiresLauncherSupport(t *testing.T) {
	m, st, _ := newChatManager(&recordingLauncher{})
	st.sessions["s-reload"] = reloadTestSession(domain.SessionModeChat)

	if _, err := m.ReloadSessionPrompt(context.Background(), "s-reload"); !errors.Is(err, ErrPromptReloadUnavailable) {
		t.Fatalf("unsupported launcher err = %v, want ErrPromptReloadUnavailable", err)
	}
}

func TestReloadSessionPromptSurfacesLauncherFailures(t *testing.T) {
	launcher := newReloadLauncher()
	launcher.err = errors.New("rollout missing")
	m, st, _ := newChatManager(launcher)
	st.sessions["s-reload"] = reloadTestSession(domain.SessionModeChat)

	if _, err := m.ReloadSessionPrompt(context.Background(), "s-reload"); err == nil ||
		!strings.Contains(err.Error(), "rollout missing") {
		t.Fatalf("err = %v, want the launcher failure", err)
	}
}
