package pinger

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// Pinger is an HTTP client that can ping hosts.
type Pinger struct {
	client *http.Client
}

// PingResult represents the result of a ping operation.
type PingResult struct {
	Success      bool
	ResponseCode int
	Error        error
	Duration     time.Duration
}

// Config holds configuration options for the Pinger
type Config struct {
	Timeout   time.Duration
	ProxyHost string
	ProxyPort int
}

// New creates a new Pinger with configuration for a given installation/proxy.
func New(config Config) (*Pinger, error) {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second // Default timeout
	}
	if config.ProxyHost == "" {
		config.ProxyHost = "127.0.0.1"
	}

	client := &http.Client{
		Timeout: config.Timeout,
	}

	// Create a dialer that uses the SOCKS5 proxy
	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", config.ProxyHost, config.ProxyPort), nil, proxy.Direct)
	if err != nil {
		return nil, err
	}

	// Create a transport that uses the proxy dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
	}

	client.Transport = transport

	return &Pinger{
		client: client,
	}, nil
}

// Ping performs a GET request to the root URL of the provided host.
// It returns information about the success, response code, any errors, and the duration.
func (p *Pinger) Ping(ctx context.Context, url string) PingResult {
	result := PingResult{}

	// Ensure the host has a scheme
	if !hasScheme(url) {
		url = "https://" + url
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		return result
	}

	// Execute the request with timing
	startTime := time.Now()
	resp, err := p.client.Do(req)
	result.Duration = time.Since(startTime)

	if err != nil {
		result.Error = fmt.Errorf("request failed: %w", err)
		return result
	}
	defer resp.Body.Close()

	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 500
	result.ResponseCode = resp.StatusCode

	return result
}

// hasScheme checks if the URL has a scheme (http:// or https://)
func hasScheme(url string) bool {
	return len(url) > 7 && (url[:7] == "http://" || url[:8] == "https://")
}
