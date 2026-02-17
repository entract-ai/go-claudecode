package sandbox

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

// NetworkFilter specifies allowed and denied network destinations for proxy filtering.
// Patterns support wildcards (e.g., "*.github.com" matches "api.github.com" but not "github.com").
// Deny rules take precedence over allow rules.
// If AllowHosts is empty, all destinations are allowed (unless explicitly denied).
// If AllowHosts is non-empty, only matching destinations are allowed.
type NetworkFilter struct {
	// AllowHosts contains patterns for allowed destinations.
	// Examples: "github.com", "*.npmjs.org", "example.com:443"
	AllowHosts []string

	// DenyHosts contains patterns for denied destinations.
	// Deny takes precedence over allow.
	DenyHosts []string
}

// ValidateNetworkFilter checks that all patterns in a NetworkFilter are valid.
// Invalid patterns include:
//   - Patterns containing "://" (protocol prefixes)
//   - Patterns containing "/" (paths)
//   - Bare "*" or TLD-only wildcards like "*.com" (too broad)
//   - Patterns starting or ending with "."
//   - "localhost" is always allowed as a special case
//
// Port-specific patterns (e.g., "example.com:443") are allowed; the port is
// stripped before validating the host part.
func ValidateNetworkFilter(filter *NetworkFilter) error {
	for _, pattern := range filter.AllowHosts {
		if err := validatePattern(pattern); err != nil {
			return fmt.Errorf("allow host %q: %w", pattern, err)
		}
	}
	for _, pattern := range filter.DenyHosts {
		if err := validatePattern(pattern); err != nil {
			return fmt.Errorf("deny host %q: %w", pattern, err)
		}
	}
	return nil
}

func validatePattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty pattern")
	}
	if strings.Contains(pattern, "://") {
		return fmt.Errorf("must not contain protocol prefix")
	}
	if strings.Contains(pattern, "/") {
		return fmt.Errorf("must not contain path separators")
	}

	// Strip port if present. Note: IPv6 literal addresses (e.g., [::1]:443) are
	// not supported as patterns; this only handles domain:port and IPv4:port.
	host := pattern
	if idx := strings.LastIndexByte(pattern, ':'); idx >= 0 {
		host = pattern[:idx]
	}

	if host == "" {
		return fmt.Errorf("empty host")
	}
	if host == "localhost" {
		return nil
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return fmt.Errorf("must not start or end with '.'")
	}
	if host == "*" {
		return fmt.Errorf("bare wildcard '*' is too broad")
	}

	// Wildcard patterns: *.suffix must have at least 2 dot-separated parts after *.
	if len(host) > 2 && host[0] == '*' && host[1] == '.' {
		suffix := host[2:] // after "*."
		if strings.Count(suffix, ".") < 1 {
			return fmt.Errorf("wildcard %q is too broad (must have at least two domain parts after *.)", host)
		}
	}

	return nil
}

// NetworkProxy manages HTTP and SOCKS5 proxy servers with optional domain filtering.
// Proxies listen on localhost TCP sockets (127.0.0.1) with OS-allocated ports on
// both macOS and Linux. On Linux, the network namespace (--unshare-net) prevents
// bypass since the loopback interface inside the namespace is isolated.
//
// The proxy must be explicitly closed via Close() to clean up resources.
// Goroutine leaks will occur if Close() is not called.
//
// Example usage:
//
//	filter := &NetworkFilter{
//	    AllowHosts: []string{"github.com", "*.npmjs.org"},
//	}
//	proxy, err := NewNetworkProxy(filter)
//	if err != nil {
//	    return err
//	}
//	defer proxy.Close()
//
//	// Use proxy.Env() to configure sandboxed processes
//	policy.NetworkProxy = proxy
type NetworkProxy struct {
	filter    *NetworkFilter
	httpAddr  string
	socksAddr string
	httpLn    net.Listener
	socksLn   net.Listener
	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup

	mu         sync.Mutex
	httpServer *http.Server
}

// NewNetworkProxy creates and starts HTTP and SOCKS5 proxy servers with the given filter.
// The proxies begin accepting connections immediately.
// The returned proxy must be closed via Close() to prevent resource leaks.
func NewNetworkProxy(filter *NetworkFilter) (*NetworkProxy, error) {
	if filter != nil {
		if err := ValidateNetworkFilter(filter); err != nil {
			return nil, fmt.Errorf("validate network filter: %w", err)
		}
	}

	httpLn, socksLn, err := createListeners()
	if err != nil {
		return nil, fmt.Errorf("create listeners: %w", err)
	}

	p := &NetworkProxy{
		filter:  filter,
		httpLn:  httpLn,
		socksLn: socksLn,
		closed:  make(chan struct{}),
	}

	// Get listener addresses
	p.httpAddr = formatHTTPAddress(httpLn.Addr())
	p.socksAddr = formatSOCKSAddress(socksLn.Addr())

	// Start HTTP proxy server
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ctx := context.Background()
		if err := p.serveHTTP(ctx); err != nil {
			// Shutdown errors are expected, ignore them
		}
	}()

	// Start SOCKS5 proxy server
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ctx := context.Background()
		if err := p.serveSOCKS(ctx); err != nil {
			// Shutdown errors are expected, ignore them
		}
	}()

	return p, nil
}

// HTTPAddr returns the HTTP proxy address in "http://127.0.0.1:PORT" format.
func (p *NetworkProxy) HTTPAddr() string {
	return p.httpAddr
}

// SOCKSAddr returns the SOCKS5 proxy address in "127.0.0.1:PORT" format.
func (p *NetworkProxy) SOCKSAddr() string {
	return p.socksAddr
}

// noProxyAddresses lists destinations that should bypass the filtering proxy.
// This includes localhost, link-local, and RFC 1918 private network ranges.
//
// Design note: private network ranges (10/8, 172.16/12, 192.168/16) bypass the
// proxy intentionally. The proxy's purpose is filtering internet-bound traffic,
// not restricting local network access. Sandboxed processes can reach hosts on
// the local network without proxy filtering. If full network auditing is required,
// use network namespace isolation (AllowNetwork=false) instead of proxy filtering.
const noProxyAddresses = "localhost,127.0.0.1,::1,*.local,.local,169.254.0.0/16,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

// Env returns environment variables configuring HTTP and SOCKS5 proxies.
// Includes both uppercase and lowercase variants for maximum compatibility,
// plus tool-specific variables (Docker, gRPC, FTP, RSYNC, GIT_SSH_COMMAND).
// The caller should append these to cmd.Env when executing sandboxed commands.
func (p *NetworkProxy) Env() []string {
	httpAddr := p.HTTPAddr()
	socksAddr := p.SOCKSAddr()

	env := []string{
		// Standard HTTP proxy variables
		"HTTP_PROXY=" + httpAddr,
		"HTTPS_PROXY=" + httpAddr,
		"http_proxy=" + httpAddr,
		"https_proxy=" + httpAddr,

		// NO_PROXY: bypass proxy for localhost and private networks
		"NO_PROXY=" + noProxyAddresses,
		"no_proxy=" + noProxyAddresses,
	}

	// Build SOCKS proxy URL: use socks5h:// so DNS resolution happens through
	// the proxy rather than locally (which would fail inside a sandboxed namespace).
	socksURL := "socks5h://" + socksAddr

	env = append(env,
		"ALL_PROXY="+socksURL,
		"all_proxy="+socksURL,

		// FTP proxy
		"FTP_PROXY="+socksURL,
		"ftp_proxy="+socksURL,

		// Docker proxy
		"DOCKER_HTTP_PROXY="+httpAddr,
		"DOCKER_HTTPS_PROXY="+httpAddr,
		"DOCKER_NO_PROXY="+noProxyAddresses,

		// gRPC proxy
		"GRPC_PROXY="+socksURL,
		"grpc_proxy="+socksURL,

		// RSYNC proxy expects host:port format without scheme
		"RSYNC_PROXY="+socksAddr,
	)

	// Google Cloud SDK proxy configuration
	httpHost := strings.TrimPrefix(httpAddr, "http://")
	if _, httpPort, err := net.SplitHostPort(httpHost); err == nil {
		env = append(env,
			"CLOUDSDK_PROXY_TYPE=http",
			"CLOUDSDK_PROXY_ADDRESS=127.0.0.1",
			"CLOUDSDK_PROXY_PORT="+httpPort,
		)
	}

	// GIT_SSH_COMMAND: route git-over-SSH through SOCKS proxy.
	// Only on macOS -- the -X 5 -x flags are BSD nc extensions.
	// Git-over-HTTPS still works on all platforms via HTTP_PROXY.
	if runtime.GOOS == "darwin" {
		env = append(env, fmt.Sprintf("GIT_SSH_COMMAND=ssh -o ProxyCommand='nc -X 5 -x %s %%h %%p'", socksAddr))
	}

	return env
}

// Close gracefully shuts down the proxy servers and cleans up resources.
// It waits for all active connections to complete before returning.
// Close is safe to call multiple times (idempotent).
func (p *NetworkProxy) Close() {
	p.closeOnce.Do(func() {
		// Signal shutdown to all goroutines
		close(p.closed)

		// Stop accepting new connections
		if p.httpLn != nil {
			p.httpLn.Close()
		}
		if p.socksLn != nil {
			p.socksLn.Close()
		}

		// Gracefully shutdown HTTP server
		p.mu.Lock()
		httpServer := p.httpServer
		p.mu.Unlock()

		if httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			httpServer.Shutdown(ctx)
		}

		// Wait for all connection handlers to finish
		p.wg.Wait()
	})
}

// serveHTTP runs the HTTP proxy server. It blocks until the listener is closed.
func (p *NetworkProxy) serveHTTP(ctx context.Context) error {
	handler := http.HandlerFunc(p.handleHTTPRequest)
	server := &http.Server{Handler: handler}

	p.mu.Lock()
	p.httpServer = server
	p.mu.Unlock()

	err := server.Serve(p.httpLn)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// serveSOCKS runs the SOCKS5 proxy server. It blocks until the listener is closed.
func (p *NetworkProxy) serveSOCKS(ctx context.Context) error {
	var tempDelay time.Duration
	for {
		conn, err := p.socksLn.Accept()
		if err != nil {
			select {
			case <-p.closed:
				return nil
			default:
				// Exponential backoff on transient accept errors, matching
				// the approach used by net/http.Server.Serve.
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if tempDelay > 1*time.Second {
					tempDelay = 1 * time.Second
				}
				time.Sleep(tempDelay)
				continue
			}
		}
		tempDelay = 0

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			// Per-connection errors are expected (client disconnect, timeout)
			// and not actionable at the server level.
			p.handleSOCKS(conn)
		}()
	}
}

// handleHTTPRequest processes HTTP proxy requests (GET, POST, CONNECT, etc.).
func (p *NetworkProxy) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}

	// For non-CONNECT requests (regular HTTP proxy), we use a reverse proxy approach
	// This handles GET, POST, etc. requests properly
	host := r.URL.Host
	if host == "" {
		host = r.Host
	}

	if host == "" {
		http.Error(w, "Bad Request: missing host", http.StatusBadRequest)
		return
	}

	// Extract host and port
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		// No port specified, assume default based on scheme
		hostname = host
		if r.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// Check filter
	if !p.isAllowed(hostname, port) {
		http.Error(w, "Forbidden: destination not allowed", http.StatusForbidden)
		return
	}

	// Create HTTP client to forward the request
	targetURL := r.URL
	if targetURL.Scheme == "" {
		targetURL.Scheme = "http"
	}

	// Create a new request to the target
	proxyReq, err := http.NewRequest(r.Method, targetURL.String(), r.Body)
	if err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Bad Gateway: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)
}

// handleConnect handles HTTP CONNECT requests for HTTPS tunneling.
func (p *NetworkProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Extract target host:port from request
	targetAddr := r.Host
	if targetAddr == "" {
		targetAddr = r.URL.Host
	}

	if targetAddr == "" {
		http.Error(w, "Bad Request: missing host", http.StatusBadRequest)
		return
	}

	// Parse host and port
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		// CONNECT requires explicit port
		http.Error(w, "Bad Request: invalid host:port", http.StatusBadRequest)
		return
	}

	// Check filter
	if !p.isAllowed(host, port) {
		http.Error(w, "Forbidden: destination not allowed", http.StatusForbidden)
		return
	}

	// Dial target
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		http.Error(w, "Bad Gateway: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	// Hijack the connection to get raw TCP access
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Internal Server Error: hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Internal Server Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send success response to client
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		return
	}

	// Start bidirectional copy
	bidirectionalCopy(targetConn, clientConn)
}

// isAllowed checks if a connection to the given host and port is allowed by the filter.
func (p *NetworkProxy) isAllowed(host, port string) bool {
	if p.filter == nil {
		return true
	}

	// Check deny list first (deny takes precedence)
	for _, pattern := range p.filter.DenyHosts {
		if matchesPattern(pattern, host, port) {
			return false
		}
	}

	// If allow list is empty, allow everything (unless already denied above)
	if len(p.filter.AllowHosts) == 0 {
		return true
	}

	// Check allow list - must match at least one pattern
	for _, pattern := range p.filter.AllowHosts {
		if matchesPattern(pattern, host, port) {
			return true
		}
	}

	// Allow list exists but no match found
	return false
}

// matchesPattern checks if a host:port matches a given pattern.
// Patterns support wildcards and optional port specifications:
//   - "example.com" matches "example.com" with any port
//   - "example.com:443" matches "example.com" only with port 443
//   - "*.example.com" matches "api.example.com" but NOT "example.com"
//   - "*.example.com:443" matches subdomains of example.com on port 443
func matchesPattern(pattern, host, port string) bool {
	// Parse pattern into host and port parts
	var patternHost, patternPort string

	// Check if pattern contains a port
	if idx := strings.LastIndexByte(pattern, ':'); idx >= 0 {
		// Pattern has a port
		patternHost = pattern[:idx]
		patternPort = pattern[idx+1:]
	} else {
		// Pattern has no port - match any port
		patternHost = pattern
		patternPort = ""
	}

	// If pattern specifies a port, it must match exactly
	if patternPort != "" && patternPort != port {
		return false
	}

	// Check host matching (with wildcard support)
	return matchesHost(patternHost, host)
}

// matchesHost checks if a host matches a pattern with wildcard support.
// Matching is case-insensitive per DNS conventions.
// Wildcards (*) only match at the beginning:
//   - "*.example.com" matches "api.example.com" and "foo.bar.example.com"
//   - "*.example.com" does NOT match "example.com" itself
func matchesHost(pattern, host string) bool {
	// Case-insensitive exact match
	if strings.EqualFold(pattern, host) {
		return true
	}

	// Wildcard match
	if len(pattern) > 2 && pattern[0] == '*' && pattern[1] == '.' {
		// Pattern is "*.suffix"
		suffix := strings.ToLower(pattern[1:]) // ".suffix"
		lowerHost := strings.ToLower(host)

		// Host must end with the suffix and have at least one character before it
		if len(lowerHost) > len(suffix) && strings.HasSuffix(lowerHost, suffix) {
			return true
		}
	}

	return false
}

// handleSOCKS processes a SOCKS5 connection.
func (p *NetworkProxy) handleSOCKS(clientConn net.Conn) error {
	defer clientConn.Close()

	// SOCKS5 handshake
	if err := socks5Handshake(clientConn); err != nil {
		return fmt.Errorf("socks5 handshake: %w", err)
	}

	// Read SOCKS5 request
	host, port, err := socks5ReadRequest(clientConn)
	if err != nil {
		socks5SendReply(clientConn, 0x01) // General failure
		return fmt.Errorf("socks5 read request: %w", err)
	}

	// Check filter
	if !p.isAllowed(host, port) {
		socks5SendReply(clientConn, 0x02) // Connection not allowed
		return fmt.Errorf("socks5: destination %s:%s not allowed", host, port)
	}

	// Dial target
	targetAddr := net.JoinHostPort(host, port)
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		socks5SendReply(clientConn, 0x05) // Connection refused
		return fmt.Errorf("socks5 dial %s: %w", targetAddr, err)
	}
	defer targetConn.Close()

	// Send success reply
	if err := socks5SendReply(clientConn, 0x00); err != nil {
		return fmt.Errorf("socks5 send reply: %w", err)
	}

	// Start bidirectional copy
	bidirectionalCopy(targetConn, clientConn)
	return nil
}

// socks5Handshake performs the SOCKS5 handshake (authentication negotiation).
// We only support "no authentication" (method 0x00).
func socks5Handshake(conn net.Conn) error {
	// Read client greeting: [version, nmethods, methods...]
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("read version and nmethods: %w", err)
	}

	version := buf[0]
	nmethods := buf[1]

	if version != 0x05 {
		return fmt.Errorf("unsupported SOCKS version: %d", version)
	}

	// Read authentication methods
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("read methods: %w", err)
	}

	// Check if "no authentication" (0x00) is supported
	noAuthSupported := false
	for _, method := range methods {
		if method == 0x00 {
			noAuthSupported = true
			break
		}
	}

	if !noAuthSupported {
		// No acceptable methods
		conn.Write([]byte{0x05, 0xFF})
		return fmt.Errorf("no acceptable authentication methods")
	}

	// Send server choice: [version, method]
	_, err := conn.Write([]byte{0x05, 0x00}) // version 5, no auth
	return err
}

// socks5ReadRequest reads the SOCKS5 request and extracts the destination host and port.
// Returns (host, port, error).
func socks5ReadRequest(conn net.Conn) (string, string, error) {
	// Read fixed part: [version, cmd, reserved, atyp]
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", "", fmt.Errorf("read request header: %w", err)
	}

	version := buf[0]
	cmd := buf[1]
	atyp := buf[3]

	if version != 0x05 {
		return "", "", fmt.Errorf("unsupported SOCKS version: %d", version)
	}

	if cmd != 0x01 { // Only support CONNECT
		return "", "", fmt.Errorf("unsupported command: %d", cmd)
	}

	var host string
	var err error

	// Read destination address based on address type
	switch atyp {
	case 0x01: // IPv4
		ipBytes := make([]byte, 4)
		if _, err := io.ReadFull(conn, ipBytes); err != nil {
			return "", "", fmt.Errorf("read IPv4 address: %w", err)
		}
		host = net.IP(ipBytes).String()

	case 0x03: // Domain name
		// Read domain length
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", "", fmt.Errorf("read domain length: %w", err)
		}
		domainLen := lenBuf[0]

		// Read domain
		domainBytes := make([]byte, domainLen)
		if _, err := io.ReadFull(conn, domainBytes); err != nil {
			return "", "", fmt.Errorf("read domain: %w", err)
		}
		host = string(domainBytes)

	case 0x04: // IPv6
		ipBytes := make([]byte, 16)
		if _, err := io.ReadFull(conn, ipBytes); err != nil {
			return "", "", fmt.Errorf("read IPv6 address: %w", err)
		}
		host = net.IP(ipBytes).String()

	default:
		return "", "", fmt.Errorf("unsupported address type: %d", atyp)
	}

	// Read port (2 bytes, big endian)
	portBytes := make([]byte, 2)
	if _, err = io.ReadFull(conn, portBytes); err != nil {
		return "", "", fmt.Errorf("read port: %w", err)
	}
	port := binary.BigEndian.Uint16(portBytes)

	return host, fmt.Sprintf("%d", port), nil
}

// socks5SendReply sends a SOCKS5 reply to the client.
// rep is the reply code: 0x00 (success), 0x01 (general failure), 0x02 (not allowed), etc.
func socks5SendReply(conn net.Conn, rep byte) error {
	// Build reply: [version, rep, reserved, atyp, bnd.addr, bnd.port]
	// We use a dummy bind address: 0.0.0.0:0
	reply := []byte{
		0x05,       // version
		rep,        // reply code
		0x00,       // reserved
		0x01,       // atyp: IPv4
		0, 0, 0, 0, // bind address: 0.0.0.0
		0, 0, // bind port: 0
	}
	_, err := conn.Write(reply)
	return err
}

// createListeners creates TCP listeners on localhost for HTTP and SOCKS5 proxies.
func createListeners() (httpLn, socksLn net.Listener, err error) {
	httpLn, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("listen on tcp: %w", err)
	}

	socksLn, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		httpLn.Close()
		return nil, nil, fmt.Errorf("listen on tcp: %w", err)
	}

	return httpLn, socksLn, nil
}

// formatHTTPAddress converts a TCP address to "http://host:port" format.
func formatHTTPAddress(addr net.Addr) string {
	return fmt.Sprintf("http://%s", addr.String())
}

// formatSOCKSAddress converts a TCP address to "host:port" format.
func formatSOCKSAddress(addr net.Addr) string {
	return addr.String()
}

// closeWriter is implemented by connections that support half-close (signaling
// write-side EOF without closing the full connection). *net.TCPConn implements
// this interface.
type closeWriter interface {
	CloseWrite() error
}

// bidirectionalCopy copies data bidirectionally between two connections.
// It returns when both directions have finished or encountered an error.
// The caller is responsible for closing the connections (typically via defer).
//
// When one direction finishes, CloseWrite is called on the destination to
// signal EOF to the peer via the closeWriter interface.
func bidirectionalCopy(dst, src net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copy := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		// Signal write-side EOF to peer. This unblocks the other direction's
		// io.Copy read, preventing a deadlock where bidirectionalCopy waits
		// on wg.Wait() while the other goroutine blocks in Read().
		if cw, ok := dst.(closeWriter); ok {
			cw.CloseWrite()
		}
	}

	go copy(dst, src)
	go copy(src, dst)

	wg.Wait()
}
