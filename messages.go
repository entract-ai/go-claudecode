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
	Usage           map[string]any        `json:"usage,omitzero"`
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

// TaskUsage represents usage statistics reported in task_progress and
// task_notification messages.
type TaskUsage struct {
	TotalTokens int `json:"total_tokens"`
	ToolUses    int `json:"tool_uses"`
	DurationMS  int `json:"duration_ms"`
}

// TaskNotificationStatus represents the status of a completed task.
type TaskNotificationStatus string

const (
	TaskNotificationStatusCompleted TaskNotificationStatus = "completed"
	TaskNotificationStatusFailed    TaskNotificationStatus = "failed"
	TaskNotificationStatusStopped   TaskNotificationStatus = "stopped"
)

// TaskStartedMessage is a system message emitted when a task starts.
// It carries the same Subtype and Data fields as SystemMessage for
// backward compatibility with code that inspects the raw payload.
type TaskStartedMessage struct {
	Subtype     string         `json:"subtype"`
	Data        map[string]any `json:"data,omitzero"`
	TaskID      string         `json:"task_id"`
	Description string         `json:"description"`
	UUID        string         `json:"uuid"`
	SessionID   string         `json:"session_id"`
	ToolUseID   *string        `json:"tool_use_id,omitzero"`
	TaskType    *string        `json:"task_type,omitzero"`
}

func (TaskStartedMessage) messageMarker() {}

// TaskProgressMessage is a system message emitted while a task is in progress.
type TaskProgressMessage struct {
	Subtype      string         `json:"subtype"`
	Data         map[string]any `json:"data,omitzero"`
	TaskID       string         `json:"task_id"`
	Description  string         `json:"description"`
	Usage        TaskUsage      `json:"usage"`
	UUID         string         `json:"uuid"`
	SessionID    string         `json:"session_id"`
	ToolUseID    *string        `json:"tool_use_id,omitzero"`
	LastToolName *string        `json:"last_tool_name,omitzero"`
}

func (TaskProgressMessage) messageMarker() {}

// TaskNotificationMessage is a system message emitted when a task completes,
// fails, or is stopped.
type TaskNotificationMessage struct {
	Subtype    string                 `json:"subtype"`
	Data       map[string]any         `json:"data,omitzero"`
	TaskID     string                 `json:"task_id"`
	Status     TaskNotificationStatus `json:"status"`
	OutputFile string                 `json:"output_file"`
	Summary    string                 `json:"summary"`
	UUID       string                 `json:"uuid"`
	SessionID  string                 `json:"session_id"`
	ToolUseID  *string                `json:"tool_use_id,omitzero"`
	Usage      *TaskUsage             `json:"usage,omitzero"`
}

func (TaskNotificationMessage) messageMarker() {}

// ResultMessage represents the final result of a conversation.
type ResultMessage struct {
	Subtype          string      `json:"subtype"`
	DurationMS       int         `json:"duration_ms"`
	DurationAPIMS    int         `json:"duration_api_ms"`
	IsError          bool        `json:"is_error"`
	NumTurns         int         `json:"num_turns"`
	SessionID        string      `json:"session_id"`
	StopReason       *string     `json:"stop_reason,omitzero"`
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

// RateLimitStatus represents the current rate limit status.
type RateLimitStatus string

const (
	RateLimitStatusAllowed        RateLimitStatus = "allowed"
	RateLimitStatusAllowedWarning RateLimitStatus = "allowed_warning"
	RateLimitStatusRejected       RateLimitStatus = "rejected"
)

// RateLimitInfo contains rate limit status emitted by the CLI when
// rate limit state changes.
//
// Field names follow Go conventions; JSON tags map to the camelCase
// wire format used by the CLI. The Raw field preserves the original
// dict including any fields not modeled above for forward compatibility.
type RateLimitInfo struct {
	Status                RateLimitStatus `json:"status"`
	ResetsAt              *int64          `json:"resetsAt,omitzero"`
	RateLimitType         *string         `json:"rateLimitType,omitzero"`
	Utilization           *float64        `json:"utilization,omitzero"`
	OverageStatus         *string         `json:"overageStatus,omitzero"`
	OverageResetsAt       *int64          `json:"overageResetsAt,omitzero"`
	OverageDisabledReason *string         `json:"overageDisabledReason,omitzero"`
	Raw                   map[string]any  `json:"-"`
}

// RateLimitEvent is emitted when rate limit info changes.
//
// The CLI emits this whenever the rate limit status transitions (e.g.
// from "allowed" to "allowed_warning"). Use this to warn users before
// they hit a hard limit, or to gracefully back off when status is
// "rejected".
type RateLimitEvent struct {
	RateLimitInfo RateLimitInfo `json:"rate_limit_info"`
	UUID          string        `json:"uuid"`
	SessionID     string        `json:"session_id"`
}

func (RateLimitEvent) messageMarker() {}

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
// with new CLI message types that it doesn't yet understand.
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
	case "rate_limit_event":
		return parseRateLimitEvent(raw)
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
			Usage   map[string]any  `json:"usage"`
		} `json:"message"`
		ParentToolUseID string                `json:"parent_tool_use_id"`
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
		Usage:           holder.Message.Usage,
	}, nil
}

func parseSystemMessage(raw json.RawMessage) (Message, error) {
	var header struct {
		Subtype string `json:"subtype"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse system message: %v", err)}
	}

	// Store the entire raw message as data for all system message types.
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse system message data: %v", err)}
	}

	switch header.Subtype {
	case "task_started":
		return parseTaskStartedMessage(raw, data)
	case "task_progress":
		return parseTaskProgressMessage(raw, data)
	case "task_notification":
		return parseTaskNotificationMessage(raw, data)
	default:
		return &SystemMessage{
			Subtype: header.Subtype,
			Data:    data,
		}, nil
	}
}

func parseTaskStartedMessage(raw json.RawMessage, data map[string]any) (*TaskStartedMessage, error) {
	var msg TaskStartedMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse task_started message: %v", err)}
	}
	msg.Data = data
	return &msg, nil
}

func parseTaskProgressMessage(raw json.RawMessage, data map[string]any) (*TaskProgressMessage, error) {
	var msg TaskProgressMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse task_progress message: %v", err)}
	}
	msg.Data = data
	return &msg, nil
}

func parseTaskNotificationMessage(raw json.RawMessage, data map[string]any) (*TaskNotificationMessage, error) {
	var msg TaskNotificationMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse task_notification message: %v", err)}
	}
	msg.Data = data
	return &msg, nil
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

func parseRateLimitEvent(raw json.RawMessage) (*RateLimitEvent, error) {
	var msg RateLimitEvent
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &MessageParseError{Message: fmt.Sprintf("failed to parse rate limit event: %v", err)}
	}

	// Preserve the raw rate_limit_info dict for forward compatibility
	// with fields not yet modeled in RateLimitInfo.
	var holder struct {
		RateLimitInfo map[string]any `json:"rate_limit_info"`
	}
	if err := json.Unmarshal(raw, &holder); err == nil {
		msg.RateLimitInfo.Raw = holder.RateLimitInfo
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
