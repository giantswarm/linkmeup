//go:build unix

package proxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTSH puts a stand-in tsh on PATH for the duration of a test.
func fakeTSH(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "tsh")

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatalf("failed to write the fake tsh: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// tsh writes warnings to stderr on success. Treating those as failures would
// stop the node list ever being refreshed, which is what the recovery path
// depends on.
func TestGetNodesIgnoresStderrOnSuccess(t *testing.T) {
	fakeTSH(t, "echo 'WARNING: cluster hint' >&2\nprintf 'node-a\\nnode-b\\n'\nexit 0\n")

	nodes, err := getNodes(context.Background(), "ins=glean")
	if err != nil {
		t.Fatalf("getNodes() returned an error for a successful lookup: %v", err)
	}

	want := []string{"node-a", "node-b"}
	if len(nodes) != len(want) || nodes[0] != want[0] || nodes[1] != want[1] {
		t.Errorf("got nodes %v, want %v", nodes, want)
	}
}

// A non-zero exit is still a failure, and the message must carry stderr.
func TestGetNodesReportsFailure(t *testing.T) {
	fakeTSH(t, "echo 'access denied' >&2\nexit 1\n")

	nodes, err := getNodes(context.Background(), "ins=glean")
	if err == nil {
		t.Fatalf("getNodes() returned %v, want an error", nodes)
	}

	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("error %q does not mention stderr", err)
	}
}

// The lookup must abandon a hanging tsh rather than block the check loop, and
// with it Close. tsh can wait on a browser login indefinitely.
func TestGetNodesHonoursContext(t *testing.T) {
	fakeTSH(t, "sleep 60\n")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := getNodes(ctx, "ins=glean")
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("getNodes() returned no error for a cancelled lookup")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("getNodes() kept waiting after its context was cancelled")
	}
}
