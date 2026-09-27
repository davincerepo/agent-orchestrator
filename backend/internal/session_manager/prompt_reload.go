package sessionmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ChatPromptReloader is the optional ChatLauncher extension that rebinds a
// live chat session to a copy of its provider conversation carrying the same
// dialogue under new standing instructions. Feature-detected so embedded and
// test launchers without the extension keep satisfying ChatLauncher.
type ChatPromptReloader interface {
	ReloadChatPrompt(ctx context.Context, id domain.SessionID, systemPrompt string) (ports.ChatPromptReloadResult, error)
}

// ErrPromptReloadUnavailable reports a session whose standing prompt cannot be
// replaced: either the build has no chat support or the session is not live in
// chat mode.
var ErrPromptReloadUnavailable = errors.New("standing prompt reload is unavailable for this session")

// ReloadSessionPrompt recomputes the session's standing instructions from the
// current project rules and swaps the live provider conversation for a copy
// that inherits the dialogue history under those instructions.
//
// The AO session, workspace, branch, and timeline are unchanged; only the
// provider conversation handle moves. Refused while the session has a turn
// running or queued (surfaced by the chat service) so the copy always
// describes a settled conversation.
func (m *Manager) ReloadSessionPrompt(
	ctx context.Context,
	id domain.SessionID,
) (ports.ChatPromptReloadResult, error) {
	rec, found, err := m.store.GetSession(ctx, id)
	if err != nil {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: session: %w", id, err)
	}
	if !found {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: %w", id, ports.ErrSessionNotFound)
	}
	if rec.IsTerminated {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: %w", id, ErrTerminated)
	}
	if domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: %w", id, ErrPromptReloadUnavailable)
	}
	if m.chat == nil {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: %w", id, ports.ErrChatUnsupported)
	}
	reloader, ok := m.chat.(ChatPromptReloader)
	if !ok {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: %w", id, ErrPromptReloadUnavailable)
	}

	// Recomputed rather than persisted, matching the restore path: the reload
	// applies the rules as they stand now.
	systemPrompt, err := m.buildSystemPrompt(ctx, rec.Kind, rec.ProjectID)
	if err != nil {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: system prompt: %w", id, err)
	}
	result, err := reloader.ReloadChatPrompt(ctx, id, systemPrompt)
	if err != nil {
		return ports.ChatPromptReloadResult{}, fmt.Errorf("reload prompt %s: %w", id, err)
	}
	return result, nil
}
