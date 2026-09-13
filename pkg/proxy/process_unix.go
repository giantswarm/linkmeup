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
//
// The caller must not have reaped the process yet. An unreaped process keeps
// its PID, and with it the group ID, so the signal cannot reach an unrelated
// group. This holds whether the process is still running or already a zombie.
func killProcessTree(p *os.Process) error {
	err := syscall.Kill(-p.Pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}

	return err
}
