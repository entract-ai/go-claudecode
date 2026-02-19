//go:build linux

package sandbox

import (
	"fmt"
	"io"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLinuxNetworkBridge_CreatesUnixSockets(t *testing.T) {
	// Start a dummy TCP server as a proxy target
	tcpLn1, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn1.Close()
	tcpLn2, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn2.Close()

	bridge, err := newLinuxNetworkBridge(tcpLn1.Addr().String(), tcpLn2.Addr().String())
	require.NoError(t, err)
	defer bridge.Close()

	// Verify socket files exist
	_, err = os.Stat(bridge.httpSocketPath)
	assert.NoError(t, err, "HTTP socket file should exist")
	_, err = os.Stat(bridge.socksSocketPath)
	assert.NoError(t, err, "SOCKS socket file should exist")

	// Verify temp directory exists
	_, err = os.Stat(bridge.tmpDir)
	assert.NoError(t, err, "temp directory should exist")
}

func TestLinuxNetworkBridge_Close_CleansUp(t *testing.T) {
	tcpLn1, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn1.Close()
	tcpLn2, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn2.Close()

	bridge, err := newLinuxNetworkBridge(tcpLn1.Addr().String(), tcpLn2.Addr().String())
	require.NoError(t, err)

	tmpDir := bridge.tmpDir
	httpSock := bridge.httpSocketPath
	socksSock := bridge.socksSocketPath

	bridge.Close()

	// Verify everything is cleaned up
	_, err = os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(err), "temp directory should be removed after Close")
	_, err = os.Stat(httpSock)
	assert.True(t, os.IsNotExist(err), "HTTP socket should be removed after Close")
	_, err = os.Stat(socksSock)
	assert.True(t, os.IsNotExist(err), "SOCKS socket should be removed after Close")

	// Double-close should not panic
	bridge.Close()
}

func TestLinuxNetworkBridge_ForwardsHTTPConnection(t *testing.T) {
	// Start a TCP echo server simulating the HTTP proxy
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echoLn.Close()

	go func() {
		for {
			conn, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()

	// Use a dummy address for SOCKS (won't be tested here)
	dummyLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummyLn.Close()

	bridge, err := newLinuxNetworkBridge(echoLn.Addr().String(), dummyLn.Addr().String())
	require.NoError(t, err)
	defer bridge.Close()

	// Connect to the HTTP unix socket
	conn, err := net.Dial("unix", bridge.httpSocketPath)
	require.NoError(t, err)
	defer conn.Close()

	// Send data and verify it comes back (echo)
	msg := "hello from bridge test"
	_, err = fmt.Fprintln(conn, msg)
	require.NoError(t, err)

	// Close write side to signal EOF
	conn.(*net.UnixConn).CloseWrite()

	buf, err := io.ReadAll(conn)
	require.NoError(t, err)
	assert.Contains(t, string(buf), msg)
}

func TestLinuxNetworkBridge_ForwardsSOCKSConnection(t *testing.T) {
	// Start a TCP echo server simulating the SOCKS proxy
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echoLn.Close()

	go func() {
		for {
			conn, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()

	// Use a dummy address for HTTP (won't be tested here)
	dummyLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer dummyLn.Close()

	bridge, err := newLinuxNetworkBridge(dummyLn.Addr().String(), echoLn.Addr().String())
	require.NoError(t, err)
	defer bridge.Close()

	// Connect to the SOCKS unix socket
	conn, err := net.Dial("unix", bridge.socksSocketPath)
	require.NoError(t, err)
	defer conn.Close()

	// Send data and verify it comes back (echo)
	msg := "hello from socks bridge test"
	_, err = fmt.Fprintln(conn, msg)
	require.NoError(t, err)

	conn.(*net.UnixConn).CloseWrite()

	buf, err := io.ReadAll(conn)
	require.NoError(t, err)
	assert.Contains(t, string(buf), msg)
}
