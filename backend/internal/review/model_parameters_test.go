package review

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestFleetReviewerParameterOverridesReachLaunch(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeChat, domain.SessionModeTUI} {
		for _, scope := range []string{"project", "session", "request", "different-harness"} {
			t.Run(string(mode)+"/"+scope, func(t *testing.T) {
				worker := liveWorker()
				worker.Mode = mode
				base := domain.AgentConfig{Model: "codex-model", Effort: "high", ServiceTier: "priority"}
				override := domain.AgentConfig{Effort: "low", ServiceTier: "default"}
				projectHarness := domain.ReviewerCodex
				var request domain.AgentConfig
				var harness domain.ReviewerHarness
				want := base
				switch scope {
				case "session":
					worker.ReviewerConfig = override
					want.Effort, want.ServiceTier = "low", "default"
				case "request":
					request = override
					want.Effort, want.ServiceTier = "low", "default"
				case "different-harness":
					projectHarness = domain.ReviewerClaudeCode
					harness, request, want = domain.ReviewerCodex, override, override
				}
				launcher := &fakeLauncher{handle: "review-mer-1"}
				projects := fakeProjects{cfg: domain.ProjectConfig{Reviewers: []domain.ReviewerConfig{{Harness: projectHarness, AgentConfig: base}}}}
				eng := newEngineForTest(&fakeStore{}, fakeSessions{rec: worker, ok: true}, prAt("sha1"), projects, launcher)
				if _, err := eng.Trigger(context.Background(), worker.ID, harness, request); err != nil {
					t.Fatal(err)
				}
				if got := launcher.gotSpec.AgentConfig; got != want {
					t.Fatalf("reviewer launch config = %+v, want %+v", got, want)
				}
			})
		}
	}
}
