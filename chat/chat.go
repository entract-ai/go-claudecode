// Package chat defines the core types for LLM tool interaction.
//
// This package provides the [Tool] interface that tools must implement
// to be used with MCP servers and Claude Code SDK, along with message
// types for representing conversations.
package chat

import (
	"context"
	"encoding/json"
	"strings"
)

// Role represents who a message came from.
type Role string

const (
	// UserRole identifies messages from the user.
	UserRole Role = "user"
	// AssistantRole identifies messages from the LLM.
	AssistantRole Role = "assistant"
)

// ToolDef represents a tool definition that can be registered with an LLM.
type ToolDef interface {
	// MCPJsonSchema returns the MCP JSON schema for the tool as a compact JSON string.
	MCPJsonSchema() string
	// Name returns the tool's name.
	Name() string
	// Description returns the tool's description.
	Description() string
}

// Tool represents a callable tool that can be registered with an LLM.
// It extends ToolDef with the ability to execute the tool.
type Tool interface {
	ToolDef
	// Call executes the tool with the given context and JSON input, returning JSON output.
	Call(ctx context.Context, input string) string
}

// ToolCall represents a request from the LLM to invoke a tool.
type ToolCall struct {
	// ID is a unique identifier for this tool call.
	ID string `json:"id"`
	// Name is the name of the tool to invoke.
	Name string `json:"name"`
	// Arguments contains the JSON-encoded arguments for the tool.
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	// ToolCallID matches the ID from the corresponding ToolCall.
	ToolCallID string `json:"toolCallID"`
	// Name is the tool name associated with this result.
	Name string `json:"name"`
	// Content is the result of the tool execution.
	Content string `json:"content"`
	// Error indicates if the tool execution failed.
	Error string `json:"error,omitzero"`
}

// ThinkingContent represents thinking/reasoning content in a message.
type ThinkingContent struct {
	// Text contains the thinking content.
	Text string `json:"text,omitzero"`
	// Signature contains the encrypted signature for thinking block verification.
	Signature string `json:"signature,omitzero"`
}

// Content represents a single piece of content within a message.
// It uses a union-like structure where only one field should be set.
type Content struct {
	// Text content (most common case).
	Text string `json:"text,omitzero"`

	// Tool-related content.
	ToolCall   *ToolCall   `json:"toolCall,omitzero"`
	ToolResult *ToolResult `json:"toolResult,omitzero"`

	// Thinking/reasoning content.
	Thinking *ThinkingContent `json:"thinking,omitzero"`
}

// Message represents a message to or from an LLM.
type Message struct {
	Role     Role      `json:"role,omitzero"`
	Contents []Content `json:"contents,omitzero"`
}

// GetText returns all text content concatenated with newlines.
func (m Message) GetText() string {
	var texts []string
	for _, c := range m.Contents {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) == 0 {
		return ""
	}
	if len(texts) == 1 {
		return texts[0]
	}
	return strings.Join(texts, "\n")
}
