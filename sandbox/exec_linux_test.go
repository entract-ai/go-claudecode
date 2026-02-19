//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBubblewrapArgs_SelectiveUnsharing(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// Should use selective unsharing, not --unshare-all
	for _, arg := range args {
		assert.NotEqual(t, "--unshare-all", arg, "Should not use --unshare-all; use selective namespace unsharing instead")
	}

	// Should always unshare PID namespace
	assert.Contains(t, args, "--unshare-pid", "Should always unshare PID namespace")

	// Should unshare network when AllowNetwork is false (default)
	assert.Contains(t, args, "--unshare-net", "Should unshare network namespace by default")
}

func TestBubblewrapArgs_DefaultNoIPCUTSUnshare(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// By default, IPC and UTS should NOT be unshared
	for _, arg := range args {
		assert.NotEqual(t, "--unshare-ipc", arg, "Should not unshare IPC by default")
		assert.NotEqual(t, "--unshare-uts", arg, "Should not unshare UTS by default")
	}
}

func TestBubblewrapArgs_UnshareIPC(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.UnshareIPC = true

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	assert.Contains(t, args, "--unshare-ipc", "Should unshare IPC when UnshareIPC is true")
}

func TestBubblewrapArgs_UnshareUTS(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.UnshareUTS = true

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	assert.Contains(t, args, "--unshare-uts", "Should unshare UTS when UnshareUTS is true")
}

func TestBubblewrapArgs_NetworkAllowed(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.AllowNetwork = true

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// PID should still be unshared
	assert.Contains(t, args, "--unshare-pid")

	// Network should NOT be unshared
	for _, arg := range args {
		assert.NotEqual(t, "--unshare-net", arg, "Should not unshare network when AllowNetwork is true")
	}
}

func TestBubblewrapArgs_NetworkProxy(t *testing.T) {
	t.Parallel()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.AllowNetwork = true // Even with AllowNetwork, proxy forces net unsharing
	policy.NetworkProxy = proxy

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// PID should be unshared
	assert.Contains(t, args, "--unshare-pid")

	// Network should be unshared (forced by proxy)
	assert.Contains(t, args, "--unshare-net", "Should unshare network when proxy is configured")
}

func TestBubblewrapArgs_DenyWritePaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	// Deny writes to a path within the writable working directory
	denyPath := filepath.Join(tmpDir, "protected")
	require.NoError(t, os.MkdirAll(denyPath, 0o755))
	policy.DenyWritePaths = []string{denyPath}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// The deny path should be re-mounted read-only after the writable mount
	// Find the writable bind for workdir
	workdirBindIdx := -1
	denyBindIdx := -1
	for i, arg := range args {
		if arg == "--bind" && i+2 < len(args) && args[i+2] == tmpDir {
			workdirBindIdx = i
		}
		if arg == "--ro-bind" && i+2 < len(args) && args[i+2] == denyPath {
			denyBindIdx = i
		}
	}

	assert.Greater(t, workdirBindIdx, -1, "Should have writable bind for working directory")
	assert.Greater(t, denyBindIdx, -1, "Should have read-only rebind for deny path")
	assert.Greater(t, denyBindIdx, workdirBindIdx, "Deny bind should come after writable bind")
}

func TestBubblewrapArgs_DenyWritePaths_NonExistent(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	// Deny a path that doesn't exist yet (should mount /dev/null to prevent creation)
	policy.DenyWritePaths = []string{"/tmp/nonexistent-deny-test-path"}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// Should have a /dev/null mount at the non-existent path
	foundDevNull := false
	for i, arg := range args {
		if arg == "--ro-bind" && i+1 < len(args) && args[i+1] == "/dev/null" &&
			i+2 < len(args) && args[i+2] == "/tmp/nonexistent-deny-test-path" {
			foundDevNull = true
			break
		}
	}
	assert.True(t, foundDevNull, "Should mount /dev/null at non-existent deny path")
}

func TestIntegrationExec_DenyWritePaths_NonExistent_CleansMountPointArtifacts(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not available")
	}

	workDir := t.TempDir()
	denyPath := filepath.Join(workDir, ".ghost-deny-file")

	policy := DefaultPolicy()
	policy.WorkDir = workDir
	policy.DenyWritePaths = []string{denyPath}

	err := policy.Exec(context.Background(), "/bin/echo", "hello")
	require.NoError(t, err)

	_, statErr := os.Stat(denyPath)
	assert.True(t, os.IsNotExist(statErr),
		"non-existent deny path mount point should be cleaned up after command execution")
}

func TestCleanupAfterCommand_RemovesTrackedEmptyFileMountPoint(t *testing.T) {
	workDir := t.TempDir()
	mountPoint := filepath.Join(workDir, ".ghost-deny-file")
	require.NoError(t, os.WriteFile(mountPoint, nil, 0o644))

	recordBwrapMountPoint(mountPoint)
	cleanupAfterCommand()

	_, err := os.Stat(mountPoint)
	assert.True(t, os.IsNotExist(err), "cleanup should remove tracked empty file mount points")
}

func TestCleanupAfterCommand_PreservesTrackedNonEmptyDirectory(t *testing.T) {
	workDir := t.TempDir()
	mountPoint := filepath.Join(workDir, "preserve-dir")
	require.NoError(t, os.MkdirAll(mountPoint, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mountPoint, "keep.txt"), []byte("keep"), 0o644))

	recordBwrapMountPoint(mountPoint)
	cleanupAfterCommand()

	info, err := os.Stat(mountPoint)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "cleanup should preserve non-empty directories")
}

func TestBubblewrapArgs_DenyReadPaths_Directory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretDir := filepath.Join(tmpDir, "secrets")
	require.NoError(t, os.MkdirAll(secretDir, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowAllReads = true
	policy.DenyReadPaths = []string{secretDir}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// Directory deny-read should use --tmpfs to hide the directory
	foundTmpfs := false
	for i, arg := range args {
		if arg == "--tmpfs" && i+1 < len(args) && args[i+1] == secretDir {
			foundTmpfs = true
			break
		}
	}
	assert.True(t, foundTmpfs, "Should use --tmpfs to hide denied read directory")
}

func TestBubblewrapArgs_DenyReadPaths_File(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret.txt")
	require.NoError(t, os.WriteFile(secretFile, []byte("secret"), 0o644))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowAllReads = true
	policy.DenyReadPaths = []string{secretFile}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// File deny-read should use --ro-bind /dev/null
	foundDevNull := false
	for i, arg := range args {
		if arg == "--ro-bind" && i+1 < len(args) && args[i+1] == "/dev/null" &&
			i+2 < len(args) && args[i+2] == secretFile {
			foundDevNull = true
			break
		}
	}
	assert.True(t, foundDevNull, "Should use --ro-bind /dev/null to hide denied read file")
}

func TestBubblewrapArgs_DenyReadPaths_NonExistent(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.AllowAllReads = true
	policy.DenyReadPaths = []string{"/tmp/nonexistent-deny-read-test-path"}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// Non-existent paths should be silently skipped
	for i, arg := range args {
		if (arg == "--tmpfs" || arg == "--ro-bind") && i+1 < len(args) {
			assert.NotContains(t, args[i+1], "nonexistent-deny-read",
				"Should not mount non-existent deny-read paths")
		}
	}
}

func TestDangerousWriteDenyPaths(t *testing.T) {
	t.Parallel()

	paths := DangerousWriteDenyPaths("/home/user/project", false)

	// Verify completeness: should contain all dangerous files, directories, and git paths
	expectedLen := len(DangerousFilesList()) + len(DangerousDirectoriesList()) + len(DangerousGitPaths(false))
	assert.Len(t, paths, expectedLen,
		"DangerousWriteDenyPaths should include all dangerous files, directories, and git paths")

	// Should include dangerous files
	assert.Contains(t, paths, "/home/user/project/.gitconfig")
	assert.Contains(t, paths, "/home/user/project/.bashrc")
	assert.Contains(t, paths, "/home/user/project/.zshrc")
	assert.Contains(t, paths, "/home/user/project/.mcp.json")

	// Should include dangerous directories
	assert.Contains(t, paths, "/home/user/project/.vscode")
	assert.Contains(t, paths, "/home/user/project/.idea")
	assert.Contains(t, paths, "/home/user/project/.claude/commands")
	assert.Contains(t, paths, "/home/user/project/.claude/agents")

	// Should include git paths
	assert.Contains(t, paths, "/home/user/project/.git/hooks")
	assert.Contains(t, paths, "/home/user/project/.git/config")
}

func TestDangerousWriteDenyPaths_AllowGitConfig(t *testing.T) {
	t.Parallel()

	paths := DangerousWriteDenyPaths("/home/user/project", true)

	// Should still have .git/hooks
	assert.Contains(t, paths, "/home/user/project/.git/hooks")

	// Should NOT have .git/config when allowGitConfig is true
	assert.NotContains(t, paths, "/home/user/project/.git/config")
}

func TestBubblewrapArgs_EnableWeakerNestedSandbox(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.EnableWeakerNestedSandbox = true

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// Should NOT mount /proc when in weaker nested sandbox mode (Docker)
	for i, arg := range args {
		if arg == "--proc" && i+1 < len(args) && args[i+1] == "/proc" {
			t.Error("Should not mount --proc /proc in EnableWeakerNestedSandbox mode")
		}
	}

	// Should still have --dev /dev
	foundDev := false
	for i, arg := range args {
		if arg == "--dev" && i+1 < len(args) && args[i+1] == "/dev" {
			foundDev = true
			break
		}
	}
	assert.True(t, foundDev, "Should still mount --dev /dev in EnableWeakerNestedSandbox mode")
}

func TestBubblewrapArgs_NormalProcMount(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// Should mount /proc in normal mode
	foundProc := false
	for i, arg := range args {
		if arg == "--proc" && i+1 < len(args) && args[i+1] == "/proc" {
			foundProc = true
			break
		}
	}
	assert.True(t, foundProc, "Should mount --proc /proc in normal mode")
}

func TestDetectWSLVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{"native linux", "Linux version 6.5.0-44-generic (buildd@lcy02-amd64-080)", ""},
		{"WSL2 explicit", "Linux version 5.15.90.1-microsoft-standard-WSL2", "2"},
		{"WSL1 microsoft keyword", "Linux version 4.4.0-19041-Microsoft", "1"},
		{"WSL3 future", "Linux version 6.0.0-microsoft-standard-WSL3", "3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmp := filepath.Join(t.TempDir(), "version")
			require.NoError(t, os.WriteFile(tmp, []byte(tt.content), 0o644))
			assert.Equal(t, tt.expected, detectWSLVersion(tmp))
		})
	}
}

func TestDetectWSLVersion_MissingFile(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", detectWSLVersion("/nonexistent/path"))
}

func TestFindSymlinkInPath(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	realDir := filepath.Join(base, "real_dir")
	require.NoError(t, os.MkdirAll(realDir, 0o755))
	linkPath := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(realDir, linkPath))

	// Symlink within allowed write path: should be detected
	result := findSymlinkInPath(filepath.Join(linkPath, "sub", "file"), []string{base})
	assert.Equal(t, linkPath, result)

	// Symlink NOT within allowed write path: should return ""
	result = findSymlinkInPath(filepath.Join(linkPath, "sub", "file"), []string{"/other"})
	assert.Equal(t, "", result)
}

func TestFindSymlinkInPath_NoSymlinks(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	result := findSymlinkInPath(filepath.Join(sub, "file"), []string{base})
	assert.Equal(t, "", result)
}

func TestBubblewrapArgs_SymlinkDenyPath(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(realDir, 0o755))
	symlink := filepath.Join(base, ".claude")
	require.NoError(t, os.Symlink(realDir, symlink))

	policy := DefaultPolicy()
	policy.WorkDir = base
	policy.AllowAllReads = true
	policy.DenyWritePaths = []string{filepath.Join(symlink, "commands")}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)

	// Should mount /dev/null at the symlink to block it
	found := false
	for i, arg := range args {
		if arg == "--ro-bind" && i+1 < len(args) && args[i+1] == "/dev/null" && i+2 < len(args) && args[i+2] == symlink {
			found = true
			break
		}
	}
	assert.True(t, found, "Should mount /dev/null at symlink within writable path")
}

func TestCommandContext_ClaudeTmpdir(t *testing.T) {
	t.Run("respects CLAUDE_TMPDIR", func(t *testing.T) {
		original := os.Getenv("CLAUDE_TMPDIR")
		os.Setenv("CLAUDE_TMPDIR", "/custom/tmp")
		defer func() {
			if original != "" {
				os.Setenv("CLAUDE_TMPDIR", original)
			} else {
				os.Unsetenv("CLAUDE_TMPDIR")
			}
		}()

		policy := DefaultPolicy()
		policy.ProvideTmp = true

		cmd, err := policy.commandContext(context.Background(), "echo", "hello")
		require.NoError(t, err)

		var tmpdir string
		for _, e := range cmd.Env {
			if strings.HasPrefix(e, "TMPDIR=") {
				tmpdir = e
			}
		}
		assert.Equal(t, "TMPDIR=/custom/tmp", tmpdir,
			"TMPDIR should be set to CLAUDE_TMPDIR value")
	})

	t.Run("defaults to /tmp when CLAUDE_TMPDIR unset", func(t *testing.T) {
		original := os.Getenv("CLAUDE_TMPDIR")
		os.Unsetenv("CLAUDE_TMPDIR")
		defer func() {
			if original != "" {
				os.Setenv("CLAUDE_TMPDIR", original)
			}
		}()

		policy := DefaultPolicy()
		policy.ProvideTmp = true

		cmd, err := policy.commandContext(context.Background(), "echo", "hello")
		require.NoError(t, err)

		var tmpdir string
		for _, e := range cmd.Env {
			if strings.HasPrefix(e, "TMPDIR=") {
				tmpdir = e
			}
		}
		assert.Equal(t, "TMPDIR=/tmp", tmpdir,
			"TMPDIR should default to /tmp when CLAUDE_TMPDIR is unset")
	})
}

func TestIntegration_SandboxRuntime_EnvVar(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	policy := DefaultPolicy()

	cmd, err := policy.Command(context.Background(), "env")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err)

	lines := strings.Split(string(output), "\n")
	var found bool
	for _, line := range lines {
		if line == "SANDBOX_RUNTIME=1" {
			found = true
			break
		}
	}
	assert.True(t, found, "SANDBOX_RUNTIME=1 should be present in sandbox environment, got:\n%s", string(output))
}

func TestBubblewrapArgs_HidesEtcSshConfigD(t *testing.T) {
	t.Parallel()
	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.AllowAllReads = true
	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, nil)
	require.NoError(t, err)
	if PathExists("/etc/ssh/ssh_config.d") {
		found := false
		for i, arg := range args {
			if arg == "--tmpfs" && i+1 < len(args) && args[i+1] == "/etc/ssh/ssh_config.d" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should hide /etc/ssh/ssh_config.d with tmpfs when it exists")
	}
}

func TestBubblewrapArgs_WithBridge_BindsMountsSockets(t *testing.T) {
	t.Parallel()

	// Create a real bridge to get valid socket paths
	tcpLn1, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn1.Close()
	tcpLn2, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn2.Close()

	bridge, err := newLinuxNetworkBridge(tcpLn1.Addr().String(), tcpLn2.Addr().String())
	require.NoError(t, err)
	defer bridge.Close()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.NetworkProxy = proxy

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, bridge)
	require.NoError(t, err)

	// Should bind-mount the HTTP socket
	foundHTTP := false
	for i, arg := range args {
		if arg == "--bind" && i+2 < len(args) && args[i+1] == bridge.httpSocketPath && args[i+2] == bridge.httpSocketPath {
			foundHTTP = true
			break
		}
	}
	assert.True(t, foundHTTP, "Should bind-mount HTTP socket into sandbox")

	// Should bind-mount the SOCKS socket
	foundSOCKS := false
	for i, arg := range args {
		if arg == "--bind" && i+2 < len(args) && args[i+1] == bridge.socksSocketPath && args[i+2] == bridge.socksSocketPath {
			foundSOCKS = true
			break
		}
	}
	assert.True(t, foundSOCKS, "Should bind-mount SOCKS socket into sandbox")
}

func TestBubblewrapArgs_WithBridge_WrapsCommandWithSocat(t *testing.T) {
	t.Parallel()

	tcpLn1, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn1.Close()
	tcpLn2, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpLn2.Close()

	bridge, err := newLinuxNetworkBridge(tcpLn1.Addr().String(), tcpLn2.Addr().String())
	require.NoError(t, err)
	defer bridge.Close()

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.NetworkProxy = proxy

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"}, bridge)
	require.NoError(t, err)

	// Find the -- separator
	sepIdx := -1
	for i, arg := range args {
		if arg == "--" {
			sepIdx = i
			break
		}
	}
	require.Greater(t, sepIdx, -1, "Should have -- separator")

	// After --, the command should be wrapped with bash -c '...'
	afterSep := args[sepIdx+1:]
	require.GreaterOrEqual(t, len(afterSep), 3, "Should have bash -c 'script' after --")
	assert.Equal(t, "bash", afterSep[0], "Should use bash as wrapper")
	assert.Equal(t, "-c", afterSep[1], "Should use -c flag")

	// The script should contain socat commands
	script := afterSep[2]
	assert.Contains(t, script, "socat TCP-LISTEN:3128", "Script should set up HTTP socat on port 3128")
	assert.Contains(t, script, "socat TCP-LISTEN:1080", "Script should set up SOCKS socat on port 1080")
	assert.Contains(t, script, bridge.httpSocketPath, "Script should reference HTTP socket path")
	assert.Contains(t, script, bridge.socksSocketPath, "Script should reference SOCKS socket path")
	assert.Contains(t, script, `exec "$@"`, "Script should exec the original command")

	// The original command should follow as positional args
	assert.Equal(t, "_", afterSep[3], "Should have _ placeholder for $0")
	assert.Equal(t, "echo", afterSep[4], "Should have original command")
	assert.Equal(t, "hello", afterSep[5], "Should have original args")
}

func TestCommandContext_WithProxy_EnvVarsPointToLocalhost(t *testing.T) {
	if _, err := exec.LookPath("socat"); err != nil {
		t.Skip("socat not available")
	}

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	policy := DefaultPolicy()
	policy.NetworkProxy = proxy

	cmd, err := policy.commandContext(context.Background(), "echo", "hello")
	require.NoError(t, err)

	// Env should have proxy vars pointing to localhost:3128/1080
	var httpProxy, allProxy string
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "HTTP_PROXY=") {
			httpProxy = e
		}
		if strings.HasPrefix(e, "ALL_PROXY=") {
			allProxy = e
		}
	}
	assert.Equal(t, "HTTP_PROXY=http://localhost:3128", httpProxy,
		"HTTP_PROXY should point to localhost:3128 inside sandbox")
	assert.Equal(t, "ALL_PROXY=socks5h://localhost:1080", allProxy,
		"ALL_PROXY should point to localhost:1080 inside sandbox")
}

func TestCommandContext_WithProxy_BridgeCleanedUpOnContextCancel(t *testing.T) {
	if _, err := exec.LookPath("socat"); err != nil {
		t.Skip("socat not available")
	}

	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	ctx, cancel := context.WithCancel(context.Background())

	policy := DefaultPolicy()
	policy.NetworkProxy = proxy

	cmd, err := policy.commandContext(ctx, "echo", "hello")
	require.NoError(t, err)
	_ = cmd // don't need to run it

	// Find the bridge socket directory from the bwrap arguments.
	// The cmd.Args contains --bind <socket-path> <socket-path> entries.
	var socketDir string
	for i, arg := range cmd.Args {
		if arg == "--bind" && i+1 < len(cmd.Args) && strings.Contains(cmd.Args[i+1], "claude-bridge-") {
			socketDir = filepath.Dir(cmd.Args[i+1])
			break
		}
	}
	require.NotEmpty(t, socketDir, "should find bridge socket directory in bwrap args")

	// Verify socket directory exists before cancel
	_, err = os.Stat(socketDir)
	require.NoError(t, err, "bridge socket directory should exist before context cancel")

	// Cancel the context, which should trigger bridge cleanup
	cancel()

	// Poll for cleanup (avoid flaky time.Sleep)
	require.Eventually(t, func() bool {
		_, err := os.Stat(socketDir)
		return os.IsNotExist(err)
	}, 2*time.Second, 10*time.Millisecond,
		"bridge socket directory should be removed after context cancel")
}

func TestIntegration_NetworkBridge_CurlThroughProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := exec.LookPath("socat"); err != nil {
		t.Skip("socat not available")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not available")
	}

	// Create a proxy that allows everything
	proxy, err := NewNetworkProxy(nil)
	require.NoError(t, err)
	defer proxy.Close()

	// Start a simple HTTP test server on localhost
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 1024)
				conn.Read(buf)
				conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 13\r\n\r\nhello sandbox"))
			}()
		}
	}()

	policy := DefaultPolicy()
	policy.NetworkProxy = proxy

	// Use python to make an HTTP request through the proxy
	pythonPath, err := findPython()
	if err != nil {
		t.Skip("python not available")
	}
	pPolicy := pythonPolicy()
	pPolicy.NetworkProxy = proxy

	// Clear NO_PROXY so Python routes through the HTTP proxy instead of
	// connecting directly. Direct connections fail inside the isolated
	// network namespace (--unshare-net).
	script := fmt.Sprintf(
		`import os; os.environ['NO_PROXY']=''; os.environ['no_proxy']=''; `+
			`import urllib.request; print(urllib.request.urlopen("http://%s").read().decode())`,
		ln.Addr().String(),
	)

	cmd, err := pPolicy.Command(context.Background(), pythonPath, "-c", script)
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	t.Logf("Output: %s", string(output))
	require.NoError(t, err, "curl through proxy should succeed, output: %s", string(output))
	assert.Contains(t, string(output), "hello sandbox")
}
