package persistenthost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/processalive"
)

// ShutdownAll stops only hosts registered under dataDir. The caller must exclude
// concurrent launches (Fleet holds its daemon lock). Unknown ownership is an
// error, never permission to kill an arbitrary PID or discard the descriptor.
func ShutdownAll(ctx context.Context, dataDir string) error {
	entries, err := os.ReadDir(filepath.Join(dataDir, "chat-hosts"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	slots := make(chan struct{}, 8)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			if err := shutdownAndWait(ctx, dataDir, id); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("stop chat host %s: %w", id, err))
				mu.Unlock()
			}
		}(entry.Name())
	}
	wg.Wait()
	return errors.Join(errs...)
}

func shutdownAndWait(ctx context.Context, dataDir, id string) error {
	d, err := readDescriptor(dataDir, id)
	if errors.Is(err, os.ErrNotExist) {
		// A directory also stores historical journals after a successful stop.
		// A lock without a descriptor, however, could be an incomplete launch.
		release, lockErr := acquireHostLock(dataDir, id)
		if lockErr != nil {
			return lockErr
		}
		release()
		return nil
	}
	if err != nil {
		return err
	}
	if err := Shutdown(ctx, dataDir, id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for processalive.Alive(d.PID) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("chat host process %d did not exit: %w", d.PID, ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}
