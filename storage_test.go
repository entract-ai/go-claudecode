package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeProjectPath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name       string
		projectDir string
		want       string
	}{
		{
			name:       "simple path",
			projectDir: "/Users/bob/myproject",
			want:       filepath.Join(home, ".claude/projects/-Users-bob-myproject"),
		},
		{
			name:       "nested path",
			projectDir: "/Users/bob/src/praxis/projects/bpowers",
			want:       filepath.Join(home, ".claude/projects/-Users-bob-src-praxis-projects-bpowers"),
		},
		{
			name:       "root path",
			projectDir: "/tmp",
			want:       filepath.Join(home, ".claude/projects/-tmp"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClaudeCodeProjectPath(tt.projectDir)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTranscriptPath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	projectDir := "/Users/bob/myproject"
	sessionID := "abc123-def456"

	got, err := TranscriptPath(projectDir, sessionID)
	require.NoError(t, err)

	want := filepath.Join(home, ".claude/projects/-Users-bob-myproject/abc123-def456.jsonl")
	assert.Equal(t, want, got)
}

func TestGetSessionMessagesFromTranscript(t *testing.T) {
	t.Run("non-existent transcript returns nil", func(t *testing.T) {
		messages, err := GetSessionMessagesFromTranscript("/nonexistent/path", "nonexistent-session")
		require.NoError(t, err)
		assert.Nil(t, messages)
	})

	t.Run("parse mock transcript", func(t *testing.T) {
		// Use a temp directory as the "project"
		tmpProjectDir := t.TempDir()
		sessionID := "test-session-id"

		// Compute where Claude Code would look for the transcript
		transcriptPath, err := TranscriptPath(tmpProjectDir, sessionID)
		require.NoError(t, err)

		// Create the directory structure
		err = os.MkdirAll(filepath.Dir(transcriptPath), 0o755)
		require.NoError(t, err)

		// Clean up the created directory after test (since it's in ~/.claude/projects/)
		t.Cleanup(func() {
			os.RemoveAll(filepath.Dir(transcriptPath))
		})

		// Create a mock transcript with user and assistant messages, plus internal
		// message types that should be skipped
		transcript := `{"type": "user", "message": {"role": "user", "content": "Hello"}, "uuid": "123"}
{"type": "assistant", "message": {"model": "claude-3", "content": [{"type": "text", "text": "Hi there!"}]}}
{"type": "queue-operation", "internal": "data"}
{"type": "result", "subtype": "success", "duration_ms": 100, "num_turns": 1, "session_id": "test"}`

		err = os.WriteFile(transcriptPath, []byte(transcript), 0o644)
		require.NoError(t, err)

		// Test parsing
		messages, err := GetSessionMessagesFromTranscript(tmpProjectDir, sessionID)
		require.NoError(t, err)
		require.NotNil(t, messages)

		// Should have user and assistant messages (result/system are not converted
		// to chat messages, queue-operation is skipped as unknown type)
		assert.Len(t, messages, 2)
		assert.Equal(t, "user", string(messages[0].Role))
		assert.Equal(t, "assistant", string(messages[1].Role))
	})
}
