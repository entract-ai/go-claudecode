package claudecode

import (
	"encoding/json"
	"fmt"
)

// Message is a marker interface for all message types.
type Message interface {
	messageMarker()
}

// UserMessage represents a message from the user.
type UserMessage struct {
	Content         any    `json:"-"` // string or []ContentBlock
	UUID            string `json:"uuid,omitzero"`
	ParentToolUseID string `json:"parent_tool_use_id,omitzero"`
	ToolUseResult   any    `json:"tool_use_result,omitzero"` // map[string]any or []any
}

func (UserMessage) messageMarker() {}

// AssistantMessage represents a message from the assistant.
type AssistantMessage struct {
	Content         []ContentBlock        `json:"content"`
	Model           string                `json:"model"`
	ParentToolUseID string                `json:"parent_tool_use_id,omitzero"`
	Error           AssistantMessageError `json:"error,omitzero"`
}

func (AssistantMessage) messageMarker() {}

// AssistantMessageError represents an error in the assistant message.
type AssistantMessageError string

const (
	ErrAuthenticationFailed AssistantMessageError = "authentication_failed"
	ErrBillingError         AssistantMessageError = "billing_error"
	ErrRateLimit            AssistantMessageError = "rate_limit"
	ErrInvalidRequest       AssistantMessageError = "invalid_request"
	ErrServerError          AssistantMessageError = "server_error"
	ErrUnknown              AssistantMessageError = "unknown"
)

// SystemMessage represents a system message with metadata.
type SystemMessage struct {
	Subtype string         `json:"subtype"`
	Data    map[string]any `json:"data,omitzero"`
}

func (SystemMessage) messageMarker() {}

// ResultMessage represents the final result of a conversation.
type ResultMessage struct {
	Subtype          string      `json:"subtype"`
	DurationMS       int         `json:"duration_ms"`
	DurationAPIMS    int         `json:"duration_api_ms"`
	IsError          bool        `json:"is_error"`
	NumTurns         int         `json:"num_turns"`
	SessionID        string      `json:"session_id"`
	TotalCostUSD     *float64    `json:"total_cost_usd,omitzero"`
	Usage            *UsageStats `json:"usage,omitzero"`
	Result           string      `json:"result,omitzero"`
	StructuredOutput any         `json:"structured_output,omitzero"`
}

func (ResultMessage) messageMarker() {}

// StreamEvent represents a partial message update during streaming.
type StreamEvent struct {
	UUID            string         `json:"uuid"`
	SessionID       string         `json:"session_id"`
	Event           map[string]any `json:"event"`
	ParentToolUseID string         `json:"parent_tool_use_id,omitzero"`
}

func (StreamEvent) messageMarker() {}

// UsageStats represents token usage statistics.
type UsageStats struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheRead    int `json:"cache_read,omitzero"`
	CacheWrite   int `json:"cache_write,omitzero"`
}

// ControlRequest represents an incoming control request from the CLI.
type ControlRequest struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
}

func (ControlRequest) messageMarker() {}

// ControlResponse represents a control response to send to the CLI.
type ControlResponse struct {
	Type     string          `json:"type"`
	Response json.RawMessage `json:"response"`
}

func (ControlResponse) messageMarker() {}

// ControlCancelRequest represents a request to cancel an operation.
type ControlCancelRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

func (ControlCancelRequest) messageMarker() {}

// parseMessage parses a raw JSON message into a typed Message.
// For recognized message types, it returns the parsed message.
// For unrecognized message types, it returns (nil, nil) to signal that the
// message should be silently skipped. This makes the SDK forward-compatible
// with new CLI message types (e.g., rate_limit_event) that it doesn't yet
// understand.
// For genuinely broken messages (missing type field, malformed JSON), it
// returns an error.
func parseMessage(raw json.RawMessage) (Message, error) {
	var typeHolder struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &typeHolder); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse message type: %v", err)}
	}

	if typeHolder.Type == "" {
		return nil, &MessageParseError{Message: "message missing 'type' field"}
	}

	switch typeHolder.Type {
	case "user":
		return parseUserMessage(raw)
	case "assistant":
		return parseAssistantMessage(raw)
	case "system":
		return parseSystemMessage(raw)
	case "result":
		return parseResultMessage(raw)
	case "stream_event":
		return parseStreamEvent(raw)
	case "control_request":
		return parseControlRequest(raw)
	case "control_response":
		return parseControlResponse(raw)
	case "control_cancel_request":
		return parseControlCancelRequest(raw)
	default:
		// Forward-compatible: skip unrecognized message types so newer
		// CLI versions don't crash older SDK versions.
		return nil, nil
	}
}

func parseUserMessage(raw json.RawMessage) (*UserMessage, error) {
	var holder struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		UUID            string `json:"uuid"`
		ParentToolUseID string `json:"parent_tool_use_id"`
		ToolUseResult   any    `json:"tool_use_result"`
	}
	if err := json.Unmarshal(raw, &holder); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse user message: %v", err)}
	}

	msg := &UserMessage{
		UUID:            holder.UUID,
		ParentToolUseID: holder.ParentToolUseID,
		ToolUseResult:   holder.ToolUseResult,
	}

	// Try to parse content as array of blocks
	if len(holder.Message.Content) > 0 && holder.Message.Content[0] == '[' {
		blocks, err := parseContentBlocks(holder.Message.Content)
		if err == nil {
			msg.Content = blocks
			return msg, nil
		}
	}

	// Parse as string
	var strContent string
	if err := json.Unmarshal(holder.Message.Content, &strContent); err == nil {
		msg.Content = strContent
		return msg, nil
	}

	// Fallback: store raw content
	msg.Content = string(holder.Message.Content)
	return msg, nil
}

func parseAssistantMessage(raw json.RawMessage) (*AssistantMessage, error) {
	var holder struct {
		Message struct {
			Content json.RawMessage `json:"content"`
			Model   string          `json:"model"`
		} `json:"message"`
		ParentToolUseID string `json:"parent_tool_use_id"`
		Error           AssistantMessageError `json:"error"`
	}
	if err := json.Unmarshal(raw, &holder); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse assistant message: %v", err)}
	}

	blocks, err := parseContentBlocks(holder.Message.Content)
	if err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse assistant content: %v", err)}
	}

	return &AssistantMessage{
		Content:         blocks,
		Model:           holder.Message.Model,
		ParentToolUseID: holder.ParentToolUseID,
		Error:           holder.Error,
	}, nil
}

func parseSystemMessage(raw json.RawMessage) (*SystemMessage, error) {
	var msg struct {
		Subtype string `json:"subtype"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse system message: %v", err)}
	}

	// Store the entire raw message as data
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse system message data: %v", err)}
	}

	return &SystemMessage{
		Subtype: msg.Subtype,
		Data:    data,
	}, nil
}

func parseResultMessage(raw json.RawMessage) (*ResultMessage, error) {
	var msg ResultMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse result message: %v", err)}
	}
	return &msg, nil
}

func parseStreamEvent(raw json.RawMessage) (*StreamEvent, error) {
	var msg StreamEvent
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse stream event: %v", err)}
	}
	return &msg, nil
}

func parseControlRequest(raw json.RawMessage) (*ControlRequest, error) {
	var msg ControlRequest
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse control request: %v", err)}
	}
	return &msg, nil
}

func parseControlResponse(raw json.RawMessage) (*ControlResponse, error) {
	var msg ControlResponse
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse control response: %v", err)}
	}
	return &msg, nil
}

func parseControlCancelRequest(raw json.RawMessage) (*ControlCancelRequest, error) {
	var msg ControlCancelRequest
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse control cancel request: %v", err)}
	}
	return &msg, nil
}

// GetText returns the text content from a UserMessage.
// If content is a string, returns it directly.
// If content is []ContentBlock, concatenates all TextBlock texts.
func (m *UserMessage) GetText() string {
	if s, ok := m.Content.(string); ok {
		return s
	}
	if blocks, ok := m.Content.([]ContentBlock); ok {
		var text string
		for _, b := range blocks {
			if tb, ok := b.(TextBlock); ok {
				if text != "" {
					text += "\n"
				}
				text += tb.Text
			}
		}
		return text
	}
	return ""
}

// GetText returns all text content from an AssistantMessage.
func (m *AssistantMessage) GetText() string {
	var text string
	for _, b := range m.Content {
		if tb, ok := b.(TextBlock); ok {
			if text != "" {
				text += "\n"
			}
			text += tb.Text
		}
	}
	return text
}

// GetToolCalls returns all ToolUseBlocks from an AssistantMessage.
func (m *AssistantMessage) GetToolCalls() []ToolUseBlock {
	var calls []ToolUseBlock
	for _, b := range m.Content {
		if tb, ok := b.(ToolUseBlock); ok {
			calls = append(calls, tb)
		}
	}
	return calls
}
