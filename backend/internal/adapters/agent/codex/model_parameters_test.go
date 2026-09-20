package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestFleetTerminalModelParameters(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	for _, tier := range []string{"default", "priority"} {
		config := ports.AgentConfig{Effort: "ultra", ServiceTier: tier}
		launch, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{WorkspacePath: t.TempDir(), Config: config})
		if err != nil {
			t.Fatal(err)
		}
		restored, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{Config: config, Session: ports.SessionRef{ID: "test-1", Metadata: map[string]string{"agentSessionId": "thread-1"}}})
		if err != nil || !ok {
			t.Fatalf("restore: %v %v", ok, err)
		}
		for _, args := range [][]string{launch, restored} {
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, `model_reasoning_effort='ultra'`) || !strings.Contains(joined, `service_tier="`+tier+`"`) {
				t.Fatalf("missing model parameters: %v", args)
			}
		}
	}
}
