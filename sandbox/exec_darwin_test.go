//go:build darwin

package sandbox

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeatbeltPolicy_NoBsdSbImport(t *testing.T) {
	t.Parallel()

	// The base seatbelt policy should NOT import bsd.sb
	assert.NotContains(t, seatbeltBasePolicy, "bsd.sb",
		"Seatbelt policy must not import bsd.sb; it includes permissions that undermine deny-by-default")
}

func TestSeatbeltPolicy_NoTrustdAgentByDefault(t *testing.T) {
	t.Parallel()

	// The base seatbelt policy should NOT include trustd.agent
	assert.NotContains(t, seatbeltBasePolicy, "com.apple.trustd.agent",
		"Seatbelt policy must not unconditionally allow trustd.agent")
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
	assert.NotContains(t, policyStr, "com.apple.trustd.agent",
		"Default policy should not include trustd.agent")

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
	denyPath := tmpDir + "/protected"
	require.NoError(t, os.MkdirAll(denyPath, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.DenyWritePaths = []string{denyPath}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Should contain deny file-write* for the path
	assert.Contains(t, policyStr, "deny file-write*",
		"Should have deny file-write* rule for deny path")

	// Should contain deny file-write-unlink for the path (prevents rename bypass)
	assert.Contains(t, policyStr, "deny file-write-unlink",
		"Should have deny file-write-unlink rule for deny path")

	// The deny path should appear in the policy
	assert.True(t, strings.Contains(policyStr, denyPath),
		"Policy should reference the deny path")
}

func TestSeatbeltArgs_FileWriteUnlinkProtection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	gitHooksDir := tmpDir + "/.git/hooks"
	require.NoError(t, os.MkdirAll(gitHooksDir, 0o755))

	policy := DefaultPolicy()
	policy.WorkDir = tmpDir
	policy.DenyWritePaths = []string{gitHooksDir}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	policyStr := args[2]

	// Should have file-write-unlink deny for .git/hooks to prevent rename attack
	assert.Contains(t, policyStr, "file-write-unlink",
		"Should block file-write-unlink (rename/move) on deny paths")
}
