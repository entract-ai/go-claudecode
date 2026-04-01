package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// tryAppend tests
// ---------------------------------------------------------------------------

func TestTryAppend(t *testing.T) {
	t.Run("append to existing file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "test.jsonl")
		require.NoError(t, os.WriteFile(f, []byte("line1\n"), 0o644))

		ok, err := tryAppend(f, "line2\n")
		require.NoError(t, err)
		assert.True(t, ok)

		data, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.Equal(t, "line1\nline2\n", string(data))
	})

	t.Run("missing file returns false", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "nonexistent.jsonl")
		ok, err := tryAppend(f, "data\n")
		require.NoError(t, err)
		assert.False(t, ok)
		_, statErr := os.Stat(f)
		assert.True(t, os.IsNotExist(statErr), "file should not have been created")
	})

	t.Run("missing parent dir returns false", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "nonexistent", "file.jsonl")
		ok, err := tryAppend(f, "data\n")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("zero byte file returns false", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "stub.jsonl")
		require.NoError(t, os.WriteFile(f, []byte(""), 0o644))

		ok, err := tryAppend(f, "data\n")
		require.NoError(t, err)
		assert.False(t, ok)

		data, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.Equal(t, "", string(data), "file should not have been modified")
	})

	t.Run("multiple appends land in order", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "test.jsonl")
		require.NoError(t, os.WriteFile(f, []byte("line1\n"), 0o644))

		ok, err := tryAppend(f, "line2\n")
		require.NoError(t, err)
		assert.True(t, ok)

		ok, err = tryAppend(f, "line3\n")
		require.NoError(t, err)
		assert.True(t, ok)

		data, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.Equal(t, "line1\nline2\nline3\n", string(data))
	})
}

// ---------------------------------------------------------------------------
// RenameSession tests
// ---------------------------------------------------------------------------

func TestRenameSession(t *testing.T) {
	t.Run("invalid session ID", func(t *testing.T) {
		configDir := setupConfigDir(t)
		err := RenameSession("not-a-uuid", "title", withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid session ID")

		err = RenameSession("", "title", withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid session ID")
	})

	t.Run("empty title", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = RenameSession(sid, "", WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title must be non-empty")

		err = RenameSession(sid, "   ", WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title must be non-empty")

		err = RenameSession(sid, "\n\t", WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title must be non-empty")
	})

	t.Run("session not found", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		makeProjectDir(t, configDir, realPath)

		sid := nextTestUUID(t)
		err = RenameSession(sid, "title", WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("no projects dir", func(t *testing.T) {
		configDir := filepath.Join(t.TempDir(), "nonexistent")
		sid := nextTestUUID(t)
		err := RenameSession(sid, "title", withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("appends custom title entry", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = RenameSession(sid, "My New Title", WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))

		// Last line should be the custom-title entry
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
		assert.Equal(t, "custom-title", entry["type"])
		assert.Equal(t, "My New Title", entry["customTitle"])
		assert.Equal(t, sid, entry["sessionId"])
	})

	t.Run("title trimmed before storing", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = RenameSession(sid, "  Trimmed Title  ", WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
		assert.Equal(t, "Trimmed Title", entry["customTitle"])
	})

	t.Run("last rename wins via ListSessions", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "original"})

		require.NoError(t, RenameSession(sid, "First Title", WithSessionDirectory(projectPath), withTestConfigDir(configDir)))
		require.NoError(t, RenameSession(sid, "Second Title", WithSessionDirectory(projectPath), withTestConfigDir(configDir)))
		require.NoError(t, RenameSession(sid, "Final Title", WithSessionDirectory(projectPath), withTestConfigDir(configDir)))

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		require.NotNil(t, sessions[0].CustomTitle)
		assert.Equal(t, "Final Title", *sessions[0].CustomTitle)
		assert.Equal(t, "Final Title", sessions[0].Summary)
	})

	t.Run("search all projects", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projDir := makeProjectDir(t, configDir, "/some/project")
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err := RenameSession(sid, "Found Without Dir", withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
		assert.Equal(t, "Found Without Dir", entry["customTitle"])
	})

	t.Run("skips zero byte stub", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projA := makeProjectDir(t, configDir, "/aaa/project")
		projZ := makeProjectDir(t, configDir, "/zzz/project")

		sid := nextTestUUID(t)
		// 0-byte stub in first dir
		stubPath := filepath.Join(projA, sid+".jsonl")
		require.NoError(t, os.WriteFile(stubPath, []byte(""), 0o644))
		// Real file in second dir
		_, realPath := makeSessionFile(t, projZ, sid, makeSessionOpts{firstPrompt: "real"})

		err := RenameSession(sid, "New Title", withTestConfigDir(configDir))
		require.NoError(t, err)

		// Stub untouched
		stubData, err := os.ReadFile(stubPath)
		require.NoError(t, err)
		assert.Equal(t, "", string(stubData))

		// Real file has the entry
		realData, err := os.ReadFile(realPath)
		require.NoError(t, err)
		assert.Contains(t, string(realData), `"customTitle":"New Title"`)
	})

	t.Run("compact JSON format", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = RenameSession(sid, "Title", WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		// Compact JSON: no spaces after : or ,
		expected := `{"type":"custom-title","customTitle":"Title","sessionId":"` + sid + `"}`
		assert.Equal(t, expected, lines[len(lines)-1])
	})
}

// splitNonEmpty splits a string by newlines and filters out empty strings.
func splitNonEmpty(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
