package store_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestFleetModelParametersPersistAndClear(t *testing.T) {
	store, sessionID, conversationID := conversationFixture(t)
	ctx := context.Background()
	for _, tier := range []string{"priority", "default", ""} {
		settings := domain.ConversationSettings{Model: "gpt-test", ReasoningEffort: "ultra", ServiceTier: tier}
		if err := store.SetConversationSettings(ctx, conversationID, settings, histClock); err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.LoadConversationSnapshot(ctx, conversationID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Conversation.Settings != settings {
			t.Fatalf("stored settings = %+v, want %+v", snapshot.Conversation.Settings, settings)
		}
	}
	record, found, err := store.GetSession(ctx, sessionID)
	if err != nil || !found {
		t.Fatalf("session: %v %v", found, err)
	}
	record.Metadata.ReasoningEffort = "high"
	record.Metadata.ServiceTier = "priority"
	if err := store.UpdateSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	restored, found, err := store.GetSession(ctx, sessionID)
	if err != nil || !found {
		t.Fatalf("reload: %v %v", found, err)
	}
	if restored.Metadata.ReasoningEffort != "high" || restored.Metadata.ServiceTier != "priority" {
		t.Fatalf("launch snapshot = %+v", restored.Metadata)
	}
}

func TestFleetInterfaceModelParametersCommitAndRollback(t *testing.T) {
	store, id, conversationID := conversationFixture(t)
	ctx := context.Background()
	rec, _, err := store.GetSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rec.Mode = domain.SessionModeChat
	rec.Metadata.Model, rec.Metadata.ReasoningEffort, rec.Metadata.ServiceTier = "original", "high", "priority"
	if err := store.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.SetConversationSettings(ctx, conversationID, domain.ConversationSettings{Model: "original", ReasoningEffort: "high", ServiceTier: "priority", ApprovalMode: domain.PermissionModeAuto}, histClock); err != nil {
		t.Fatal(err)
	}
	for _, p := range []domain.AgentConfig{{Model: "new", Effort: "low", ServiceTier: "default"}, {Model: "legacy", Effort: "xhigh"}, {}} {
		rec, _, _ = store.GetSession(ctx, id)
		source, target := rec.Mode, domain.SessionModeTUI
		if source == target {
			target = domain.SessionModeChat
		}
		changed, err := store.CommitSessionControllerEpoch(ctx, id, source, target, "native-1", histClock, p)
		if err != nil || !changed {
			t.Fatalf("commit: %v %v", changed, err)
		}
		// Stale epoch writers must not change either parameter record.
		changed, err = store.CommitSessionControllerEpoch(ctx, id, source, target, "native-1", histClock, domain.AgentConfig{Model: "stale-writer"})
		if err != nil || changed {
			t.Fatalf("stale CAS: %v %v", changed, err)
		}
		rec, _, _ = store.GetSession(ctx, id)
		conv, err := store.ConversationForSession(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Metadata.Model != p.Model || rec.Metadata.ReasoningEffort != p.Effort || rec.Metadata.ServiceTier != p.ServiceTier {
			t.Fatalf("session snapshot: %+v", rec.Metadata)
		}
		if conv.Settings.Model != p.Model || conv.Settings.ReasoningEffort != p.Effort || conv.Settings.ServiceTier != p.ServiceTier || conv.Settings.ApprovalMode != domain.PermissionModeAuto {
			t.Fatalf("conversation snapshot: %+v", conv.Settings)
		}
		_, err = store.CommitSessionControllerEpoch(ctx, id, target, source, "native-1", histClock, domain.AgentConfig{Model: "bad-write", ServiceTier: "invalid"})
		if err == nil {
			t.Fatal("invalid snapshot committed")
		}
		after, _, _ := store.GetSession(ctx, id)
		if after.Mode != target || after.Metadata != rec.Metadata {
			t.Fatal("failed transaction changed mode or snapshot")
		}
	}
}
