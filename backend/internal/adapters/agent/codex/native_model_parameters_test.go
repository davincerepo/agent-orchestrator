package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const settingsThreadID = "01900000-0000-7000-8000-000000000001"
const nativeHigh = `{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"sol","reasoning_effort":"high","service_tier":"priority"}}}`
const nativeLow = `{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"luna","reasoning_effort":"low","service_tier":null}}}`

func TestFleetNativeModelParameters(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       ports.AgentConfig
		bad        bool
	}{
		{"latest-menu-without-turn", nativeHigh + "\n" + nativeLow + "\n", ports.AgentConfig{Model: "luna", Effort: "low", ServiceTier: "default"}, false},
		{"fast-on", nativeHigh + "\n", ports.AgentConfig{Model: "sol", Effort: "high", ServiceTier: "priority"}, false},
		{"legacy-does-not-invent-fast", `{"type":"turn_context","payload":{"model":"legacy","effort":"xhigh"}}` + "\n", ports.AgentConfig{Model: "legacy", Effort: "xhigh"}, false},
		{"null-effort", `{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"plain","reasoning_effort":null}}}` + "\n", ports.AgentConfig{Model: "plain", ServiceTier: "default"}, false},
		{"foreign-owner", nativeHigh + "\n" + `{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_id":"parent-thread","thread_settings":{"model":"wrong","service_tier":"default"}}}` + "\n", ports.AgentConfig{Model: "sol", Effort: "high", ServiceTier: "priority"}, false},
		{"ignore-message-content", `{"type":"response_item","payload":{"type":"message","content":[{"text":"thread_settings_applied"}]}}` + "\n", ports.AgentConfig{}, false},
		{"partial-tail", nativeHigh + "\n" + `{"type":"event_msg","payload":`, ports.AgentConfig{Model: "sol", Effort: "high", ServiceTier: "priority"}, false},
		{"missing-model", `{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"service_tier":"priority"}}}` + "\n", ports.AgentConfig{}, true},
		{"malformed", `{"type":"event_msg","payload":{"type":"thread_settings_applied",bad}}` + "\n", ports.AgentConfig{}, true},
		{"bounded-tail", strings.Repeat("{}\n", nativeSettingsTailLimit/3+1) + nativeLow + "\n", ports.AgentConfig{Model: "luna", Effort: "low", ServiceTier: "default"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "sessions"), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "sessions", "rollout-"+settingsThreadID+".jsonl")
			body := `{"type":"session_meta","payload":{"id":"` + settingsThreadID + `"}}` + "\n" + tc.body
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := New().ReadNativeModelParameters(context.Background(), ports.NativeSessionRef{ConfigDir: root, NativeSessionID: settingsThreadID})
			if (err != nil) != tc.bad {
				t.Fatalf("err=%v want error=%v", err, tc.bad)
			}
			if !tc.bad && got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}
