//go:build windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/persistenthost"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/conpty/ptyregistry"
	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// RunFleet serializes the daemon and the post-daemon shutdown phase. The OS
// releases this lock even on a crash. No database or workspace is changed by
// the stop-only phase, and no reconciliation can restart a host during it.
func RunFleet(ctx context.Context, stopBackground bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	root := os.Getenv("AO_FLEET_HOME")
	if root == "" || !filepath.IsAbs(root) ||
		!strings.EqualFold(filepath.Clean(cfg.DataDir), filepath.Join(root, "data")) ||
		!strings.EqualFold(filepath.Clean(cfg.RunFilePath), filepath.Join(root, "running.json")) {
		return errors.New("Fleet requires its isolated data and run-file paths")
	}
	release, err := lockFleetDaemon(root)
	if err != nil {
		return err
	}
	defer release()
	if !stopBackground {
		return Run()
	}
	// Also refuse an older daemon which predates the shared lock. An inaccessible
	// or still-exiting recorded PID is not evidence that it is safe to proceed.
	if live, err := runfile.CheckStale(cfg.RunFilePath); err != nil {
		return err
	} else if live != nil {
		return fmt.Errorf("Fleet daemon (pid %d) must exit before stopping background hosts", live.PID)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	rt := conpty.New(conpty.Options{RunFilePath: cfg.RunFilePath})
	// Both inventories are instance scoped and include orphaned hosts with no
	// surviving session row. Continue the other inventory if one fails.
	var chatErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); chatErr = persistenthost.ShutdownAll(ctx, cfg.DataDir) }()
	entries, ptyErr := ptyregistry.List(ctx)
	for _, entry := range entries {
		if err := rt.Destroy(ctx, ports.RuntimeHandle{ID: entry.SessionID}); err != nil {
			ptyErr = errors.Join(ptyErr, fmt.Errorf("stop terminal %s: %w", entry.SessionID, err))
		}
	}
	wg.Wait()
	return errors.Join(chatErr, ptyErr)
}

func lockFleetDaemon(root string) (func(), error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, "fleet-daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlap windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlap); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("Fleet daemon or background shutdown is still running: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
