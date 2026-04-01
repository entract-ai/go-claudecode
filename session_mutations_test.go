package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// ---------------------------------------------------------------------------
// TagSession tests
// ---------------------------------------------------------------------------

func TestTagSession(t *testing.T) {
	t.Run("invalid session ID", func(t *testing.T) {
		configDir := setupConfigDir(t)
		err := TagSession("not-a-uuid", strPtr("tag"), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid session ID")

		err = TagSession("", strPtr("tag"), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid session ID")
	})

	t.Run("empty tag", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = TagSession(sid, strPtr(""), WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tag must be non-empty")

		err = TagSession(sid, strPtr("   "), WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tag must be non-empty")
	})

	t.Run("session not found", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		makeProjectDir(t, configDir, realPath)

		sid := nextTestUUID(t)
		err = TagSession(sid, strPtr("tag"), WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("appends tag entry", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = TagSession(sid, strPtr("experiment"), WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
		assert.Equal(t, "tag", entry["type"])
		assert.Equal(t, "experiment", entry["tag"])
		assert.Equal(t, sid, entry["sessionId"])
	})

	t.Run("tag trimmed before storing", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = TagSession(sid, strPtr("  my-tag  "), WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
		assert.Equal(t, "my-tag", entry["tag"])
	})

	t.Run("nil clears tag", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		// Set a tag first, then clear it
		err = TagSession(sid, strPtr("original-tag"), WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)
		err = TagSession(sid, nil, WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		// Last entry is the clear
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
		assert.Equal(t, "tag", entry["type"])
		assert.Equal(t, "", entry["tag"])
		assert.Equal(t, sid, entry["sessionId"])
	})

	t.Run("last tag wins", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		require.NoError(t, TagSession(sid, strPtr("first"), WithSessionDirectory(projectPath), withTestConfigDir(configDir)))
		require.NoError(t, TagSession(sid, strPtr("second"), WithSessionDirectory(projectPath), withTestConfigDir(configDir)))
		require.NoError(t, TagSession(sid, strPtr("third"), WithSessionDirectory(projectPath), withTestConfigDir(configDir)))

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		var lastEntry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &lastEntry))
		assert.Equal(t, "third", lastEntry["tag"])

		// All three tag entries present
		tagCount := 0
		for _, line := range lines {
			if strings.Contains(line, `"type":"tag"`) {
				tagCount++
			}
		}
		assert.Equal(t, 3, tagCount)
	})

	t.Run("compact JSON format", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		err = TagSession(sid, strPtr("mytag"), WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		expected := `{"type":"tag","tag":"mytag","sessionId":"` + sid + `"}`
		assert.Equal(t, expected, lines[len(lines)-1])
	})

	t.Run("unicode sanitization applied", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		// Tag with zero-width space and BOM embedded
		dirtyTag := "clean\u200btag\ufeff"
		err = TagSession(sid, &dirtyTag, WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		lines := splitNonEmpty(string(data))
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
		assert.Equal(t, "cleantag", entry["tag"])
	})

	t.Run("pure invisible chars rejected", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		invisible := "\u200b\u200c\ufeff"
		err = TagSession(sid, &invisible, WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tag must be non-empty")
	})
}

// strPtr is a helper that returns a pointer to a string value.
func strPtr(s string) *string {
	return &s
}

// ---------------------------------------------------------------------------
// sanitizeUnicode tests
// ---------------------------------------------------------------------------

func TestSanitizeUnicode(t *testing.T) {
	t.Run("clean strings pass through unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", sanitizeUnicode("hello"))
		assert.Equal(t, "tag-with-dashes_123", sanitizeUnicode("tag-with-dashes_123"))
	})

	t.Run("strips zero-width chars", func(t *testing.T) {
		assert.Equal(t, "ab", sanitizeUnicode("a\u200bb"))   // zero-width space
		assert.Equal(t, "ab", sanitizeUnicode("a\u200cb"))   // zero-width non-joiner
		assert.Equal(t, "ab", sanitizeUnicode("a\u200db"))   // zero-width joiner
	})

	t.Run("strips BOM", func(t *testing.T) {
		assert.Equal(t, "hello", sanitizeUnicode("\ufeffhello"))
	})

	t.Run("strips directional marks", func(t *testing.T) {
		assert.Equal(t, "abc", sanitizeUnicode("a\u202ab\u202cc"))
		assert.Equal(t, "abc", sanitizeUnicode("a\u2066b\u2069c"))
	})

	t.Run("strips private use area", func(t *testing.T) {
		assert.Equal(t, "ab", sanitizeUnicode("a\ue000b"))
		assert.Equal(t, "ab", sanitizeUnicode("a\uf8ffb"))
	})

	t.Run("NFKC normalization applied", func(t *testing.T) {
		// Fullwidth 'A' -> ASCII 'A'
		assert.Equal(t, "A", sanitizeUnicode("\uff21"))
	})

	t.Run("iterative convergence", func(t *testing.T) {
		// A string that needs stripping still converges
		result := sanitizeUnicode("a" + strings.Repeat("\u200b", 20) + "b")
		assert.Equal(t, "ab", result)
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		assert.Equal(t, "", sanitizeUnicode(""))
	})

	t.Run("pure invisible returns empty", func(t *testing.T) {
		assert.Equal(t, "", sanitizeUnicode("\u200b\u200c\ufeff"))
	})

	t.Run("soft hyphen stripped", func(t *testing.T) {
		assert.Equal(t, "ab", sanitizeUnicode("a\u00adb"))
	})

	t.Run("word joiner stripped", func(t *testing.T) {
		assert.Equal(t, "ab", sanitizeUnicode("a\u2060b"))
	})
}
