// Package dispatchscope checks optional AO caller context before dispatch.
package dispatchscope

import (
	"context"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type callerSessionKey struct{}

// SessionReader supplies the authoritative project membership.
type SessionReader interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
}

// WithCallerSession carries optional agent context across HTTP/service calls.
// It is a guard against accidental cross-project dispatch, not authentication.
// Human UI requests and internal daemon work omit it. Message text is never
// interpreted as caller identity, including legacy "[from ...]" prefixes.
func WithCallerSession(ctx context.Context, id domain.SessionID) context.Context {
	return context.WithValue(ctx, callerSessionKey{}, id)
}

func callerProject(ctx context.Context, store SessionReader) (domain.ProjectID, error) {
	id, _ := ctx.Value(callerSessionKey{}).(domain.SessionID)
	if id == "" {
		return "", nil
	}
	if strings.TrimSpace(string(id)) != string(id) || len(id) > 128 {
		return "", apierr.Invalid("INVALID_CALLER_SESSION", "Invalid callerSessionId", nil)
	}
	rec, ok, err := store.GetSession(ctx, id)
	if err != nil {
		return "", fmt.Errorf("resolve caller session %s: %w", id, err)
	}
	if !ok || rec.IsTerminated || strings.TrimSpace(string(rec.ProjectID)) == "" {
		return "", apierr.Conflict("CALLER_SESSION_UNAVAILABLE", "Caller session is missing, terminated, or has no project; resume the agent before retrying", nil)
	}
	return rec.ProjectID, nil
}

func requireSameProject(callerProject, targetProject domain.ProjectID) error {
	if callerProject != "" && callerProject != targetProject {
		return apierr.Forbidden("CROSS_PROJECT_DISPATCH", fmt.Sprintf("Agent in project %s cannot dispatch work to project %s", callerProject, targetProject))
	}
	return nil
}

// CheckProject rejects dispatch outside the caller project.
func CheckProject(ctx context.Context, store SessionReader, project domain.ProjectID) error {
	callerProject, err := callerProject(ctx, store)
	if err != nil {
		return err
	}
	return requireSameProject(callerProject, project)
}

// CheckSession rejects dispatch to a session outside the caller project.
func CheckSession(ctx context.Context, store SessionReader, id domain.SessionID) error {
	project, err := callerProject(ctx, store)
	if err != nil || project == "" {
		return err
	}
	target, ok, err := store.GetSession(ctx, id)
	if err != nil {
		return fmt.Errorf("get dispatch target %s: %w", id, err)
	}
	if !ok {
		return apierr.NotFound("SESSION_NOT_FOUND", "Unknown target session")
	}
	return requireSameProject(project, target.ProjectID)
}
