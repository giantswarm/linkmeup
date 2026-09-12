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
	err := syscall.Kill(-p.Pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}

	return p.Kill()
}
