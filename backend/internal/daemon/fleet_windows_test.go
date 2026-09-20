//go:build windows

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

func fleetTestEnvironment(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AO_FLEET_HOME", root)
	t.Setenv("AO_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("AO_RUN_FILE", filepath.Join(root, "running.json"))
	return root
}

func TestFleetStopExcludesDaemonAndPreservesFiles(t *testing.T) {
	root := fleetTestEnvironment(t)
	release, err := lockFleetDaemon(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunFleet(context.Background(), true); err == nil {
		t.Fatal("cleanup ran while daemon lock was held")
	}
	release()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(data, "session-history.db")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunFleet(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "keep" {
		t.Fatalf("data changed: %q %v", b, err)
	}
	release, err = lockFleetDaemon(root)
	if err != nil {
		t.Fatalf("cleanup did not release lock: %v", err)
	}
	release()
}

func TestFleetStopRefusesLiveLegacyDaemon(t *testing.T) {
	root := fleetTestEnvironment(t)
	if err := runfile.Write(filepath.Join(root, "running.json"), runfile.Info{PID: os.Getpid(), Port: 13001, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := RunFleet(context.Background(), true); err == nil {
		t.Fatal("legacy daemon was still alive")
	}
}

func TestFleetStopRefusesUnscopedDataAndBrokenRegistry(t *testing.T) {
	root := fleetTestEnvironment(t)
	t.Setenv("AO_DATA_DIR", t.TempDir())
	if err := RunFleet(context.Background(), true); err == nil {
		t.Fatal("accepted foreign data path")
	}
	t.Setenv("AO_DATA_DIR", filepath.Join(root, "data"))
	if err := os.WriteFile(filepath.Join(root, "windows-pty-hosts.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunFleet(context.Background(), true); err == nil {
		t.Fatal("corrupt registry reported success")
	}
}
