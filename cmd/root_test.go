//go:build unix

package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHeadlessMode(t *testing.T) {
	for _, tc := range []struct{ flag, terminal, want bool }{
		{false, true, false}, {true, true, true}, {false, false, true}, {true, false, true},
	} {
		t.Run(fmt.Sprintf("flag=%v/terminal=%v", tc.flag, tc.terminal), func(t *testing.T) {
			require.Equal(t, tc.want, headlessMode(tc.flag, tc.terminal))
		})
	}
}

// Run the CLI in its own process so real signals and non-terminal stdin are tested.
func TestHeadlessProcess(t *testing.T) {
	if os.Getenv("LINKMEUP_TEST_PROCESS") != "1" {
		return
	}
	rootCmd.SetArgs(strings.Split(os.Getenv("LINKMEUP_TEST_ARGS"), "\n"))
	if err := Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestHeadlessLifecycle(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%v", explicit), func(t *testing.T) {
			process, output, pidFile := startHeadlessProcess(t, explicit)
			require.Eventually(t, func() bool {
				response, err := (&http.Client{Timeout: 200 * time.Millisecond}).Get("http://localhost:9999/proxy.pac")
				if err != nil {
					return false
				}
				defer func() { _ = response.Body.Close() }()
				body, err := io.ReadAll(response.Body)
				return err == nil && response.StatusCode == http.StatusOK && strings.Contains(string(body), "SOCKS5 localhost:1080")
			}, 5*time.Second, 20*time.Millisecond)
			require.Eventually(t, func() bool { _, err := os.Stat(pidFile); return err == nil }, time.Second, 10*time.Millisecond)
			pidBytes, err := os.ReadFile(pidFile) //nolint:gosec // Path belongs to this test's temporary directory.
			require.NoError(t, err)
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
			require.NoError(t, err)
			require.NoError(t, process.Process.Signal(syscall.SIGTERM))
			require.NoError(t, process.Wait())
			require.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH, "tunnel must be reaped")
			logs, err := os.ReadFile(output) //nolint:gosec // Path belongs to this test's temporary directory.
			require.NoError(t, err)
			require.Contains(t, string(logs), "Running without terminal UI")
			require.Contains(t, string(logs), "Stopping linkmeup")
			require.Contains(t, string(logs), "port=1080")
			listener, err := net.Listen("tcp", "127.0.0.1:9999")
			require.NoError(t, err, "PAC port must be released")
			require.NoError(t, listener.Close())
		})
	}
}

func TestHeadlessPACBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:9999")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	process, output, _ := startHeadlessProcess(t, true)
	require.Error(t, process.Wait())
	logs, err := os.ReadFile(output) //nolint:gosec // Path belongs to this test's temporary directory.
	require.NoError(t, err)
	require.Contains(t, string(logs), "failed to start PAC server")
}

func startHeadlessProcess(t *testing.T, explicit bool) (*exec.Cmd, string, string) {
	t.Helper()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "tunnel.pid")
	script := `#!/bin/sh
case "$1" in
 status) printf '{"active":{"profile_url":"https://teleport.example.com"}}';;
 ls) printf 'test-node\n';;
 ssh) echo $$ > "$LINKMEUP_TEST_PID"; exec sleep 120;;
 *) exit 1;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tsh"), []byte(script), 0600))
	require.NoError(t, os.Chmod(filepath.Join(dir, "tsh"), 0700)) //nolint:gosec // The test fixture must be executable.
	cfg := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("installations:\n  - name: test\n    domain: example.com\n"), 0600))
	args := []string{"--config", cfg}
	if explicit {
		args = append(args, "--no-tui")
	}
	output := filepath.Join(dir, "output.log")
	logFile, err := os.Create(output) //nolint:gosec // Path belongs to this test's temporary directory.
	require.NoError(t, err)
	t.Cleanup(func() { _ = logFile.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	process := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHeadlessProcess$") //nolint:gosec // Executes this test binary, never external input.
	process.Env = append(os.Environ(), "LINKMEUP_TEST_PROCESS=1", "LINKMEUP_TEST_ARGS="+strings.Join(args, "\n"), "LINKMEUP_TEST_PID="+pidFile, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	process.Stdout = logFile
	process.Stderr = logFile
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		_ = process.Process.Signal(syscall.SIGTERM)
		if process.ProcessState == nil {
			_ = process.Wait()
		}
	})
	return process, output, pidFile
}
