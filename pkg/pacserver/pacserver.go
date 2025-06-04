// Package pacserver implements a simple PAC (Proxy Auto-Configuration) server.
package pacserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/giantswarm/linkmeup/pkg/proxy"
)

type PacServer struct {
	logger *slog.Logger
	server *http.Server

	Port int
	Body string
}

func New(logger *slog.Logger, proxies []*proxy.Proxy, port int) (*PacServer, error) {
	if proxies == nil {
		return nil, fmt.Errorf("proxies cannot be nil")
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", port)
	}
	if port == 0 {
		port = 9999 // Default port
	}

	return &PacServer{
		logger: logger,
		Body:   renderPacFile(proxies),
		Port:   port,
	}, nil
}

func (p *PacServer) Serve() {
	// Create web server to serve PAC
	path := "/proxy.pac"
	url := fmt.Sprintf("http://localhost:%d%s", p.Port, path)
	p.logger.Info("Serving proxy auto-configuration (PAC) file", slog.String("url", url))

	http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		p.logger.Debug("Serving request to PAC file", slog.String("url", r.URL.String()))
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		_, _ = fmt.Fprint(w, p.Body)
	})

	go func() {
		p.server = &http.Server{
			Addr:         fmt.Sprintf(":%d", p.Port),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  5 * time.Second,
		}
		err := p.server.ListenAndServe()
		if err != nil {
			p.logger.Error("Auto-configuration web server error", slog.String("error", err.Error()))
		}
	}()

	// Set up channel to capture signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	p.logger.Info("Press Ctrl+C to quit.")
	<-sigChan
	p.logger.Info("Shutting down proxies and auto-configuration server")
}

func renderPacFile(proxies []*proxy.Proxy) string {
	// Generate PAC from privateInstallations and port numbers.
	body := "function FindProxyForURL(url, host) {"
	for _, p := range proxies {
		body += fmt.Sprintf("\n  if (dnsDomainIs(host, '%s')) { return 'SOCKS5 localhost:%d'; }", p.Domain, p.Port)
	}
	body += "\n  return 'DIRECT';\n}\n"

	return body
}
