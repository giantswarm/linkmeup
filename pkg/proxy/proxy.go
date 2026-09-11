// Package proxy configures a SOCKS5 proxy that is actually an SSH tunnel.
// A proxy is used for one domain only and should have a unique port. There
// is self-checking logic to ensure that the proxy is running and reachable.
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	rand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

var (
	startPort = 1080

	pingTimeout  = 10 * time.Second
	pingInterval = 30 * time.Second

	proxyHost = "localhost"
)

// Number of events kept per proxy.
const maxEvents = 20

type pingResult struct {
	success    bool
	statusCode int
	err        error
	duration   time.Duration
}

// describe renders the ping outcome as a single human readable line.
func (r *pingResult) describe() string {
	parts := make([]string, 0, 2)
	if r.statusCode > 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", r.statusCode))
	}
	if r.err != nil {
		parts = append(parts, r.err.Error())
	}
	if len(parts) == 0 {
		parts = append(parts, "no response")
	}
	return fmt.Sprintf("%s (%s)", strings.Join(parts, ", "), r.duration.Round(time.Millisecond))
}

// Event is a timestamped diagnostic message about a proxy.
type Event struct {
	Time    time.Time
	Message string
}

type Proxy struct {
	// Name of the installation/management cluster this proxy is for.
	Name string
	// Proxy port
	Port int
	// Domain the proxy should be used for.
	Domain string
	// CheckEndpoint is the endpoint to ping for this proxy
	CheckEndpoint string

	// mu guards all mutable fields below. They are written by the ping
	// goroutine and read by the TUI.
	mu sync.RWMutex
	// List of Teleport node names available for this proxy.
	// Only one will be used.
	nodes []string
	// Error returned by the last node lookup, if any
	nodesErr error
	// The node actually used for the SSH tunnel
	nodeActive string
	// SSH tunnel Teleport process
	process *os.Process
	// Healthy determines if the proxy is healthy
	healthy bool
	// Last ping result
	lastPingResult *pingResult
	// Time of the last ping
	lastCheck time.Time
	// Error returned by the last attempt to start the tunnel, if any
	lastStartErr error
	// Number of tunnel restarts after a failed check
	restarts int
	// Recent diagnostic events, oldest first
	events []Event

	// Logger
	logger *slog.Logger
	// Pinger
	pinger *http.Client
	// Ensure there is only one pinger per proxy
	pingerMu sync.Mutex
}

func New(logger *slog.Logger, name string, domain string, checkEndpoint string) (*Proxy, error) {
	if name == "" {
		return nil, fmt.Errorf("name must not be empty")
	}
	if domain == "" {
		return nil, fmt.Errorf("domain must not be empty")
	}
	if checkEndpoint == "" {
		return nil, fmt.Errorf("checkEndpoint must not be empty")
	}

	port := startPort
	startPort++ // Increment the port for the next proxy

	// Selector for command `tsh ls --format=names ins=MC_NAME,cluster=MC_NAME,role=control-plane`
	selector := fmt.Sprintf("ins=%s,cluster=%s,role=control-plane", name, name)

	nodes, nodesErr := getNodes(selector)
	if nodesErr != nil {
		logger.Error("Failed to get nodes for installation", slog.String("selector", selector), slog.String("name", name), slog.String("domain", domain), slog.String("error", nodesErr.Error()))
	}
	if len(nodes) == 0 {
		logger.Error("No nodes found for installation", slog.String("selector", selector), slog.String("name", name), slog.String("domain", domain))
	}

	logger.Debug("Nodes for installation", slog.String("selector", selector), slog.Int("count", len(nodes)), slog.String("name", name), slog.String("nodes", strings.Join(nodes, ", ")))

	pinger, err := newPinger(port)
	if err != nil {
		return nil, fmt.Errorf("failed to create pinger for proxy %s: %v", name, err)
	}

	p := &Proxy{
		Name:          name,
		Port:          port,
		Domain:        domain,
		CheckEndpoint: checkEndpoint,

		nodes:    nodes,
		nodesErr: nodesErr,
		logger:   logger,
		pinger:   pinger,
	}

	switch {
	case nodesErr != nil:
		p.addEvent("node lookup failed: %v", nodesErr)
	case len(nodes) == 0:
		p.addEvent("no nodes found for selector %s", selector)
	default:
		p.addEvent("found %d node(s) for selector %s", len(nodes), selector)
	}

	_ = p.selectNode()
	_ = p.Start()

	return p, nil
}

// addEvent appends a diagnostic event, dropping the oldest one when full.
func (p *Proxy) addEvent(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addEventLocked(format, args...)
}

// addEventLocked is addEvent for callers already holding the write lock.
func (p *Proxy) addEventLocked(format string, args ...any) {
	p.events = append(p.events, Event{Time: time.Now(), Message: fmt.Sprintf(format, args...)})
	if len(p.events) > maxEvents {
		p.events = p.events[len(p.events)-maxEvents:]
	}
}

// Selects the node to use for the SSH tunnel.
// If a node was previously selected, a different one will be chosen if possible.
func (p *Proxy) selectNode() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.nodes) == 0 {
		return ""
	}

	// shuffle nodes
	for i := range p.nodes {
		j := rand.IntN(i + 1) //nolint:gosec
		p.nodes[i], p.nodes[j] = p.nodes[j], p.nodes[i]
	}

	for i := range p.nodes {
		if p.nodeActive != p.nodes[i] {
			p.nodeActive = p.nodes[i]
			p.logger.Debug("Selected new node for proxy", slog.String("name", p.Name), slog.String("domain", p.Domain), slog.String("node", p.nodeActive))
			return p.nodeActive
		}
	}

	p.nodeActive = p.nodes[0] // Fallback to the first node if no other is available
	return p.nodes[0]
}

// Start creates the SSH tunnel and thus starts the proxy.
func (p *Proxy) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.nodes) == 0 {
		p.lastStartErr = fmt.Errorf("failed to start proxy for %s: no nodes available", p.Name)
		return p.lastStartErr
	}

	// Pick a random node
	node := p.nodes[rand.IntN(len(p.nodes))] //nolint:gosec

	p.logger.Info("Starting proxy", slog.String("name", p.Name), slog.String("domain", p.Domain), slog.String("node", node), slog.Int("port", p.Port))
	host := fmt.Sprintf("root@node=%s,ins=%s", node, p.Name)
	cmd := exec.Command("tsh", "ssh", "--no-remote-exec", "--dynamic-forward", fmt.Sprintf("%d", p.Port), host) //nolint:gosec

	err := cmd.Start()
	if err != nil {
		p.lastStartErr = fmt.Errorf("failed to start proxy for %s: %v", p.Name, err)
		p.addEventLocked("failed to start tunnel on node %s: %v", node, err)
		return p.lastStartErr
	}

	p.process = cmd.Process
	p.nodeActive = node
	p.lastStartErr = nil
	p.addEventLocked("tunnel started on node %s (pid %d)", node, cmd.Process.Pid)

	return nil
}

func (p *Proxy) PingConstantly() {
	p.pingerMu.Lock()
	defer p.pingerMu.Unlock()
	ctx := context.Background()
	go func() {
		// Do an initial ping immediately after a short delay for the tunnel to establish
		time.Sleep(2 * time.Second)
		if p.nodeCount() > 0 {
			p.Ping(ctx)
		}

		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// TODO: Handle case where no nodes are available
				if p.nodeCount() > 0 {
					success := p.Ping(ctx)
					if !success {
						p.logger.Debug("Restarting proxy with different node", slog.String("name", p.Name))
						p.addEvent("restarting proxy after failed check")
						p.incRestarts()
						err := p.Stop()
						if err != nil {
							p.logger.Error("Failed to stop proxy", slog.String("name", p.Name), slog.String("error", err.Error()))
							p.addEvent("failed to stop tunnel: %v", err)
						}
						p.selectNode()
						err = p.Start()
						if err != nil {
							p.logger.Error("Failed to restart proxy", slog.String("name", p.Name), slog.String("error", err.Error()))
						}
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (p *Proxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.process == nil {
		return nil // Nothing to stop
	}

	pid := p.process.Pid
	p.logger.Debug("Killing proxy process", slog.String("name", p.Name), slog.Int("pid", pid))

	err := p.process.Kill()
	if err != nil {
		return fmt.Errorf("failed to stop proxy for %s: %v", p.Name, err)
	}

	p.process = nil
	p.healthy = false
	p.addEventLocked("tunnel stopped (pid %d)", pid)

	return nil
}

func (p *Proxy) nodeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.nodes)
}

func (p *Proxy) incRestarts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restarts++
}

// Returns available Teleport nodes for a given selector.
func getNodes(selector string) ([]string, error) {
	cmd := exec.Command("tsh", "ls", "--format=names", selector) //nolint:gosec

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., command not found)
			return nil, fmt.Errorf("failed to execute command: %v", err)
		}
	}

	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())

	// Log the results for debugging
	if exitCode != 0 || stderrStr != "" {
		return nil, fmt.Errorf("command failed with exit code %d, stderr: %s", exitCode, stderrStr)
	}

	if stdoutStr == "" {
		return nil, fmt.Errorf("no nodes found for selector %s", selector)
	}

	nodes := strings.Split(stdoutStr, "\n")
	if len(nodes) == 0 || (len(nodes) == 1 && nodes[0] == "") {
		return nil, fmt.Errorf("no nodes found for selector %s", selector)
	}

	return nodes, nil
}

func newPinger(port int) (*http.Client, error) {
	client := &http.Client{
		Timeout: pingTimeout,
	}

	// Create a dialer that uses the SOCKS5 proxy
	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", proxyHost, port), nil, proxy.Direct)
	if err != nil {
		return nil, err
	}

	// Create a transport that uses the proxy dialer
	client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
	}

	return client, nil
}

// Ping performs a GET request to the root URL of the provided host.
// It returns information about the success, response code, any errors, and the duration.
func (p *Proxy) Ping(ctx context.Context) bool {
	result := &pingResult{}
	if p.nodeCount() == 0 {
		return false
	}

	// Ensure the URL has a scheme
	url := p.CheckEndpoint
	if !hasScheme(url) {
		url = "https://" + p.CheckEndpoint
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		p.logger.Error("Failed to create ping request", slog.String("name", p.Name), slog.String("domain", p.Domain), slog.String("error", err.Error()))
		result.err = fmt.Errorf("failed to create request: %w", err)
		p.recordPingResult(result)
		return false
	}

	// Execute the request with timing
	startTime := time.Now()
	resp, err := p.pinger.Do(req)
	result.duration = time.Since(startTime)
	if err != nil {
		result.err = fmt.Errorf("request failed: %w", err)
	}

	if resp != nil {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		result.statusCode = resp.StatusCode
		result.success = resp.StatusCode >= 200 && resp.StatusCode < 500
	}

	p.recordPingResult(result)

	return result.success
}

// recordPingResult stores the outcome of a ping and records an event for every
// failure and for every recovery.
func (p *Proxy) recordPingResult(result *pingResult) {
	p.mu.Lock()
	defer p.mu.Unlock()

	wasHealthy := p.healthy
	isFirst := p.lastPingResult == nil

	p.healthy = result.success
	p.lastPingResult = result
	p.lastCheck = time.Now()

	if result.success {
		if !wasHealthy || isFirst {
			p.logger.Info("Proxy changed to healthy", slog.String("name", p.Name), slog.String("domain", p.Domain))
			p.addEventLocked("check succeeded: %s", result.describe())
		}
		p.logger.Debug("Ping succeeded", slog.String("name", p.Name), slog.Duration("duration", result.duration))
		return
	}

	if wasHealthy || isFirst {
		p.logger.Warn("Proxy changed to unhealthy", slog.String("name", p.Name), slog.String("domain", p.Domain))
	}
	p.addEventLocked("check failed on node %s: %s", p.nodeActive, result.describe())
	p.logger.Debug("Ping failed", slog.String("name", p.Name), slog.String("domain", p.Domain), slog.String("node", p.nodeActive), slog.Int("status_code", result.statusCode), slog.Duration("duration", result.duration), slog.String("error", fmt.Sprintf("%v", result.err)))
}

// hasScheme checks if the URL has a scheme (http:// or https://)
func hasScheme(url string) bool {
	return len(url) > 7 && (url[:7] == "http://" || url[:8] == "https://")
}

// ProxyStatus represents the current status of a proxy for display purposes.
type ProxyStatus struct {
	Name       string
	Domain     string
	Port       int
	Healthy    bool
	ActiveNode string
	NodeCount  int

	// CheckEndpoint is the URL pinged to determine health.
	CheckEndpoint string
	// Nodes are all Teleport nodes known for this proxy.
	Nodes []string
	// NodesError explains why no nodes are available, if applicable.
	NodesError string
	// LastCheck is when the last ping happened. Zero if never pinged.
	LastCheck time.Time
	// LastStatusCode is the HTTP status of the last ping. Zero if there was no response.
	LastStatusCode int
	// LastDuration is how long the last ping took.
	LastDuration time.Duration
	// LastError is the error of the last ping, if any.
	LastError string
	// LastStartError is the error of the last tunnel start attempt, if any.
	LastStartError string
	// Restarts counts tunnel restarts triggered by failed checks.
	Restarts int
	// PID of the tunnel process. Zero if no tunnel is running.
	PID int
	// Events are recent diagnostic events, oldest first.
	Events []Event
}

// Status returns the current status of the proxy.
func (p *Proxy) Status() ProxyStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := ProxyStatus{
		Name:       p.Name,
		Domain:     p.Domain,
		Port:       p.Port,
		Healthy:    p.healthy,
		ActiveNode: p.nodeActive,
		NodeCount:  len(p.nodes),

		CheckEndpoint: p.CheckEndpoint,
		Nodes:         append([]string(nil), p.nodes...),
		LastCheck:     p.lastCheck,
		Restarts:      p.restarts,
		Events:        append([]Event(nil), p.events...),
	}

	if p.nodesErr != nil {
		status.NodesError = p.nodesErr.Error()
	}
	if p.lastStartErr != nil {
		status.LastStartError = p.lastStartErr.Error()
	}
	if p.lastPingResult != nil {
		status.LastStatusCode = p.lastPingResult.statusCode
		status.LastDuration = p.lastPingResult.duration
		if p.lastPingResult.err != nil {
			status.LastError = p.lastPingResult.err.Error()
		}
	}
	if p.process != nil {
		status.PID = p.process.Pid
	}

	return status
}

// IsHealthy returns whether the proxy is currently healthy.
func (p *Proxy) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthy
}
