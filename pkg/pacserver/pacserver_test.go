// Package pacserver implements a simple PAC (Proxy Auto-Configuration) server.
package pacserver

import (
	"testing"

	"github.com/giantswarm/linkmeup/pkg/proxy"
)

func Test_renderPacFile(t *testing.T) {
	tests := []struct {
		name    string
		proxies []*proxy.Proxy
		want    string
	}{
		{
			name: "single proxy",
			proxies: []*proxy.Proxy{
				{Name: "test-installation", Port: 1080, Domain: "example.com"},
			},
			want: "function FindProxyForURL(url, host) {\n  if (dnsDomainIs(host, 'example.com')) { return 'SOCKS5 localhost:1080'; }\n  return 'DIRECT';\n}\n",
		},
		{
			name:    "empty proxies list",
			proxies: []*proxy.Proxy{},
			want:    "function FindProxyForURL(url, host) {\n  return 'DIRECT';\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderPacFile(tt.proxies); got != tt.want {
				t.Errorf("renderPacFile() = %v, want %v", got, tt.want)
			}
		})
	}
}
