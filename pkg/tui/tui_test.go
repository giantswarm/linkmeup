package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/giantswarm/linkmeup/pkg/proxy"
)

const (
	// Wide enough that no value gets wrapped, so assertions can match plain text.
	testWidth = 200

	keyEnter = "enter"

	testName     = "mycluster"
	testDomain   = "mybase.example.com"
	testNode     = "ip-10-0-1-5"
	testCheckURL = "https://happaapi.mybase.example.com/healthz"
)

func TestRenderInfoUnhealthy(t *testing.T) {
	now := time.Now()
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

	out := renderInfo(status, testWidth)

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

	out := renderInfo(status, testWidth)

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
		LastCheck:      time.Now().Add(-5 * time.Second),
		LastStatusCode: 200,
		LastDuration:   180 * time.Millisecond,
	}

	out := renderInfo(status, testWidth)

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
	if got := formatAge(time.Time{}); got != "never" {
		t.Errorf("formatAge(zero) = %q, want %q", got, "never")
	}
	if got := formatAge(time.Now()); got != "just now" {
		t.Errorf("formatAge(now) = %q, want %q", got, "just now")
	}
	if got := formatAge(time.Now().Add(-90 * time.Second)); got != "1m30s ago" {
		t.Errorf("formatAge(-90s) = %q, want %q", got, "1m30s ago")
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
