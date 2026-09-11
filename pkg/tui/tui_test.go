package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giantswarm/linkmeup/pkg/proxy"
)

const (
	// Wide enough that no value gets wrapped, so assertions can match plain text.
	testWidth = 200
	// Tall enough that the event list is never trimmed.
	testHeight = 100

	keyEnter = "enter"

	testName     = "mycluster"
	testDomain   = "mybase.example.com"
	testNode     = "ip-10-0-1-5"
	testCheckURL = "https://happaapi.mybase.example.com/healthz"
)

// testNow is a fixed instant, so age rendering never depends on wall clock time.
func testNow() time.Time {
	return time.Date(2026, 9, 11, 13, 5, 0, 0, time.UTC)
}

func TestRenderInfoUnhealthy(t *testing.T) {
	now := testNow()
	status := proxy.ProxyStatus{
		Name:           testName,
		Domain:         testDomain,
		Port:           1080,
		Healthy:        false,
		ActiveNode:     testNode,
		NodeCount:      2,
		CheckEndpoint:  testCheckURL,
		Nodes:          []string{testNode, "ip-10-0-1-9"},
		LastCheck:      now.Add(-12 * time.Second),
		LastStatusCode: 503,
		LastDuration:   240 * time.Millisecond,
		LastError:      "request failed: unexpected EOF",
		Restarts:       3,
		PID:            4711,
		Events: []proxy.Event{
			{Time: now.Add(-time.Minute), Message: "tunnel started on node ip-10-0-1-9 (pid 4711)"},
			{Time: now.Add(-12 * time.Second), Message: "check failed on node " + testNode + ": HTTP 503"},
		},
	}

	out := renderInfo(status, testWidth, testHeight, testNow())

	for _, want := range []string{
		testName + " — details",
		testDomain,
		"✗ Unhealthy",
		testCheckURL,
		testNode,
		"2 known",
		"503",
		"request failed: unexpected EOF",
		"12s ago",
		"240ms",
		"4711",
		"Recent events",
		"check failed on node " + testNode + ": HTTP 503",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected detail view to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRenderInfoNoNodes(t *testing.T) {
	status := proxy.ProxyStatus{
		Name:          testName,
		Domain:        testDomain,
		Port:          1080,
		NodeCount:     0,
		CheckEndpoint: testCheckURL,
		NodesError:    "command failed with exit code 1, stderr: access denied",
	}

	out := renderInfo(status, testWidth, testHeight, testNow())

	for _, want := range []string{
		"- No Nodes",
		"never - no nodes available",
		"command failed with exit code 1, stderr: access denied",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected detail view to contain %q, got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "Recent events") {
		t.Errorf("expected no events section without events, got:\n%s", out)
	}
}

func TestRenderInfoHealthy(t *testing.T) {
	status := proxy.ProxyStatus{
		Name:           testName,
		Domain:         testDomain,
		Port:           1080,
		Healthy:        true,
		ActiveNode:     testNode,
		NodeCount:      1,
		CheckEndpoint:  testCheckURL,
		Nodes:          []string{testNode},
		LastCheck:      testNow().Add(-5 * time.Second),
		LastStatusCode: 200,
		LastDuration:   180 * time.Millisecond,
	}

	out := renderInfo(status, testWidth, testHeight, testNow())

	for _, want := range []string{"✓ Healthy", "5s ago", "HTTP 200"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected detail view to contain %q, got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "Error") {
		t.Errorf("expected no error field for a healthy proxy, got:\n%s", out)
	}
}

func TestFormatAge(t *testing.T) {
	now := testNow()

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{"zero", time.Time{}, "never"},
		{"same instant", now, infoJustNow},
		{"sub second", now.Add(-999 * time.Millisecond), infoJustNow},
		{"seconds", now.Add(-12 * time.Second), "12s ago"},
		{"rounded up", now.Add(-12500 * time.Millisecond), "13s ago"},
		{"minutes", now.Add(-90 * time.Second), "1m30s ago"},
	}

	for _, tc := range tests {
		if got := formatAge(tc.when, now); got != tc.want {
			t.Errorf("formatAge(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormatLastCheck(t *testing.T) {
	now := testNow()

	tests := []struct {
		name   string
		status proxy.ProxyStatus
		want   string
	}{
		{"never checked", proxy.ProxyStatus{NodeCount: 1}, "never"},
		{"no nodes", proxy.ProxyStatus{}, "never - no nodes available"},
		{
			"failed",
			proxy.ProxyStatus{NodeCount: 1, LastCheck: now.Add(-12 * time.Second), LastDuration: 10 * time.Second},
			"12s ago · took 10s · HTTP -",
		},
		{
			"succeeded",
			proxy.ProxyStatus{NodeCount: 1, LastCheck: now.Add(-5 * time.Second), LastDuration: 180 * time.Millisecond, LastStatusCode: 200},
			"5s ago · took 180ms · HTTP 200",
		},
	}

	for _, tc := range tests {
		if got := formatLastCheck(tc.status, now); got != tc.want {
			t.Errorf("formatLastCheck(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRenderInfoTrimsEventsToHeight(t *testing.T) {
	now := testNow()

	status := proxy.ProxyStatus{
		Name: testName, Domain: testDomain, Port: 1080,
		ActiveNode: testNode, NodeCount: 1, CheckEndpoint: testCheckURL,
		Nodes: []string{testNode}, LastCheck: now.Add(-12 * time.Second),
	}
	for i := 0; i < 10; i++ {
		status.Events = append(status.Events, proxy.Event{Time: now, Message: fmt.Sprintf("event number %d", i)})
	}

	const height = 24
	out := renderInfo(status, testWidth, height, now)

	if got := lineCount(out); got > height {
		t.Errorf("rendered %d lines into a %d line terminal:\n%s", got, height, out)
	}
	if !strings.Contains(out, "Enter/Esc: Back") {
		t.Errorf("footer was cut off:\n%s", out)
	}
	// Newest events survive, oldest are dropped and accounted for.
	if !strings.Contains(out, "event number 9") {
		t.Errorf("newest event missing:\n%s", out)
	}
	if !strings.Contains(out, "not shown") {
		t.Errorf("expected a note about omitted events:\n%s", out)
	}
}

func TestFormatStatusCode(t *testing.T) {
	if got := formatStatusCode(0); got != "-" {
		t.Errorf("formatStatusCode(0) = %q, want %q", got, "-")
	}
	if got := formatStatusCode(200); got != "200" {
		t.Errorf("formatStatusCode(200) = %q, want %q", got, "200")
	}
}

func press(t *testing.T, m Model, key string) Model {
	t.Helper()

	var msg tea.KeyPressMsg
	switch key {
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case keyEnter:
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}

	updated, _ := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	if got := msg.String(); got != key {
		t.Fatalf("constructed key renders as %q, want %q", got, key)
	}
	return next
}

func TestInfoKeyTogglesDetailView(t *testing.T) {
	// enter is the advertised key, i is kept as an alias.
	for _, key := range []string{keyEnter, "i"} {
		m := Model{proxies: []*proxy.Proxy{{Name: testName}}}

		m = press(t, m, key)
		if !m.showInfo {
			t.Fatalf("expected %q to open the detail view", key)
		}

		m = press(t, m, key)
		if m.showInfo {
			t.Fatalf("expected %q to close the detail view again", key)
		}
	}
}

func TestEscClosesDetailViewBeforeQuitting(t *testing.T) {
	m := Model{proxies: []*proxy.Proxy{{Name: testName}}}

	m = press(t, m, keyEnter)
	m = press(t, m, "esc")
	if m.showInfo {
		t.Fatal("expected esc to close the detail view")
	}
	if m.quitting {
		t.Fatal("expected esc to not quit while the detail view was open")
	}

	m = press(t, m, "esc")
	if !m.quitting {
		t.Fatal("expected esc to quit from the list view")
	}
}

func TestInfoKeyIgnoredWithoutProxies(t *testing.T) {
	m := press(t, Model{}, keyEnter)
	if m.showInfo {
		t.Fatal("expected enter to do nothing without proxies")
	}
}
