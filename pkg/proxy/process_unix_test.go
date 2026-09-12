//go:build unix

package proxy

import (
	"bufio"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Killing the tunnel must take its children with it, or they stay behind as
// orphans for as long as linkmeup runs.
func TestKillProcessTreeKillsChildren(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & echo $!; wait")
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to pipe stdout: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() { _ = cmd.Wait() }()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read the child PID: %v", err)
	}

	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("failed to parse the child PID %q: %v", line, err)
	}

	if err := killProcessTree(cmd.Process); err != nil {
		t.Fatalf("killProcessTree() returned an error: %v", err)
	}

	// The child is reparented once its shell dies, so it takes a moment to be
	// reaped and disappear.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if errors.Is(syscall.Kill(child, 0), syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d still exists after killProcessTree()", child)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
