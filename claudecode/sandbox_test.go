package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/bpowers/go-claudecode/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClaudeCodeSandboxPolicy(t *testing.T) {
	workDir := t.TempDir()

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)
	require.NotNil(t, policy)

	// Verify WorkDir is set
	assert.Equal(t, workDir, policy.WorkDir)

	// Verify network is allowed (required for Anthropic API)
	assert.True(t, policy.AllowNetwork)

	// Verify AllowAllReads is enabled (CLI needs to read system files)
	assert.True(t, policy.AllowAllReads)

	// Verify ProvideTmp is enabled (from DefaultPolicy)
	assert.True(t, policy.ProvideTmp)

	// Check that we have some read-only mounts from DefaultPolicy
	assert.NotEmpty(t, policy.ReadOnlyMounts)

	// Check for system directories
	hasUsr := false
	hasBin := false
	for _, m := range policy.ReadOnlyMounts {
		if m.Source == "/usr" {
			hasUsr = true
		}
		if m.Source == "/bin" {
			hasBin = true
		}
	}
	assert.True(t, hasUsr, "should have /usr mounted read-only")
	assert.True(t, hasBin, "should have /bin mounted read-only")
}

func TestNewClaudeCodeSandboxPolicy_DenyWritePaths(t *testing.T) {
	workDir := t.TempDir()

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)

	// DenyWritePaths should be populated with dangerous paths
	assert.NotEmpty(t, policy.DenyWritePaths, "DenyWritePaths should be set")

	// Should contain expected paths
	assert.Contains(t, policy.DenyWritePaths, filepath.Join(workDir, ".gitconfig"))
	assert.Contains(t, policy.DenyWritePaths, filepath.Join(workDir, ".bashrc"))
	assert.Contains(t, policy.DenyWritePaths, filepath.Join(workDir, ".git/hooks"))
	assert.Contains(t, policy.DenyWritePaths, filepath.Join(workDir, ".vscode"))
}

func TestNewClaudeCodeSandboxPolicy_DenyWritePaths_IncludesNestedDangerousPaths(t *testing.T) {
	workDir := t.TempDir()

	nestedGitDir := filepath.Join(workDir, "subproject", ".git")
	nestedHooks := filepath.Join(nestedGitDir, "hooks")
	nestedConfig := filepath.Join(nestedGitDir, "config")

	require.NoError(t, os.MkdirAll(nestedHooks, 0o755))
	require.NoError(t, os.WriteFile(nestedConfig, []byte("[core]\n"), 0o644))

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)

	assert.Contains(t, policy.DenyWritePaths, nestedHooks)
	assert.Contains(t, policy.DenyWritePaths, nestedConfig)
}

func TestNewClaudeCodeSandboxPolicy_HomeDirMounts(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	workDir := t.TempDir()

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)

	// ~/.claude should always be in read-write mounts (created if it doesn't exist)
	claudeConfig := filepath.Join(home, ".claude")
	hasClaudeConfig := false
	for _, m := range policy.ReadWriteMounts {
		if m.Source == claudeConfig {
			hasClaudeConfig = true
			break
		}
	}
	assert.True(t, hasClaudeConfig, "should have ~/.claude mounted read-write")

	// Check for ~/.local/bin in read-only mounts if it exists
	localBin := filepath.Join(home, ".local/bin")
	if sandbox.PathExists(localBin) {
		hasLocalBin := false
		for _, m := range policy.ReadOnlyMounts {
			if m.Source == localBin {
				hasLocalBin = true
				break
			}
		}
		assert.True(t, hasLocalBin, "should have ~/.local/bin mounted read-only when it exists")
	}

	// Check for ~/.local/share/claude in read-only mounts if it exists
	claudeLocalShare := filepath.Join(home, ".local/share/claude")
	if sandbox.PathExists(claudeLocalShare) {
		hasClaudeLocalShare := false
		for _, m := range policy.ReadOnlyMounts {
			if m.Source == claudeLocalShare {
				hasClaudeLocalShare = true
				break
			}
		}
		assert.True(t, hasClaudeLocalShare, "should have ~/.local/share/claude mounted read-only when it exists")
	}
}

func TestNewClaudeCodeSandboxPolicy_OptMounts(t *testing.T) {
	workDir := t.TempDir()

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)

	// Check for /opt in read-only mounts if it exists (Homebrew)
	if sandbox.PathExists("/opt") {
		hasOpt := false
		for _, m := range policy.ReadOnlyMounts {
			if m.Source == "/opt" {
				hasOpt = true
				break
			}
		}
		assert.True(t, hasOpt, "should have /opt mounted read-only when it exists")
	}
}

func TestNewClaudeCodeSandboxPolicy_WithVirtualEnv(t *testing.T) {
	workDir := t.TempDir()

	// Create a mock virtualenv structure
	venvDir := filepath.Join(workDir, "venv")
	venvBinDir := filepath.Join(venvDir, "bin")
	require.NoError(t, os.MkdirAll(venvBinDir, 0o755))

	// Create a mock python executable in the venv
	mockPython := filepath.Join(venvBinDir, "python")
	require.NoError(t, os.WriteFile(mockPython, []byte("#!/bin/bash\necho mock python"), 0o755))

	policy, err := NewClaudeCodeSandboxPolicy(workDir, SandboxOptions{
		VirtualEnvPath: venvDir,
	})
	require.NoError(t, err)
	require.NotNil(t, policy)

	// Verify virtualenv directory is in read-only mounts
	hasVenvMount := false
	for _, m := range policy.ReadOnlyMounts {
		if m.Source == venvDir {
			hasVenvMount = true
			break
		}
	}
	assert.True(t, hasVenvMount, "virtualenv directory should be mounted read-only")

	// Verify VIRTUAL_ENV is set in policy.Env
	require.NotNil(t, policy.Env, "policy.Env should not be nil")
	assert.Equal(t, venvDir, policy.Env["VIRTUAL_ENV"], "VIRTUAL_ENV should be set to virtualenv path")

	// Verify PATH is modified to include virtualenv bin
	pathVal, ok := policy.Env["PATH"]
	require.True(t, ok, "PATH should be set in policy.Env")
	assert.Contains(t, pathVal, venvBinDir, "PATH should contain virtualenv bin directory")
	assert.True(t, len(pathVal) > len(venvBinDir)+1, "PATH should include original PATH after venv bin")
}

func TestNewClaudeCodeSandboxPolicy_VirtualEnvNotExists(t *testing.T) {
	workDir := t.TempDir()

	// Try to use a virtualenv that doesn't exist
	_, err := NewClaudeCodeSandboxPolicy(workDir, SandboxOptions{
		VirtualEnvPath: "/nonexistent/venv",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtualenv path does not exist")
}

func TestNewClaudeCodeSandboxPolicy_VirtualEnvNoBinDir(t *testing.T) {
	workDir := t.TempDir()

	// Create a virtualenv directory without bin subdirectory
	venvDir := filepath.Join(workDir, "bad_venv")
	require.NoError(t, os.MkdirAll(venvDir, 0o755))

	_, err := NewClaudeCodeSandboxPolicy(workDir, SandboxOptions{
		VirtualEnvPath: venvDir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bin directory does not exist")
}

func TestNewClaudeCodeSandboxPolicy_ClaudeCliTmpDir(t *testing.T) {
	workDir := t.TempDir()

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)

	// The CLI hardcodes /tmp/claude-<UID>/ (not os.TempDir()) for its per-user
	// tmp directory. On macOS, /tmp is a symlink to /private/tmp, and Seatbelt
	// operates on canonical paths, so the resolved path must be used.
	expectedTmpDir := filepath.Join("/tmp", "claude-"+strconv.Itoa(os.Getuid()))
	if runtime.GOOS == "darwin" {
		if resolved, err := filepath.EvalSymlinks(expectedTmpDir); err == nil {
			expectedTmpDir = resolved
		}
	}

	hasClaudeTmp := false
	for _, m := range policy.ReadWriteMounts {
		if m.Source == expectedTmpDir {
			hasClaudeTmp = true
			break
		}
	}
	assert.True(t, hasClaudeTmp, "should have /tmp/claude-<UID> mounted read-write, want %s", expectedTmpDir)
}

func TestClaudeCodeSandboxPolicy_CliTmpDirWritable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	workDir := t.TempDir()

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)

	// The CLI creates subdirectories under /tmp/claude-<UID>/ for internal
	// sandbox operations. Verify we can actually mkdir inside that path.
	uid := strconv.Itoa(os.Getuid())
	testSubdir := "sandbox-test-" + strconv.FormatInt(int64(os.Getpid()), 10)

	cmd, err := policy.Command(context.Background(), "/bin/bash", "-c",
		"mkdir -p /tmp/claude-"+uid+"/"+testSubdir+" && echo ok && rm -rf /tmp/claude-"+uid+"/"+testSubdir)
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "mkdir under /tmp/claude-<UID>/ should succeed in sandbox, output: %s", string(output))
	assert.Contains(t, string(output), "ok")
}

func TestNewClaudeCodeSandboxPolicy_NoOptions(t *testing.T) {
	workDir := t.TempDir()

	// Test that calling without options works (backward compatibility)
	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)
	require.NotNil(t, policy)

	// Env should be nil when no virtualenv is configured
	assert.Nil(t, policy.Env, "policy.Env should be nil when no options specified")
}

func TestNewClaudeCodeSandboxPolicy_DenyNestedSandbox(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("nested seatbelt prevention is macOS-only")
	}

	workDir := t.TempDir()

	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)

	assert.Contains(t, policy.DenyReadPaths, "/usr/bin/sandbox-exec")
}

func TestNewClaudeCodeSandboxPolicy_WithMplConfigDir(t *testing.T) {
	workDir := t.TempDir()
	mplDir := filepath.Join(workDir, "mplconfig")
	require.NoError(t, os.MkdirAll(mplDir, 0o755))

	policy, err := NewClaudeCodeSandboxPolicy(workDir, SandboxOptions{
		MplConfigDir: mplDir,
	})
	require.NoError(t, err)
	require.NotNil(t, policy)

	hasMplMount := false
	for _, m := range policy.ReadWriteMounts {
		if m.Source == mplDir {
			hasMplMount = true
			break
		}
	}
	assert.True(t, hasMplMount, "mpl config directory should be mounted read-write")

	require.NotNil(t, policy.Env)
	assert.Equal(t, mplDir, policy.Env["MPLCONFIGDIR"])
}

func TestNewClaudeCodeSandboxPolicy_MplConfigDirCreatedIfMissing(t *testing.T) {
	workDir := t.TempDir()
	mplDir := filepath.Join(workDir, "nonexistent", "mplconfig")

	policy, err := NewClaudeCodeSandboxPolicy(workDir, SandboxOptions{
		MplConfigDir: mplDir,
	})
	require.NoError(t, err)
	require.NotNil(t, policy)

	info, err := os.Stat(mplDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	hasMplMount := false
	for _, m := range policy.ReadWriteMounts {
		if m.Source == mplDir {
			hasMplMount = true
			break
		}
	}
	assert.True(t, hasMplMount, "mpl config directory should be mounted read-write")
}

func TestMplConfigDirInSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	workDir := t.TempDir()
	mplDir := filepath.Join(workDir, "mplconfig")
	require.NoError(t, os.MkdirAll(mplDir, 0o755))

	policy, err := NewClaudeCodeSandboxPolicy(workDir, SandboxOptions{
		MplConfigDir: mplDir,
	})
	require.NoError(t, err)

	cmd, err := policy.Command(context.Background(), "/bin/bash", "-c",
		"echo test-content > $MPLCONFIGDIR/fontlist.json && cat $MPLCONFIGDIR/fontlist.json")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "writing to MPLCONFIGDIR should succeed in sandbox, output: %s", string(output))
	assert.Contains(t, string(output), "test-content")
}
