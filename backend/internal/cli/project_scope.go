package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Both Chat tool shells and TUI agents inherit the AO session identity.
// Resolve its project from the daemon, never from cwd or a session-id prefix.
func callerSessionID() (string, error) {
	id := strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
	if id == "" {
		return "", nil
	}
	if len(id) > 128 || !sessionIDPattern.MatchString(id) {
		return "", usageError{fmt.Errorf("invalid AO_SESSION_ID %q", id)}
	}
	return id, nil
}

func (c *commandContext) callerProject(ctx context.Context) (string, string, error) {
	id, err := callerSessionID()
	if err != nil || id == "" {
		return id, "", err
	}
	sess, err := c.fetchScopedSession(ctx, id, "")
	if err != nil {
		return "", "", fmt.Errorf("resolve project for AO_SESSION_ID %q: %w", id, err)
	}
	if strings.TrimSpace(sess.ProjectID) == "" || sess.IsTerminated {
		return "", "", usageError{fmt.Errorf("AO_SESSION_ID %q has no active project context; resume the agent before retrying", id)}
	}
	return id, sess.ProjectID, nil
}

func (c *commandContext) sessionListProject(ctx context.Context, explicit string, allProjects bool) (string, error) {
	_, project, err := c.callerProject(ctx)
	if err != nil {
		return "", err
	}
	if explicit != "" {
		return explicit, nil
	}
	if allProjects {
		return "", nil
	}
	return project, nil
}

func checkCallerProject(caller, ownProject, targetProject string) error {
	if caller != "" && ownProject != targetProject {
		return usageError{fmt.Errorf("session %s belongs to project %s; cross-project dispatch to %s is not allowed", caller, ownProject, targetProject)}
	}
	return nil
}
