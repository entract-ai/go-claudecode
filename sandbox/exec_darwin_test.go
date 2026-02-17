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
	policy.DenyWritePaths = []string{tmpDir + `/evil"(allow file-write*)`}

	args, _, _, err := seatbeltArgs(policy, "echo", []string{"echo", "hello"})
	require.NoError(t, err)

	// The policy string must not contain the injected S-expression
	policyStr := args[2]
	assert.NotContains(t, policyStr, `evil"(allow file-write*)`,
		"Deny paths with special characters must not be interpolated into the policy string")
	assert.Contains(t, policyStr, `(param "DENY_WRITE_0")`,
		"Deny paths should use parameter indirection regardless of path content")
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

	// file-write* is a glob that covers all file-write operations including
	// file-write-unlink (rename/move). A single deny rule is sufficient.
	assert.Contains(t, policyStr, "deny file-write*",
		"Should have deny file-write* rule covering all write ops including unlink")
}
