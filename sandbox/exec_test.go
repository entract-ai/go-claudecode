package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandReturnsCmd(t *testing.T) {
	policy := DefaultPolicy()

	cmd, err := policy.Command(context.Background(), "echo", "hello")
	require.NoError(t, err)
	require.NotNil(t, cmd)

	// Should be able to set stdout
	cmd.Stdout = os.Stdout
}

func TestCommandContextWithTimeout(t *testing.T) {
	policy := DefaultPolicy()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// sleep is in /usr/bin on macOS and /bin on Linux, both should be in DefaultPolicy
	cmd, err := policy.Command(ctx, "sleep", "10")
	require.NoError(t, err)

	err = cmd.Run()
	assert.Error(t, err)
	// Should have timed out or been killed
	// On Linux: "signal: killed", on macOS: "signal: abort trap" or "signal: killed"
	errMsg := err.Error()
	assert.True(t, contains(errMsg, "signal"), "expected signal error, got: %s", errMsg)
}

func TestCommandNilPolicy(t *testing.T) {
	var policy *Policy
	cmd, err := policy.Command(context.Background(), "echo", "hi")
	require.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "policy must not be nil")
}

func TestCommandEmptyName(t *testing.T) {
	policy := DefaultPolicy()
	cmd, err := policy.Command(context.Background(), "", "arg")
	require.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "command name must not be empty")
}

func TestIntegrationEchoCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	policy := DefaultPolicy()

	cmd, err := policy.Command(context.Background(), "echo", "hello", "from", "sandbox")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), "hello from sandbox")
}

func TestIntegrationNetworkBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	// Python3 is required for tests (minimum 3.11)
	pythonPath, err := findPython()
	require.NoError(t, err, "python3 is required for integration tests (minimum 3.11)")

	policy := pythonPolicy()
	policy.AllowNetwork = false

	// Try to make a network request
	cmd, err := policy.Command(context.Background(), pythonPath, "-c",
		"import urllib.request; urllib.request.urlopen('http://example.com', timeout=1)")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	assert.Error(t, err, "network request should fail when network is blocked")

	// The exact error varies by platform, but should contain some network-related error
	outputStr := string(output)
	t.Logf("Output: %s", outputStr)
	// Either URLError, connection error, or other network-related failure
}

func TestIntegrationSSHWriteBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err, "failed to get home directory")
	sshDir := filepath.Join(homeDir, ".ssh")

	// Ensure ~/.ssh exists for this test
	err = os.MkdirAll(sshDir, 0o700)
	require.NoError(t, err, "failed to create ~/.ssh directory")

	pythonPath, err := findPython()
	require.NoError(t, err, "python3 is required for integration tests (minimum 3.11)")

	// Use Python policy but do NOT mount ~/.ssh directory.
	// This tests that the sandbox properly blocks write access to sensitive paths.
	policy := pythonPolicy()

	testFile := filepath.Join(sshDir, "test_sandbox_write.txt")

	cmd, err := policy.Command(context.Background(), pythonPath, "-c",
		"with open('"+testFile+"', 'w') as f: f.write('test')")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// The sandbox MUST block write access to ~/.ssh as it's not mounted
	require.Error(t, err, "Sandbox must block write access to %s (~/.ssh not mounted)", testFile)

	// Verify we get a sandbox denial error
	// macOS: "not permitted" or "unable to load libxcrun"
	// Linux: "FileNotFoundError" or "No such file or directory"
	require.Truef(t,
		strings.Contains(outputStr, "not permitted") ||
			strings.Contains(outputStr, "unable to load libxcrun") ||
			strings.Contains(outputStr, "FileNotFoundError") ||
			strings.Contains(outputStr, "No such file or directory"),
		"Expected sandbox denial when writing to unmounted path %s, got: %s",
		testFile, outputStr,
	)

	// Verify the file was NOT created
	_, statErr := os.Stat(testFile)
	require.True(t, os.IsNotExist(statErr), "Security failure: file was created in unmounted path %s", testFile)
}

func TestIntegrationWorkingDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("hello world"), 0o644))

	policy := DefaultPolicy()
	// Set working directory for the sandboxed command using Policy.WorkDir
	// This tests that the sandbox properly sets the working directory
	policy.WorkDir = tmpDir

	// Read the file we created using a relative path
	cmd, err := policy.Command(context.Background(), "cat", "test.txt")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(output))
}

func TestExecWithInheritedStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	policy := DefaultPolicy()

	// This should succeed with no output captured (goes to os.Stdout)
	err := policy.Exec(context.Background(), "echo", "test")
	require.NoError(t, err)
}

// Helper functions

func findPython() (string, error) {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("python not found in PATH")
}

// pythonPolicy returns a Policy configured to run Python interpreters.
// On macOS: mounts Homebrew paths for Python dependencies.
// On Linux: uses system Python (no additional mounts needed).
func pythonPolicy() *Policy {
	policy := DefaultPolicy()

	if runtime.GOOS == "darwin" {
		// Homebrew on macOS installs to /opt/homebrew (Apple Silicon) or /usr/local (Intel)
		// These paths must be mounted for Python to find its libraries and modules.
		policy.ReadOnlyMounts = append(policy.ReadOnlyMounts,
			Mount{Source: "/opt", Target: "/opt"},
			Mount{Source: "/usr/local", Target: "/usr/local"},
		)
	}
	// On Linux: system Python is already available via DefaultPolicy's system mounts

	return policy
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestSeatbeltBasePolicy_NoBsdSbImport(t *testing.T) {
	t.Parallel()

	// Read the seatbelt policy file directly to verify it doesn't import bsd.sb
	policyContent, err := os.ReadFile("seatbelt_base_policy.sbpl")
	require.NoError(t, err)

	policyStr := string(policyContent)
	// Check for actual import directive, not comments mentioning it
	assert.NotContains(t, policyStr, `(import "`,
		"Seatbelt policy must not import any external profiles (bsd.sb or otherwise)")
}

func TestSeatbeltBasePolicy_NoUnconditionalTrustd(t *testing.T) {
	t.Parallel()

	policyContent, err := os.ReadFile("seatbelt_base_policy.sbpl")
	require.NoError(t, err)

	policyStr := string(policyContent)
	// Check that trustd.agent is not in an (allow mach-lookup ...) rule.
	// It may appear in comments explaining that it's conditionally added.
	assert.NotContains(t, policyStr, `(global-name "com.apple.trustd.agent")`,
		"Seatbelt base policy must not have an allow rule for trustd.agent (should be added conditionally by exec_darwin.go)")
}

func TestSandboxPolicyGeneration(t *testing.T) {
	policy := DefaultPolicy()

	// Verify policy defaults
	assert.True(t, policy.ProvideTmp)
	assert.False(t, policy.AllowNetwork)
	assert.NotEmpty(t, policy.ReadOnlyMounts)

	// Create a command to inspect generated args
	cmd, err := policy.Command(context.Background(), "echo", "hello")
	require.NoError(t, err)
	require.NotNil(t, cmd)

	// Verify appropriate sandbox tool is being used (platform-dependent)
	// On macOS: sandbox-exec, on Linux: bwrap
	if _, err := exec.LookPath("sandbox-exec"); err == nil {
		// macOS
		assert.Equal(t, "/usr/bin/sandbox-exec", cmd.Path)

		// Find and log the policy string (3rd argument after sandbox-exec and -p)
		if len(cmd.Args) >= 3 && cmd.Args[1] == "-p" {
			policyStr := cmd.Args[2]
			t.Logf("Generated policy:\n%s", policyStr)
		}

		// Count -D parameters (macOS-specific)
		dParamCount := 0
		for _, arg := range cmd.Args {
			if len(arg) > 2 && arg[:2] == "-D" {
				dParamCount++
			}
		}

		// Should have parameters for all mounts plus working dir plus temp dir
		expectedMin := len(policy.ReadOnlyMounts) + 1 + 1 // mounts + workdir + tmpdir
		assert.GreaterOrEqual(t, dParamCount, expectedMin,
			"Should have at least %d -D parameters for mounts", expectedMin)
	} else if _, err := exec.LookPath("bwrap"); err == nil {
		// Linux
		assert.Contains(t, cmd.Path, "bwrap")
		t.Logf("Using bubblewrap with args: %v", cmd.Args)
	} else {
		require.Fail(t, "No sandbox tool available - need either sandbox-exec (macOS) or bwrap (Linux)")
	}
}

func TestFilterEnvVar(t *testing.T) {
	tests := []struct {
		name     string
		env      []string
		varName  string
		expected []string
	}{
		{
			name:     "removes single occurrence",
			env:      []string{"PATH=/usr/bin", "TMPDIR=/var/folders", "HOME=/home/user"},
			varName:  "TMPDIR",
			expected: []string{"PATH=/usr/bin", "HOME=/home/user"},
		},
		{
			name:     "removes multiple occurrences",
			env:      []string{"TMPDIR=/var/folders", "PATH=/usr/bin", "TMPDIR=/tmp"},
			varName:  "TMPDIR",
			expected: []string{"PATH=/usr/bin"},
		},
		{
			name:     "no match leaves env unchanged",
			env:      []string{"PATH=/usr/bin", "HOME=/home/user"},
			varName:  "TMPDIR",
			expected: []string{"PATH=/usr/bin", "HOME=/home/user"},
		},
		{
			name:     "empty env returns empty",
			env:      []string{},
			varName:  "TMPDIR",
			expected: []string{},
		},
		{
			name:     "partial match not removed",
			env:      []string{"TMPDIR_OLD=/old", "TMPDIR=/new", "MY_TMPDIR=/other"},
			varName:  "TMPDIR",
			expected: []string{"TMPDIR_OLD=/old", "MY_TMPDIR=/other"},
		},
		{
			name:     "case sensitive",
			env:      []string{"tmpdir=/lowercase", "TMPDIR=/uppercase"},
			varName:  "TMPDIR",
			expected: []string{"tmpdir=/lowercase"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := filterEnvVar(tc.env, tc.varName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestBuildEnv(t *testing.T) {
	t.Run("sets TMPDIR when tmpDir provided", func(t *testing.T) {
		policy := &Policy{}
		env := buildEnv(policy, "/sandbox/tmp")

		// Find TMPDIR in result
		var found bool
		for _, e := range env {
			if strings.HasPrefix(e, "TMPDIR=") {
				assert.Equal(t, "TMPDIR=/sandbox/tmp", e)
				found = true
			}
		}
		assert.True(t, found, "TMPDIR should be set")

		// Count TMPDIRs - should be exactly one
		count := 0
		for _, e := range env {
			if strings.HasPrefix(e, "TMPDIR=") {
				count++
			}
		}
		assert.Equal(t, 1, count, "should have exactly one TMPDIR")
	})

	t.Run("no TMPDIR when tmpDir empty", func(t *testing.T) {
		originalTmpdir := os.Getenv("TMPDIR")
		os.Setenv("TMPDIR", "/original/tmp")
		defer func() {
			if originalTmpdir != "" {
				os.Setenv("TMPDIR", originalTmpdir)
			} else {
				os.Unsetenv("TMPDIR")
			}
		}()

		policy := &Policy{}
		env := buildEnv(policy, "")

		// Should preserve original TMPDIR
		var found bool
		for _, e := range env {
			if e == "TMPDIR=/original/tmp" {
				found = true
			}
		}
		assert.True(t, found, "original TMPDIR should be preserved when no sandbox tmp")
	})

	t.Run("custom env vars applied", func(t *testing.T) {
		policy := &Policy{
			Env: map[string]string{
				"VIRTUAL_ENV": "/path/to/venv",
				"CUSTOM_VAR":  "custom_value",
			},
		}
		env := buildEnv(policy, "/sandbox/tmp")

		hasVirtualEnv := false
		hasCustomVar := false
		for _, e := range env {
			if e == "VIRTUAL_ENV=/path/to/venv" {
				hasVirtualEnv = true
			}
			if e == "CUSTOM_VAR=custom_value" {
				hasCustomVar = true
			}
		}
		assert.True(t, hasVirtualEnv, "VIRTUAL_ENV should be set")
		assert.True(t, hasCustomVar, "CUSTOM_VAR should be set")
	})

	t.Run("custom env vars override existing", func(t *testing.T) {
		// Save and restore PATH
		originalPath := os.Getenv("PATH")
		defer os.Setenv("PATH", originalPath)

		os.Setenv("PATH", "/original/path")
		policy := &Policy{
			Env: map[string]string{
				"PATH": "/custom/path:/original/path",
			},
		}
		env := buildEnv(policy, "")

		// Count PATHs and verify the value
		var paths []string
		for _, e := range env {
			if strings.HasPrefix(e, "PATH=") {
				paths = append(paths, e)
			}
		}
		assert.Len(t, paths, 1, "should have exactly one PATH")
		assert.Equal(t, "PATH=/custom/path:/original/path", paths[0])
	})
}

func TestIntegrationHeredocInSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	pythonPath, err := findPython()
	require.NoError(t, err, "python3 is required for integration tests")

	policy := pythonPolicy()

	// Test that heredocs work in the sandbox by running a Python script via bash
	// This specifically tests that TMPDIR is properly set and writable
	cmd, err := policy.Command(context.Background(), "/bin/bash", "-c",
		pythonPath+" << 'EOF'\nprint('heredoc works')\nEOF")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "heredoc should work in sandbox, output: %s", string(output))
	assert.Contains(t, string(output), "heredoc works")
}

func TestIntegrationReadAccessScope(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	pythonPath, err := findPython()
	require.NoError(t, err, "python3 is required for integration tests (minimum 3.11)")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err, "failed to get home directory")

	// Create a test file in home directory (not in working dir)
	testFile := filepath.Join(homeDir, ".sandbox_read_test.txt")
	testContent := "secret content for read test"
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0o644))
	defer os.Remove(testFile)

	// Use Python policy but explicitly NOT mount HOME directory.
	// This tests that the sandbox properly blocks access to unmounted paths.
	policy := pythonPolicy()

	// Try to read the file from home directory
	cmd, err := policy.Command(context.Background(), pythonPath, "-c",
		"with open('"+testFile+"', 'r') as f: print(f.read())")
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// The sandbox MUST block read access to files outside explicitly mounted directories.
	// Home directory is not in the mount list, so reading from it must fail.
	require.Error(t, err, "Sandbox must block read access to %s (home directory not mounted)", testFile)

	// Verify we get a sandbox denial error. The exact message depends on the platform:
	// - macOS (Seatbelt): "Operation not permitted" or "unable to load libxcrun"
	// - Linux (bubblewrap): "FileNotFoundError" or "No such file or directory"
	// We require that at least one of these specific errors is present.
	require.Truef(t,
		strings.Contains(outputStr, "not permitted") ||
			strings.Contains(outputStr, "unable to load libxcrun") ||
			strings.Contains(outputStr, "FileNotFoundError") ||
			strings.Contains(outputStr, "No such file or directory"),
		"Expected sandbox denial when reading unmounted path %s, got: %s",
		testFile, outputStr,
	)

	// Ensure the actual content is NOT accessible
	require.False(t, contains(outputStr, testContent),
		"SECURITY FAILURE: Sandbox allowed reading file content from unmounted path %s", testFile)
}
