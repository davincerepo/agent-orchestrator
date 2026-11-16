package persistenthost

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/processalive"
)

func TestShutdownAllStopsDetachedHostsOnlyInSelectedInstance(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	start := func(data, id string) (int, int) {
		cfg := Config{SessionID: id, DataDir: data, Workdir: t.TempDir(),
			Env:  append(os.Environ(), "AO_CHAT_HOST_PROVIDER_HELPER=1"),
			Argv: []string{os.Args[0], "-test.run=TestProviderHelper"}}
		tr, err := ConnectOrStart(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = Shutdown(context.Background(), data, id) })
		d, err := readDescriptor(data, id)
		if err != nil {
			t.Fatal(err)
		}
		provider := requestProviderPID(t, tr, 1, "pid")
		_ = tr.Stdin.Close()
		return d.PID, provider
	}
	first, firstProvider := start(root, "worker-one")
	second, secondProvider := start(root, "orphan-worker")
	untouched, untouchedProvider := start(other, "worker-one")
	journal := filepath.Join(root, "chat-hosts", "worker-one", "history.journal")
	if err := os.WriteFile(journal, []byte("keep history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ShutdownAll(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{first, firstProvider, second, secondProvider} {
		if processalive.Alive(pid) {
			t.Fatalf("process %d survived full exit", pid)
		}
	}
	for _, pid := range []int{untouched, untouchedProvider} {
		if !processalive.Alive(pid) {
			t.Fatalf("other instance process %d was stopped", pid)
		}
	}
	if b, err := os.ReadFile(journal); err != nil || string(b) != "keep history" {
		t.Fatalf("history changed: %q, %v", b, err)
	}
	if err := ShutdownAll(context.Background(), root); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestShutdownAllRejectsIncompleteOwnership(t *testing.T) {
	for _, name := range []string{"malformed descriptor", "launch in progress"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir, _ := hostDir(root, "worker")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if name == "malformed descriptor" {
				file, _ := descriptorPath(root, "worker")
				if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				release, err := acquireHostLock(root, "worker")
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			}
			if err := ShutdownAll(context.Background(), root); err == nil {
				t.Fatal("incomplete ownership reported successful shutdown")
			}
		})
	}
}
