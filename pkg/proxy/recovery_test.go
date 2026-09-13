package proxy

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// stubNodes makes the node lookup return a fixed answer for the duration of a test.
func stubNodes(t *testing.T, nodes []string, err error) {
	t.Helper()

	previous := lookupNodes
	lookupNodes = func(string) ([]string, error) { return nodes, err }
	t.Cleanup(func() { lookupNodes = previous })
}

func testProxy(nodes ...string) *Proxy {
	return &Proxy{
		Name:     "glean",
		Domain:   "glean.example.com",
		selector: "ins=glean,cluster=glean,role=control-plane",
		nodes:    nodes,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// hasEvent reports whether any recorded event contains substr.
func hasEvent(p *Proxy, substr string) bool {
	for _, event := range p.Status().Events {
		if strings.Contains(event.Message, substr) {
			return true
		}
	}

	return false
}

// Restart attempts must spread out, or a permanently broken installation keeps
// spawning tsh processes for as long as linkmeup runs.
func TestRestartBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: pingInterval},
		{attempt: 1, want: 30 * time.Second},
		{attempt: 2, want: time.Minute},
		{attempt: 3, want: 2 * time.Minute},
		{attempt: 4, want: 4 * time.Minute},
		{attempt: 5, want: 8 * time.Minute},
		{attempt: 6, want: maxRestartBackoff},
		{attempt: 50, want: maxRestartBackoff},
	}

	for _, tt := range tests {
		if got := restartBackoff(tt.attempt); got != tt.want {
			t.Errorf("restartBackoff(%d) = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}

// maybeRestart must do nothing while a previous attempt's pause is still running.
func TestMaybeRestartWaitsForBackoff(t *testing.T) {
	p := testProxy("glean-bkznm")
	p.nextRestart = time.Now().Add(time.Hour)

	p.maybeRestart()

	if p.failedRestarts != 0 {
		t.Errorf("failedRestarts = %d, want 0: the attempt should have been skipped", p.failedRestarts)
	}
}

// The incident this came from: an installation's control plane is replaced, so
// every cached node name points at nothing.
func TestRefreshNodesPicksUpReplacedNodes(t *testing.T) {
	p := testProxy("glean-sq67r")
	p.nodeActive = "glean-sq67r"

	replacements := []string{"glean-bkznm", "glean-nc655", "glean-r8srn"}
	stubNodes(t, replacements, nil)

	p.refreshNodes()

	status := p.Status()
	if len(status.Nodes) != len(replacements) {
		t.Errorf("got %d nodes, want %d", len(status.Nodes), len(replacements))
	}
	if p.nodeActive != "" {
		t.Errorf("nodeActive = %q, want it cleared so selectNode picks a live node", p.nodeActive)
	}
	if !hasEvent(p, "no longer exists") {
		t.Error("expected an event naming the node that disappeared")
	}
	if !hasEvent(p, "node list changed") {
		t.Error("expected an event about the changed node list")
	}
}

// A node that is still present must keep being used.
func TestRefreshNodesKeepsLiveActiveNode(t *testing.T) {
	p := testProxy("elver-2zcm5")
	p.nodeActive = "elver-2zcm5"

	stubNodes(t, []string{"elver-2zcm5", "elver-h2t2d"}, nil)

	p.refreshNodes()

	if p.nodeActive != "elver-2zcm5" {
		t.Errorf("nodeActive = %q, want it kept", p.nodeActive)
	}
	if hasEvent(p, "no longer exists") {
		t.Error("did not expect an event about a disappeared node")
	}
}

// Reordering alone is not a change worth reporting, since selectNode shuffles
// the list in place.
func TestRefreshNodesIgnoresReordering(t *testing.T) {
	p := testProxy("a", "b", "c")

	stubNodes(t, []string{"c", "a", "b"}, nil)

	p.refreshNodes()

	if hasEvent(p, "node list changed") {
		t.Error("a reordered but otherwise identical list should not count as a change")
	}
}

// A failed lookup must not throw the known nodes away: they may still work.
func TestRefreshNodesKeepsNodesWhenLookupFails(t *testing.T) {
	p := testProxy("glean-bkznm")

	stubNodes(t, nil, io.ErrUnexpectedEOF)

	p.refreshNodes()

	if len(p.Status().Nodes) != 1 {
		t.Errorf("got %d nodes, want the known one kept", len(p.Status().Nodes))
	}
	if p.Status().NodesError == "" {
		t.Error("expected the lookup error to be reported")
	}
}

// A successful check clears the pause, so a recovered proxy restarts promptly
// if it fails again later.
func TestSuccessfulCheckResetsBackoff(t *testing.T) {
	p := testProxy("glean-bkznm")
	p.failedRestarts = 4
	p.nextRestart = time.Now().Add(time.Hour)

	p.recordPingResult(&pingResult{success: true, statusCode: 200})

	if p.failedRestarts != 0 {
		t.Errorf("failedRestarts = %d, want 0", p.failedRestarts)
	}
	if !p.nextRestart.IsZero() {
		t.Errorf("nextRestart = %v, want it cleared", p.nextRestart)
	}
}
