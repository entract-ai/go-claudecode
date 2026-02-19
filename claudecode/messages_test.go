package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUserMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *UserMessage
		wantErr bool
	}{
		{
			name: "string content",
			input: `{
				"type": "user",
				"message": {"role": "user", "content": "Hello"},
				"uuid": "abc123"
			}`,
			want: &UserMessage{
				Content: "Hello",
				UUID:    "abc123",
			},
		},
		{
			name: "array content with text block",
			input: `{
				"type": "user",
				"message": {
					"role": "user",
					"content": [{"type": "text", "text": "Hello world"}]
				}
			}`,
			want: &UserMessage{
				Content: []ContentBlock{TextBlock{Text: "Hello world"}},
			},
		},
		{
			name: "with tool result map",
			input: `{
				"type": "user",
				"message": {"role": "user", "content": "result"},
				"parent_tool_use_id": "tool_123",
				"tool_use_result": {"status": "success"}
			}`,
			want: &UserMessage{
				Content:         "result",
				ParentToolUseID: "tool_123",
				ToolUseResult:   map[string]any{"status": "success"},
			},
		},
		{
			name: "with tool result array",
			input: `{
				"type": "user",
				"message": {"role": "user"},
				"parent_tool_use_id": "tool_456",
				"tool_use_result": [{"type": "text", "text": "file contents here"}]
			}`,
			want: &UserMessage{
				ParentToolUseID: "tool_456",
				ToolUseResult:   []any{map[string]any{"type": "text", "text": "file contents here"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := parseMessage(json.RawMessage(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			userMsg, ok := msg.(*UserMessage)
			require.True(t, ok, "expected *UserMessage, got %T", msg)

			assert.Equal(t, tt.want.UUID, userMsg.UUID)
			assert.Equal(t, tt.want.ParentToolUseID, userMsg.ParentToolUseID)

			// Compare content type
			switch want := tt.want.Content.(type) {
			case string:
				got, ok := userMsg.Content.(string)
				require.True(t, ok, "expected string content, got %T", userMsg.Content)
				assert.Equal(t, want, got)
			case []ContentBlock:
				got, ok := userMsg.Content.([]ContentBlock)
				require.True(t, ok, "expected []ContentBlock, got %T", userMsg.Content)
				assert.Equal(t, len(want), len(got))
			}
		})
	}
}

func TestParseAssistantMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "text content",
			input: `{
				"type": "assistant",
				"message": {
					"model": "claude-3-5-sonnet",
					"content": [{"type": "text", "text": "Hello!"}]
				}
			}`,
		},
		{
			name: "with tool use",
			input: `{
				"type": "assistant",
				"message": {
					"model": "claude-3-5-sonnet",
					"content": [
						{"type": "text", "text": "Let me help."},
						{
							"type": "tool_use",
							"id": "tool_123",
							"name": "Bash",
							"input": {"command": "ls"}
						}
					]
				}
			}`,
		},
		{
			name: "with thinking",
			input: `{
				"type": "assistant",
				"message": {
					"model": "claude-3-5-sonnet",
					"content": [
						{
							"type": "thinking",
							"thinking": "Analyzing the problem...",
							"signature": "sig123"
						},
						{"type": "text", "text": "Here's my analysis."}
					]
				}
			}`,
		},
		{
			name: "with error",
			input: `{
				"type": "assistant",
				"message": {
					"model": "claude-3-5-sonnet",
					"content": []
				},
				"error": "rate_limit"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := parseMessage(json.RawMessage(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			assistantMsg, ok := msg.(*AssistantMessage)
			require.True(t, ok, "expected *AssistantMessage, got %T", msg)
			require.NotNil(t, assistantMsg)
		})
	}
}

func TestParseAssistantMessage_ErrorFromTopLevel(t *testing.T) {
	input := `{
		"type": "assistant",
		"message": {
			"model": "<synthetic>",
			"content": [{"type": "text", "text": "Rate limit exceeded"}]
		},
		"error": "rate_limit"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	assistantMsg, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage, got %T", msg)
	assert.Equal(t, ErrRateLimit, assistantMsg.Error)
}

func TestParseResultMessage(t *testing.T) {
	input := `{
		"type": "result",
		"subtype": "success",
		"duration_ms": 1500,
		"duration_api_ms": 1200,
		"is_error": false,
		"num_turns": 3,
		"session_id": "session_abc",
		"total_cost_usd": 0.0045,
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_read": 10
		},
		"result": "Task completed successfully"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	resultMsg, ok := msg.(*ResultMessage)
	require.True(t, ok, "expected *ResultMessage, got %T", msg)

	assert.Equal(t, "success", resultMsg.Subtype)
	assert.Equal(t, 1500, resultMsg.DurationMS)
	assert.Equal(t, 1200, resultMsg.DurationAPIMS)
	assert.False(t, resultMsg.IsError)
	assert.Equal(t, 3, resultMsg.NumTurns)
	assert.Equal(t, "session_abc", resultMsg.SessionID)
	require.NotNil(t, resultMsg.TotalCostUSD)
	assert.InDelta(t, 0.0045, *resultMsg.TotalCostUSD, 0.0001)
	require.NotNil(t, resultMsg.Usage)
	assert.Equal(t, 100, resultMsg.Usage.InputTokens)
	assert.Equal(t, 50, resultMsg.Usage.OutputTokens)
}

func TestParseSystemMessage(t *testing.T) {
	input := `{
		"type": "system",
		"subtype": "status_update",
		"status": "processing"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	systemMsg, ok := msg.(*SystemMessage)
	require.True(t, ok, "expected *SystemMessage, got %T", msg)

	assert.Equal(t, "status_update", systemMsg.Subtype)
	assert.Equal(t, "processing", systemMsg.Data["status"])
}

func TestParseStreamEvent(t *testing.T) {
	input := `{
		"type": "stream_event",
		"uuid": "event_123",
		"session_id": "session_abc",
		"event": {"type": "content_block_delta", "delta": {"text": "Hello"}},
		"parent_tool_use_id": "tool_456"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	streamEvent, ok := msg.(*StreamEvent)
	require.True(t, ok, "expected *StreamEvent, got %T", msg)

	assert.Equal(t, "event_123", streamEvent.UUID)
	assert.Equal(t, "session_abc", streamEvent.SessionID)
	assert.Equal(t, "tool_456", streamEvent.ParentToolUseID)
	assert.NotNil(t, streamEvent.Event)
}

func TestParseControlRequest(t *testing.T) {
	input := `{
		"type": "control_request",
		"request_id": "req_123",
		"request": {"subtype": "can_use_tool", "tool_name": "Bash"}
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	controlReq, ok := msg.(*ControlRequest)
	require.True(t, ok, "expected *ControlRequest, got %T", msg)

	assert.Equal(t, "control_request", controlReq.Type)
	assert.Equal(t, "req_123", controlReq.RequestID)
}

func TestParseUnknownType(t *testing.T) {
	input := `{"type": "unknown_type"}`

	_, err := parseMessage(json.RawMessage(input))
	require.Error(t, err)

	assert.ErrorIs(t, err, ErrUnknownMessageType)
	assert.Contains(t, err.Error(), "unknown_type")
}

func TestUserMessage_GetText(t *testing.T) {
	tests := []struct {
		name    string
		content any
		want    string
	}{
		{
			name:    "string content",
			content: "Hello world",
			want:    "Hello world",
		},
		{
			name: "single text block",
			content: []ContentBlock{
				TextBlock{Text: "Hello"},
			},
			want: "Hello",
		},
		{
			name: "multiple text blocks",
			content: []ContentBlock{
				TextBlock{Text: "Hello"},
				TextBlock{Text: "World"},
			},
			want: "Hello\nWorld",
		},
		{
			name: "mixed blocks",
			content: []ContentBlock{
				TextBlock{Text: "Result"},
				ToolResultBlock{ToolUseID: "123"},
			},
			want: "Result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &UserMessage{Content: tt.content}
			assert.Equal(t, tt.want, msg.GetText())
		})
	}
}

func TestAssistantMessage_GetText(t *testing.T) {
	msg := &AssistantMessage{
		Content: []ContentBlock{
			TextBlock{Text: "First"},
			ToolUseBlock{ID: "123", Name: "Bash"},
			TextBlock{Text: "Second"},
		},
	}

	assert.Equal(t, "First\nSecond", msg.GetText())
}

func TestAssistantMessage_GetToolCalls(t *testing.T) {
	msg := &AssistantMessage{
		Content: []ContentBlock{
			TextBlock{Text: "Let me help"},
			ToolUseBlock{ID: "tool_1", Name: "Bash", Input: map[string]any{"command": "ls"}},
			TextBlock{Text: "And also"},
			ToolUseBlock{ID: "tool_2", Name: "Read", Input: map[string]any{"path": "/file"}},
		},
	}

	calls := msg.GetToolCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "tool_1", calls[0].ID)
	assert.Equal(t, "Bash", calls[0].Name)
	assert.Equal(t, "tool_2", calls[1].ID)
	assert.Equal(t, "Read", calls[1].Name)
}
