//go:build linux

package sandbox

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBubblewrapArgs_SelectiveUnsharing(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	assert.Contains(t, args, "--unshare-ipc", "Should unshare IPC when UnshareIPC is true")
}

func TestBubblewrapArgs_UnshareUTS(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.UnshareUTS = true

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	assert.Contains(t, args, "--unshare-uts", "Should unshare UTS when UnshareUTS is true")
}

func TestBubblewrapArgs_NetworkAllowed(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.WorkDir = "/tmp"
	policy.AllowNetwork = true

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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
	denyPath := tmpDir + "/protected"
	require.NoError(t, os.MkdirAll(denyPath, 0o755))
	policy.DenyWritePaths = []string{denyPath}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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

func TestBubblewrapArgs_DenyReadPaths_Directory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretDir := tmpDir + "/secrets"
	require.NoError(t, os.MkdirAll(secretDir, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowAllReads = true
	policy.DenyReadPaths = []string{secretDir}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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
	secretFile := tmpDir + "/secret.txt"
	require.NoError(t, os.WriteFile(secretFile, []byte("secret"), 0o644))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowAllReads = true
	policy.DenyReadPaths = []string{secretFile}

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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
	assert.Contains(t, paths, "/home/user/project/.gitattributes")
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
	assert.Contains(t, paths, "/home/user/project/.git/info")
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

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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

	args, err := bubblewrapArgs(policy, "echo", []string{"echo", "hello"})
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
