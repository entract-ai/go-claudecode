//go:build darwin

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeatbeltPolicy_NoBsdSbImport(t *testing.T) {
	t.Parallel()

	// The base seatbelt policy should NOT import bsd.sb via an (import ...) directive.
	// Comments mentioning bsd.sb are expected (they explain why it's not imported).
	assert.NotContains(t, seatbeltBasePolicy, `(import "bsd.sb")`,
		"Seatbelt policy must not import bsd.sb; it includes permissions that undermine deny-by-default")
}

func TestSeatbeltPolicy_NoTrustdAgentByDefault(t *testing.T) {
	t.Parallel()

	// The base seatbelt policy should not have an (allow ...) rule for trustd.agent.
	// Comments mentioning it are expected (they explain why it's conditional).
	assert.NotContains(t, seatbeltBasePolicy, `(global-name "com.apple.trustd.agent")`,
		"Seatbelt base policy must not have an allow rule for trustd.agent")
}

func TestSeatbeltArgs_TrustdAgentConditional(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Default: no trustd.agent
	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2] // seatbeltPath, "-p", <policy string>
	assert.NotContains(t, policyStr, `(allow mach-lookup (global-name "com.apple.trustd.agent"))`,
		"Default policy should not have an allow rule for trustd.agent")

	// With EnableWeakerNetworkIsolation: should include trustd.agent
	policy2 := DefaultPolicy()
	policy2.WorkDir = tmpDir
	policy2.EnableWeakerNetworkIsolation = true
	args2, _, _, err := seatbeltArgs(policy2, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr2 := args2[2]
	assert.Contains(t, policyStr2, "com.apple.trustd.agent",
		"Policy with EnableWeakerNetworkIsolation should include trustd.agent")
}

func TestSeatbeltArgs_DenyWritePaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	denyPath := filepath.Join(tmpDir, "protected")
	require.NoError(t, os.MkdirAll(denyPath, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.DenyWritePaths = []string{denyPath}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Should contain deny file-write* rule using parameter indirection
	assert.Contains(t, policyStr, "deny file-write*",
		"Should have deny file-write* rule for deny path")
	assert.Contains(t, policyStr, `(param "DENY_WRITE_0")`,
		"Deny paths should use parameter indirection to prevent injection")

	// The deny path must NOT appear directly in the policy string (injection prevention)
	assert.NotContains(t, policyStr, denyPath,
		"Deny paths must use parameter indirection, not direct string interpolation")

	// The deny path should be passed as a -D parameter
	foundDenyParam := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DDENY_WRITE_") && strings.Contains(arg, denyPath) {
			foundDenyParam = true
			break
		}
	}
	assert.True(t, foundDenyParam, "Deny path should be passed as -D parameter")
}

func TestSeatbeltArgs_DenyPathInjection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	// Path with quote character that could break S-expression syntax if interpolated directly
	policy.DenyWritePaths = []string{filepath.Join(tmpDir, `evil"(allow file-write*)`)}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	// The policy string must not contain the injected S-expression
	policyStr := args[2]
	assert.NotContains(t, policyStr, `evil"(allow file-write*)`,
		"Deny paths with special characters must not be interpolated into the policy string")
	assert.Contains(t, policyStr, `(param "DENY_WRITE_0")`,
		"Deny paths should use parameter indirection regardless of path content")
}

func TestSeatbeltArgs_DenyAfterAllowOrdering(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	denyPath := filepath.Join(tmpDir, "protected")
	require.NoError(t, os.MkdirAll(denyPath, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.DenyWritePaths = []string{denyPath}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Deny rules must appear after allow rules in the policy string.
	// In Seatbelt, when a deny subpath is more specific than (nested within)
	// an allow subpath, deny takes precedence. But if someone refactors and
	// moves deny before allow, the more specific allow could override the deny.
	allowIdx := strings.Index(policyStr, "allow file-write*")
	denyIdx := strings.Index(policyStr, "deny file-write*")
	require.Greater(t, allowIdx, -1, "Should have allow file-write* rule")
	require.Greater(t, denyIdx, -1, "Should have deny file-write* rule")
	assert.Greater(t, denyIdx, allowIdx,
		"Deny rules must appear after allow rules in the Seatbelt policy")
}

func TestSeatbeltArgs_DenyReadPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	secretDir := filepath.Join(tmpDir, "secrets")
	require.NoError(t, os.MkdirAll(secretDir, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowAllReads = true
	policy.DenyReadPaths = []string{secretDir}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Should contain deny file-read* rule using parameter indirection
	assert.Contains(t, policyStr, "deny file-read*",
		"Should have deny file-read* rule for deny read path")
	assert.Contains(t, policyStr, `(param "DENY_READ_0")`,
		"Deny read paths should use parameter indirection")

	// The path should be passed as a -D parameter, not in the policy string
	assert.NotContains(t, policyStr, secretDir,
		"Deny read paths must use parameter indirection")

	foundDenyReadParam := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DDENY_READ_") && strings.Contains(arg, secretDir) {
			foundDenyReadParam = true
			break
		}
	}
	assert.True(t, foundDenyReadParam, "Deny read path should be passed as -D parameter")
}

func TestSeatbeltArgs_FileWriteUnlinkProtection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	gitHooksDir := filepath.Join(tmpDir, ".git", "hooks")
	require.NoError(t, os.MkdirAll(gitHooksDir, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.DenyWritePaths = []string{gitHooksDir}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// file-write* is a glob that covers all file-write operations including
	// file-write-unlink (rename/move). A single deny rule is sufficient.
	assert.Contains(t, policyStr, "deny file-write*",
		"Should have deny file-write* rule covering all write ops including unlink")
}

func TestSeatbeltArgs_AncestorUnlinkProtection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	gitHooksDir := filepath.Join(tmpDir, ".git", "hooks")
	require.NoError(t, os.MkdirAll(gitHooksDir, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.DenyWritePaths = []string{gitHooksDir}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Should have deny file-write-unlink rules for ancestor directories
	assert.Contains(t, policyStr, "deny file-write-unlink",
		"Should have deny file-write-unlink rule for ancestor directories")

	// Ancestor .git should be protected (between hooks and workdir)
	gitDir := filepath.Join(tmpDir, ".git")
	foundGitAncestor := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DDENY_ANCESTOR_") && strings.Contains(arg, gitDir) {
			foundGitAncestor = true
			break
		}
	}
	assert.True(t, foundGitAncestor,
		"Ancestor directory .git should be protected from unlink")
}

func TestSeatbeltArgs_AllowLocalhostOnly(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowLocalhostOnly = true

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Should use (local ip "*:*") patterns, not "localhost:*"
	assert.NotContains(t, policyStr, `"localhost:*"`,
		"AllowLocalhostOnly should not use localhost:* (misses IPv6 loopback)")
	assert.Contains(t, policyStr, `(local ip "*:*")`,
		"AllowLocalhostOnly should use (local ip \"*:*\") for IPv6 compatibility")

	// Should have network-outbound, network-bind, and network-inbound rules
	assert.Contains(t, policyStr, "allow network-outbound",
		"AllowLocalhostOnly should allow outbound network")
	assert.Contains(t, policyStr, "allow network-bind",
		"AllowLocalhostOnly should allow network bind")
	assert.Contains(t, policyStr, "allow network-inbound",
		"AllowLocalhostOnly should allow inbound network")

	// Should not use (remote ip ...) for outbound
	assert.NotContains(t, policyStr, "remote ip",
		"AllowLocalhostOnly should use (local ip) not (remote ip) for outbound")
}

func TestSeatbeltArgs_ProxyWithAllowLocalhostOnly(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Stub proxy — seatbeltArgs only reads HTTPAddr()/SOCKSAddr(), no real listeners needed.
	proxy := &NetworkProxy{
		httpAddr:  "http://127.0.0.1:18080",
		socksAddr: "127.0.0.1:18081",
		closed:    make(chan struct{}),
	}

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.NetworkProxy = proxy
	policy.AllowLocalhostOnly = true

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Proxy outbound rules should still be present
	assert.Contains(t, policyStr, "allow network-outbound",
		"Proxy branch should allow outbound to proxy ports")

	// AllowLocalhostOnly should add bind and inbound rules even with proxy
	assert.Contains(t, policyStr, "allow network-bind",
		"Proxy + AllowLocalhostOnly should allow network bind for localhost")
	assert.Contains(t, policyStr, "allow network-inbound",
		"Proxy + AllowLocalhostOnly should allow inbound for localhost")
	assert.Contains(t, policyStr, `(local ip "*:*")`,
		"Bind/inbound rules should use (local ip \"*:*\") for IPv6 compatibility")
}

func TestSeatbeltArgs_ProxyWithoutAllowLocalhostOnly(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Stub proxy — seatbeltArgs only reads HTTPAddr()/SOCKSAddr(), no real listeners needed.
	proxy := &NetworkProxy{
		httpAddr:  "http://127.0.0.1:18080",
		socksAddr: "127.0.0.1:18081",
		closed:    make(chan struct{}),
	}

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.NetworkProxy = proxy
	// AllowLocalhostOnly is false (default)

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Proxy outbound rules should be present
	assert.Contains(t, policyStr, "allow network-outbound",
		"Proxy branch should allow outbound to proxy ports")

	// Without AllowLocalhostOnly, bind and inbound should NOT be present
	assert.NotContains(t, policyStr, "allow network-bind",
		"Proxy without AllowLocalhostOnly should not allow bind")
	assert.NotContains(t, policyStr, "allow network-inbound",
		"Proxy without AllowLocalhostOnly should not allow inbound")
}

func TestSeatbelt_NestedSandboxExecFails(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowAllReads = true

	// Try to run sandbox-exec inside an already-sandboxed process.
	// macOS does not allow nested seatbelt sandboxing.
	cmd, err := policy.Command(
		context.Background(),
		"/usr/bin/sandbox-exec", "-p", `(version 1)(allow default)`, "--", "/bin/echo", "hello",
	)
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.Error(t, err, "nested sandbox-exec should fail, got output: %s", string(output))

	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "expected *exec.ExitError, got %T: %v", err, err)
	assert.Equal(t, 71, exitErr.ExitCode(), "nested sandbox-exec should fail with exit code 71 (sandbox_apply), output: %s", string(output))
}

func TestSeatbelt_DenyReadPathsBlocksSandboxExec(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.AllowAllReads = true
	policy.DenyReadPaths = []string{"/usr/bin/sandbox-exec"}

	// With sandbox-exec hidden via DenyReadPaths, the binary should not be accessible.
	cmd, err := policy.Command(
		context.Background(),
		"/bin/test", "-x", "/usr/bin/sandbox-exec",
	)
	require.NoError(t, err)

	output, err := cmd.CombinedOutput()
	require.Error(t, err, "sandbox-exec should not be accessible when in DenyReadPaths, output: %s", string(output))
}

func TestAncestorDirectories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		root     string
		expected []string
	}{
		{
			"two levels deep",
			"/workdir/.git/hooks",
			"/workdir",
			[]string{"/workdir/.git"},
		},
		{
			"three levels deep",
			"/workdir/.git/hooks/pre-commit",
			"/workdir",
			[]string{"/workdir/.git/hooks", "/workdir/.git"},
		},
		{
			"direct child",
			"/workdir/.bashrc",
			"/workdir",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ancestorDirectories(tt.path, tt.root)
			if tt.expected == nil {
				assert.Empty(t, got)
			} else {
				assert.ElementsMatch(t, tt.expected, got)
			}
		})
	}
}
