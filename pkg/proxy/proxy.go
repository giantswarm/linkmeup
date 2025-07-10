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

type pingResult struct {
	success    bool
	statusCode int
	err        error
	duration   time.Duration
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

	// List of Teleport node names available for this proxy.
	// Only one will be used.
	nodes []string
	// The node actually used for the SSH tunnel
	nodeActive string
	// SSH tunnel Teleport process
	process *os.Process
	// Healthy determines if the proxy is healthy
	healthy bool
	// Last ping result
	lastPingResult *pingResult

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

	nodes, err := getNodes(selector)
	if err != nil {
		logger.Error("Failed to get nodes for installation", slog.String("selector", selector), slog.String("name", name), slog.String("domain", domain), slog.String("error", err.Error()))
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

		nodes:  nodes,
		logger: logger,
		pinger: pinger,
	}

	_ = p.selectNode()
	_ = p.Start()

	return p, nil
}

// Selects the node to use for the SSH tunnel.
// If a node was previously selected, a different one will be chosen if possible.
func (p *Proxy) selectNode() string {
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
	if len(p.nodes) == 0 {
		return fmt.Errorf("failed to start proxy for %s: no nodes available", p.Name)
	}

	// Pick a random node
	node := p.nodes[rand.IntN(len(p.nodes))] //nolint:gosec

	p.logger.Info("Starting proxy", slog.String("name", p.Name), slog.String("domain", p.Domain), slog.String("node", node), slog.Int("port", p.Port))
	host := fmt.Sprintf("root@node=%s,ins=%s", node, p.Name)
	cmd := exec.Command("tsh", "ssh", "--no-remote-exec", "--dynamic-forward", fmt.Sprintf("%d", p.Port), host) //nolint:gosec

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start proxy for %s: %v", p.Name, err)
	}

	p.process = cmd.Process
	p.nodeActive = node

	return nil
}

func (p *Proxy) PingConstantly() {
	p.pingerMu.Lock()
	defer p.pingerMu.Unlock()
	ctx := context.Background()
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// TODO: Handle case where no nodes are available
				if len(p.nodes) > 0 {
					success := p.Ping(ctx)
					if !success {
						p.logger.Debug("Restarting proxy with different node", slog.String("name", p.Name))
						err := p.Stop()
						if err != nil {
							p.logger.Error("Failed to stop proxy", slog.String("name", p.Name), slog.String("error", err.Error()))
						}
						p.selectNode()
						err = p.Start()
						if err != nil {
							p.logger.Error("Failed to restart proxy", slog.String("name", p.Name), slog.String("error", err.Error()))
						}
					}
				}
			case <-ctx.Done():
				p.pingerMu.Unlock()
				return
			}
		}
	}()
}

func (p *Proxy) Stop() error {
	if p.process == nil {
		return nil // Nothing to stop
	}

	p.logger.Debug("Killing proxy process", slog.String("name", p.Name), slog.Int("pid", p.process.Pid))

	err := p.process.Kill()
	if err != nil {
		return fmt.Errorf("failed to stop proxy for %s: %v", p.Name, err)
	}

	p.process = nil
	p.healthy = false

	return nil
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
	if len(p.nodes) == 0 {
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
	} else {
		result.success = false
	}

	if result.success {
		if !p.healthy || p.lastPingResult == nil {
			p.logger.Info("Proxy changed to healthy", slog.String("name", p.Name), slog.String("domain", p.Domain))
		}
		p.healthy = true
		p.logger.Debug("Ping succeeded", slog.String("name", p.Name), slog.Duration("duration", result.duration))
	} else {
		if p.healthy || p.lastPingResult == nil {
			p.logger.Warn("Proxy changed to unhealthy", slog.String("name", p.Name), slog.String("domain", p.Domain))
		}
		p.healthy = false
		p.logger.Debug("Ping failed", slog.String("name", p.Name), slog.String("domain", p.Domain), slog.String("node", p.nodeActive), slog.Int("status_code", result.statusCode), slog.Duration("duration", result.duration), slog.String("error", fmt.Sprintf("%v", result.err)))
	}

	p.lastPingResult = result

	return result.success
}

// hasScheme checks if the URL has a scheme (http:// or https://)
func hasScheme(url string) bool {
	return len(url) > 7 && (url[:7] == "http://" || url[:8] == "https://")
}
