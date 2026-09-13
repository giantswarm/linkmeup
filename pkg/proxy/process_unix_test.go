//go:build unix

package proxy

import (
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startGroupWithChild starts a process group whose leader spawns a child that
// outlives it, and returns the command and the child's PID. When leaderExits is
// set, the leader is gone by the time this returns, standing in for a tunnel
// that died on its own. The command is left unreaped, as Start leaves it.
func startGroupWithChild(t *testing.T, leaderExits bool) (*exec.Cmd, int) {
	t.Helper()

	// The child's output goes to /dev/null so that EOF on the pipe below means
	// the leader itself exited, rather than merely stopping to write.
	script := "sleep 30 >/dev/null 2>&1 & echo $!; wait"
	if leaderExits {
		script = "sleep 30 >/dev/null 2>&1 & echo $!; exit 0"
	}

	cmd := exec.Command("sh", "-c", script)
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to pipe stdout: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	var out []byte
	if leaderExits {
		// Reading to EOF waits for the leader to exit without reaping it.
		out, err = io.ReadAll(stdout)
	} else {
		buf := make([]byte, 32)
		var n int
		n, err = stdout.Read(buf)
		out = buf[:n]
	}
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("failed to read the child PID: %v", err)
	}

	child, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("failed to parse the child PID %q: %v", out, err)
	}

	return cmd, child
}

// assertReaped fails unless the process disappears within a few seconds. An
// orphan is reparented before it is reaped, so this takes a moment.
func assertReaped(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d still exists after killProcessTree()", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Killing a running tunnel must take its children with it, or they stay behind
// as orphans for as long as linkmeup runs.
func TestKillProcessTreeKillsChildren(t *testing.T) {
	cmd, child := startGroupWithChild(t, false)

	if err := killProcessTree(cmd.Process); err != nil {
		t.Fatalf("killProcessTree() returned an error: %v", err)
	}
	_ = cmd.Wait()

	assertReaped(t, child)
}

// A tunnel that exits on its own is the case the process group handling exists
// for. Nothing reaps it before Stop runs, so it still owns its PID and its
// children must still be killed.
func TestKillProcessTreeKillsChildrenOfExitedLeader(t *testing.T) {
	cmd, child := startGroupWithChild(t, true)

	if err := killProcessTree(cmd.Process); err != nil {
		t.Fatalf("killProcessTree() returned an error: %v", err)
	}
	_ = cmd.Wait()

	assertReaped(t, child)
}
