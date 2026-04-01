package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeProjectDir creates a sanitized project directory for the given path.
func makeProjectDir(t *testing.T, configDir, projectPath string) string {
	t.Helper()
	sanitized := sanitizePath(projectPath)
	dir := filepath.Join(configDir, "projects", sanitized)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

// makeSessionFile creates a .jsonl session file with the given metadata.
// Returns (sessionID, filePath).
func makeSessionFile(
	t *testing.T,
	projectDir string,
	sessionID string,
	opts makeSessionOpts,
) (string, string) {
	t.Helper()

	if sessionID == "" {
		sessionID = nextTestUUID(t)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")

	var lines []string

	// First line: user message
	firstEntry := map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": opts.firstPrompt},
	}
	if opts.cwd != "" {
		firstEntry["cwd"] = opts.cwd
	}
	if opts.gitBranch != "" {
		firstEntry["gitBranch"] = opts.gitBranch
	}
	if opts.isSidechain {
		firstEntry["isSidechain"] = true
	}
	if opts.isMeta {
		firstEntry["isMeta"] = true
	}
	b, err := json.Marshal(firstEntry)
	require.NoError(t, err)
	lines = append(lines, string(b))

	// Assistant response
	asst := map[string]any{
		"type":    "assistant",
		"message": map[string]any{"role": "assistant", "content": "Hi there!"},
	}
	b, err = json.Marshal(asst)
	require.NoError(t, err)
	lines = append(lines, string(b))

	// Tail metadata (summary entry)
	tail := map[string]any{"type": "summary"}
	if opts.summary != "" {
		tail["summary"] = opts.summary
	}
	if opts.customTitle != "" {
		tail["customTitle"] = opts.customTitle
	}
	if opts.gitBranch != "" {
		tail["gitBranch"] = opts.gitBranch
	}
	b, err = json.Marshal(tail)
	require.NoError(t, err)
	lines = append(lines, string(b))

	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	if opts.mtime > 0 {
		mt := time.Unix(int64(opts.mtime), 0)
		require.NoError(t, os.Chtimes(filePath, mt, mt))
	}

	return sessionID, filePath
}

type makeSessionOpts struct {
	firstPrompt string
	summary     string
	customTitle string
	gitBranch   string
	cwd         string
	isSidechain bool
	isMeta      bool
	mtime       float64 // seconds
}

var testUUIDCounter uint64 = 0x550e8400

// nextTestUUID generates a unique valid UUID string for testing.
// Each call increments the counter to ensure uniqueness.
func nextTestUUID(t *testing.T) string {
	t.Helper()
	testUUIDCounter++
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		testUUIDCounter, testUUIDCounter&0xffff,
		0x4000|(testUUIDCounter&0x0fff),
		0x8000|(testUUIDCounter&0x3fff),
		testUUIDCounter,
	)
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestValidateUUID(t *testing.T) {
	t.Run("valid UUIDs", func(t *testing.T) {
		assert.True(t, validateUUID("550e8400-e29b-41d4-a716-446655440000"))
		assert.True(t, validateUUID("550E8400-E29B-41D4-A716-446655440000"))
	})

	t.Run("invalid UUIDs", func(t *testing.T) {
		assert.False(t, validateUUID("not-a-uuid"))
		assert.False(t, validateUUID(""))
		assert.False(t, validateUUID("550e8400-e29b-41d4-a716"))
	})
}

func TestSanitizePath(t *testing.T) {
	t.Run("basic paths", func(t *testing.T) {
		assert.Equal(t, "-Users-foo-my-project", sanitizePath("/Users/foo/my-project"))
		assert.Equal(t, "plugin-name-server", sanitizePath("plugin:name:server"))
	})

	t.Run("long paths get truncated with hash suffix", func(t *testing.T) {
		longPath := ""
		for i := 0; i < 150; i++ {
			longPath += "/x"
		}
		result := sanitizePath(longPath)
		assert.Greater(t, len(result), 200)
		assert.True(t, result[:4] == "-x-x")
		// Hash suffix is appended after the 200-char prefix
		assert.Contains(t, result[200:], "-")
	})
}

func TestSimpleHash(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		assert.Equal(t, simpleHash("hello"), simpleHash("hello"))
		assert.NotEqual(t, simpleHash("hello"), simpleHash("world"))
	})

	t.Run("empty string produces 0", func(t *testing.T) {
		assert.Equal(t, "0", simpleHash(""))
	})
}

func TestExtractJSONStringField(t *testing.T) {
	t.Run("simple fields", func(t *testing.T) {
		text := `{"foo":"bar","baz":"qux"}`
		v, ok := extractJSONStringField(text, "foo")
		assert.True(t, ok)
		assert.Equal(t, "bar", v)

		v, ok = extractJSONStringField(text, "baz")
		assert.True(t, ok)
		assert.Equal(t, "qux", v)

		_, ok = extractJSONStringField(text, "missing")
		assert.False(t, ok)
	})

	t.Run("with space after colon", func(t *testing.T) {
		text := `{"foo": "bar"}`
		v, ok := extractJSONStringField(text, "foo")
		assert.True(t, ok)
		assert.Equal(t, "bar", v)
	})

	t.Run("escaped quotes", func(t *testing.T) {
		text := `{"foo":"bar\"baz"}`
		v, ok := extractJSONStringField(text, "foo")
		assert.True(t, ok)
		assert.Equal(t, `bar"baz`, v)
	})
}

func TestExtractLastJSONStringField(t *testing.T) {
	t.Run("returns last occurrence", func(t *testing.T) {
		text := "{\"summary\":\"first\"}\n{\"summary\":\"second\"}\n{\"summary\":\"third\"}"
		v, ok := extractLastJSONStringField(text, "summary")
		assert.True(t, ok)
		assert.Equal(t, "third", v)
	})
}

func TestExtractFirstPromptFromHead(t *testing.T) {
	t.Run("simple prompt", func(t *testing.T) {
		head := jsonLine(map[string]any{"type": "user", "message": map[string]any{"content": "Hello!"}}) + "\n"
		assert.Equal(t, "Hello!", extractFirstPromptFromHead(head))
	})

	t.Run("skips meta messages", func(t *testing.T) {
		head := jsonLine(map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"content": "meta"}}) + "\n" +
			jsonLine(map[string]any{"type": "user", "message": map[string]any{"content": "real prompt"}}) + "\n"
		assert.Equal(t, "real prompt", extractFirstPromptFromHead(head))
	})

	t.Run("skips tool_result", func(t *testing.T) {
		head := jsonLine(map[string]any{
			"type":    "user",
			"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": "x"}}},
		}) + "\n" +
			jsonLine(map[string]any{"type": "user", "message": map[string]any{"content": "actual prompt"}}) + "\n"
		assert.Equal(t, "actual prompt", extractFirstPromptFromHead(head))
	})

	t.Run("content blocks", func(t *testing.T) {
		head := jsonLine(map[string]any{
			"type":    "user",
			"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "block prompt"}}},
		}) + "\n"
		assert.Equal(t, "block prompt", extractFirstPromptFromHead(head))
	})

	t.Run("truncates long prompts", func(t *testing.T) {
		longPrompt := ""
		for i := 0; i < 300; i++ {
			longPrompt += "x"
		}
		head := jsonLine(map[string]any{"type": "user", "message": map[string]any{"content": longPrompt}}) + "\n"
		result := extractFirstPromptFromHead(head)
		assert.LessOrEqual(t, len(result), 204) // 200 chars + ellipsis (3 bytes UTF-8)
		assert.True(t, len(result) > 200)
	})

	t.Run("command name fallback", func(t *testing.T) {
		head := jsonLine(map[string]any{
			"type":    "user",
			"message": map[string]any{"content": "<command-name>/help</command-name>stuff"},
		}) + "\n"
		assert.Equal(t, "/help", extractFirstPromptFromHead(head))
	})

	t.Run("empty input", func(t *testing.T) {
		assert.Equal(t, "", extractFirstPromptFromHead(""))
		head := jsonLine(map[string]any{"type": "assistant"}) + "\n"
		assert.Equal(t, "", extractFirstPromptFromHead(head))
	})
}

// jsonLine marshals a map to a compact JSON string (no trailing newline).
func jsonLine(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------------------------------------------------------------------------
// ListSessions integration tests
// ---------------------------------------------------------------------------

func TestListSessions(t *testing.T) {
	t.Run("empty projects dir", func(t *testing.T) {
		configDir := setupConfigDir(t)
		sessions, err := ListSessions(withTestConfigDir(configDir))
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	t.Run("no config dir", func(t *testing.T) {
		sessions, err := ListSessions(withTestConfigDir(filepath.Join(t.TempDir(), "nonexistent")))
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	t.Run("single session", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "my-project")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid, _ := makeSessionFile(t, projDir, "", makeSessionOpts{
			firstPrompt: "What is 2+2?",
			gitBranch:   "main",
			cwd:         projectPath,
		})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)

		s := sessions[0]
		assert.Equal(t, sid, s.SessionID)
		assert.Equal(t, "What is 2+2?", *s.FirstPrompt)
		assert.Equal(t, "What is 2+2?", s.Summary) // no custom title or summary -> first prompt
		assert.Equal(t, "main", *s.GitBranch)
		assert.Equal(t, projectPath, *s.Cwd)
		assert.Greater(t, s.FileSize, int64(0))
		assert.Greater(t, s.LastModified, int64(0))
		assert.Nil(t, s.CustomTitle)
	})

	t.Run("custom title wins over summary", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		makeSessionFile(t, projDir, "", makeSessionOpts{
			firstPrompt: "original question",
			summary:     "auto summary",
			customTitle: "My Custom Title",
		})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "My Custom Title", sessions[0].Summary)
		assert.Equal(t, "My Custom Title", *sessions[0].CustomTitle)
		assert.Equal(t, "original question", *sessions[0].FirstPrompt)
	})

	t.Run("summary wins over first prompt", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		makeSessionFile(t, projDir, "", makeSessionOpts{
			firstPrompt: "question",
			summary:     "better summary",
		})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "better summary", sessions[0].Summary)
		assert.Nil(t, sessions[0].CustomTitle)
	})

	t.Run("multiple sessions sorted by mtime", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sidOld, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "old", mtime: 1000.0})
		sidNew, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "new", mtime: 3000.0})
		sidMid, _ := makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "mid", mtime: 2000.0})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 3)

		// Sorted newest first
		assert.Equal(t, sidNew, sessions[0].SessionID)
		assert.Equal(t, sidMid, sessions[1].SessionID)
		assert.Equal(t, sidOld, sessions[2].SessionID)
		// Verify mtime conversion to milliseconds
		assert.Equal(t, int64(3_000_000), sessions[0].LastModified)
		assert.Equal(t, int64(2_000_000), sessions[1].LastModified)
		assert.Equal(t, int64(1_000_000), sessions[2].LastModified)
	})

	t.Run("limit", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		for i := 0; i < 5; i++ {
			makeSessionFile(t, projDir, "", makeSessionOpts{
				firstPrompt: fmt.Sprintf("prompt %d", i),
				mtime:       1000.0 + float64(i),
			})
		}

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithSessionLimit(2),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, sessions, 2)
		// Should be the 2 newest
		assert.GreaterOrEqual(t, sessions[0].LastModified, sessions[1].LastModified)
	})

	t.Run("offset pagination", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		for i := 0; i < 5; i++ {
			makeSessionFile(t, projDir, "", makeSessionOpts{
				firstPrompt: fmt.Sprintf("prompt %d", i),
				mtime:       1000.0 + float64(i),
			})
		}

		page1, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithSessionLimit(2),
			WithSessionOffset(0),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithSessionLimit(2),
			WithSessionOffset(2),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, page2, 2)

		// Pages should have different sessions
		page1IDs := map[string]bool{page1[0].SessionID: true, page1[1].SessionID: true}
		assert.False(t, page1IDs[page2[0].SessionID])
		assert.False(t, page1IDs[page2[1].SessionID])

		// Page 1 should be newer than page 2
		assert.Greater(t, page1[0].LastModified, page2[0].LastModified)

		// Offset beyond available returns empty
		pageEmpty, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithSessionOffset(100),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Empty(t, pageEmpty)
	})

	t.Run("filters sidechain sessions", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "normal"})
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "sidechain", isSidechain: true})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "normal", *sessions[0].FirstPrompt)
	})

	t.Run("filters meta-only sessions", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "ignored meta", isMeta: true})
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "real content"})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "real content", *sessions[0].FirstPrompt)
	})

	t.Run("filters non-UUID filenames", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		// Non-UUID .jsonl file
		nonUUID := filepath.Join(projDir, "not-a-uuid.jsonl")
		require.NoError(t, os.WriteFile(nonUUID, []byte(`{"type":"user","message":{"content":"x"}}`+"\n"), 0o644))
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "valid session"})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "valid session", *sessions[0].FirstPrompt)
	})

	t.Run("list all sessions", func(t *testing.T) {
		configDir := setupConfigDir(t)
		proj1 := makeProjectDir(t, configDir, "/some/path/one")
		proj2 := makeProjectDir(t, configDir, "/some/path/two")

		makeSessionFile(t, proj1, "", makeSessionOpts{firstPrompt: "from proj1", mtime: 1000.0})
		makeSessionFile(t, proj2, "", makeSessionOpts{firstPrompt: "from proj2", mtime: 2000.0})

		sessions, err := ListSessions(withTestConfigDir(configDir))
		require.NoError(t, err)
		require.Len(t, sessions, 2)
		// Sorted newest first
		assert.Equal(t, "from proj2", *sessions[0].FirstPrompt)
		assert.Equal(t, "from proj1", *sessions[1].FirstPrompt)
	})

	t.Run("list all sessions deduplicates", func(t *testing.T) {
		configDir := setupConfigDir(t)
		proj1 := makeProjectDir(t, configDir, "/path/one")
		proj2 := makeProjectDir(t, configDir, "/path/two")

		sharedSID := nextTestUUID(t)
		makeSessionFile(t, proj1, sharedSID, makeSessionOpts{firstPrompt: "older", mtime: 1000.0})
		makeSessionFile(t, proj2, sharedSID, makeSessionOpts{firstPrompt: "newer", mtime: 2000.0})

		sessions, err := ListSessions(withTestConfigDir(configDir))
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "newer", *sessions[0].FirstPrompt)
		assert.Equal(t, int64(2_000_000), sessions[0].LastModified)
	})

	t.Run("nonexistent project dir", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "never-used")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	t.Run("empty file filtered", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)
		require.NoError(t, os.WriteFile(filepath.Join(projDir, sid+".jsonl"), []byte(""), 0o644))

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	t.Run("limit zero returns all", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		for i := 0; i < 3; i++ {
			makeSessionFile(t, projDir, "", makeSessionOpts{
				firstPrompt: fmt.Sprintf("p%d", i),
			})
		}

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithSessionLimit(0),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, sessions, 3)
	})

	t.Run("cwd falls back to project path", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		// Session without cwd field
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "no cwd field"})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, realPath, *sessions[0].Cwd)
	})

	t.Run("git branch from tail preferred", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)
		filePath := filepath.Join(projDir, sid+".jsonl")
		lines := jsonLine(map[string]any{
			"type": "user", "message": map[string]any{"content": "hello"}, "gitBranch": "old-branch",
		}) + "\n" + jsonLine(map[string]any{
			"type": "summary", "gitBranch": "new-branch",
		}) + "\n"
		require.NoError(t, os.WriteFile(filePath, []byte(lines), 0o644))

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "new-branch", *sessions[0].GitBranch)
	})
}

// ---------------------------------------------------------------------------
// Tag extraction tests
// ---------------------------------------------------------------------------

func TestTagExtraction(t *testing.T) {
	t.Run("tag extracted from tail", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)
		filePath := filepath.Join(projDir, sid+".jsonl")
		lines := jsonLine(map[string]any{"type": "user", "message": map[string]any{"content": "hello"}}) + "\n" +
			compactTagLine("my-tag", sid) + "\n"
		require.NoError(t, os.WriteFile(filePath, []byte(lines), 0o644))

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "my-tag", *sessions[0].Tag)
	})

	t.Run("tag last wins", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)
		filePath := filepath.Join(projDir, sid+".jsonl")
		lines := jsonLine(map[string]any{"type": "user", "message": map[string]any{"content": "hello"}}) + "\n" +
			compactTagLine("first-tag", sid) + "\n" +
			compactTagLine("second-tag", sid) + "\n"
		require.NoError(t, os.WriteFile(filePath, []byte(lines), 0o644))

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, "second-tag", *sessions[0].Tag)
	})

	t.Run("tag absent", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "hello"})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Nil(t, sessions[0].Tag)
	})
}

// compactTagLine produces a compact JSON tag line matching the CLI's on-disk format.
// The CLI always writes {"type":"tag",...} with type as the first key, which
// the tag extraction code relies on (startsWith check).
func compactTagLine(tag, sessionID string) string {
	// Manually construct to ensure "type" is the first key
	return `{"type":"tag","tag":"` + tag + `","sessionId":"` + sessionID + `"}`
}

// ---------------------------------------------------------------------------
// SDKSessionInfo type tests
// ---------------------------------------------------------------------------

func TestSDKSessionInfoType(t *testing.T) {
	t.Run("required fields", func(t *testing.T) {
		info := SDKSessionInfo{
			SessionID:    "abc",
			Summary:      "test",
			LastModified: 1000,
			FileSize:     42,
		}
		assert.Equal(t, "abc", info.SessionID)
		assert.Equal(t, "test", info.Summary)
		assert.Equal(t, int64(1000), info.LastModified)
		assert.Equal(t, int64(42), info.FileSize)
		assert.Nil(t, info.CustomTitle)
		assert.Nil(t, info.FirstPrompt)
		assert.Nil(t, info.GitBranch)
		assert.Nil(t, info.Cwd)
		assert.Nil(t, info.Tag)
		assert.Nil(t, info.CreatedAt)
	})
}

// ---------------------------------------------------------------------------
// Transcript entry helpers for GetSessionMessages tests
// ---------------------------------------------------------------------------

func makeTranscriptEntry(
	entryType string,
	entryUUID string,
	parentUUID string,
	sessionID string,
	content any,
	extras map[string]any,
) map[string]any {
	entry := map[string]any{
		"type":       entryType,
		"uuid":       entryUUID,
		"parentUuid": parentUUID,
		"sessionId":  sessionID,
	}
	if parentUUID == "" {
		entry["parentUuid"] = nil
	}
	if content != nil {
		role := entryType
		if role != "user" && role != "assistant" {
			role = "user"
		}
		entry["message"] = map[string]any{"role": role, "content": content}
	}
	for k, v := range extras {
		entry[k] = v
	}
	return entry
}

func writeTranscript(t *testing.T, projectDir, sessionID string, entries []map[string]any) string {
	t.Helper()
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	var content string
	for _, e := range entries {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		content += string(b) + "\n"
	}
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))
	return filePath
}

// ---------------------------------------------------------------------------
// GetSessionMessages tests
// ---------------------------------------------------------------------------

func TestGetSessionMessages(t *testing.T) {
	t.Run("invalid session ID", func(t *testing.T) {
		configDir := setupConfigDir(t)
		msgs, err := GetSessionMessages("not-a-uuid", withTestConfigDir(configDir))
		require.NoError(t, err)
		assert.Empty(t, msgs)

		msgs, err = GetSessionMessages("", withTestConfigDir(configDir))
		require.NoError(t, err)
		assert.Empty(t, msgs)
	})

	t.Run("nonexistent session", func(t *testing.T) {
		configDir := setupConfigDir(t)
		sid := nextTestUUID(t)
		msgs, err := GetSessionMessages(sid, withTestConfigDir(configDir))
		require.NoError(t, err)
		assert.Empty(t, msgs)
	})

	t.Run("no config dir", func(t *testing.T) {
		sid := nextTestUUID(t)
		msgs, err := GetSessionMessages(sid, withTestConfigDir(filepath.Join(t.TempDir(), "nonexistent")))
		require.NoError(t, err)
		assert.Empty(t, msgs)
	})

	t.Run("simple chain", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		a1 := nextTestUUID(t)
		u2 := nextTestUUID(t)
		a2 := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", u1, "", sid, "hello", nil),
			makeTranscriptEntry("assistant", a1, u1, sid, "hi!", nil),
			makeTranscriptEntry("user", u2, a1, sid, "thanks", nil),
			makeTranscriptEntry("assistant", a2, u2, sid, "welcome", nil),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, msgs, 4)

		// Chronological order: root -> leaf
		assert.Equal(t, "user", msgs[0].Type)
		assert.Equal(t, u1, msgs[0].UUID)
		assert.Equal(t, sid, msgs[0].SessionID)
		assert.NotNil(t, msgs[0].Message)

		assert.Equal(t, "assistant", msgs[1].Type)
		assert.Equal(t, a1, msgs[1].UUID)

		assert.Equal(t, "user", msgs[2].Type)
		assert.Equal(t, u2, msgs[2].UUID)

		assert.Equal(t, "assistant", msgs[3].Type)
		assert.Equal(t, a2, msgs[3].UUID)
	})

	t.Run("filters meta messages", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		meta := nextTestUUID(t)
		a1 := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", u1, "", sid, "hello", nil),
			makeTranscriptEntry("user", meta, u1, sid, "meta", map[string]any{"isMeta": true}),
			makeTranscriptEntry("assistant", a1, meta, sid, "hi", nil),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Equal(t, u1, msgs[0].UUID)
		assert.Equal(t, a1, msgs[1].UUID)
	})

	t.Run("filters non-user/assistant from chain", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		prog := nextTestUUID(t)
		a1 := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", u1, "", sid, "hello", nil),
			makeTranscriptEntry("progress", prog, u1, sid, nil, nil),
			makeTranscriptEntry("assistant", a1, prog, sid, "hi", nil),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Equal(t, u1, msgs[0].UUID)
		assert.Equal(t, a1, msgs[1].UUID)
	})

	t.Run("keeps compact summary", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		a1 := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", u1, "", sid, "compact summary", map[string]any{"isCompactSummary": true}),
			makeTranscriptEntry("assistant", a1, u1, sid, "hi", nil),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Equal(t, u1, msgs[0].UUID)
	})

	t.Run("limit and offset", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		// Build a chain of 6 messages: u->a->u->a->u->a
		uuids := make([]string, 6)
		for i := range uuids {
			uuids[i] = nextTestUUID(t)
		}
		entries := make([]map[string]any, 6)
		for i, uid := range uuids {
			parent := ""
			if i > 0 {
				parent = uuids[i-1]
			}
			entryType := "user"
			if i%2 == 1 {
				entryType = "assistant"
			}
			entries[i] = makeTranscriptEntry(entryType, uid, parent, sid, fmt.Sprintf("m%d", i), nil)
		}
		writeTranscript(t, projDir, sid, entries)

		// No limit/offset
		allMsgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, allMsgs, 6)

		// limit=2
		page, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			WithMessageLimit(2),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, page, 2)
		assert.Equal(t, uuids[0], page[0].UUID)
		assert.Equal(t, uuids[1], page[1].UUID)

		// offset=2, limit=2
		page, err = GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			WithMessageLimit(2),
			WithMessageOffset(2),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, page, 2)
		assert.Equal(t, uuids[2], page[0].UUID)
		assert.Equal(t, uuids[3], page[1].UUID)

		// offset only (no limit)
		page, err = GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			WithMessageOffset(4),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, page, 2)
		assert.Equal(t, uuids[4], page[0].UUID)
		assert.Equal(t, uuids[5], page[1].UUID)

		// limit=0 returns all
		page, err = GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			WithMessageLimit(0),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, page, 6)

		// offset beyond end
		page, err = GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			WithMessageOffset(100),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Empty(t, page)
	})

	t.Run("picks main chain over sidechain", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		root := nextTestUUID(t)
		mainLeaf := nextTestUUID(t)
		sideLeaf := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", root, "", sid, "root", nil),
			makeTranscriptEntry("assistant", mainLeaf, root, sid, "main", nil),
			makeTranscriptEntry("assistant", sideLeaf, root, sid, "side", map[string]any{"isSidechain": true}),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Equal(t, root, msgs[0].UUID)
		assert.Equal(t, mainLeaf, msgs[1].UUID)
	})

	t.Run("picks latest leaf by file position", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		root := nextTestUUID(t)
		oldLeaf := nextTestUUID(t)
		newLeaf := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", root, "", sid, "root", nil),
			makeTranscriptEntry("assistant", oldLeaf, root, sid, "old", nil),
			makeTranscriptEntry("assistant", newLeaf, root, sid, "new", nil),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Equal(t, root, msgs[0].UUID)
		assert.Equal(t, newLeaf, msgs[1].UUID)
	})

	t.Run("terminal non-message walked back", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		a1 := nextTestUUID(t)
		prog := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", u1, "", sid, "hi", nil),
			makeTranscriptEntry("assistant", a1, u1, sid, "hello", nil),
			makeTranscriptEntry("progress", prog, a1, sid, nil, nil),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Equal(t, u1, msgs[0].UUID)
		assert.Equal(t, a1, msgs[1].UUID)
	})

	t.Run("corrupt lines skipped", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		a1 := nextTestUUID(t)

		content := jsonLine(makeTranscriptEntry("user", u1, "", sid, "hi", nil)) + "\n" +
			"not valid json {{{\n" +
			"\n" +
			jsonLine(makeTranscriptEntry("assistant", a1, u1, sid, "hello", nil)) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(projDir, sid+".jsonl"), []byte(content), 0o644))

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, msgs, 2)
	})

	t.Run("search all projects when no dir", func(t *testing.T) {
		configDir := setupConfigDir(t)
		proj1 := makeProjectDir(t, configDir, "/path/one")
		proj2 := makeProjectDir(t, configDir, "/path/two")
		_ = proj1

		sid := nextTestUUID(t)
		u1 := nextTestUUID(t)
		a1 := nextTestUUID(t)

		entries := []map[string]any{
			makeTranscriptEntry("user", u1, "", sid, "hi", nil),
			makeTranscriptEntry("assistant", a1, u1, sid, "hello", nil),
		}
		writeTranscript(t, proj2, sid, entries)

		msgs, err := GetSessionMessages(sid, withTestConfigDir(configDir))
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Equal(t, u1, msgs[0].UUID)
	})

	t.Run("cycle detection", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		a1 := nextTestUUID(t)

		// a1 -> u1 -> a1 (cycle!)
		entries := []map[string]any{
			makeTranscriptEntry("user", u1, a1, sid, "hi", nil),
			makeTranscriptEntry("assistant", a1, u1, sid, "hello", nil),
		}
		writeTranscript(t, projDir, sid, entries)

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		// No terminals found (both are parents) -> returns empty
		assert.Empty(t, msgs)
	})

	t.Run("empty transcript file", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)
		require.NoError(t, os.WriteFile(filepath.Join(projDir, sid+".jsonl"), []byte(""), 0o644))

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Empty(t, msgs)
	})

	t.Run("ignores non-transcript types", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)

		u1 := nextTestUUID(t)
		a1 := nextTestUUID(t)

		content := jsonLine(makeTranscriptEntry("user", u1, "", sid, "hi", nil)) + "\n" +
			jsonLine(map[string]any{"type": "summary", "summary": "A nice chat"}) + "\n" +
			jsonLine(makeTranscriptEntry("assistant", a1, u1, sid, "hello", nil)) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(projDir, sid+".jsonl"), []byte(content), 0o644))

		msgs, err := GetSessionMessages(sid,
			WithSessionDirectory(projectPath),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		assert.Len(t, msgs, 2)
	})
}

// ---------------------------------------------------------------------------
// BuildConversationChain unit tests
// ---------------------------------------------------------------------------

func TestBuildConversationChain(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		result := buildConversationChain(nil)
		assert.Empty(t, result)
	})

	t.Run("single entry", func(t *testing.T) {
		entry := transcriptEntry{Type: "user", UUID: "a"}
		result := buildConversationChain([]transcriptEntry{entry})
		require.Len(t, result, 1)
		assert.Equal(t, "a", result[0].UUID)
	})

	t.Run("linear chain", func(t *testing.T) {
		entries := []transcriptEntry{
			{Type: "user", UUID: "a"},
			{Type: "assistant", UUID: "b", ParentUUID: "a"},
			{Type: "user", UUID: "c", ParentUUID: "b"},
		}
		result := buildConversationChain(entries)
		require.Len(t, result, 3)
		assert.Equal(t, "a", result[0].UUID)
		assert.Equal(t, "b", result[1].UUID)
		assert.Equal(t, "c", result[2].UUID)
	})

	t.Run("only progress entries returns empty", func(t *testing.T) {
		entries := []transcriptEntry{
			{Type: "progress", UUID: "a"},
			{Type: "progress", UUID: "b", ParentUUID: "a"},
		}
		result := buildConversationChain(entries)
		assert.Empty(t, result)
	})
}

// ---------------------------------------------------------------------------
// SessionMessage type tests
// ---------------------------------------------------------------------------

func TestSessionMessageType(t *testing.T) {
	t.Run("creation", func(t *testing.T) {
		msg := SessionMessage{
			Type:      "user",
			UUID:      "abc",
			SessionID: "sess",
			Message:   map[string]any{"role": "user", "content": "hi"},
		}
		assert.Equal(t, "user", msg.Type)
		assert.Equal(t, "abc", msg.UUID)
		assert.Equal(t, "sess", msg.SessionID)
		assert.Equal(t, map[string]any{"role": "user", "content": "hi"}, msg.Message)
	})
}

// ---------------------------------------------------------------------------
// CreatedAt tests
// ---------------------------------------------------------------------------

func TestCreatedAtExtraction(t *testing.T) {
	t.Run("from ISO timestamp", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		sid := nextTestUUID(t)
		filePath := filepath.Join(projDir, sid+".jsonl")
		lines := jsonLine(map[string]any{
			"type": "user", "message": map[string]any{"content": "hello"}, "timestamp": "2026-01-15T10:30:00.000Z",
		}) + "\n" +
			jsonLine(map[string]any{
				"type": "assistant", "message": map[string]any{"content": "hi"}, "timestamp": "2026-01-15T10:35:00.000Z",
			}) + "\n"
		require.NoError(t, os.WriteFile(filePath, []byte(lines), 0o644))

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		// 2026-01-15T10:30:00Z = 1768473000 seconds = 1768473000000 ms
		require.NotNil(t, sessions[0].CreatedAt)
		assert.Equal(t, int64(1768473000000), *sessions[0].CreatedAt)
	})

	t.Run("none when missing", func(t *testing.T) {
		configDir := setupConfigDir(t)
		projectPath := filepath.Join(t.TempDir(), "proj")
		require.NoError(t, os.MkdirAll(projectPath, 0o755))
		realPath, err := filepath.EvalSymlinks(projectPath)
		require.NoError(t, err)

		projDir := makeProjectDir(t, configDir, realPath)
		makeSessionFile(t, projDir, "", makeSessionOpts{firstPrompt: "no timestamp"})

		sessions, err := ListSessions(
			WithSessionDirectory(projectPath),
			WithIncludeWorktrees(false),
			withTestConfigDir(configDir),
		)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Nil(t, sessions[0].CreatedAt)
	})

	t.Run("without Z suffix", func(t *testing.T) {
		sid := nextTestUUID(t)
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, sid+".jsonl")
		lines := jsonLine(map[string]any{
			"type": "user", "message": map[string]any{"content": "hello"}, "timestamp": "2026-01-15T10:30:00+00:00",
		}) + "\n"
		require.NoError(t, os.WriteFile(filePath, []byte(lines), 0o644))

		lite := readSessionLite(filePath)
		require.NotNil(t, lite)
		info := parseSessionInfoFromLite(sid, lite, "")
		require.NotNil(t, info)
		require.NotNil(t, info.CreatedAt)
		assert.Equal(t, int64(1768473000000), *info.CreatedAt)
	})
}

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

func setupConfigDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".claude")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "projects"), 0o755))
	return dir
}

// withTestConfigDir is a test-only option that overrides the config directory.
func withTestConfigDir(dir string) SessionOption {
	return func(o *sessionOptions) {
		o.configDir = dir
	}
}
