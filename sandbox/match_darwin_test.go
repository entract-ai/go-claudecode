//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanDangerousWriteDenyPaths_CaseInsensitive(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, ".GitConfig"), nil, 0o644))

	paths, err := ScanDangerousWriteDenyPaths(base, false, 3)
	require.NoError(t, err)
	found := false
	for _, p := range paths {
		if filepath.Base(p) == ".GitConfig" {
			found = true
		}
	}
	assert.True(t, found, "Should find .GitConfig via case-insensitive match on macOS")
}

func TestMatchFilename_CaseInsensitive(t *testing.T) {
	t.Parallel()
	assert.True(t, matchFilename(".GitConfig", ".gitconfig"))
	assert.True(t, matchFilename(".BASHRC", ".bashrc"))
	assert.False(t, matchFilename(".gitconfig", ".bashrc"))
}
