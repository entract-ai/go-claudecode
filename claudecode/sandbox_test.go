package claudecode

import (
	"os"
	"path/filepath"
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

func TestNewClaudeCodeSandboxPolicy_NoOptions(t *testing.T) {
	workDir := t.TempDir()

	// Test that calling without options works (backward compatibility)
	policy, err := NewClaudeCodeSandboxPolicy(workDir)
	require.NoError(t, err)
	require.NotNil(t, policy)

	// Env should be nil when no virtualenv is configured
	assert.Nil(t, policy.Env, "policy.Env should be nil when no options specified")
}
