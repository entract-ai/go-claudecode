package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTranscript(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantCount  int
		wantTypes  []string // expected message types (user, assistant, etc.)
		wantErr    bool
		errContain string
	}{
		{
			name: "valid JSONL with user and assistant messages",
			content: `{"type":"user","message":{"role":"user","content":"Hello"},"uuid":"u1"}
{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"text","text":"Hi there!"}]}}
{"type":"result","subtype":"success","duration_ms":100,"duration_api_ms":80,"is_error":false,"num_turns":1,"session_id":"s1"}`,
			wantCount: 3,
			wantTypes: []string{"user", "assistant", "result"},
		},
		{
			name:      "empty file returns empty slice",
			content:   "",
			wantCount: 0,
			wantTypes: []string{},
		},
		{
			name: "empty lines are skipped",
			content: `{"type":"user","message":{"role":"user","content":"Hello"},"uuid":"u1"}

{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"text","text":"Hi!"}]}}
`,
			wantCount: 2,
			wantTypes: []string{"user", "assistant"},
		},
		{
			name: "whitespace-only lines are skipped",
			content: `{"type":"user","message":{"role":"user","content":"Hello"},"uuid":"u1"}


{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"text","text":"Hi!"}]}}`,
			wantCount: 2,
			wantTypes: []string{"user", "assistant"},
		},
		{
			name: "malformed JSON returns error with line number",
			content: `{"type":"user","message":{"role":"user","content":"Hello"},"uuid":"u1"}
{invalid json here}
{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"text","text":"Hi!"}]}}`,
			wantErr:    true,
			errContain: "line 2",
		},
		{
			name: "system message is parsed",
			content: `{"type":"system","subtype":"status_update","status":"processing"}
{"type":"user","message":{"role":"user","content":"Hello"},"uuid":"u1"}`,
			wantCount: 2,
			wantTypes: []string{"system", "user"},
		},
		{
			name: "stream event is parsed",
			content: `{"type":"stream_event","uuid":"e1","session_id":"s1","event":{"type":"delta"}}
{"type":"user","message":{"role":"user","content":"Hello"},"uuid":"u1"}`,
			wantCount: 2,
			wantTypes: []string{"stream_event", "user"},
		},
		{
			name:      "thinking block in assistant message",
			content:   `{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"thinking","thinking":"Let me think...","signature":"sig1"},{"type":"text","text":"Here's my answer"}]}}`,
			wantCount: 1,
			wantTypes: []string{"assistant"},
		},
		{
			name: "tool use and result",
			content: `{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file1\nfile2"}]},"parent_tool_use_id":"t1"}`,
			wantCount: 2,
			wantTypes: []string{"assistant", "user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test content to temp file
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "transcript.jsonl")
			err := os.WriteFile(path, []byte(tt.content), 0o644)
			require.NoError(t, err)

			// Parse the transcript
			messages, err := ParseTranscript(path)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			assert.Len(t, messages, tt.wantCount)

			// Verify message types
			for i, wantType := range tt.wantTypes {
				switch wantType {
				case "user":
					_, ok := messages[i].(*UserMessage)
					assert.True(t, ok, "message %d: expected *UserMessage, got %T", i, messages[i])
				case "assistant":
					_, ok := messages[i].(*AssistantMessage)
					assert.True(t, ok, "message %d: expected *AssistantMessage, got %T", i, messages[i])
				case "system":
					_, ok := messages[i].(*SystemMessage)
					assert.True(t, ok, "message %d: expected *SystemMessage, got %T", i, messages[i])
				case "result":
					_, ok := messages[i].(*ResultMessage)
					assert.True(t, ok, "message %d: expected *ResultMessage, got %T", i, messages[i])
				case "stream_event":
					_, ok := messages[i].(*StreamEvent)
					assert.True(t, ok, "message %d: expected *StreamEvent, got %T", i, messages[i])
				}
			}
		})
	}
}

func TestParseTranscript_UnknownTypesSkipped(t *testing.T) {
	// Unknown message types should be silently skipped in transcript
	// parsing, not cause errors. rate_limit_event is now a known type
	// and should be included in the results.
	content := `{"type":"user","message":{"role":"user","content":"Hello"},"uuid":"u1"}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning"},"uuid":"rle-1","session_id":"s1"}
{"type":"some_future_event","data":"whatever"}
{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"text","text":"Hi!"}]}}`

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "transcript.jsonl")
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)

	messages, err := ParseTranscript(path)
	require.NoError(t, err, "unknown message types in transcript should not cause errors")
	assert.Len(t, messages, 3, "rate_limit_event is now known; only some_future_event should be skipped")

	_, ok := messages[0].(*UserMessage)
	assert.True(t, ok, "first message should be UserMessage")
	_, ok = messages[1].(*RateLimitEvent)
	assert.True(t, ok, "second message should be RateLimitEvent")
	_, ok = messages[2].(*AssistantMessage)
	assert.True(t, ok, "third message should be AssistantMessage")
}

func TestParseTranscript_FileNotFound(t *testing.T) {
	_, err := ParseTranscript("/nonexistent/path/to/transcript.jsonl")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open transcript file")
}

func TestParseTranscript_AssistantMessageContents(t *testing.T) {
	content := `{"type":"assistant","message":{"model":"claude-3-5-sonnet","content":[{"type":"thinking","thinking":"I'll analyze this","signature":"sig123"},{"type":"text","text":"Here's my response"},{"type":"tool_use","id":"tool_1","name":"Read","input":{"file_path":"/test"}}]}}`

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "transcript.jsonl")
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)

	messages, err := ParseTranscript(path)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	assistantMsg, ok := messages[0].(*AssistantMessage)
	require.True(t, ok)
	require.Len(t, assistantMsg.Content, 3)

	// Verify thinking block
	thinkingBlock, ok := assistantMsg.Content[0].(ThinkingBlock)
	require.True(t, ok)
	assert.Equal(t, "I'll analyze this", thinkingBlock.Thinking)
	assert.Equal(t, "sig123", thinkingBlock.Signature)

	// Verify text block
	textBlock, ok := assistantMsg.Content[1].(TextBlock)
	require.True(t, ok)
	assert.Equal(t, "Here's my response", textBlock.Text)

	// Verify tool use block
	toolUseBlock, ok := assistantMsg.Content[2].(ToolUseBlock)
	require.True(t, ok)
	assert.Equal(t, "tool_1", toolUseBlock.ID)
	assert.Equal(t, "Read", toolUseBlock.Name)
}
