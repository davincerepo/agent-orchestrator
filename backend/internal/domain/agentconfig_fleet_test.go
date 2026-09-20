package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFleetAgentConfigLegacyEffort(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"model":"gpt-test","reasoningEffort":"high","serviceTier":"priority"}`, "high"},
		{`{"model":"gpt-test","reasoningEffort":"high","effort":"low","serviceTier":"priority"}`, "low"},
		{`{"model":"gpt-test","reasoningEffort":"high","effort":"","serviceTier":"priority"}`, ""},
	} {
		var cfg AgentConfig
		if err := json.Unmarshal([]byte(tc.body), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Effort != tc.want || cfg.Model != "gpt-test" || cfg.ServiceTier != "priority" {
			t.Fatalf("config = %+v", cfg)
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "reasoningEffort") {
			t.Fatalf("legacy key persisted: %s", data)
		}
	}
}
