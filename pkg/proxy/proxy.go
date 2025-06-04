// Package proxies configures some proxies that are actually SSH tunnels
// but used as SOCK5 proxies. Each proxy is used for one domain only
// and should have a unique port.
package proxy

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

var (
	startPort = 1080
)

type Proxy struct {
	// Name of the installation/management cluster this proxy is for.
	Name string
	// Proxy port
	Port int
	// Domain the proxy should be used for.
	Domain string
}

func New(logger *slog.Logger, name string, domain string) (*Proxy, error) {
	if name == "" {
		return nil, fmt.Errorf("installation name cannot be empty")
	}
	if domain == "" {
		return nil, fmt.Errorf("domain cannot be empty")
	}

	port := startPort
	startPort++ // Increment the port for the next proxy

	selector := fmt.Sprintf("ins=%s,cluster=%s,role=control-plane", name, name)

	// Detect available nodes by executing `tsh ls --format=names ins=MC_NAME,cluster=MC_NAME,role=control-plane`
	cmd := exec.Command("tsh", "ls", "--format=names", selector)
	output, err := cmd.CombinedOutput()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			logger.Debug("Node listing failed",
				slog.String("name", name),
				slog.Int("exit_code", exitErr.ExitCode()),
				slog.String("output", string(output)))
			return nil, fmt.Errorf("nodes could not be listed: %v", err)
		}
		return nil, fmt.Errorf("failed to check installation %s: %v", name, err)
	}
	nodes := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(nodes) == 0 || (len(nodes) == 1 && nodes[0] == "") {
		return nil, fmt.Errorf("no nodes found for installation %s", name)
	}

	logger.Debug("Nodes for installation", slog.Int("count", len(nodes)), slog.String("name", name), slog.String("nodes", strings.Join(nodes, ", ")))

	logger.Info("Starting proxy", slog.String("name", name), slog.String("domain", domain), slog.Int("port", port))
	host := fmt.Sprintf("root@%s", selector)
	//nolint:gosec
	err = exec.Command(
		"tsh",
		"ssh",
		"--no-remote-exec",
		"--dynamic-forward",
		fmt.Sprintf("%d", port),
		host).Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start proxy for %s: %v", name, err)
	}

	return &Proxy{
		Name:   name,
		Port:   port,
		Domain: domain,
	}, nil
}
