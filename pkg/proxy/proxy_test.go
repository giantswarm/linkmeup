package proxy

import (
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// socksPackage appears in the stack of every goroutine inside the SOCKS5 dial.
const socksPackage = "golang.org/x/net/internal/socks"

// dialGoroutines counts the goroutines currently in the SOCKS5 dial path.
// Counting those instead of every goroutine in the process keeps the result
// independent of the GC workers and of whatever other tests are running.
func dialGoroutines() int {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}

	count := 0
	for _, stack := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(stack, socksPackage) {
			count++
		}
	}

	return count
}

// stalledTunnel listens on a free port, accepts every connection and then stays
// silent. It stands in for a tunnel whose SOCKS5 handshake never completes.
func stalledTunnel(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		var conns []net.Conn
		for {
			conn, err := listener.Accept()
			if err != nil {
				for _, c := range conns {
					_ = c.Close()
				}
				return
			}
			conns = append(conns, conn)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})

	return listener.Addr().(*net.TCPAddr).Port
}

// A stalled handshake must fail within the dial timeout instead of blocking
// forever, and must not leave the dial goroutine behind.
func TestPingerDialTimesOut(t *testing.T) {
	port := stalledTunnel(t)

	const timeout = 200 * time.Millisecond

	client, err := newPinger(port, timeout)
	if err != nil {
		t.Fatalf("newPinger() returned an error: %v", err)
	}

	request := func() time.Duration {
		t.Helper()

		req, err := http.NewRequest(http.MethodGet, "https://example.com/healthz", nil)
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}

		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("expected the stalled handshake to fail, got a response")
		}

		return elapsed
	}

	if elapsed := request(); elapsed > time.Second {
		t.Errorf("dial took %s, want it bounded by the %s dial timeout", elapsed, timeout)
	}

	const attempts = 5
	for range attempts {
		request()
	}

	deadline := time.Now().Add(5 * time.Second)
	for dialGoroutines() > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if leaked := dialGoroutines(); leaked > 0 {
		t.Errorf("%d goroutine(s) still in the dial path after %d stalled dials, want none", leaked, attempts)
	}
}

// Close must be safe on a proxy that never started a check loop.
func TestCloseWithoutPingLoop(t *testing.T) {
	p := &Proxy{Name: "test", Domain: "example.com"}

	if err := p.Close(); err != nil {
		t.Errorf("Close() returned an error: %v", err)
	}
}
