//go:build windows

package fleetprocess

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/processalive"
)

func TestFleetProcessHelper(t *testing.T) {
	switch os.Getenv("AO_FLEET_JOB_HELPER") {
	case "descendant":
		time.Sleep(time.Hour)
		os.Exit(0)
	case "parent":
		scan := bufio.NewScanner(os.Stdin)
		if !scan.Scan() {
			os.Exit(2)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestFleetProcessHelper$")
		cmd.Env = append(os.Environ(), "AO_FLEET_JOB_HELPER=descendant")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		fmt.Println(cmd.Process.Pid)
		_ = cmd.Process.Release()
		scan.Scan()
		os.Exit(0)
	}
}

func TestFleetJobReapsDescendantAfterParentExits(t *testing.T) {
	t.Setenv("AO_FLEET_HOME", t.TempDir())
	cmd := exec.Command(os.Args[0], "-test.run=^TestFleetProcessHelper$")
	cmd.Env = append(os.Environ(), "AO_FLEET_JOB_HELPER=parent")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	release, err := Contain(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fmt.Fprintln(input, "spawn")
	scan := bufio.NewScanner(output)
	if !scan.Scan() {
		t.Fatal("missing descendant PID")
	}
	pid, err := strconv.Atoi(scan.Text())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(input, "exit")
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !processalive.Alive(pid) {
		t.Fatal("descendant exited before job cleanup was exercised")
	}
	release()
	deadline := time.Now().Add(5 * time.Second)
	for processalive.Alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d survived job cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
