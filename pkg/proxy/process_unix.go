//go:build unix

package proxy

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup makes the command the leader of its own process group, so
// that everything it spawns can be signalled in one go.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills the process and everything it spawned. Killing only
// the tunnel process would leave its children behind as orphans.
func killProcessTree(p *os.Process) error {
	// Signal the leader through its os.Process handle, which refuses to touch
	// a process that has already been reaped. Signalling the raw PID would
	// give that up, and the PID may by then belong to something else.
	if err := p.Kill(); err != nil {
		return err
	}

	// The leader is dead but not yet reaped, so the PID is still ours and the
	// group ID with it. Take the remaining members along.
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	return nil
}
