package sessionmanager

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestFleetModelParameterPrecedenceAndResumeSnapshot(t *testing.T) {
	project := domain.ProjectConfig{
		AgentConfig: domain.AgentConfig{Model: "gpt-test", Effort: "low", ServiceTier: "priority"},
		Worker:      domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Effort: "high", ServiceTier: "default"}},
	}
	worker := effectiveAgentConfig(domain.HarnessCodex, domain.KindWorker, project)
	if worker.Effort != "high" || worker.ServiceTier != "default" {
		t.Fatalf("worker = %+v", worker)
	}
	orchestrator := effectiveAgentConfig(domain.HarnessCodex, domain.KindOrchestrator, project)
	if orchestrator.Effort != "low" || orchestrator.ServiceTier != "priority" {
		t.Fatalf("orchestrator = %+v", orchestrator)
	}
	resolved := applySpawnAgentConfig(worker, ports.AgentConfig{Effort: "ultra", ServiceTier: "priority"})
	if resolved.Effort != "ultra" || resolved.ServiceTier != "priority" {
		t.Fatalf("override = %+v", resolved)
	}
	record := domain.SessionRecord{Harness: domain.HarnessCodex, Kind: domain.KindWorker, Metadata: domain.SessionMetadata{ReasoningEffort: "medium", ServiceTier: "default"}}
	restored := restoredAgentConfig(record, project)
	if restored.Effort != "medium" || restored.ServiceTier != "default" {
		t.Fatalf("resume used new project defaults: %+v", restored)
	}
	record.Metadata = domain.SessionMetadata{}
	restored = restoredAgentConfig(record, project)
	if restored.Effort != "" || restored.ServiceTier != "" {
		t.Fatalf("legacy session must retain native settings: %+v", restored)
	}
	other := normalizeAgentConfigForHarness(domain.HarnessClaudeCode, worker)
	if other.Effort != "" || other.ServiceTier != "" {
		t.Fatalf("leaked Codex options: %+v", other)
	}
}
