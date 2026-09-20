package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/codexappserver/codexproto"
)

type threadSettingsRefresh struct {
	done chan struct{}
	err  error
}

// ensureThreadSettings repairs the in-memory display cache. It never writes AO
// turn overrides or reads global config as a substitute for the loaded thread.
// Concurrent viewers share one bounded attempt; a later visit may retry failure.
func (c *conversation) ensureThreadSettings(ctx context.Context) error {
	c.mu.Lock()
	if c.threadSettingsKnown {
		c.mu.Unlock()
		return nil
	}
	if pending := c.settingsRefresh; pending != nil {
		c.mu.Unlock()
		select {
		case <-pending.done:
			return pending.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	pending := &threadSettingsRefresh{done: make(chan struct{})}
	c.settingsRefresh = pending
	c.mu.Unlock()

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := c.readLoadedThreadSettings(readCtx)
	cancel()
	c.mu.Lock()
	pending.err = err
	c.settingsRefresh = nil
	close(pending.done)
	c.mu.Unlock()
	return err
}

func (c *conversation) readLoadedThreadSettings(ctx context.Context) error {
	// Do not race a new turn's overrides with the snapshot read.
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.mu.Lock()
	known, revision := c.threadSettingsKnown, c.threadSettingsRevision
	c.mu.Unlock()
	if known {
		return nil
	}

	// thread/read has no effective model settings. Bare thread/resume returns
	// the loaded thread's config snapshot. Check membership first so this read
	// does not intentionally cold-resume a thread in a replacement process.
	cursor := ""
	seen := map[string]bool{}
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page codexproto.ThreadLoadedListResponse
		if err := c.conn.request(ctx, "thread/loaded/list", params, &page); err != nil {
			return fmt.Errorf("read loaded Codex threads: %w", err)
		}
		found := false
		for _, id := range page.Data {
			found = found || id == c.threadID
		}
		if found {
			break
		}
		if page.NextCursor == nil || *page.NextCursor == "" || seen[*page.NextCursor] {
			return errors.New("Codex thread is not loaded; cannot confirm its model settings")
		}
		cursor = *page.NextCursor
		seen[cursor] = true
	}

	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model           string `json:"model"`
		ReasoningEffort string `json:"reasoningEffort"`
		ServiceTier     string `json:"serviceTier"`
	}
	// No model, config, cwd, instructions, or permission overrides: this is a
	// rejoin of the existing thread, never an application of current defaults.
	if err := c.conn.request(ctx, "thread/resume", map[string]any{"threadId": c.threadID}, &resp); err != nil {
		return fmt.Errorf("read Codex thread settings: %w", err)
	}
	if resp.Thread.ID != c.threadID || resp.Model == "" {
		return errors.New("Codex returned no matching thread model settings")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A provider settings event received during the request is newer evidence.
	if c.threadSettingsRevision == revision {
		c.threadModel, c.threadEffort, c.threadServiceTier = resp.Model, resp.ReasoningEffort, resp.ServiceTier
		c.threadSettingsKnown = true
		c.threadSettingsRevision++
	}
	return nil
}

func (c *conversation) trackThreadSettings(n notification) {
	if n.Method != codexproto.MethodThreadSettingsUpdated {
		return
	}
	var params struct {
		ThreadID string `json:"threadId"`
		Settings struct {
			Model       string `json:"model"`
			Effort      string `json:"effort"`
			ServiceTier string `json:"serviceTier"`
		} `json:"threadSettings"`
	}
	if json.Unmarshal(n.Params, &params) != nil || params.ThreadID != c.threadID || params.Settings.Model == "" {
		return
	}
	c.mu.Lock()
	c.threadModel, c.threadEffort, c.threadServiceTier = params.Settings.Model, params.Settings.Effort, params.Settings.ServiceTier
	c.threadSettingsKnown = true
	c.threadSettingsRevision++
	c.mu.Unlock()
}

func effectiveServiceTier(tier string) string {
	if tier == "" {
		return "default"
	}
	return tier
}
