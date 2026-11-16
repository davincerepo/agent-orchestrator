package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestProviderEventRetentionPrunesDeltasAndCapsRows(t *testing.T) {
	ctx := context.Background()
	st := sqlitetest.MustOpenAt(t, t.TempDir())
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	if err := st.UpsertProject(ctx, domain.ProjectRecord{ID: "proj", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	rec := sampleRecord("proj")
	rec.Mode = domain.SessionModeChat
	session, err := st.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	conv, err := st.CreateConversation(ctx, "retention-conv", domain.ConversationScopeSession, "proj", session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(eventID, method string, at time.Time) {
		t.Helper()
		if _, err := st.ProjectProviderEvent(ctx, conv.ID, session.ID, "", eventID, method,
			"{}", at, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	insert("d1", "reasoning.delta", now.Add(-49*time.Hour))
	insert("d2", "message.delta", now.Add(-49*time.Hour))
	insert("d3", "reasoning.delta", now.Add(-time.Hour))
	insert("c1", "turn.completed", now.Add(-200*time.Hour))

	removed, err := st.PruneProviderEventDeltasBefore(ctx, now.Add(-48*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("deltas removed = %d, want 2", removed)
	}

	// The cap removes oldest rows regardless of kind.
	for i := 0; i < 5; i++ {
		insert(fmt.Sprintf("cap-%d", i), "turn.completed", now.Add(time.Duration(i)*time.Minute))
	}
	removed, err = st.PruneProviderEventsToMaxRows(ctx, 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 3 {
		t.Fatalf("capped rows removed = %d, want 3", removed)
	}
}
