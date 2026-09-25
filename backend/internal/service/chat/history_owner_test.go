package chat_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

// Exercise unchanged inline edits through successive native forks. Use the real
// ownership CAS so a replacement cannot hide a stale cached launch identity.
func TestEditMessageRepeatedNativeForksPreserveControllerOwner(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	source := newHistoryRecorder()
	source.forkedTo = "thread-fork-1"
	currentProvider := source
	var ids atomic.Int64
	resumes := 0
	prepare := func(ctx context.Context, expected domain.SessionControllerOwner) (map[string]string, error) {
		applied, err := st.UpdateBrowserCapabilityVerifier(ctx, testSession, expected, "test-verifier")
		if err != nil {
			return nil, err
		}
		if !applied {
			rec, _, readErr := st.GetSession(ctx, testSession)
			t.Logf("owner CAS failed: expected=%+v actual=%+v readErr=%v", expected, rec.ControllerOwner(), readErr)
			return nil, errors.New("session controller ownership changed before browser capability rotation")
		}
		return map[string]string{"AO_BROWSER_CAPABILITY": "test-token"}, nil
	}
	driver := fakeDriver{
		conv: source,
		resume: func(cfg ports.ChatResumeConfig) (ports.ChatConversation, error) {
			resumes++
			next := newHistoryRecorder()
			next.providerConversationID = cfg.ProviderConversationID
			next.turnSeq = resumes * 100
			next.forkedTo = fmt.Sprintf("thread-fork-%d", resumes+1)
			currentProvider = next
			return next, nil
		},
	}
	svc := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st, Reader: fullSnapshotReader(st),
		Drivers: fakeRegistry{driver: driver}, Log: slog.New(slog.DiscardHandler),
		NewID: func() string { return fmt.Sprintf("edit-owner-%d", ids.Add(1)) },
	})
	rec, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found {
		t.Fatalf("read initial session: %v", err)
	}
	// Match a resumed production session whose native identity is already known.
	rec.Metadata.AgentSessionID = "thread-1"
	if err := st.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	ctrl, err := svc.Start(ctx, chatsvc.StartConfig{
		SessionID: testSession, ProjectID: testProject, Kind: domain.KindWorker,
		Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(),
		ExpectedControllerOwner: rec.ControllerOwner(), PrepareControllerEnv: prepare,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), testSession) })
	rec, found, err = st.GetSession(ctx, testSession)
	if err != nil || !found {
		t.Fatalf("read started session: found=%v err=%v", found, err)
	}
	rec.Metadata.ProviderConversationID = "thread-1"
	rec.Metadata.ControllerGeneration = ctrl.Generation()
	if err := st.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	h := &harness{svc: svc, st: st, conv: source.fakeConversation, ctrl: ctrl}
	completeTurn(t, h, "A", "provider-turn-1")
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool { return len(s.Messages) == 2 })
	anchor := completeTurn(t, h, "B", "provider-turn-2")
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool { return len(s.Messages) == 4 })
	completeTurn(t, h, "C", "provider-turn-3")
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool { return len(s.Messages) == 6 })
	for edit := 1; edit <= 3; edit++ {
		before, err := svc.Controller(testSession)
		if err != nil {
			t.Fatalf("controller before edit %d: %v", edit, err)
		}
		result, err := svc.EditMessage(ctx, testSession, anchor, ports.ChatUserMessage{
			Text: "B", ClientMessageID: fmt.Sprintf("unchanged-edit-%d", edit), Origin: domain.MessageOriginHuman,
		})
		if err != nil {
			t.Fatalf("edit %d failed; uncertain=%v controllerState=%v driverResumes=%d: %v", edit,
				errors.Is(err, chatsvc.ErrEditDeliveryUncertain), before.State(), resumes, err)
		}
		t.Logf("edit %d succeeded; branch=%s provider=%s", edit, result.ActiveBranchID, currentProvider.ProviderConversationID())
		providerTurn := fmt.Sprintf("provider-turn-%d", edit*100+1)
		currentProvider.emit(
			ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: providerTurn},
			ports.ChatEvent{Kind: ports.ChatEventMessageCompleted, ProviderTurnID: providerTurn, ProviderItemID: "reply-" + providerTurn, Text: "reply to B"},
			ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: providerTurn, TurnState: domain.TurnStateCompleted},
		)
		snapshot := h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
			for _, turn := range s.Turns {
				if turn.ID == result.Turn.ID && turn.State.Terminal() {
					return true
				}
			}
			return false
		})
		requireMessageTexts(t, snapshot.Messages, []string{"A", "reply to A", "B", "reply to B"})
		anchor = result.Turn.ID
	}
	// Normal follow-up sends must still work after editing the history repeatedly.
	h.conv = currentProvider.fakeConversation
	completeTurn(t, h, "D", "provider-turn-302")
	snapshot := h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool { return len(s.Messages) == 6 })
	requireMessageTexts(t, snapshot.Messages, []string{"A", "reply to A", "B", "reply to B", "D", "reply to D"})
}
