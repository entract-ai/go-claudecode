//go:build linux

package sandbox

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// linuxNetworkBridge forwards connections from Unix domain sockets (inside the
// sandbox) to the host-side proxy TCP ports. This bridges the network namespace
// gap created by bwrap --unshare-net.
//
// Architecture:
//
//	Host: net.Listener on Unix socket -> io.Copy goroutines -> proxy TCP port
//	bwrap: --bind mounts Unix sockets into isolated namespace
//	Sandbox: socat TCP-LISTEN:PORT -> UNIX-CONNECT:socket -> host proxy
//
// The host side is pure Go (no socat). Socat is only used inside the sandbox
// to convert TCP connections to Unix socket connections.
type linuxNetworkBridge struct {
	httpSocketPath  string
	socksSocketPath string
	httpLn          net.Listener
	socksLn         net.Listener
	httpProxyAddr   string // host HTTP proxy "127.0.0.1:PORT"
	socksProxyAddr  string // host SOCKS proxy "127.0.0.1:PORT"
	tmpDir          string
	wg              sync.WaitGroup
	closed          chan struct{}
	closeOnce       sync.Once
}

// newLinuxNetworkBridge creates Unix socket listeners that forward connections
// to the given TCP proxy addresses. The Unix sockets are created in a temporary
// directory that can be bind-mounted into the sandbox.
//
// httpProxyAddr and socksProxyAddr should be "host:port" format (e.g., "127.0.0.1:3128").
func newLinuxNetworkBridge(httpProxyAddr, socksProxyAddr string) (*linuxNetworkBridge, error) {
	tmpDir, err := os.MkdirTemp("", "claude-bridge-")
	if err != nil {
		return nil, fmt.Errorf("create bridge temp dir: %w", err)
	}

	httpSocketPath := filepath.Join(tmpDir, "http.sock")
	socksSocketPath := filepath.Join(tmpDir, "socks.sock")

	httpLn, err := net.Listen("unix", httpSocketPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("listen on HTTP unix socket: %w", err)
	}

	socksLn, err := net.Listen("unix", socksSocketPath)
	if err != nil {
		httpLn.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("listen on SOCKS unix socket: %w", err)
	}

	b := &linuxNetworkBridge{
		httpSocketPath:  httpSocketPath,
		socksSocketPath: socksSocketPath,
		httpLn:          httpLn,
		socksLn:         socksLn,
		httpProxyAddr:   httpProxyAddr,
		socksProxyAddr:  socksProxyAddr,
		tmpDir:          tmpDir,
		closed:          make(chan struct{}),
	}

	// Start accept loops
	b.wg.Add(2)
	go b.acceptLoop(b.httpLn, b.httpProxyAddr)
	go b.acceptLoop(b.socksLn, b.socksProxyAddr)

	return b, nil
}

// acceptLoop accepts connections on ln and forwards each to targetAddr via TCP.
func (b *linuxNetworkBridge) acceptLoop(ln net.Listener, targetAddr string) {
	defer b.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-b.closed:
				return
			default:
				// Brief backoff to prevent CPU spin on persistent errors
				// (e.g., too many open files).
				time.Sleep(5 * time.Millisecond)
				continue
			}
		}
		go b.forward(conn, targetAddr)
	}
}

// forward bridges a single connection to the target TCP address.
func (b *linuxNetworkBridge) forward(src net.Conn, targetAddr string) {
	dst, err := net.Dial("tcp", targetAddr)
	if err != nil {
		src.Close()
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(dst, src)
		// Signal EOF to the other direction
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		io.Copy(src, dst)
		// Signal EOF to the other direction
		if uc, ok := src.(*net.UnixConn); ok {
			uc.CloseWrite()
		}
	}()
	wg.Wait()
	src.Close()
	dst.Close()
}

// Close stops the bridge, closing all listeners and cleaning up the temp directory.
// Safe to call multiple times.
func (b *linuxNetworkBridge) Close() {
	b.closeOnce.Do(func() {
		close(b.closed)
		b.httpLn.Close()
		b.socksLn.Close()
		b.wg.Wait()
		os.RemoveAll(b.tmpDir)
	})
}
