package cdc

import (
	"context"
	"log/slog"
	"time"
)

const (
	// DefaultProviderEventDeltaRetention keeps streamed deltas for a debugging
	// window. Deltas are the bulk of the archive and have no runtime reader:
	// the projector folds their text into durable rows as it processes them,
	// so old rows only answer "what exactly streamed" during an investigation.
	DefaultProviderEventDeltaRetention = 48 * time.Hour
	// DefaultProviderEventMaxRows bounds the whole archive. Non-delta events
	// grow slowly, but a session left running for weeks without the delta rule
	// firing still needs a hard cap.
	DefaultProviderEventMaxRows int64 = 200_000
	// DefaultProviderEventRetentionInterval runs hourly: deletes are cheap
	// when each run only removes what accumulated since the last one.
	DefaultProviderEventRetentionInterval = time.Hour
)

const (
	DefaultProviderEventRetentionBatch       int64 = 10_000
	defaultProviderEventRetentionBatchesPerRun int  = 8
)

// ProviderEventRetentionStore is the storage surface the provider-event
// janitor needs. Small on purpose so tests run without a real database.
type ProviderEventRetentionStore interface {
	PruneProviderEventDeltasBefore(context.Context, time.Time, int64) (int64, error)
	PruneProviderEventsToMaxRows(context.Context, int64, int64) (int64, error)
}

// ProviderEventRetentionConfig customizes the archive janitor. Zero values
// use the safe production defaults.
type ProviderEventRetentionConfig struct {
	DeltaRetention time.Duration
	MaxRows        int64
	Interval       time.Duration
	Batch          int64
	Clock          func() time.Time
	Logger         *slog.Logger
}

// ProviderEventJanitor bounds the raw provider-event archive by delta age and
// total row count. Official #6806 batches delta writes but still archives one
// row per delta on the persistent-host path, so without this the table grows
// without bound (11M rows over two months on the Fleet install that prompted
// this branch).
type ProviderEventJanitor struct {
	store      ProviderEventRetentionStore
	deltaAge   time.Duration
	maxRows    int64
	interval   time.Duration
	batch      int64
	clock      func() time.Time
	logger     *slog.Logger
	maxBatches int
}

// NewProviderEventJanitor constructs the archive cleanup worker.
func NewProviderEventJanitor(store ProviderEventRetentionStore, cfg ProviderEventRetentionConfig) *ProviderEventJanitor {
	j := &ProviderEventJanitor{
		store:      store,
		deltaAge:   cfg.DeltaRetention,
		maxRows:    cfg.MaxRows,
		interval:   cfg.Interval,
		batch:      cfg.Batch,
		clock:      cfg.Clock,
		logger:     cfg.Logger,
		maxBatches: defaultProviderEventRetentionBatchesPerRun,
	}
	if j.deltaAge <= 0 {
		j.deltaAge = DefaultProviderEventDeltaRetention
	}
	if j.maxRows <= 0 {
		j.maxRows = DefaultProviderEventMaxRows
	}
	if j.interval <= 0 {
		j.interval = DefaultProviderEventRetentionInterval
	}
	if j.batch <= 0 {
		j.batch = DefaultProviderEventRetentionBatch
	}
	if j.clock == nil {
		j.clock = time.Now
	}
	if j.logger == nil {
		j.logger = slog.Default()
	}
	return j
}

// Start runs cleanup immediately and then at the configured interval. The
// returned channel closes after cancellation, safe to wait on before closing
// the store.
func (j *ProviderEventJanitor) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		j.run(ctx)
		t := time.NewTicker(j.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				j.run(ctx)
			}
		}
	}()
	return done
}

// RunOnce performs bounded cleanup; exported for tests and callers that want
// to trigger maintenance during startup.
func (j *ProviderEventJanitor) RunOnce(ctx context.Context) error {
	before := j.clock().UTC().Add(-j.deltaAge)
	var total int64
	for i := 0; i < j.maxBatches; i++ {
		removedDeltas, err := j.store.PruneProviderEventDeltasBefore(ctx, before, j.batch)
		if err != nil {
			return err
		}
		removedBySize, err := j.store.PruneProviderEventsToMaxRows(ctx, j.maxRows, j.batch)
		if err != nil {
			return err
		}
		total += removedDeltas + removedBySize
		if removedDeltas == 0 && removedBySize == 0 {
			break
		}
	}
	if total > 0 {
		j.logger.Info("provider event archive pruned rows", "rows", total)
	}
	return nil
}

func (j *ProviderEventJanitor) run(ctx context.Context) {
	if err := j.RunOnce(ctx); err != nil && ctx.Err() == nil {
		j.logger.Warn("provider event retention failed", "err", err)
	}
}
