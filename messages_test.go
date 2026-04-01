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
	// Unknown message types should return (nil, nil) for forward compatibility.
	// The SDK must not crash when the CLI emits new message types that the
	// SDK doesn't know about yet.
	input := `{"type": "unknown_type"}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err, "unknown message types should not return an error")
	assert.Nil(t, msg, "unknown message types should return nil message")
}

func TestParseRateLimitEvent(t *testing.T) {
	// CLI v2.1.45+ emits rate_limit_event messages. The SDK should
	// skip these gracefully rather than crashing.
	t.Run("allowed warning", func(t *testing.T) {
		input := `{
			"type": "rate_limit_event",
			"rate_limit_info": {
				"status": "allowed_warning",
				"resetsAt": 1700000000,
				"rateLimitType": "five_hour",
				"utilization": 0.85,
				"isUsingOverage": false
			},
			"uuid": "550e8400-e29b-41d4-a716-446655440000",
			"session_id": "test-session-id"
		}`

		msg, err := parseMessage(json.RawMessage(input))
		require.NoError(t, err, "rate_limit_event should not return an error")
		assert.Nil(t, msg, "rate_limit_event should return nil message")
	})

	t.Run("rejected", func(t *testing.T) {
		input := `{
			"type": "rate_limit_event",
			"rate_limit_info": {
				"status": "rejected",
				"resetsAt": 1700003600,
				"rateLimitType": "seven_day",
				"isUsingOverage": false,
				"overageStatus": "rejected",
				"overageDisabledReason": "out_of_credits"
			},
			"uuid": "660e8400-e29b-41d4-a716-446655440001",
			"session_id": "test-session-id"
		}`

		msg, err := parseMessage(json.RawMessage(input))
		require.NoError(t, err, "rate_limit_event should not return an error")
		assert.Nil(t, msg, "rate_limit_event should return nil message")
	})
}

func TestParseForwardCompatibility(t *testing.T) {
	// The SDK should handle arbitrary future message types gracefully.
	futureTypes := []string{
		"some_future_event",
		"debug_info",
		"telemetry",
		"performance_metrics",
		"billing_update",
	}

	for _, msgType := range futureTypes {
		t.Run(msgType, func(t *testing.T) {
			input := `{"type": "` + msgType + `", "data": {"key": "value"}}`

			msg, err := parseMessage(json.RawMessage(input))
			require.NoError(t, err, "future message type %q should not return an error", msgType)
			assert.Nil(t, msg, "future message type %q should return nil message", msgType)
		})
	}
}

func TestParseKnownTypesStillWork(t *testing.T) {
	// After the forward-compatibility change, known types must still parse normally.
	input := `{
		"type": "assistant",
		"message": {
			"content": [{"type": "text", "text": "hello"}],
			"model": "claude-sonnet-4-6-20250929"
		}
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)
	require.NotNil(t, msg, "known message types must not return nil")

	assistantMsg, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage, got %T", msg)
	assert.Equal(t, "hello", assistantMsg.GetText())
}

func TestParseMalformedKnownTypeStillErrors(t *testing.T) {
	// Malformed data for a known type should still return an error.
	// The forward-compatibility change only affects truly unknown types.
	// Use invalid JSON in the content field to trigger a real parse error.
	input := `{"type": "assistant", "message": {"content": "not-an-array", "model": "test"}}`

	_, err := parseMessage(json.RawMessage(input))
	require.Error(t, err, "malformed known types should still return errors")
}

func TestParseMissingTypeFieldStillErrors(t *testing.T) {
	// A message with no "type" field at all is not forward-compatible --
	// it's genuinely broken. This should still be a MessageParseError.
	input := `{"data": "no type field here"}`

	msg, err := parseMessage(json.RawMessage(input))
	require.Error(t, err, "missing type field should return an error")
	assert.Nil(t, msg)

	var parseErr *MessageParseError
	assert.ErrorAs(t, err, &parseErr, "missing type should be a MessageParseError")
}

func TestParseEmptyTypeFieldStillErrors(t *testing.T) {
	// An explicit empty string type is also invalid.
	input := `{"type": ""}`

	msg, err := parseMessage(json.RawMessage(input))
	require.Error(t, err, "empty type field should return an error")
	assert.Nil(t, msg)

	var parseErr *MessageParseError
	assert.ErrorAs(t, err, &parseErr, "empty type should be a MessageParseError")
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
