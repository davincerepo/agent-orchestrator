package cdc

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProviderEventRetentionStore struct {
	deltas  []providerEventRow
	removed []providerEventRow
	now     time.Time
	err     error
}

type providerEventRow struct {
	id     int64
	method string
	at     time.Time
}

func (s *fakeProviderEventRetentionStore) PruneProviderEventDeltasBefore(_ context.Context, before time.Time, limit int64) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	var removed int64
	kept := s.deltas[:0]
	for _, row := range s.deltas {
		if isDeltaMethod(row.method) && row.at.Before(before) && removed < limit {
			s.removed = append(s.removed, row)
			removed++
			continue
		}
		kept = append(kept, row)
	}
	s.deltas = kept
	return removed, nil
}

func (s *fakeProviderEventRetentionStore) PruneProviderEventsToMaxRows(_ context.Context, maxRows, limit int64) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	overflow := int64(len(s.deltas)) - maxRows
	if overflow <= 0 {
		return 0, nil
	}
	if overflow > limit {
		overflow = limit
	}
	s.removed = append(s.removed, s.deltas[:overflow]...)
	s.deltas = s.deltas[overflow:]
	return overflow, nil
}

func isDeltaMethod(method string) bool {
	return method == "reasoning.delta" || method == "message.delta" || method == "command.output.delta"
}

func TestProviderEventJanitorPrunesAgedDeltasUntilStable(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	store := &fakeProviderEventRetentionStore{now: now, deltas: []providerEventRow{
		{1, "reasoning.delta", now.Add(-49 * time.Hour)},
		{2, "message.delta", now.Add(-49 * time.Hour)},
		{3, "reasoning.delta", now.Add(-1 * time.Hour)},
		{4, "turn.completed", now.Add(-100 * time.Hour)},
	}}
	j := NewProviderEventJanitor(store, ProviderEventRetentionConfig{
		DeltaRetention: 48 * time.Hour, MaxRows: 100, Batch: 10, Clock: func() time.Time { return now },
	})
	if err := j.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deltas) != 2 {
		t.Fatalf("rows left = %+v, want the fresh delta and the old completed event", store.deltas)
	}
	for _, row := range store.deltas {
		if row.method == "message.delta" && row.at.Before(now.Add(-48*time.Hour)) {
			t.Fatalf("aged delta survived: %+v", row)
		}
	}
}

func TestProviderEventJanitorCapsTotalRows(t *testing.T) {
	now := time.Now().UTC()
	rows := make([]providerEventRow, 250)
	for i := range rows {
		rows[i] = providerEventRow{int64(i), "turn.completed", now}
	}
	store := &fakeProviderEventRetentionStore{deltas: rows}
	j := NewProviderEventJanitor(store, ProviderEventRetentionConfig{
		DeltaRetention: time.Hour, MaxRows: 200, Batch: 10, Clock: func() time.Time { return now },
	})
	if err := j.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deltas) != 200 {
		t.Fatalf("rows left = %d, want 200", len(store.deltas))
	}
	if store.deltas[0].id != 50 {
		t.Fatalf("oldest rows were not removed first: first id = %d", store.deltas[0].id)
	}
}

func TestProviderEventJanitorStopsOnError(t *testing.T) {
	store := &fakeProviderEventRetentionStore{err: errors.New("locked")}
	j := NewProviderEventJanitor(store, ProviderEventRetentionConfig{Clock: func() time.Time { return time.Now() }})
	if err := j.RunOnce(context.Background()); err == nil {
		t.Fatal("retention error was swallowed")
	}
}
