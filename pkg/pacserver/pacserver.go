// Package pacserver implements a simple PAC (Proxy Auto-Configuration) server.
package pacserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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

// Serve binds the PAC port before returning and serves requests in the background.
func (p *PacServer) Serve() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", p.Port))
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/proxy.pac", func(w http.ResponseWriter, r *http.Request) {
		p.logger.Debug("Serving request to PAC file", slog.String("url", r.URL.String()))
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		_, _ = fmt.Fprint(w, p.Body)
	})
	p.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  5 * time.Second,
	}
	p.logger.Info("Serving proxy auto-configuration (PAC) file", slog.String("url", fmt.Sprintf("http://localhost:%d/proxy.pac", p.Port)))
	go func() {
		if err := p.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.logger.Error("Auto-configuration web server error", slog.String("error", err.Error()))
		}
	}()
	return nil
}

// Shutdown stops accepting PAC requests and waits for active requests to finish.
func (p *PacServer) Shutdown(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	return p.server.Shutdown(ctx)
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
