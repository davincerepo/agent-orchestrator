package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var ErrModelParameters = errors.New("unsupported model parameters")

func validateModelParameters(ctx context.Context, harness domain.AgentHarness, conv ports.ChatConversation, settings domain.ConversationSettings) error {
	if err := (domain.AgentConfig{Effort: settings.ReasoningEffort, ServiceTier: settings.ServiceTier}).Validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrModelParameters, err)
	}
	if harness != domain.HarnessCodex {
		if settings.ServiceTier != "" {
			return fmt.Errorf("%w: %s does not accept serviceTier", ErrModelParameters, harness)
		}
		return nil // ACP adapters own their reasoning controls.
	}
	if settings.ReasoningEffort == "" && settings.ServiceTier != "priority" {
		return nil
	}
	lister, ok := conv.(ports.ChatModelLister)
	if !ok {
		if settings.ServiceTier == "priority" {
			return fmt.Errorf("%w: Fast is not advertised", ErrModelParameters)
		}
		return nil
	}
	models, err := lister.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model.ID != settings.Model && !(settings.Model == "" && model.Default) {
			continue
		}
		if settings.ReasoningEffort != "" && !slices.Contains(model.Efforts, settings.ReasoningEffort) {
			return fmt.Errorf("%w: %s does not support effort %s", ErrModelParameters, model.ID, settings.ReasoningEffort)
		}
		if settings.ServiceTier == "priority" && !slices.ContainsFunc(model.ServiceTiers, func(tier ports.ModelServiceTier) bool { return tier.ID == "priority" }) {
			return fmt.Errorf("%w: %s does not support Fast", ErrModelParameters, model.ID)
		}
		return nil
	}
	return fmt.Errorf("%w: model %q has no advertised parameter choices", ErrModelParameters, settings.Model)
}
