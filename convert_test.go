package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/bpowers/go-claudecode/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToChatMessage_UserMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     *UserMessage
		wantErr bool
	}{
		{
			name: "string content",
			msg: &UserMessage{
				Content: "Hello world",
				UUID:    "u1",
			},
		},
		{
			name: "text block content",
			msg: &UserMessage{
				Content: []ContentBlock{
					TextBlock{Text: "First part"},
					TextBlock{Text: "Second part"},
				},
			},
		},
		{
			name: "tool result content",
			msg: &UserMessage{
				Content: []ContentBlock{
					ToolResultBlock{
						ToolUseID: "tool_1",
						Content:   "result text",
						IsError:   false,
					},
				},
				ParentToolUseID: "tool_1",
			},
		},
		{
			name: "tool result with error",
			msg: &UserMessage{
				Content: []ContentBlock{
					ToolResultBlock{
						ToolUseID: "tool_2",
						Content:   "error message",
						IsError:   true,
					},
				},
				ParentToolUseID: "tool_2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ToChatMessage(tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, chat.UserRole, result.Role)
			assert.NotEmpty(t, result.Contents)
		})
	}
}

func TestToChatMessage_UserMessage_StringContent(t *testing.T) {
	msg := &UserMessage{Content: "Hello world"}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, chat.UserRole, result.Role)
	require.Len(t, result.Contents, 1)
	assert.Equal(t, "Hello world", result.Contents[0].Text)
}

func TestToChatMessage_UserMessage_ContentBlocks(t *testing.T) {
	msg := &UserMessage{
		Content: []ContentBlock{
			TextBlock{Text: "First"},
			TextBlock{Text: "Second"},
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, chat.UserRole, result.Role)
	require.Len(t, result.Contents, 2)
	assert.Equal(t, "First", result.Contents[0].Text)
	assert.Equal(t, "Second", result.Contents[1].Text)
}

func TestToChatMessage_UserMessage_ToolResult(t *testing.T) {
	msg := &UserMessage{
		Content: []ContentBlock{
			ToolResultBlock{
				ToolUseID: "tool_1",
				Content:   "file contents here",
				IsError:   false,
			},
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	require.Len(t, result.Contents, 1)
	require.NotNil(t, result.Contents[0].ToolResult)
	assert.Equal(t, "tool_1", result.Contents[0].ToolResult.ToolCallID)
	assert.Equal(t, "file contents here", result.Contents[0].ToolResult.Content)
	assert.Empty(t, result.Contents[0].ToolResult.Error)
}

func TestToChatMessage_UserMessage_ToolResultError(t *testing.T) {
	msg := &UserMessage{
		Content: []ContentBlock{
			ToolResultBlock{
				ToolUseID: "tool_2",
				Content:   "permission denied",
				IsError:   true,
			},
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	require.Len(t, result.Contents, 1)
	require.NotNil(t, result.Contents[0].ToolResult)
	assert.Equal(t, "tool_2", result.Contents[0].ToolResult.ToolCallID)
	assert.Equal(t, "permission denied", result.Contents[0].ToolResult.Error)
	assert.Empty(t, result.Contents[0].ToolResult.Content)
}

func TestToChatMessage_AssistantMessage_Text(t *testing.T) {
	msg := &AssistantMessage{
		Content: []ContentBlock{
			TextBlock{Text: "Hello!"},
		},
		Model: "claude-3-5-sonnet",
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, chat.AssistantRole, result.Role)
	require.Len(t, result.Contents, 1)
	assert.Equal(t, "Hello!", result.Contents[0].Text)
}

func TestToChatMessage_AssistantMessage_Thinking(t *testing.T) {
	msg := &AssistantMessage{
		Content: []ContentBlock{
			ThinkingBlock{
				Thinking:  "Let me analyze this problem...",
				Signature: "sig123",
			},
			TextBlock{Text: "Here's my answer"},
		},
		Model: "claude-3-5-sonnet",
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, chat.AssistantRole, result.Role)
	require.Len(t, result.Contents, 2)

	// Thinking block
	require.NotNil(t, result.Contents[0].Thinking)
	assert.Equal(t, "Let me analyze this problem...", result.Contents[0].Thinking.Text)
	assert.Equal(t, "sig123", result.Contents[0].Thinking.Signature)

	// Text block
	assert.Equal(t, "Here's my answer", result.Contents[1].Text)
}

func TestToChatMessage_AssistantMessage_ToolUse(t *testing.T) {
	msg := &AssistantMessage{
		Content: []ContentBlock{
			TextBlock{Text: "Let me read that file"},
			ToolUseBlock{
				ID:   "tool_1",
				Name: "Read",
				Input: map[string]any{
					"file_path": "/test/file.txt",
				},
			},
		},
		Model: "claude-3-5-sonnet",
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	require.Len(t, result.Contents, 2)

	// Text
	assert.Equal(t, "Let me read that file", result.Contents[0].Text)

	// Tool call
	require.NotNil(t, result.Contents[1].ToolCall)
	assert.Equal(t, "tool_1", result.Contents[1].ToolCall.ID)
	assert.Equal(t, "Read", result.Contents[1].ToolCall.Name)

	// Verify arguments are JSON-encoded
	var args map[string]any
	err = json.Unmarshal(result.Contents[1].ToolCall.Arguments, &args)
	require.NoError(t, err)
	assert.Equal(t, "/test/file.txt", args["file_path"])
}

func TestToChatMessage_AssistantMessage_UnknownContentBlock(t *testing.T) {
	msg := &AssistantMessage{
		Content: []ContentBlock{
			TextBlock{Text: "Regular text"},
			UnknownContentBlock{Type: "future_type", Data: map[string]any{"foo": "bar"}},
			TextBlock{Text: "More text"},
		},
		Model: "claude-3-5-sonnet",
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	// Unknown blocks should be skipped for forward compatibility
	require.Len(t, result.Contents, 2)
	assert.Equal(t, "Regular text", result.Contents[0].Text)
	assert.Equal(t, "More text", result.Contents[1].Text)
}

func TestToChatMessage_SystemMessage_ReturnsError(t *testing.T) {
	msg := &SystemMessage{Subtype: "status"}
	_, err := ToChatMessage(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert")
}

func TestToChatMessage_ResultMessage_ReturnsError(t *testing.T) {
	msg := &ResultMessage{Subtype: "success"}
	_, err := ToChatMessage(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert")
}

func TestToChatMessage_StreamEvent_ReturnsError(t *testing.T) {
	msg := &StreamEvent{UUID: "e1"}
	_, err := ToChatMessage(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert")
}

func TestToChatMessage_RateLimitEvent_ReturnsError(t *testing.T) {
	msg := &RateLimitEvent{
		RateLimitInfo: RateLimitInfo{Status: "allowed_warning"},
		UUID:          "e1",
		SessionID:     "s1",
	}
	_, err := ToChatMessage(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert")
}

func TestToChatMessages_FiltersToUserAndAssistant(t *testing.T) {
	messages := []Message{
		&SystemMessage{Subtype: "init"},
		&UserMessage{Content: "Hello"},
		&AssistantMessage{Content: []ContentBlock{TextBlock{Text: "Hi!"}}, Model: "claude-3-5-sonnet"},
		&StreamEvent{UUID: "e1"},
		&ResultMessage{Subtype: "success"},
		&UserMessage{Content: "Another question"},
	}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)

	// Only user and assistant messages should be included
	require.Len(t, result, 3)
	assert.Equal(t, chat.UserRole, result[0].Role)
	assert.Equal(t, chat.AssistantRole, result[1].Role)
	assert.Equal(t, chat.UserRole, result[2].Role)
}

func TestToChatMessages_EmptyInput(t *testing.T) {
	result, err := ToChatMessages(nil)
	require.NoError(t, err)
	assert.Empty(t, result)

	result, err = ToChatMessages([]Message{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestToChatMessages_OnlySystemMessages(t *testing.T) {
	messages := []Message{
		&SystemMessage{Subtype: "init"},
		&StreamEvent{UUID: "e1"},
		&ResultMessage{Subtype: "success"},
	}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestToChatMessages_PreservesOrder(t *testing.T) {
	messages := []Message{
		&UserMessage{Content: "First"},
		&AssistantMessage{Content: []ContentBlock{TextBlock{Text: "Response 1"}}, Model: "claude"},
		&UserMessage{Content: "Second"},
		&AssistantMessage{Content: []ContentBlock{TextBlock{Text: "Response 2"}}, Model: "claude"},
	}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)

	require.Len(t, result, 4)
	assert.Equal(t, "First", result[0].GetText())
	assert.Equal(t, "Response 1", result[1].GetText())
	assert.Equal(t, "Second", result[2].GetText())
	assert.Equal(t, "Response 2", result[3].GetText())
}

func TestStripSystemReminders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no system-reminder tags",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "single system-reminder tag at start",
			input:    "<system-reminder>The user is currently viewing: models/test.sd.json</system-reminder>\n\nHello world",
			expected: "Hello world",
		},
		{
			name:     "system-reminder tag with multiline content",
			input:    "<system-reminder>Line 1\nLine 2\nLine 3</system-reminder>\n\nActual message",
			expected: "Actual message",
		},
		{
			name:     "multiple system-reminder tags",
			input:    "<system-reminder>First</system-reminder> <system-reminder>Second</system-reminder> Actual content",
			expected: "Actual content",
		},
		{
			name:     "system-reminder only",
			input:    "<system-reminder>Focus context</system-reminder>",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripSystemReminders(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToChatMessages_StripsSystemReminders(t *testing.T) {
	messages := []Message{
		&UserMessage{
			Content: "<system-reminder>The user is currently viewing: models/test.sd.json</system-reminder>\n\nWhat does this model do?",
		},
		&AssistantMessage{
			Content: []ContentBlock{TextBlock{Text: "This model simulates..."}},
			Model:   "claude",
		},
	}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)

	require.Len(t, result, 2)
	assert.Equal(t, "What does this model do?", result[0].GetText())
}

func TestToChatMessages_PopulatesToolNames(t *testing.T) {
	messages := []Message{
		&UserMessage{Content: "Please read the file"},
		&AssistantMessage{
			Content: []ContentBlock{
				TextBlock{Text: "Let me read that file"},
				ToolUseBlock{
					ID:    "tool_123",
					Name:  "Read",
					Input: map[string]any{"file_path": "/test/file.txt"},
				},
			},
			Model: "claude",
		},
		&UserMessage{
			Content: []ContentBlock{
				ToolResultBlock{
					ToolUseID: "tool_123",
					Content:   "file contents here",
					IsError:   false,
				},
			},
		},
		&AssistantMessage{
			Content: []ContentBlock{
				TextBlock{Text: "I found the file"},
				ToolUseBlock{
					ID:    "tool_456",
					Name:  "Display",
					Input: map[string]any{"html": "<div>analysis</div>"},
				},
			},
			Model: "claude",
		},
		&UserMessage{
			Content: []ContentBlock{
				ToolResultBlock{
					ToolUseID: "tool_456",
					Content:   "displayed successfully",
					IsError:   false,
				},
			},
		},
	}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)

	// Find the tool result messages and check their names
	var toolResultContents []*chat.ToolResult
	for _, msg := range result {
		for _, c := range msg.Contents {
			if c.ToolResult != nil {
				toolResultContents = append(toolResultContents, c.ToolResult)
			}
		}
	}

	require.Len(t, toolResultContents, 2)
	assert.Equal(t, "Read", toolResultContents[0].Name)
	assert.Equal(t, "tool_123", toolResultContents[0].ToolCallID)
	assert.Equal(t, "Display", toolResultContents[1].Name)
	assert.Equal(t, "tool_456", toolResultContents[1].ToolCallID)
}

func TestToChatMessage_UserMessage_ToolUseResultMap(t *testing.T) {
	msg := &UserMessage{
		Content:         "Tool result below",
		ParentToolUseID: "tool_123",
		ToolUseResult: map[string]any{
			"content":  "file contents here",
			"is_error": false,
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)
	require.Len(t, result.Contents, 2) // text + tool result

	// Verify text content
	assert.Equal(t, "Tool result below", result.Contents[0].Text)

	// Verify tool result
	require.NotNil(t, result.Contents[1].ToolResult)
	assert.Equal(t, "tool_123", result.Contents[1].ToolResult.ToolCallID)
	assert.Equal(t, "file contents here", result.Contents[1].ToolResult.Content)
	assert.Empty(t, result.Contents[1].ToolResult.Error)
}

func TestToChatMessage_UserMessage_ToolUseResultMapError(t *testing.T) {
	msg := &UserMessage{
		Content:         "Tool error below",
		ParentToolUseID: "tool_456",
		ToolUseResult: map[string]any{
			"content":  "permission denied",
			"is_error": true,
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)
	require.Len(t, result.Contents, 2)

	require.NotNil(t, result.Contents[1].ToolResult)
	assert.Equal(t, "tool_456", result.Contents[1].ToolResult.ToolCallID)
	assert.Equal(t, "permission denied", result.Contents[1].ToolResult.Error)
	assert.Empty(t, result.Contents[1].ToolResult.Content)
}

func TestToChatMessage_UserMessage_ToolUseResultWithToolUseID(t *testing.T) {
	// tool_use_id in map should take precedence over ParentToolUseID
	msg := &UserMessage{
		Content:         "Tool result below",
		ParentToolUseID: "parent_id",
		ToolUseResult: map[string]any{
			"tool_use_id": "map_id",
			"content":     "result",
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)
	require.Len(t, result.Contents, 2)

	assert.Equal(t, "map_id", result.Contents[1].ToolResult.ToolCallID)
}

func TestToChatMessage_UserMessage_ToolUseResultArrayContent(t *testing.T) {
	msg := &UserMessage{
		Content:         "Tool result below",
		ParentToolUseID: "tool_789",
		ToolUseResult: map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "first part"},
				map[string]any{"type": "text", "text": "second part"},
			},
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)
	require.Len(t, result.Contents, 2)

	require.NotNil(t, result.Contents[1].ToolResult)
	assert.Equal(t, "first part\nsecond part", result.Contents[1].ToolResult.Content)
}

func TestToChatMessage_UserMessage_ToolUseResultTopLevelArray(t *testing.T) {
	// This tests the case where tool_use_result is itself an array (not a map with content)
	msg := &UserMessage{
		ParentToolUseID: "tool_456",
		ToolUseResult: []any{
			map[string]any{"type": "text", "text": "file contents here"},
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)
	require.Len(t, result.Contents, 1)

	require.NotNil(t, result.Contents[0].ToolResult)
	assert.Equal(t, "tool_456", result.Contents[0].ToolResult.ToolCallID)
	assert.Equal(t, "file contents here", result.Contents[0].ToolResult.Content)
}

func TestToChatMessage_UserMessage_NoDuplicateToolResults(t *testing.T) {
	// When both Content and ToolUseResult have same tool ID, should not duplicate
	msg := &UserMessage{
		Content: []ContentBlock{
			ToolResultBlock{ToolUseID: "tool_123", Content: "from content"},
		},
		ParentToolUseID: "tool_123",
		ToolUseResult: map[string]any{
			"content": "from map",
		},
	}

	result, err := ToChatMessage(msg)
	require.NoError(t, err)

	// Should only have 1 tool result (from Content, not duplicated)
	var toolResults int
	for _, c := range result.Contents {
		if c.ToolResult != nil {
			toolResults++
		}
	}
	assert.Equal(t, 1, toolResults)
	// The Content block version should be used
	assert.Equal(t, "from content", result.Contents[0].ToolResult.Content)
}

func TestToChatMessages_ToolUseResultMapWithToolNames(t *testing.T) {
	messages := []Message{
		&UserMessage{Content: "Please read the file"},
		&AssistantMessage{
			Content: []ContentBlock{
				TextBlock{Text: "Let me read that file"},
				ToolUseBlock{
					ID:    "tool_123",
					Name:  "Read",
					Input: map[string]any{"file_path": "/test/file.txt"},
				},
			},
			Model: "claude",
		},
		// Tool result via ToolUseResult map instead of Content blocks
		&UserMessage{
			ParentToolUseID: "tool_123",
			ToolUseResult: map[string]any{
				"content": "file contents here",
			},
		},
	}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)

	// Find the tool result and verify it has the tool name
	var toolResult *chat.ToolResult
	for _, msg := range result {
		for _, c := range msg.Contents {
			if c.ToolResult != nil {
				toolResult = c.ToolResult
			}
		}
	}

	require.NotNil(t, toolResult)
	assert.Equal(t, "Read", toolResult.Name)
	assert.Equal(t, "tool_123", toolResult.ToolCallID)
	assert.Equal(t, "file contents here", toolResult.Content)
}

func TestToChatMessage_ToolResultBlockContentTypes(t *testing.T) {
	// Test string content
	t.Run("string content", func(t *testing.T) {
		msg := &UserMessage{
			Content: []ContentBlock{
				ToolResultBlock{
					ToolUseID: "t1",
					Content:   "plain text result",
				},
			},
		}

		result, err := ToChatMessage(msg)
		require.NoError(t, err)
		require.NotNil(t, result.Contents[0].ToolResult)
		assert.Equal(t, "plain text result", result.Contents[0].ToolResult.Content)
	})

	// Test content block array
	t.Run("content block array", func(t *testing.T) {
		msg := &UserMessage{
			Content: []ContentBlock{
				ToolResultBlock{
					ToolUseID: "t2",
					Content: []ContentBlock{
						TextBlock{Text: "first part"},
						TextBlock{Text: "second part"},
					},
				},
			},
		}

		result, err := ToChatMessage(msg)
		require.NoError(t, err)
		require.NotNil(t, result.Contents[0].ToolResult)
		// Content blocks should be joined
		assert.Equal(t, "first part\nsecond part", result.Contents[0].ToolResult.Content)
	})
}

func TestExtractToolResultContent_JSONUnmarshal(t *testing.T) {
	// JSON as it arrives from Claude Code CLI transcripts - when unmarshaled,
	// the content array becomes []any containing map[string]any, NOT []ContentBlock
	jsonData := `{
		"type": "tool_result",
		"tool_use_id": "test123",
		"content": [
			{"type": "text", "text": "First part"},
			{"type": "text", "text": "Second part"}
		]
	}`

	// Parse as a generic map to simulate what happens with JSON unmarshaling
	var raw map[string]any
	err := json.Unmarshal([]byte(jsonData), &raw)
	require.NoError(t, err)

	// The content field is now []any containing map[string]any
	content := raw["content"]

	// This is what extractToolResultContent receives after JSON unmarshal
	text, err := extractToolResultContent(content)
	require.NoError(t, err)
	assert.Equal(t, "First part\nSecond part", text)
}

func TestToChatMessages_UserMessageWithArrayAnyContent(t *testing.T) {
	// When JSON is unmarshaled without custom parsing, Content can arrive as []any
	// containing map[string]any elements instead of []ContentBlock. This simulates
	// what happens when reading transcripts from Claude Code CLI.
	msg := &UserMessage{
		Content: []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "tool_123",
				"content":     "result text",
			},
		},
	}
	messages := []Message{msg}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Contents, 1)
	require.NotNil(t, result[0].Contents[0].ToolResult)
	assert.Equal(t, "tool_123", result[0].Contents[0].ToolResult.ToolCallID)
	assert.Equal(t, "result text", result[0].Contents[0].ToolResult.Content)
}

func TestToChatMessages_UserMessageWithArrayAnyContentError(t *testing.T) {
	// Test tool_result with is_error flag in []any content
	msg := &UserMessage{
		Content: []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "tool_456",
				"content":     "permission denied",
				"is_error":    true,
			},
		},
	}
	messages := []Message{msg}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Contents, 1)
	require.NotNil(t, result[0].Contents[0].ToolResult)
	assert.Equal(t, "tool_456", result[0].Contents[0].ToolResult.ToolCallID)
	assert.Equal(t, "permission denied", result[0].Contents[0].ToolResult.Error)
	assert.Empty(t, result[0].Contents[0].ToolResult.Content)
}

func TestToChatMessages_UserMessageWithArrayAnyContentText(t *testing.T) {
	// Test text blocks in []any content
	msg := &UserMessage{
		Content: []any{
			map[string]any{
				"type": "text",
				"text": "Hello world",
			},
		},
	}
	messages := []Message{msg}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Contents, 1)
	assert.Equal(t, "Hello world", result[0].Contents[0].Text)
}

func TestToChatMessages_UserMessageWithArrayAnyContentArrayResult(t *testing.T) {
	// Test tool_result with array content (text blocks) in []any format
	msg := &UserMessage{
		Content: []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "tool_789",
				"content": []any{
					map[string]any{"type": "text", "text": "first part"},
					map[string]any{"type": "text", "text": "second part"},
				},
			},
		},
	}
	messages := []Message{msg}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Contents, 1)
	require.NotNil(t, result[0].Contents[0].ToolResult)
	assert.Equal(t, "tool_789", result[0].Contents[0].ToolResult.ToolCallID)
	assert.Equal(t, "first part\nsecond part", result[0].Contents[0].ToolResult.Content)
}

func TestToChatMessages_UserMessageWithArrayAnyPopulatesToolNames(t *testing.T) {
	// Verify tool names are populated for []any content
	messages := []Message{
		&AssistantMessage{
			Content: []ContentBlock{
				ToolUseBlock{
					ID:    "tool_123",
					Name:  "Read",
					Input: map[string]any{"file_path": "/test/file.txt"},
				},
			},
			Model: "claude",
		},
		&UserMessage{
			Content: []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "tool_123",
					"content":     "file contents",
				},
			},
		},
	}

	result, err := ToChatMessages(messages)
	require.NoError(t, err)
	require.Len(t, result, 2)

	// Find the tool result
	toolResult := result[1].Contents[0].ToolResult
	require.NotNil(t, toolResult)
	assert.Equal(t, "Read", toolResult.Name)
	assert.Equal(t, "tool_123", toolResult.ToolCallID)
}
