package sandbox

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeTracker wraps a net.Conn and counts how many times Close() is called
// through the wrapper. Direct closes on the underlying connection are not tracked.
type closeTracker struct {
	net.Conn
	mu         sync.Mutex
	closeCount int
}

func (c *closeTracker) Close() error {
	c.mu.Lock()
	c.closeCount++
	c.mu.Unlock()
	return c.Conn.Close()
}

func (c *closeTracker) CloseCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCount
}

func TestNetworkProxy_StartStop(t *testing.T) {
	t.Parallel()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	require.NotNil(t, proxy)

	// Verify addresses are populated
	assert.NotEmpty(t, proxy.HTTPAddr())
	assert.NotEmpty(t, proxy.SOCKSAddr())

	// Verify address formats (TCP on all platforms)
	assert.True(t, strings.HasPrefix(proxy.HTTPAddr(), "http://127.0.0.1:"))
	assert.True(t, strings.Contains(proxy.SOCKSAddr(), "127.0.0.1:"))

	// Verify environment variables
	env := proxy.Env()
	assert.NotEmpty(t, env)

	foundHTTP := false
	foundSOCKS := false
	for _, e := range env {
		if strings.HasPrefix(e, "HTTP_PROXY=") || strings.HasPrefix(e, "http_proxy=") {
			foundHTTP = true
		}
		if strings.HasPrefix(e, "ALL_PROXY=") || strings.HasPrefix(e, "all_proxy=") {
			foundSOCKS = true
		}
	}
	assert.True(t, foundHTTP, "Should have HTTP_PROXY environment variable")
	assert.True(t, foundSOCKS, "Should have ALL_PROXY environment variable")

	// Close should succeed (idempotent)
	proxy.Close()
	proxy.Close()
}

func TestNetworkProxy_MultipleInstances(t *testing.T) {
	t.Parallel()

	// Should be able to create multiple proxies concurrently
	proxy1, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy1.Close()

	proxy2, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy2.Close()

	// Addresses should be different
	assert.NotEqual(t, proxy1.HTTPAddr(), proxy2.HTTPAddr())
	assert.NotEqual(t, proxy1.SOCKSAddr(), proxy2.SOCKSAddr())
}

func TestNetworkProxy_HTTPConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	t.Parallel()

	// Create a test HTTP server
	testServer := &testHTTPServer{}
	testServer.Start(t)
	defer testServer.Stop()

	// Create proxy with no filter (allow all)
	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	// Extract proxy URL (should be http://127.0.0.1:PORT on macOS)
	proxyURL, err := url.Parse(proxy.HTTPAddr())
	require.NoError(t, err)

	// Create HTTP client with proxy
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}

	// Make an HTTP request through the proxy
	resp, err := client.Get(testServer.URL + "/test")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	// Verify we can read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "test response")
}

func TestNetworkProxy_SOCKS5(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	t.Parallel()

	// Create a test HTTP server
	testServer := &testHTTPServer{}
	testServer.Start(t)
	defer testServer.Stop()

	// Extract target host and port
	targetURL, err := url.Parse(testServer.URL)
	require.NoError(t, err)
	targetHost := targetURL.Hostname()
	targetPort := targetURL.Port()

	// Create proxy with no filter (allow all)
	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	// Extract SOCKS proxy address (should be 127.0.0.1:PORT on macOS)
	socksAddr := proxy.SOCKSAddr()
	require.True(t, strings.Contains(socksAddr, "127.0.0.1:"))

	// Connect to SOCKS5 proxy
	socksConn, err := net.Dial("tcp", socksAddr)
	require.NoError(t, err)
	defer socksConn.Close()

	// Perform SOCKS5 handshake
	// Send: [version, nmethods, methods]
	_, err = socksConn.Write([]byte{0x05, 0x01, 0x00}) // version 5, 1 method, no auth
	require.NoError(t, err)

	// Read: [version, method]
	reply := make([]byte, 2)
	_, err = io.ReadFull(socksConn, reply)
	require.NoError(t, err)
	assert.Equal(t, byte(0x05), reply[0]) // version 5
	assert.Equal(t, byte(0x00), reply[1]) // no auth accepted

	// Send SOCKS5 request: CONNECT to test server
	// [version, cmd, reserved, atyp, dst.addr, dst.port]
	request := []byte{
		0x05, // version
		0x01, // cmd: CONNECT
		0x00, // reserved
		0x03, // atyp: domain name
	}
	request = append(request, byte(len(targetHost))) // domain length
	request = append(request, []byte(targetHost)...) // domain
	portNum, _ := strconv.Atoi(targetPort)           // port number
	request = append(request, byte(portNum>>8))      // port high byte
	request = append(request, byte(portNum&0xff))    // port low byte

	_, err = socksConn.Write(request)
	require.NoError(t, err)

	// Read SOCKS5 reply
	replyHeader := make([]byte, 4)
	_, err = io.ReadFull(socksConn, replyHeader)
	require.NoError(t, err)
	assert.Equal(t, byte(0x05), replyHeader[0]) // version 5
	assert.Equal(t, byte(0x00), replyHeader[1]) // success

	// Read bind address (we don't care about it, but need to consume it)
	atyp := replyHeader[3]
	switch atyp {
	case 0x01: // IPv4
		bindAddr := make([]byte, 4+2) // 4 bytes IP + 2 bytes port
		io.ReadFull(socksConn, bindAddr)
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		io.ReadFull(socksConn, lenBuf)
		bindAddr := make([]byte, int(lenBuf[0])+2) // domain + port
		io.ReadFull(socksConn, bindAddr)
	case 0x04: // IPv6
		bindAddr := make([]byte, 16+2) // 16 bytes IP + 2 bytes port
		io.ReadFull(socksConn, bindAddr)
	}

	// Now the connection is established, send HTTP request
	httpRequest := fmt.Sprintf("GET /test HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", targetHost)
	_, err = socksConn.Write([]byte(httpRequest))
	require.NoError(t, err)

	// Read HTTP response
	response, err := io.ReadAll(socksConn)
	require.NoError(t, err)

	// Verify we got a valid HTTP response
	responseStr := string(response)
	assert.Contains(t, responseStr, "HTTP/1.1 200 OK")
	assert.Contains(t, responseStr, "test response")
}

func TestNetworkFilter_Wildcards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		host    string
		port    string
		want    bool
	}{
		// Exact matches
		{"exact match", "example.com", "example.com", "80", true},
		{"exact match different port", "example.com", "example.com", "443", true},
		{"exact no match", "example.com", "other.com", "80", false},

		// Wildcard matches
		{"wildcard matches subdomain", "*.example.com", "api.example.com", "80", true},
		{"wildcard matches nested subdomain", "*.example.com", "foo.bar.example.com", "80", true},
		{"wildcard does not match base", "*.example.com", "example.com", "80", false},
		{"wildcard does not match different domain", "*.example.com", "example.org", "80", false},

		// Port-specific patterns
		{"port match", "example.com:443", "example.com", "443", true},
		{"port no match", "example.com:443", "example.com", "80", false},
		{"wildcard with port match", "*.example.com:443", "api.example.com", "443", true},
		{"wildcard with port no match", "*.example.com:443", "api.example.com", "80", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPattern(tt.pattern, tt.host, tt.port)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNetworkFilter_CaseInsensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		host    string
		port    string
		want    bool
	}{
		{"exact match different case", "Example.COM", "example.com", "80", true},
		{"exact match uppercase host", "example.com", "EXAMPLE.COM", "80", true},
		{"wildcard case insensitive", "*.Example.COM", "api.example.com", "80", true},
		{"wildcard uppercase host", "*.example.com", "API.EXAMPLE.COM", "80", true},
		{"wildcard mixed case", "*.EXAMPLE.com", "Foo.Bar.example.COM", "80", true},
		{"wildcard no match different domain", "*.Example.COM", "example.org", "80", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPattern(tt.pattern, tt.host, tt.port)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateNetworkFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"valid domain", "example.com", false},
		{"valid wildcard", "*.example.com", false},
		{"valid port specific", "example.com:443", false},
		{"valid wildcard with port", "*.example.com:443", false},
		{"valid localhost", "localhost", false},
		{"valid localhost with port", "localhost:8080", false},
		{"protocol prefix", "https://example.com", true},
		{"path in pattern", "example.com/path", true},
		{"bare wildcard", "*", true},
		{"tld wildcard", "*.com", true},
		{"starts with dot", ".example.com", true},
		{"ends with dot", "example.com.", true},
		{"empty pattern", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := &NetworkFilter{AllowHosts: []string{tt.pattern}}
			err := ValidateNetworkFilter(filter)
			if tt.wantErr {
				assert.Error(t, err, "pattern %q should be rejected", tt.pattern)
			} else {
				assert.NoError(t, err, "pattern %q should be accepted", tt.pattern)
			}
		})
	}
}

func TestValidateNetworkFilter_DenyList(t *testing.T) {
	t.Parallel()

	filter := &NetworkFilter{
		DenyHosts: []string{"https://evil.com"},
	}
	err := ValidateNetworkFilter(filter)
	assert.Error(t, err, "should reject invalid deny patterns too")
}

func TestNewNetworkProxy_RejectsInvalidFilter(t *testing.T) {
	t.Parallel()

	filter := &NetworkFilter{
		AllowHosts: []string{"https://example.com"},
	}
	_, err := NewNetworkProxy(filter)
	assert.Error(t, err, "should reject proxy creation with invalid filter")
}

func TestNetworkFilter_AllowList(t *testing.T) {
	t.Parallel()

	filter := &NetworkFilter{
		AllowHosts: []string{"github.com", "*.npmjs.org"},
	}

	proxy := &NetworkProxy{filter: filter}

	// Should allow github.com
	assert.True(t, proxy.isAllowed("github.com", "443"))

	// Should allow npmjs.org subdomains
	assert.True(t, proxy.isAllowed("registry.npmjs.org", "443"))

	// Should NOT allow npmjs.org itself
	assert.False(t, proxy.isAllowed("npmjs.org", "443"))

	// Should NOT allow other domains
	assert.False(t, proxy.isAllowed("evil.com", "80"))
}

func TestNetworkFilter_DenyList(t *testing.T) {
	t.Parallel()

	filter := &NetworkFilter{
		DenyHosts: []string{"evil.com", "*.malware.org"},
	}

	proxy := &NetworkProxy{filter: filter}

	// Should deny evil.com
	assert.False(t, proxy.isAllowed("evil.com", "80"))

	// Should deny malware.org subdomains
	assert.False(t, proxy.isAllowed("download.malware.org", "80"))

	// Should allow everything else (no allow list)
	assert.True(t, proxy.isAllowed("github.com", "443"))
	assert.True(t, proxy.isAllowed("example.com", "80"))
}

func TestNetworkFilter_DenyPrecedence(t *testing.T) {
	t.Parallel()

	filter := &NetworkFilter{
		AllowHosts: []string{"*.example.com"},
		DenyHosts:  []string{"bad.example.com"},
	}

	proxy := &NetworkProxy{filter: filter}

	// Should allow other subdomains
	assert.True(t, proxy.isAllowed("api.example.com", "80"))

	// Should deny bad.example.com (deny wins)
	assert.False(t, proxy.isAllowed("bad.example.com", "80"))
}

func TestNetworkFilter_PortMatching(t *testing.T) {
	t.Parallel()

	filter := &NetworkFilter{
		AllowHosts: []string{"example.com:443", "api.example.com"},
	}

	proxy := &NetworkProxy{filter: filter}

	// Should allow example.com:443
	assert.True(t, proxy.isAllowed("example.com", "443"))

	// Should NOT allow example.com:80
	assert.False(t, proxy.isAllowed("example.com", "80"))

	// Should allow api.example.com on any port
	assert.True(t, proxy.isAllowed("api.example.com", "80"))
	assert.True(t, proxy.isAllowed("api.example.com", "443"))
	assert.True(t, proxy.isAllowed("api.example.com", "8080"))
}

func TestNetworkProxy_Env_NoProxy(t *testing.T) {
	t.Parallel()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	env := proxy.Env()

	// Should have NO_PROXY and no_proxy
	foundNoProxy := false
	foundNoProxyLower := false
	for _, e := range env {
		if strings.HasPrefix(e, "NO_PROXY=") {
			foundNoProxy = true
			// Should contain localhost, private networks
			assert.Contains(t, e, "localhost")
			assert.Contains(t, e, "127.0.0.1")
			assert.Contains(t, e, "::1")
			assert.Contains(t, e, "192.168.0.0/16")
			assert.Contains(t, e, "10.0.0.0/8")
			assert.Contains(t, e, "172.16.0.0/12")
		}
		if strings.HasPrefix(e, "no_proxy=") {
			foundNoProxyLower = true
		}
	}
	assert.True(t, foundNoProxy, "Should have NO_PROXY environment variable")
	assert.True(t, foundNoProxyLower, "Should have no_proxy environment variable")
}

func TestNetworkProxy_Env_Socks5h(t *testing.T) {
	t.Parallel()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	env := proxy.Env()

	for _, e := range env {
		if strings.HasPrefix(e, "ALL_PROXY=") {
			// Must use socks5h:// (not socks5://) for DNS through proxy
			assert.Contains(t, e, "socks5h://", "ALL_PROXY should use socks5h:// for proxy-side DNS resolution")
			assert.NotContains(t, e, "socks5://1", "ALL_PROXY should not use plain socks5://")
		}
	}
}

func TestNetworkProxy_Env_AdditionalProxyVars(t *testing.T) {
	t.Parallel()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	env := proxy.Env()
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Should have FTP_PROXY
	assert.Contains(t, envMap, "FTP_PROXY", "Should set FTP_PROXY")
	assert.Contains(t, envMap, "ftp_proxy", "Should set ftp_proxy")

	// RSYNC_PROXY set on all platforms (TCP sockets)
	assert.Contains(t, envMap, "RSYNC_PROXY", "Should set RSYNC_PROXY")

	// Should have Docker proxy vars
	assert.Contains(t, envMap, "DOCKER_HTTP_PROXY", "Should set DOCKER_HTTP_PROXY")
	assert.Contains(t, envMap, "DOCKER_HTTPS_PROXY", "Should set DOCKER_HTTPS_PROXY")
	assert.Contains(t, envMap, "DOCKER_NO_PROXY", "Should set DOCKER_NO_PROXY")

	// Should have gRPC proxy vars
	assert.Contains(t, envMap, "GRPC_PROXY", "Should set GRPC_PROXY")
	assert.Contains(t, envMap, "grpc_proxy", "Should set grpc_proxy")

	// Should have Google Cloud SDK proxy vars
	assert.Contains(t, envMap, "CLOUDSDK_PROXY_TYPE")
	assert.Equal(t, "https", envMap["CLOUDSDK_PROXY_TYPE"])
	assert.Contains(t, envMap, "CLOUDSDK_PROXY_ADDRESS")
	assert.Equal(t, "127.0.0.1", envMap["CLOUDSDK_PROXY_ADDRESS"])
	assert.Contains(t, envMap, "CLOUDSDK_PROXY_PORT")
	assert.NotEmpty(t, envMap["CLOUDSDK_PROXY_PORT"])
}

func TestNetworkProxy_Env_GitSSHCommand(t *testing.T) {
	t.Parallel()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	env := proxy.Env()
	foundGitSSH := false
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_SSH_COMMAND=") {
			foundGitSSH = true
			assert.Contains(t, e, "ssh")
			assert.Contains(t, e, "ProxyCommand")
			assert.Contains(t, e, "nc")
		}
	}
	if runtime.GOOS == "darwin" {
		assert.True(t, foundGitSSH, "Should have GIT_SSH_COMMAND on macOS")
	} else {
		assert.False(t, foundGitSSH, "Should NOT have GIT_SSH_COMMAND on Linux")
	}
}

func TestBidirectionalCopy_DoesNotClose(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()

	// Wrap connections in close trackers to count Close() calls
	serverTracker := &closeTracker{Conn: server}
	clientTracker := &closeTracker{Conn: client}

	done := make(chan struct{})
	go func() {
		defer close(done)
		bidirectionalCopy(serverTracker, clientTracker)
	}()

	// Close underlying connections directly (bypassing trackers) to unblock
	// bidirectionalCopy. This simulates the caller's defer Close() pattern.
	client.Close()
	server.Close()
	<-done

	// bidirectionalCopy must not call Close() on connections it receives.
	// Callers handle connection lifecycle via defer.
	assert.Equal(t, 0, serverTracker.CloseCount(),
		"bidirectionalCopy must not call Close on dst connection")
	assert.Equal(t, 0, clientTracker.CloseCount(),
		"bidirectionalCopy must not call Close on src connection")
}

// closeWriteTracker wraps a net.Conn and tracks CloseWrite() calls.
// Used to verify bidirectionalCopy signals half-close via the closeWriter interface.
type closeWriteTracker struct {
	net.Conn
	mu              sync.Mutex
	closeWriteCount int
}

func (c *closeWriteTracker) CloseWrite() error {
	c.mu.Lock()
	c.closeWriteCount++
	c.mu.Unlock()
	// Don't actually close -- net.Pipe doesn't support CloseWrite
	return nil
}

func (c *closeWriteTracker) CloseWriteCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeWriteCount
}

func TestBidirectionalCopy_CallsCloseWrite(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()

	// Wrap with closeWriteTracker to verify CloseWrite is called
	serverTracker := &closeWriteTracker{Conn: server}
	clientTracker := &closeWriteTracker{Conn: client}

	done := make(chan struct{})
	go func() {
		defer close(done)
		bidirectionalCopy(serverTracker, clientTracker)
	}()

	// Close underlying connections to trigger EOF
	client.Close()
	server.Close()
	<-done

	// bidirectionalCopy should call CloseWrite on both sides via the
	// closeWriter interface (preventing deadlocks on Unix domain sockets)
	assert.Greater(t, serverTracker.CloseWriteCount(), 0,
		"bidirectionalCopy should call CloseWrite on dst via closeWriter interface")
	assert.Greater(t, clientTracker.CloseWriteCount(), 0,
		"bidirectionalCopy should call CloseWrite on src via closeWriter interface")
}

// Test helpers

// testHTTPServer is a simple HTTP server for testing proxy functionality.
type testHTTPServer struct {
	server *httptest.Server
	URL    string
}

func (s *testHTTPServer) Start(t *testing.T) {
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response from " + r.URL.Path))
	}))
	s.URL = s.server.URL
}

func (s *testHTTPServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}
