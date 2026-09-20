package codexappserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestFleetModelParametersStartResumeAndTurn(t *testing.T) {
	for _, tier := range []string{"", "priority", "default"} {
		for _, resume := range []bool{false, true} {
			t.Run(tier+map[bool]string{false: "/start", true: "/resume"}[resume], func(t *testing.T) {
				d, server := newTestDriver(t)
				ctx := context.Background()
				var conv ports.ChatConversation
				var err error
				method := "thread/start"
				if resume {
					method = "thread/resume"
					conv, err = d.Resume(ctx, ports.ChatResumeConfig{WorkspacePath: t.TempDir(), ProviderConversationID: "thread-1", Model: "gpt-test", Effort: "ultra", ServiceTier: tier})
				} else {
					conv, err = d.Start(ctx, ports.ChatStartConfig{WorkspacePath: t.TempDir(), Model: "gpt-test", Effort: "ultra", ServiceTier: tier})
				}
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conv.Close() })
				request := server.awaitFrame(func(f frame) bool { return f.Method == method })
				var params map[string]any
				if err := json.Unmarshal(request.Params, &params); err != nil {
					t.Fatal(err)
				}
				if params["config"].(map[string]any)["model_reasoning_effort"] != "ultra" {
					t.Fatalf("launch config = %v", params)
				}
				assertFleetTier(t, params, tier)
				if _, err := conv.SendTurn(ctx, ports.ChatUserMessage{Text: "test", Settings: ports.ChatTurnSettings{Effort: "high", ServiceTier: tier}}); err != nil {
					t.Fatal(err)
				}
				request = server.awaitFrame(func(f frame) bool { return f.Method == "turn/start" })
				params = nil
				if err := json.Unmarshal(request.Params, &params); err != nil {
					t.Fatal(err)
				}
				assertFleetTier(t, params, tier)
				if params["effort"] != "high" {
					t.Fatalf("turn effort = %v", params)
				}
			})
		}
	}
}

func assertFleetTier(t *testing.T, params map[string]any, tier string) {
	t.Helper()
	got, present := params["serviceTier"]
	if (tier == "" && present) || (tier != "" && got != tier) {
		t.Fatalf("serviceTier = %v (present %v), want %q", got, present, tier)
	}
}

func TestFleetModelCapabilitiesModernAndLegacy(t *testing.T) {
	d, server := newTestDriver(t)
	server.reply("thread/start", `{"thread":{"id":"thread-1"},"model":"modern","reasoningEffort":"high"}`)
	server.responses["model/list"] = `{"data":[{"id":"modern","defaultReasoningEffort":"high","supportedReasoningEfforts":[{"reasoningEffort":"low"},{"reasoningEffort":"ultra"}],"serviceTiers":[{"id":"priority","name":"Fast","description":"Lower latency"}],"defaultServiceTier":"default"},{"id":"legacy","additionalSpeedTiers":["priority"]},{"id":"unsupported","serviceTiers":[],"additionalSpeedTiers":["priority"]},{"id":"plain"}]}`
	conv, err := d.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conv.Close() })
	models, err := conv.(ports.ChatModelLister).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 4 || len(models[0].Efforts) != 2 || models[0].Efforts[1] != "ultra" || models[0].DefaultEffort != "high" {
		t.Fatalf("models = %+v", models)
	}
	if len(models[0].ServiceTiers) != 1 || models[0].ServiceTiers[0].Description != "Lower latency" || models[0].DefaultServiceTier != "default" {
		t.Fatalf("modern tiers = %+v", models[0])
	}
	if len(models[1].ServiceTiers) != 1 || models[1].ServiceTiers[0].ID != "priority" {
		t.Fatalf("legacy tiers = %+v", models[1])
	}
	if len(models[2].ServiceTiers) != 0 || len(models[3].ServiceTiers) != 0 {
		t.Fatal("invented unsupported capabilities")
	}
}
