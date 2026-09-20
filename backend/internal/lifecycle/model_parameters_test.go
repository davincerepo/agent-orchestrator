package lifecycle

import (
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"testing"
)

func TestFleetSpawnAndRestoreRetainModelParameters(t *testing.T) {
	m, st, _ := newManager()
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Mode: domain.SessionModeTUI}
	if err := m.MarkSpawned(ctx, "mer-1", domain.SessionMetadata{Model: "sol", ReasoningEffort: "xhigh", ServiceTier: "priority", RuntimeHandleID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkSpawned(ctx, "mer-1", domain.SessionMetadata{RuntimeHandleID: "restored"}); err != nil {
		t.Fatal(err)
	}
	got := st.sessions["mer-1"].Metadata
	if got.Model != "sol" || got.ReasoningEffort != "xhigh" || got.ServiceTier != "priority" {
		t.Fatalf("lost launch parameters: %+v", got)
	}
	if _, err := m.CommitControllerEpoch(ctx, "mer-1", domain.SessionModeTUI, domain.SessionModeChat, "native-1", false, domain.AgentConfig{Model: "luna", ServiceTier: "default"}); err != nil {
		t.Fatal(err)
	}
	got = st.sessions["mer-1"].Metadata
	if got.Model != "luna" || got.ReasoningEffort != "" || got.ServiceTier != "default" {
		t.Fatalf("did not replace stale parameters: %+v", got)
	}
}
