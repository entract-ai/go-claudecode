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

func TestParseAssistantMessage_WithUsage(t *testing.T) {
	// Per-turn usage is preserved on AssistantMessage.
	// The CLI emits the API's full usage dict (including cache token
	// breakdown) on every assistant message. This was previously dropped
	// by the parser, forcing consumers to wait for the aggregate in
	// ResultMessage.
	input := `{
		"type": "assistant",
		"message": {
			"content": [{"type": "text", "text": "hi"}],
			"model": "claude-opus-4-5",
			"usage": {
				"input_tokens": 100,
				"output_tokens": 50,
				"cache_read_input_tokens": 2000,
				"cache_creation_input_tokens": 500
			}
		}
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	assistantMsg, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage, got %T", msg)

	require.NotNil(t, assistantMsg.Usage, "usage should not be nil when present in message")
	// JSON numbers unmarshal to float64 in map[string]any
	assert.Equal(t, float64(100), assistantMsg.Usage["input_tokens"])
	assert.Equal(t, float64(50), assistantMsg.Usage["output_tokens"])
	assert.Equal(t, float64(2000), assistantMsg.Usage["cache_read_input_tokens"])
	assert.Equal(t, float64(500), assistantMsg.Usage["cache_creation_input_tokens"])
}

func TestParseAssistantMessage_WithoutUsage(t *testing.T) {
	// usage defaults to nil when absent (e.g. synthetic messages).
	input := `{
		"type": "assistant",
		"message": {
			"content": [{"type": "text", "text": "hi"}],
			"model": "claude-opus-4-5"
		}
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	assistantMsg, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage, got %T", msg)

	assert.Nil(t, assistantMsg.Usage, "usage should be nil when absent from message")
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
	assert.Nil(t, resultMsg.StopReason, "stop_reason should be nil when absent from JSON")
}

func TestParseResultMessage_WithStopReason(t *testing.T) {
	input := `{
		"type": "result",
		"subtype": "success",
		"duration_ms": 1000,
		"duration_api_ms": 500,
		"is_error": false,
		"num_turns": 2,
		"session_id": "session_123",
		"stop_reason": "end_turn",
		"result": "Done"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	resultMsg, ok := msg.(*ResultMessage)
	require.True(t, ok, "expected *ResultMessage, got %T", msg)

	require.NotNil(t, resultMsg.StopReason, "stop_reason should not be nil")
	assert.Equal(t, "end_turn", *resultMsg.StopReason)
	assert.Equal(t, "Done", resultMsg.Result)
}

func TestParseResultMessage_WithNullStopReason(t *testing.T) {
	input := `{
		"type": "result",
		"subtype": "error_max_turns",
		"duration_ms": 1000,
		"duration_api_ms": 500,
		"is_error": true,
		"num_turns": 10,
		"session_id": "session_123",
		"stop_reason": null
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	resultMsg, ok := msg.(*ResultMessage)
	require.True(t, ok, "expected *ResultMessage, got %T", msg)

	assert.Nil(t, resultMsg.StopReason, "explicit null stop_reason should produce nil")
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

func TestParseTaskStartedMessage(t *testing.T) {
	input := `{
		"type": "system",
		"subtype": "task_started",
		"task_id": "task-abc",
		"tool_use_id": "toolu_01",
		"description": "Reticulating splines",
		"task_type": "background",
		"uuid": "uuid-1",
		"session_id": "session-1"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	taskMsg, ok := msg.(*TaskStartedMessage)
	require.True(t, ok, "expected *TaskStartedMessage, got %T", msg)

	assert.Equal(t, "task-abc", taskMsg.TaskID)
	assert.Equal(t, "Reticulating splines", taskMsg.Description)
	assert.Equal(t, "uuid-1", taskMsg.UUID)
	assert.Equal(t, "session-1", taskMsg.SessionID)
	require.NotNil(t, taskMsg.ToolUseID)
	assert.Equal(t, "toolu_01", *taskMsg.ToolUseID)
	require.NotNil(t, taskMsg.TaskType)
	assert.Equal(t, "background", *taskMsg.TaskType)

	// Base fields should be populated
	assert.Equal(t, "task_started", taskMsg.Subtype)
	assert.NotNil(t, taskMsg.Data)
	assert.Equal(t, "task-abc", taskMsg.Data["task_id"])
}

func TestParseTaskStartedMessage_OptionalFieldsAbsent(t *testing.T) {
	input := `{
		"type": "system",
		"subtype": "task_started",
		"task_id": "task-abc",
		"description": "Working",
		"uuid": "uuid-1",
		"session_id": "session-1"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	taskMsg, ok := msg.(*TaskStartedMessage)
	require.True(t, ok, "expected *TaskStartedMessage, got %T", msg)

	assert.Equal(t, "task-abc", taskMsg.TaskID)
	assert.Equal(t, "Working", taskMsg.Description)
	assert.Nil(t, taskMsg.ToolUseID)
	assert.Nil(t, taskMsg.TaskType)
}

func TestParseTaskProgressMessage(t *testing.T) {
	input := `{
		"type": "system",
		"subtype": "task_progress",
		"task_id": "task-abc",
		"tool_use_id": "toolu_01",
		"description": "Halfway there",
		"usage": {
			"total_tokens": 1234,
			"tool_uses": 5,
			"duration_ms": 9876
		},
		"last_tool_name": "Read",
		"uuid": "uuid-2",
		"session_id": "session-1"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	taskMsg, ok := msg.(*TaskProgressMessage)
	require.True(t, ok, "expected *TaskProgressMessage, got %T", msg)

	assert.Equal(t, "task-abc", taskMsg.TaskID)
	assert.Equal(t, "Halfway there", taskMsg.Description)
	assert.Equal(t, 1234, taskMsg.Usage.TotalTokens)
	assert.Equal(t, 5, taskMsg.Usage.ToolUses)
	assert.Equal(t, 9876, taskMsg.Usage.DurationMS)
	require.NotNil(t, taskMsg.LastToolName)
	assert.Equal(t, "Read", *taskMsg.LastToolName)
	require.NotNil(t, taskMsg.ToolUseID)
	assert.Equal(t, "toolu_01", *taskMsg.ToolUseID)
	assert.Equal(t, "uuid-2", taskMsg.UUID)
	assert.Equal(t, "session-1", taskMsg.SessionID)

	// Base fields
	assert.Equal(t, "task_progress", taskMsg.Subtype)
	assert.NotNil(t, taskMsg.Data)
}

func TestParseTaskNotificationMessage(t *testing.T) {
	input := `{
		"type": "system",
		"subtype": "task_notification",
		"task_id": "task-abc",
		"tool_use_id": "toolu_01",
		"status": "completed",
		"output_file": "/tmp/out.md",
		"summary": "All done",
		"usage": {
			"total_tokens": 2000,
			"tool_uses": 7,
			"duration_ms": 12345
		},
		"uuid": "uuid-3",
		"session_id": "session-1"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	taskMsg, ok := msg.(*TaskNotificationMessage)
	require.True(t, ok, "expected *TaskNotificationMessage, got %T", msg)

	assert.Equal(t, "task-abc", taskMsg.TaskID)
	assert.Equal(t, TaskNotificationStatus("completed"), taskMsg.Status)
	assert.Equal(t, "/tmp/out.md", taskMsg.OutputFile)
	assert.Equal(t, "All done", taskMsg.Summary)
	require.NotNil(t, taskMsg.Usage)
	assert.Equal(t, 2000, taskMsg.Usage.TotalTokens)
	assert.Equal(t, 7, taskMsg.Usage.ToolUses)
	assert.Equal(t, 12345, taskMsg.Usage.DurationMS)
	require.NotNil(t, taskMsg.ToolUseID)
	assert.Equal(t, "toolu_01", *taskMsg.ToolUseID)
	assert.Equal(t, "uuid-3", taskMsg.UUID)
	assert.Equal(t, "session-1", taskMsg.SessionID)

	// Base fields
	assert.Equal(t, "task_notification", taskMsg.Subtype)
	assert.NotNil(t, taskMsg.Data)
}

func TestParseTaskNotificationMessage_OptionalFieldsAbsent(t *testing.T) {
	input := `{
		"type": "system",
		"subtype": "task_notification",
		"task_id": "task-abc",
		"status": "failed",
		"output_file": "/tmp/out.md",
		"summary": "Boom",
		"uuid": "uuid-3",
		"session_id": "session-1"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	taskMsg, ok := msg.(*TaskNotificationMessage)
	require.True(t, ok, "expected *TaskNotificationMessage, got %T", msg)

	assert.Equal(t, TaskNotificationStatus("failed"), taskMsg.Status)
	assert.Nil(t, taskMsg.Usage)
	assert.Nil(t, taskMsg.ToolUseID)
}

func TestParseSystemMessage_UnknownSubtype_YieldsGenericSystemMessage(t *testing.T) {
	// Unknown system subtypes should still produce a plain *SystemMessage,
	// not one of the typed task message types.
	input := `{
		"type": "system",
		"subtype": "some_future_subtype",
		"foo": "bar"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	// Should be *SystemMessage, not any of the task message types
	systemMsg, ok := msg.(*SystemMessage)
	require.True(t, ok, "expected *SystemMessage, got %T", msg)

	assert.Equal(t, "some_future_subtype", systemMsg.Subtype)
	assert.Equal(t, "bar", systemMsg.Data["foo"])
}

func TestParseTaskNotificationStatus_Values(t *testing.T) {
	for _, status := range []TaskNotificationStatus{
		TaskNotificationStatusCompleted,
		TaskNotificationStatusFailed,
		TaskNotificationStatusStopped,
	} {
		input := `{
			"type": "system",
			"subtype": "task_notification",
			"task_id": "t1",
			"status": "` + string(status) + `",
			"output_file": "/o",
			"summary": "s",
			"uuid": "u1",
			"session_id": "s1"
		}`

		msg, err := parseMessage(json.RawMessage(input))
		require.NoError(t, err)

		taskMsg, ok := msg.(*TaskNotificationMessage)
		require.True(t, ok, "expected *TaskNotificationMessage, got %T", msg)
		assert.Equal(t, status, taskMsg.Status)
	}
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
	// CLI v2.1.45+ emits rate_limit_event messages. The SDK parses
	// these into typed RateLimitEvent so callers can act on warnings.
	t.Run("allowed warning with full fields", func(t *testing.T) {
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
		require.NotNil(t, msg, "rate_limit_event should return a typed message")

		rle, ok := msg.(*RateLimitEvent)
		require.True(t, ok, "expected *RateLimitEvent, got %T", msg)
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", rle.UUID)
		assert.Equal(t, "test-session-id", rle.SessionID)

		info := rle.RateLimitInfo
		assert.Equal(t, RateLimitStatus("allowed_warning"), info.Status)
		require.NotNil(t, info.ResetsAt)
		assert.Equal(t, int64(1700000000), *info.ResetsAt)
		require.NotNil(t, info.RateLimitType)
		assert.Equal(t, "five_hour", *info.RateLimitType)
		require.NotNil(t, info.Utilization)
		assert.InDelta(t, 0.85, *info.Utilization, 0.001)
		// Unmodeled field preserved in Raw
		assert.Equal(t, false, info.Raw["isUsingOverage"])
	})

	t.Run("rejected with overage info", func(t *testing.T) {
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
		require.NotNil(t, msg, "rate_limit_event should return a typed message")

		rle, ok := msg.(*RateLimitEvent)
		require.True(t, ok, "expected *RateLimitEvent, got %T", msg)

		info := rle.RateLimitInfo
		assert.Equal(t, RateLimitStatus("rejected"), info.Status)
		require.NotNil(t, info.OverageStatus)
		assert.Equal(t, "rejected", *info.OverageStatus)
		require.NotNil(t, info.OverageDisabledReason)
		assert.Equal(t, "out_of_credits", *info.OverageDisabledReason)
	})

	t.Run("minimal fields - only status required", func(t *testing.T) {
		input := `{
			"type": "rate_limit_event",
			"rate_limit_info": {"status": "allowed"},
			"uuid": "770e8400-e29b-41d4-a716-446655440002",
			"session_id": "test-session-id"
		}`

		msg, err := parseMessage(json.RawMessage(input))
		require.NoError(t, err)
		require.NotNil(t, msg)

		rle, ok := msg.(*RateLimitEvent)
		require.True(t, ok, "expected *RateLimitEvent, got %T", msg)

		info := rle.RateLimitInfo
		assert.Equal(t, RateLimitStatus("allowed"), info.Status)
		assert.Nil(t, info.ResetsAt)
		assert.Nil(t, info.RateLimitType)
		assert.Nil(t, info.Utilization)
		assert.Nil(t, info.OverageStatus)
		assert.Nil(t, info.OverageResetsAt)
		assert.Nil(t, info.OverageDisabledReason)
	})

	t.Run("unknown types still return nil", func(t *testing.T) {
		input := `{
			"type": "some_future_event_type",
			"uuid": "880e8400-e29b-41d4-a716-446655440003",
			"session_id": "test-session-id"
		}`

		msg, err := parseMessage(json.RawMessage(input))
		require.NoError(t, err, "unknown types should not return an error")
		assert.Nil(t, msg, "unknown types should return nil for forward compat")
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

func TestParseAssistantMessage_WithAllNewFields(t *testing.T) {
	// AssistantMessage preserves message_id, stop_reason (from nested message),
	// and session_id, uuid (from top level).
	input := `{
		"type": "assistant",
		"message": {
			"content": [{"type": "text", "text": "Hello"}],
			"model": "claude-sonnet-4-5-20250929",
			"id": "msg_01HRq7YZE3apPqSHydvG77Ve",
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		},
		"session_id": "fdf2d90a-fd9e-4736-ae35-806edd13643f",
		"uuid": "0dbd2453-1209-4fe9-bd51-4102f64e33df"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	assistantMsg, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage, got %T", msg)

	require.NotNil(t, assistantMsg.MessageID)
	assert.Equal(t, "msg_01HRq7YZE3apPqSHydvG77Ve", *assistantMsg.MessageID)
	require.NotNil(t, assistantMsg.StopReason)
	assert.Equal(t, "end_turn", *assistantMsg.StopReason)
	require.NotNil(t, assistantMsg.SessionID)
	assert.Equal(t, "fdf2d90a-fd9e-4736-ae35-806edd13643f", *assistantMsg.SessionID)
	require.NotNil(t, assistantMsg.UUID)
	assert.Equal(t, "0dbd2453-1209-4fe9-bd51-4102f64e33df", *assistantMsg.UUID)
	require.NotNil(t, assistantMsg.Usage)
	assert.Equal(t, float64(10), assistantMsg.Usage["input_tokens"])
	assert.Equal(t, float64(5), assistantMsg.Usage["output_tokens"])
}

func TestParseAssistantMessage_NewOptionalFieldsAbsent(t *testing.T) {
	// New optional fields default to nil when absent from the JSON.
	input := `{
		"type": "assistant",
		"message": {
			"content": [{"type": "text", "text": "hi"}],
			"model": "claude-opus-4-5"
		}
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	assistantMsg, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage, got %T", msg)

	assert.Nil(t, assistantMsg.MessageID)
	assert.Nil(t, assistantMsg.StopReason)
	assert.Nil(t, assistantMsg.SessionID)
	assert.Nil(t, assistantMsg.UUID)
}

func TestParseResultMessage_WithModelUsage(t *testing.T) {
	// ResultMessage preserves modelUsage, permission_denials, and uuid.
	input := `{
		"type": "result",
		"subtype": "success",
		"duration_ms": 3000,
		"duration_api_ms": 2000,
		"is_error": false,
		"num_turns": 1,
		"session_id": "fdf2d90a-fd9e-4736-ae35-806edd13643f",
		"stop_reason": "end_turn",
		"total_cost_usd": 0.0106,
		"usage": {"input_tokens": 3, "output_tokens": 24},
		"result": "Hello",
		"modelUsage": {
			"claude-sonnet-4-5-20250929": {
				"inputTokens": 3,
				"outputTokens": 24,
				"cacheReadInputTokens": 20012,
				"costUSD": 0.0106,
				"contextWindow": 200000,
				"maxOutputTokens": 64000
			}
		},
		"permission_denials": [],
		"uuid": "d379c496-f33a-4ea4-b920-3c5483baa6f7"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	resultMsg, ok := msg.(*ResultMessage)
	require.True(t, ok, "expected *ResultMessage, got %T", msg)

	require.NotNil(t, resultMsg.ModelUsage)
	modelEntry, ok := resultMsg.ModelUsage["claude-sonnet-4-5-20250929"]
	require.True(t, ok, "expected model key in ModelUsage")
	modelMap, ok := modelEntry.(map[string]any)
	require.True(t, ok, "expected model entry to be map[string]any")
	assert.Equal(t, 0.0106, modelMap["costUSD"])

	require.NotNil(t, resultMsg.PermissionDenials)
	assert.Empty(t, resultMsg.PermissionDenials)

	require.NotNil(t, resultMsg.UUID)
	assert.Equal(t, "d379c496-f33a-4ea4-b920-3c5483baa6f7", *resultMsg.UUID)
}

func TestParseResultMessage_NewOptionalFieldsAbsent(t *testing.T) {
	// New optional fields default to nil when absent from the JSON.
	input := `{
		"type": "result",
		"subtype": "success",
		"duration_ms": 1000,
		"duration_api_ms": 500,
		"is_error": false,
		"num_turns": 1,
		"session_id": "session_123"
	}`

	msg, err := parseMessage(json.RawMessage(input))
	require.NoError(t, err)

	resultMsg, ok := msg.(*ResultMessage)
	require.True(t, ok, "expected *ResultMessage, got %T", msg)

	assert.Nil(t, resultMsg.ModelUsage)
	assert.Nil(t, resultMsg.PermissionDenials)
	assert.Nil(t, resultMsg.UUID)
}
