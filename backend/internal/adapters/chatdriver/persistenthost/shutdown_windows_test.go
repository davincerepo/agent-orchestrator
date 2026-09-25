//go:build windows

package persistenthost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func holdDescriptor(t *testing.T, path string) func() {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() { _ = windows.CloseHandle(handle) }
}

func TestShutdownAllRetriesBusyDescriptor(t *testing.T) {
	root := t.TempDir()
	const id = "busy-before-shutdown"
	if err := writeDescriptor(root, Descriptor{
		Version: ProtocolVersion, SessionID: id, Address: "127.0.0.1:0", PID: 2147483647, Token: "test",
	}); err != nil {
		t.Fatal(err)
	}
	path, _ := descriptorPath(root, id)
	release := holdDescriptor(t, path)
	if _, err := readDescriptor(root, id); !descriptorBusy(err) {
		release()
		t.Fatalf("exclusive handle did not block reading: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		release()
		close(released)
	}()
	defer func() { <-released }()
	if err := ShutdownAll(context.Background(), root); err != nil {
		t.Fatalf("transient descriptor lock prevented shutdown: %v", err)
	}
}

func TestShutdownBusyDescriptorHonorsDeadline(t *testing.T) {
	root := t.TempDir()
	const id = "busy-until-deadline"
	if err := writeDescriptor(root, Descriptor{
		Version: ProtocolVersion, SessionID: id, Address: "127.0.0.1:0", PID: os.Getpid(), Token: "test",
	}); err != nil {
		t.Fatal(err)
	}
	path, _ := descriptorPath(root, id)
	release := holdDescriptor(t, path)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Shutdown(ctx, root, id); !errors.Is(err, context.DeadlineExceeded) || !descriptorBusy(err) {
		t.Fatalf("shutdown = %v, want deadline and sharing violation", err)
	}
}

func TestShutdownRetriesBusyDescriptorAfterAcknowledgement(t *testing.T) {
	root := t.TempDir()
	const id = "busy-during-shutdown"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := writeDescriptor(root, Descriptor{
		Version: ProtocolVersion, SessionID: id, Address: listener.Addr().String(), PID: os.Getpid(), Token: "test",
	}); err != nil {
		t.Fatal(err)
	}
	path, _ := descriptorPath(root, id)
	result := make(chan error, 1)
	go func() { result <- Shutdown(context.Background(), root, id) }()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var request hello
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		t.Fatal(err)
	}
	if request.Action != "shutdown" || request.Token != "test" {
		t.Fatalf("unexpected request: action=%q", request.Action)
	}
	release := holdDescriptor(t, path)
	if err := json.NewEncoder(conn).Encode(helloResponse{OK: true}); err != nil {
		release()
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	release()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("descriptor removal race prevented shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after descriptor removal")
	}
}

func TestInheritedStderrProviderHelper(t *testing.T) {
	mode := os.Getenv("AO_TEST_INHERITED_STDERR")
	if mode == "" {
		return
	}
	if mode == "holder" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestInheritedStderrProviderHelper$")
	child.Env = append(os.Environ(), "AO_TEST_INHERITED_STDERR=holder")
	child.Stderr = os.Stderr
	configureProviderProcess(child)
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(mode, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		_ = child.Process.Kill()
		os.Exit(3)
	}
	_ = child.Process.Release()
	os.Exit(0)
}

func TestHostExitsWhenDescendantKeepsStderrOpen(t *testing.T) {
	// Exercise a descendant outside Fleet's job, as can happen when a wrapper
	// exits before containment. Its inherited stderr must not strand cmd.Wait.
	t.Setenv("AO_FLEET_HOME", "")
	root := t.TempDir()
	pidFile := filepath.Join(root, "holder.pid")
	cfg := Config{
		SessionID: "inherited-stderr", DataDir: root, Workdir: root,
		Env:  append(os.Environ(), "AO_TEST_INHERITED_STDERR="+pidFile),
		Argv: []string{os.Args[0], "-test.run=^TestInheritedStderrProviderHelper$"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg) }()
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				if process, err := os.FindProcess(pid); err == nil {
					_ = process.Kill()
					_ = process.Release()
				}
			}
		}
	})
	select {
	case err := <-result:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host remained blocked after its provider exited")
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("stderr holder did not start: %v", err)
	}
	for _, name := range []string{"host.json", "host.lock"} {
		if _, err := os.Stat(filepath.Join(root, "chat-hosts", cfg.SessionID, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("host left %s behind: %v", name, err)
		}
	}
}
