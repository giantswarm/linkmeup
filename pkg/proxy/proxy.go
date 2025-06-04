// Package proxies configures some proxies that are actually SSH tunnels
// but used as SOCK5 proxies. Each proxy is used for one domain only
// and should have a unique port.
package proxy

import (
	"fmt"
	"log/slog"
	"os/exec"
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
	port := startPort
	startPort++ // Increment the port for the next proxy

	logger.Info("Starting proxy", slog.String("name", name), slog.String("domain", domain), slog.Int("port", port))
	host := fmt.Sprintf("root@role=control-plane,mc=%s", name)
	err := exec.Command(
		"tsh",
		"ssh",
		"--no-remote-exec",
		"--dynamic-forward",
		fmt.Sprintf("%d", port),
		host).Start() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to start proxy for %s: %v", name, err)
	}

	return &Proxy{
		Name:   name,
		Port:   port,
		Domain: domain,
	}, nil
}
