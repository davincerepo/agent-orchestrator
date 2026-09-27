package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Standing-prompt replacement: rebind a live Chat session to a copy of its
// provider conversation that carries the SAME dialogue history under NEW
// standing instructions.
//
// The session, workspace, timeline rows, and branch ownership are untouched;
// only the provider conversation handle moves. The flow is the native-fork
// edit path minus the message dispatch: copy provider-side (capability), close
// the fenced source controller, cold-resume the copy with the new prompt, then
// swap the controller through the same durable branch transaction an edit
// uses. The provider implementations and the experiments that proved them are
// documented on ports.ChatPromptRefresher.

// ErrPromptReloadUnsupported reports a provider that cannot replace standing
// instructions on a copy of its conversation. The sentinel lives in ports so
// the session-facing surface can map it without importing this package.
var ErrPromptReloadUnsupported = ports.ErrChatPromptReloadUnsupported

// promptRefresher is the resolved copy operation: either the live conversation
// performs it over its provider connection, or the driver prepares it from
// durable native records alone. See the two port interfaces.
type promptRefresher func(ctx context.Context, systemPrompt string) (string, error)

// resolvePromptRefresher feature-detects the copy operation for one session.
func resolvePromptRefresher(source *Controller, cfg StartConfig, driver ports.ChatDriver) (promptRefresher, error) {
	if conversationRefresher, ok := source.conv.(ports.ChatPromptRefresher); ok {
		return conversationRefresher.RefreshStandingPrompt, nil
	}
	if driverRefresher, ok := driver.(ports.ChatDriverPromptRefresher); ok {
		env := cfg.Env
		providerConversationID := source.ProviderConversationID()
		return func(ctx context.Context, systemPrompt string) (string, error) {
			return driverRefresher.RefreshStandingPrompt(ctx, providerConversationID, systemPrompt, env)
		}, nil
	}
	return nil, ErrPromptReloadUnsupported
}

// ReloadStandingPrompt replaces the session's standing instructions by copying
// its provider conversation and rebinding to the copy. Refused while a turn is
// running or queued; a failure after the source controller closed restores the
// original conversation rather than leaving the session without one.
func (s *Service) ReloadStandingPrompt(
	ctx context.Context,
	id domain.SessionID,
	systemPrompt string,
) (ports.ChatPromptReloadResult, error) {
	gate := s.controllerGate(id)
	if err := gate.lock(ctx); err != nil {
		return ports.ChatPromptReloadResult{}, err
	}
	defer gate.unlock()

	if _, err := s.requireChatSession(ctx, id); err != nil {
		return ports.ChatPromptReloadResult{}, err
	}
	source, err := s.Controller(id)
	if err != nil {
		return ports.ChatPromptReloadResult{}, err
	}
	cfg, driver, err := s.branchLaunchConfig(id, source)
	if err != nil {
		return ports.ChatPromptReloadResult{}, err
	}
	refresher, err := resolvePromptRefresher(source, cfg, driver)
	if err != nil {
		return ports.ChatPromptReloadResult{}, err
	}
	// Refused before any provider work: a running or queued turn makes the copy
	// describe a conversation that is already moving on.
	if err := source.BeginIdleBranchHandoff(ctx); err != nil {
		return ports.ChatPromptReloadResult{}, err
	}
	abortSource := true
	defer func() {
		if abortSource {
			source.AbortHandoff()
		}
	}()
	sourceBranch, err := s.store.ConversationBranch(ctx, source.conversation.ID, source.conversation.ActiveBranchID)
	if err != nil {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("load active conversation branch: %w", err)
	}

	// The copy is provider-side state with no AO facts attached, so the caller's
	// context governs it: cancelling here leaves a provider orphan that a retry
	// never mistakes for progress. Everything after is AO-observable and must
	// finish independently of the request (same budget as a native edit).
	providerConversationID, err := refresher(ctx, systemPrompt)
	if err != nil {
		return ports.ChatPromptReloadResult{}, classify(fmt.Errorf("copy conversation for %s: %w", id, err))
	}
	detachedCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), nativeEditHandoffLimit)
	defer cancel()
	abortSource = false // the deferred AbortHandoff must not reopen a stopped source

	if err := source.closeForBranchHandoff(detachedCtx); err != nil {
		err = fmt.Errorf("close source writer after prompt copy: %w", err)
		if restoreErr := s.restoreClosedSourceController(
			ctx, id, source, sourceBranch, cfg, driver); restoreErr != nil {
			return ports.ChatPromptReloadResult{}, errors.Join(err, restoreErr)
		}
		return ports.ChatPromptReloadResult{}, err
	}

	cfg.SystemPrompt = systemPrompt
	launchEnv, err := s.prepareBranchControllerEnv(detachedCtx, cfg)
	if err != nil {
		if restoreErr := s.restoreClosedSourceController(
			ctx, id, source, sourceBranch, cfg, driver); restoreErr != nil {
			return ports.ChatPromptReloadResult{}, errors.Join(err, restoreErr)
		}
		return ports.ChatPromptReloadResult{}, err
	}
	provider, err := driver.Resume(detachedCtx, ports.ChatResumeConfig{
		SessionID: cfg.SessionID, ProviderConversationID: providerConversationID,
		DataDir: cfg.DataDir, WorkspacePath: cfg.WorkspacePath, Env: launchEnv,
		Model: cfg.Model, Effort: cfg.Effort, Permissions: cfg.Permissions,
		SystemPrompt: systemPrompt,
		// The copy is the same conversation continuing, so it inherits the
		// source branch's opaque-id namespace and the rows already projected
		// under it deduplicate rather than duplicate on later native reads.
		ProviderScopeID:       sourceBranch.ProviderScopeID,
		ProviderIDsScoped:     sourceBranch.ProviderIDsScoped,
		AdditionalDirectories: cfg.AdditionalDirectories, MCPServers: cfg.MCPServers,
	})
	if err != nil {
		err = fmt.Errorf("resume prompt-replaced conversation: %w", err)
		if restoreErr := s.restoreClosedSourceController(
			ctx, id, source, sourceBranch, cfg, driver); restoreErr != nil {
			return ports.ChatPromptReloadResult{}, errors.Join(err, restoreErr)
		}
		return ports.ChatPromptReloadResult{}, err
	}

	branchID := s.newID()
	generation := s.newID()
	branch := domain.ConversationBranch{
		ID: branchID, ConversationID: source.conversation.ID, SessionID: id,
		ProviderConversationID: providerConversationID, ParentBranchID: sourceBranch.ID,
		// Nothing is replaced: the fork point is the conversation's settled head.
		ForkAfterSequence: source.conversation.LatestSequence,
		CreatedAt:         s.now(), Strategy: domain.ConversationBranchStrategyNative,
		ProviderScopeID:   "",
		ProviderIDsScoped: sourceBranch.ProviderIDsScoped,
	}
	conversation := source.conversation
	conversation.ActiveBranchID = branchID
	replacement := newController(id, conversation, generation, source.harness, provider,
		s.store, s.activity, s.log, s.newID, s.now, s.onAccountChanged, s.onCodexCapacityChanged)
	if err := s.store.CreateAndActivateConversationBranch(
		detachedCtx, id, branch, generation, s.now(),
	); err != nil {
		_ = provider.Close()
		err = fmt.Errorf("activate prompt-replaced conversation: %w", err)
		if restoreErr := s.restoreClosedSourceController(
			ctx, id, source, sourceBranch, cfg, driver); restoreErr != nil {
			return ports.ChatPromptReloadResult{}, errors.Join(err, restoreErr)
		}
		return ports.ChatPromptReloadResult{}, err
	}
	if err := s.installBranchController(detachedCtx, id, source, replacement, sourceBranch.ID); err != nil {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("install prompt-replaced controller: %w", err)
	}
	// Later branch operations resume from the cached launch config; without this
	// they would replay the superseded prompt back into the provider.
	s.mu.Lock()
	if cfg, ok := s.startConfigs[id]; ok {
		cfg.SystemPrompt = systemPrompt
		s.startConfigs[id] = cfg
	}
	s.mu.Unlock()
	return ports.ChatPromptReloadResult{ProviderConversationID: providerConversationID, BranchID: branchID}, nil
}
