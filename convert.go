package claudecode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/bpowers/go-claudecode/chat"
)

// systemReminderRegex matches <system-reminder>...</system-reminder> tags and any trailing whitespace
var systemReminderRegex = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>\s*`)

// stripSystemReminders removes system-reminder tags from text content.
// This is used when converting transcripts to clean persisted messages.
func stripSystemReminders(text string) string {
	return strings.TrimSpace(systemReminderRegex.ReplaceAllString(text, ""))
}

// ToChatMessage converts a single claudecode.Message to chat.Message.
// Returns an error for unsupported message types (system, result, stream_event, control).
func ToChatMessage(msg Message) (chat.Message, error) {
	switch m := msg.(type) {
	case *UserMessage:
		return userMessageToChatMessage(m)
	case *AssistantMessage:
		return assistantMessageToChatMessage(m)
	case *SystemMessage:
		return chat.Message{}, fmt.Errorf("cannot convert SystemMessage to chat.Message")
	case *TaskStartedMessage:
		return chat.Message{}, fmt.Errorf("cannot convert TaskStartedMessage to chat.Message")
	case *TaskProgressMessage:
		return chat.Message{}, fmt.Errorf("cannot convert TaskProgressMessage to chat.Message")
	case *TaskNotificationMessage:
		return chat.Message{}, fmt.Errorf("cannot convert TaskNotificationMessage to chat.Message")
	case *ResultMessage:
		return chat.Message{}, fmt.Errorf("cannot convert ResultMessage to chat.Message")
	case *StreamEvent:
		return chat.Message{}, fmt.Errorf("cannot convert StreamEvent to chat.Message")
	case *ControlRequest:
		return chat.Message{}, fmt.Errorf("cannot convert ControlRequest to chat.Message")
	case *ControlResponse:
		return chat.Message{}, fmt.Errorf("cannot convert ControlResponse to chat.Message")
	case *ControlCancelRequest:
		return chat.Message{}, fmt.Errorf("cannot convert ControlCancelRequest to chat.Message")
	default:
		return chat.Message{}, fmt.Errorf("cannot convert unknown message type %T to chat.Message", msg)
	}
}

// ToChatMessages converts a slice of claudecode messages to chat messages.
// Filters to only user and assistant messages, silently skipping others.
// Builds a lookup map to populate tool names on ToolResultBlock conversions.
func ToChatMessages(messages []Message) ([]chat.Message, error) {
	// First pass: build a map of tool_use_id -> tool_name from assistant messages
	toolNameMap := make(map[string]string)
	for _, msg := range messages {
		if am, ok := msg.(*AssistantMessage); ok {
			for _, block := range am.Content {
				if tu, ok := block.(ToolUseBlock); ok {
					toolNameMap[tu.ID] = tu.Name
				}
			}
		}
	}

	// Second pass: convert messages using the tool name map
	var result []chat.Message
	for _, msg := range messages {
		switch m := msg.(type) {
		case *UserMessage:
			chatMsg, err := userMessageToChatMessageWithToolNames(m, toolNameMap)
			if err != nil {
				return nil, fmt.Errorf("ToChatMessages: %w", err)
			}
			result = append(result, chatMsg)
		case *AssistantMessage:
			chatMsg, err := assistantMessageToChatMessage(m)
			if err != nil {
				return nil, fmt.Errorf("ToChatMessages: %w", err)
			}
			result = append(result, chatMsg)
		}
	}
	return result, nil
}

func userMessageToChatMessage(m *UserMessage) (chat.Message, error) {
	return userMessageToChatMessageWithToolNames(m, nil)
}

func userMessageToChatMessageWithToolNames(m *UserMessage, toolNameMap map[string]string) (chat.Message, error) {
	var contents []chat.Content

	// Track tool IDs we've already seen to avoid duplicates
	seenToolIDs := make(map[string]bool)

	switch c := m.Content.(type) {
	case nil:
		// Content can be nil when only ToolUseResult is present
	case string:
		contents = append(contents, chat.Content{Text: stripSystemReminders(c)})
	case []ContentBlock:
		for _, block := range c {
			content, err := contentBlockToChatContentWithToolNames(block, toolNameMap)
			if err != nil {
				return chat.Message{}, fmt.Errorf("userMessageToChatMessageWithToolNames: %w", err)
			}
			if content != nil {
				// Track tool result IDs to avoid duplicates
				if content.ToolResult != nil {
					seenToolIDs[content.ToolResult.ToolCallID] = true
				}
				contents = append(contents, *content)
			}
		}
	case []any:
		// Handle JSON-unmarshaled content that arrives as []any containing map[string]any
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := m["type"].(string)
			switch blockType {
			case "tool_result":
				toolUseID, _ := m["tool_use_id"].(string)
				content := extractMapContent(m, "content")
				isError, _ := m["is_error"].(bool)
				tr := &chat.ToolResult{ToolCallID: toolUseID}
				if toolNameMap != nil {
					tr.Name = toolNameMap[toolUseID]
				}
				if isError {
					tr.Error = content
				} else {
					tr.Content = content
				}
				seenToolIDs[toolUseID] = true
				contents = append(contents, chat.Content{ToolResult: tr})
			case "text":
				text, _ := m["text"].(string)
				contents = append(contents, chat.Content{Text: stripSystemReminders(text)})
			}
		}
	default:
		return chat.Message{}, fmt.Errorf("userMessageToChatMessageWithToolNames: unexpected content type %T", c)
	}

	// Handle ToolUseResult if not already in Content (can be map or array)
	if m.ToolUseResult != nil {
		toolID := m.ParentToolUseID
		var resultContent string
		var isError bool

		switch tur := m.ToolUseResult.(type) {
		case map[string]any:
			// Get tool ID from map if present
			if id, ok := tur["tool_use_id"].(string); ok && id != "" {
				toolID = id
			}
			isError, _ = tur["is_error"].(bool)
			resultContent = extractMapContent(tur, "content")
		case []any:
			// Array format: extract text content from blocks
			resultContent = extractArrayContent(tur)
		}

		// Only add if we haven't seen this tool ID already
		if toolID != "" && !seenToolIDs[toolID] {
			tr := &chat.ToolResult{ToolCallID: toolID}
			if toolNameMap != nil {
				tr.Name = toolNameMap[toolID]
			}
			if isError {
				tr.Error = resultContent
			} else {
				tr.Content = resultContent
			}
			contents = append(contents, chat.Content{ToolResult: tr})
		}
	}

	return chat.Message{
		Role:     chat.UserRole,
		Contents: contents,
	}, nil
}

// extractArrayContent extracts text content from an array of content blocks.
func extractArrayContent(arr []any) string {
	var parts []string
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractMapContent extracts a string content from a map at the given key.
// Handles both string and []any (array of text blocks) content formats.
func extractMapContent(m map[string]any, key string) string {
	content, ok := m[key]
	if !ok {
		return ""
	}
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			if cm, ok := item.(map[string]any); ok {
				if text, ok := cm["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func assistantMessageToChatMessage(m *AssistantMessage) (chat.Message, error) {
	var contents []chat.Content

	for _, block := range m.Content {
		content, err := contentBlockToChatContent(block)
		if err != nil {
			return chat.Message{}, fmt.Errorf("assistantMessageToChatMessage: %w", err)
		}
		if content != nil {
			contents = append(contents, *content)
		}
	}

	return chat.Message{
		Role:     chat.AssistantRole,
		Contents: contents,
	}, nil
}

// contentBlockToChatContent converts a claudecode ContentBlock to a chat.Content.
// Returns nil for unknown block types (for forward compatibility).
func contentBlockToChatContent(block ContentBlock) (*chat.Content, error) {
	return contentBlockToChatContentWithToolNames(block, nil)
}

func contentBlockToChatContentWithToolNames(block ContentBlock, toolNameMap map[string]string) (*chat.Content, error) {
	switch b := block.(type) {
	case TextBlock:
		return &chat.Content{Text: stripSystemReminders(b.Text)}, nil

	case ThinkingBlock:
		return &chat.Content{
			Thinking: &chat.ThinkingContent{
				Text:      b.Thinking,
				Signature: b.Signature,
			},
		}, nil

	case ToolUseBlock:
		args, err := json.Marshal(b.Input)
		if err != nil {
			return nil, fmt.Errorf("marshal tool input: %w", err)
		}
		return &chat.Content{
			ToolCall: &chat.ToolCall{
				ID:        b.ID,
				Name:      b.Name,
				Arguments: args,
			},
		}, nil

	case ToolResultBlock:
		tr := &chat.ToolResult{
			ToolCallID: b.ToolUseID,
		}

		// Look up tool name from the map if available
		if toolNameMap != nil {
			tr.Name = toolNameMap[b.ToolUseID]
		}

		// Extract text content from the tool result
		contentText, err := extractToolResultContent(b.Content)
		if err != nil {
			return nil, fmt.Errorf("extractToolResultContent: %w", err)
		}

		if b.IsError {
			tr.Error = contentText
		} else {
			tr.Content = contentText
		}

		return &chat.Content{ToolResult: tr}, nil

	case UnknownContentBlock:
		// Skip unknown blocks for forward compatibility
		return nil, nil

	default:
		// Skip any other unknown block types
		return nil, nil
	}
}

// extractToolResultContent extracts text content from a tool result's content field.
func extractToolResultContent(content any) (string, error) {
	switch c := content.(type) {
	case string:
		return c, nil
	case []ContentBlock:
		var parts []string
		for _, block := range c {
			if tb, ok := block.(TextBlock); ok {
				parts = append(parts, tb.Text)
			}
		}
		return strings.Join(parts, "\n"), nil
	case []any:
		// Handle JSON-unmarshaled content arrays (arrive as []any with map[string]any elements)
		var parts []string
		for _, item := range c {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n"), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("extractToolResultContent: unexpected content type %T", c)
	}
}
