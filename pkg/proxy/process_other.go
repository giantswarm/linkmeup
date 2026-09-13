//go:build !unix

package proxy

import (
	"os"
	"os/exec"
)

// setProcessGroup does nothing outside Unix, where process groups do not exist
// in the POSIX sense.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessTree kills the tunnel process only. Its children are not covered.
func killProcessTree(p *os.Process) error {
	return p.Kill()
}
