package claudecode

import (
	"encoding/json"
	"fmt"
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

// ---------------------------------------------------------------------------
// DeleteSession tests
// ---------------------------------------------------------------------------

func TestDeleteSession(t *testing.T) {
	t.Run("invalid session ID", func(t *testing.T) {
		configDir := setupConfigDir(t)
		err := DeleteSession("not-a-uuid", withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid session ID")
	})

	t.Run("session not found", func(t *testing.T) {
		configDir := setupConfigDir(t)
		sid := nextTestUUID(t)
		err := DeleteSession(sid, withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("deletes session file", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr)

		err = DeleteSession(sid, WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		_, statErr = os.Stat(filePath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("deletes without directory", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projDir := makeProjectDir(t, configDir, "/any/project")
		sid, filePath := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr)

		err := DeleteSession(sid, withTestConfigDir(configDir))
		require.NoError(t, err)

		_, statErr = os.Stat(filePath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("no longer in list sessions", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		found := false
		for _, s := range sessions {
			if s.SessionID == sid {
				found = true
			}
		}
		assert.True(t, found)

		err = DeleteSession(sid, WithSessionDirectory(projectPath), withTestConfigDir(configDir))
		require.NoError(t, err)

		sessions, err = ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		for _, s := range sessions {
			assert.NotEqual(t, sid, s.SessionID)
		}
	})
}

// ---------------------------------------------------------------------------
// newUUID tests
// ---------------------------------------------------------------------------

func TestNewUUID(t *testing.T) {
	t.Run("generates valid UUID", func(t *testing.T) {
		id := newUUID()
		assert.True(t, validateUUID(id), "generated UUID %q should be valid", id)
	})

	t.Run("generates unique UUIDs", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id := newUUID()
			assert.False(t, seen[id], "UUID collision: %s", id)
			seen[id] = true
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers for fork tests -- need transcript entries with uuid + parentUuid
// ---------------------------------------------------------------------------

// makeTranscriptSession creates a session file with a proper uuid/parentUuid chain.
// Returns (sessionID, filePath, listOfMessageUUIDs).
func makeTranscriptSession(
	t *testing.T,
	projectDir string,
	sessionID string,
	numTurns int,
) (string, string, []string) {
	t.Helper()

	if sessionID == "" {
		sessionID = nextTestUUID(t)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	var uuids []string
	var lines []string
	parentUUID := ""

	for i := 0; i < numTurns; i++ {
		// User message
		userUUID := nextTestUUID(t)
		uuids = append(uuids, userUUID)
		userEntry := map[string]any{
			"type":      "user",
			"uuid":      userUUID,
			"parentUuid": nilIfEmpty(parentUUID),
			"sessionId": sessionID,
			"timestamp": "2026-03-01T00:00:00Z",
			"message": map[string]any{
				"role":    "user",
				"content": fmt.Sprintf("Turn %d question", i+1),
			},
		}
		b, err := json.Marshal(userEntry)
		require.NoError(t, err)
		lines = append(lines, string(b))
		parentUUID = userUUID

		// Assistant message
		asstUUID := nextTestUUID(t)
		uuids = append(uuids, asstUUID)
		asstEntry := map[string]any{
			"type":      "assistant",
			"uuid":      asstUUID,
			"parentUuid": parentUUID,
			"sessionId": sessionID,
			"timestamp": "2026-03-01T00:00:00Z",
			"message": map[string]any{
				"role":    "assistant",
				"content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("Turn %d answer", i+1)}},
			},
		}
		b, err = json.Marshal(asstEntry)
		require.NoError(t, err)
		lines = append(lines, string(b))
		parentUUID = asstUUID
	}

	content := strings.Join(lines, "\n") + "\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o600))
	return sessionID, filePath, uuids
}

// nilIfEmpty returns nil for empty strings, otherwise the string pointer value.
// Used for JSON marshaling where parentUuid should be null for root entries.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---------------------------------------------------------------------------
// ForkSession tests
// ---------------------------------------------------------------------------

func TestForkSession(t *testing.T) {
	t.Run("invalid session ID", func(t *testing.T) {
		configDir := setupConfigDir(t)
		_, err := ForkSession("not-a-uuid", withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid session ID")
	})

	t.Run("session not found", func(t *testing.T) {
		configDir := setupConfigDir(t)
		sid := nextTestUUID(t)
		_, err := ForkSession(sid, withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("invalid up to message ID", func(t *testing.T) {
		configDir := setupConfigDir(t)
		sid := nextTestUUID(t)
		_, err := ForkSession(sid, WithForkUpToMessageID("not-valid"), withTestConfigDir(configDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid up_to_message_id")
	})

	t.Run("creates new session", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, _ := makeTranscriptSession(t, projDir, "", 2)

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.NotEqual(t, sid, result.SessionID)

		// New file exists
		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		_, statErr := os.Stat(forkPath)
		require.NoError(t, statErr)
	})

	t.Run("remaps UUIDs", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, originalUUIDs := makeTranscriptSession(t, projDir, "", 2)

		originalSet := make(map[string]bool)
		for _, u := range originalUUIDs {
			originalSet[u] = true
		}

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		data, err := os.ReadFile(forkPath)
		require.NoError(t, err)

		for _, line := range splitNonEmpty(string(data)) {
			var entry map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &entry))
			entryType, _ := entry["type"].(string)
			if entryType == "user" || entryType == "assistant" {
				assert.False(t, originalSet[entry["uuid"].(string)], "uuid should be remapped")
				if parentUUID, ok := entry["parentUuid"].(string); ok && parentUUID != "" {
					assert.False(t, originalSet[parentUUID], "parentUuid should be remapped")
				}
			}
		}
	})

	t.Run("preserves message count", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, _ := makeTranscriptSession(t, projDir, "", 3)

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		originalMsgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkMsgs, err := GetSessionMessages(result.SessionID,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		assert.Equal(t, len(originalMsgs), len(forkMsgs))
	})

	t.Run("up to message ID", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, uuids := makeTranscriptSession(t, projDir, "", 3)

		// Fork up to the first assistant response (uuid index 1)
		cutoffUUID := uuids[1]
		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			WithForkUpToMessageID(cutoffUUID),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkMsgs, err := GetSessionMessages(result.SessionID,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		// Should have 2 messages (1 user + 1 assistant from first turn)
		assert.Equal(t, 2, len(forkMsgs))
	})

	t.Run("up to message ID not found", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, _ := makeTranscriptSession(t, projDir, "", 2)

		fakeUUID := nextTestUUID(t)
		_, err = ForkSession(sid,
			WithSessionDirectory(projectPath),
			WithForkUpToMessageID(fakeUUID),
			withTestConfigDir(configDir),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in session")
	})

	t.Run("custom title", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, _ := makeTranscriptSession(t, projDir, "", 2)

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			WithForkTitle("My Fork"),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		var forkInfo *SDKSessionInfo
		for i, s := range sessions {
			if s.SessionID == result.SessionID {
				forkInfo = &sessions[i]
				break
			}
		}
		require.NotNil(t, forkInfo)
		require.NotNil(t, forkInfo.CustomTitle)
		assert.Equal(t, "My Fork", *forkInfo.CustomTitle)
	})

	t.Run("default title has fork suffix", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, _ := makeTranscriptSession(t, projDir, "", 2)

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		var forkInfo *SDKSessionInfo
		for i, s := range sessions {
			if s.SessionID == result.SessionID {
				forkInfo = &sessions[i]
				break
			}
		}
		require.NotNil(t, forkInfo)
		require.NotNil(t, forkInfo.CustomTitle)
		assert.True(t, strings.HasSuffix(*forkInfo.CustomTitle, "(fork)"))
	})

	t.Run("session ID in entries", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, _ := makeTranscriptSession(t, projDir, "", 2)

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		data, err := os.ReadFile(forkPath)
		require.NoError(t, err)

		for _, line := range splitNonEmpty(string(data)) {
			var entry map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &entry))
			assert.Equal(t, result.SessionID, entry["sessionId"])
		}
	})

	t.Run("forkedFrom field", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)
		sid, _, _ := makeTranscriptSession(t, projDir, "", 2)

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		data, err := os.ReadFile(forkPath)
		require.NoError(t, err)

		for _, line := range splitNonEmpty(string(data)) {
			var entry map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &entry))
			entryType, _ := entry["type"].(string)
			if entryType == "user" || entryType == "assistant" {
				forkedFrom, ok := entry["forkedFrom"].(map[string]any)
				require.True(t, ok, "forkedFrom should be present on %s entry", entryType)
				assert.Equal(t, sid, forkedFrom["sessionId"])
			}
		}
	})

	t.Run("fork without directory", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projDir := makeProjectDir(t, configDir, "/any/project")
		sid, _, _ := makeTranscriptSession(t, projDir, "", 2)

		result, err := ForkSession(sid, withTestConfigDir(configDir))
		require.NoError(t, err)

		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		_, statErr := os.Stat(forkPath)
		require.NoError(t, statErr)
	})

	t.Run("remaps logicalParentUuid", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)

		// Build a transcript where the 3rd entry has a logicalParentUuid
		// pointing to the 1st entry (simulating a compact-boundary backpointer).
		sid := nextTestUUID(t)
		filePath := filepath.Join(projDir, sid+".jsonl")

		uuid1 := nextTestUUID(t)
		uuid2 := nextTestUUID(t)
		uuid3 := nextTestUUID(t)

		entries := []map[string]any{
			{
				"type": "user", "uuid": uuid1, "parentUuid": nil,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "user", "content": "Q1"},
			},
			{
				"type": "assistant", "uuid": uuid2, "parentUuid": uuid1,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "assistant", "content": []any{
					map[string]any{"type": "text", "text": "A1"},
				}},
			},
			{
				"type": "user", "uuid": uuid3, "parentUuid": uuid2,
				"logicalParentUuid": uuid1,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "user", "content": "Q2"},
			},
		}
		var lines []string
		for _, e := range entries {
			b, marshalErr := json.Marshal(e)
			require.NoError(t, marshalErr)
			lines = append(lines, string(b))
		}
		require.NoError(t, os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		data, err := os.ReadFile(forkPath)
		require.NoError(t, err)

		// Collect the new UUID for each original UUID by matching forkedFrom.
		newUUIDs := make(map[string]string) // original -> new
		for _, line := range splitNonEmpty(string(data)) {
			var e map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &e))
			if ff, ok := e["forkedFrom"].(map[string]any); ok {
				origUUID, _ := ff["messageUuid"].(string)
				newUUID, _ := e["uuid"].(string)
				if origUUID != "" && newUUID != "" {
					newUUIDs[origUUID] = newUUID
				}
			}
		}

		// Find the forked entry corresponding to uuid3 and verify
		// its logicalParentUuid was remapped to the new UUID for uuid1.
		for _, line := range splitNonEmpty(string(data)) {
			var e map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &e))
			if ff, ok := e["forkedFrom"].(map[string]any); ok {
				if origUUID, _ := ff["messageUuid"].(string); origUUID == uuid3 {
					remapped, ok := e["logicalParentUuid"].(string)
					require.True(t, ok, "logicalParentUuid should be a remapped string")
					assert.Equal(t, newUUIDs[uuid1], remapped,
						"logicalParentUuid should point to new UUID of uuid1")
				}
			}
		}
	})

	t.Run("nullifies logicalParentUuid when reference sliced off", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)

		// Build a 3-entry transcript. The 3rd entry has logicalParentUuid
		// pointing to the 1st entry. Fork up to the 2nd entry so the
		// referenced UUID is sliced off.
		sid := nextTestUUID(t)
		filePath := filepath.Join(projDir, sid+".jsonl")

		uuid1 := nextTestUUID(t)
		uuid2 := nextTestUUID(t)
		uuid3 := nextTestUUID(t)

		entries := []map[string]any{
			{
				"type": "user", "uuid": uuid1, "parentUuid": nil,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "user", "content": "Q1"},
			},
			{
				"type": "assistant", "uuid": uuid2, "parentUuid": uuid1,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "assistant", "content": []any{
					map[string]any{"type": "text", "text": "A1"},
				}},
			},
			{
				"type": "user", "uuid": uuid3, "parentUuid": uuid2,
				"logicalParentUuid": uuid1,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "user", "content": "Q2"},
			},
		}
		var lines []string
		for _, e := range entries {
			b, marshalErr := json.Marshal(e)
			require.NoError(t, marshalErr)
			lines = append(lines, string(b))
		}
		require.NoError(t, os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

		// Fork up to uuid2, which slices off uuid3. But let's verify
		// the scenario where the logicalParentUuid target is sliced off.
		// We need: entry with logicalParentUuid present, but the target
		// is NOT in the fork. Build a different scenario:
		// uuid1 -> uuid2 (has logicalParentUuid -> uuid_outside)
		// Fork includes uuid1 and uuid2, but uuid_outside was never
		// in the transcript (or was sliced off).

		// Simpler approach: 4 entries where entry 4 points to entry 1
		// via logicalParentUuid. Fork up to entry 3 so entry 1's UUID
		// IS in mapping but entry 4 is NOT included. But we want the
		// inverse: the logicalParentUuid target to be outside the fork.
		// Restructure: entry 3 has logicalParentUuid -> entry 1,
		// fork up to entry 2. Then entry 3 is excluded entirely. That
		// doesn't test the bug.

		// The real scenario: entry 2 has logicalParentUuid -> some UUID
		// that is NOT in the fork's UUID mapping. Rebuild the file.
		outsideUUID := nextTestUUID(t)
		entries2 := []map[string]any{
			{
				"type": "user", "uuid": uuid1, "parentUuid": nil,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "user", "content": "Q1"},
			},
			{
				"type": "assistant", "uuid": uuid2, "parentUuid": uuid1,
				"logicalParentUuid": outsideUUID,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "assistant", "content": []any{
					map[string]any{"type": "text", "text": "A1"},
				}},
			},
			{
				// This entry has the outsideUUID, but we'll fork up to uuid2
				// so it will be sliced off.
				"type": "user", "uuid": outsideUUID, "parentUuid": uuid2,
				"sessionId": sid, "timestamp": "2026-03-01T00:00:00Z",
				"message": map[string]any{"role": "user", "content": "Q2"},
			},
		}
		var lines2 []string
		for _, e := range entries2 {
			b, marshalErr := json.Marshal(e)
			require.NoError(t, marshalErr)
			lines2 = append(lines2, string(b))
		}
		require.NoError(t, os.WriteFile(filePath, []byte(strings.Join(lines2, "\n")+"\n"), 0o600))

		// Fork up to uuid2; outsideUUID is sliced off.
		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			WithForkUpToMessageID(uuid2),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		data, err := os.ReadFile(forkPath)
		require.NoError(t, err)

		// The forked entry for uuid2 should have logicalParentUuid = nil
		// because outsideUUID was sliced off.
		for _, line := range splitNonEmpty(string(data)) {
			var e map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &e))
			if ff, ok := e["forkedFrom"].(map[string]any); ok {
				if origUUID, _ := ff["messageUuid"].(string); origUUID == uuid2 {
					assert.Nil(t, e["logicalParentUuid"],
						"logicalParentUuid should be nil when referenced UUID is sliced off")
				}
			}
		}
	})

	t.Run("clears stale fields", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)
		projDir := makeProjectDir(t, configDir, realPath)

		sid := nextTestUUID(t)
		filePath := filepath.Join(projDir, sid+".jsonl")
		entry := map[string]any{
			"type":      "user",
			"uuid":      nextTestUUID(t),
			"parentUuid": nil,
			"sessionId": sid,
			"timestamp": "2026-03-01T00:00:00Z",
			"teamName":  "test-team",
			"agentName": "test-agent",
			"slug":      "test-slug",
			"sourceToolAssistantUUID": "some-uuid",
			"message":   map[string]any{"role": "user", "content": "Hello"},
		}
		b, err := json.Marshal(entry)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filePath, append(b, '\n'), 0o600))

		result, err := ForkSession(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)

		forkPath := filepath.Join(projDir, result.SessionID+".jsonl")
		data, err := os.ReadFile(forkPath)
		require.NoError(t, err)

		for _, line := range splitNonEmpty(string(data)) {
			var e map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &e))
			entryType, _ := e["type"].(string)
			if entryType == "user" {
				assert.Nil(t, e["teamName"], "teamName should be removed")
				assert.Nil(t, e["agentName"], "agentName should be removed")
				assert.Nil(t, e["slug"], "slug should be removed")
				assert.Nil(t, e["sourceToolAssistantUUID"], "sourceToolAssistantUUID should be removed")
			}
		}
	})
}
