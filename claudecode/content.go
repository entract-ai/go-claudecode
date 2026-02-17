package claudecode

import (
	"encoding/json"
	"fmt"
)

// ContentBlock is a marker interface for all content block types.
type ContentBlock interface {
	contentBlockMarker()
}

// TextBlock represents text content.
type TextBlock struct {
	Text string `json:"text"`
}

func (TextBlock) contentBlockMarker() {}

// ThinkingBlock represents thinking/reasoning content.
type ThinkingBlock struct {
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

func (ThinkingBlock) contentBlockMarker() {}

// ToolUseBlock represents a tool invocation.
type ToolUseBlock struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (ToolUseBlock) contentBlockMarker() {}

// ToolResultBlock represents the result of a tool execution.
type ToolResultBlock struct {
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content,omitzero"` // string or []ContentBlock
	IsError   bool   `json:"is_error,omitzero"`
}

func (ToolResultBlock) contentBlockMarker() {}

// UnknownContentBlock represents an unrecognized content block type.
// This enables forward compatibility when the CLI introduces new block types.
type UnknownContentBlock struct {
	Type string         `json:"type"`
	Data map[string]any `json:"-"` // Raw data preserved for inspection
}

func (UnknownContentBlock) contentBlockMarker() {}

// parseContentBlocks parses a JSON array of content blocks.
func parseContentBlocks(raw json.RawMessage) ([]ContentBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var rawBlocks []json.RawMessage
	if err := json.Unmarshal(raw, &rawBlocks); err != nil {
		return nil, fmt.Errorf("unmarshal content blocks: %w", err)
	}

	blocks := make([]ContentBlock, 0, len(rawBlocks))
	for _, rawBlock := range rawBlocks {
		block, err := parseContentBlock(rawBlock)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

// parseContentBlock parses a single content block from JSON.
func parseContentBlock(raw json.RawMessage) (ContentBlock, error) {
	var typeHolder struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &typeHolder); err != nil {
		return nil, fmt.Errorf("parse content block type: %w", err)
	}

	switch typeHolder.Type {
	case "text":
		var block TextBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, fmt.Errorf("parse text block: %w", err)
		}
		return block, nil

	case "thinking":
		var block ThinkingBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, fmt.Errorf("parse thinking block: %w", err)
		}
		return block, nil

	case "tool_use":
		var block ToolUseBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, fmt.Errorf("parse tool_use block: %w", err)
		}
		return block, nil

	case "tool_result":
		var block ToolResultBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, fmt.Errorf("parse tool_result block: %w", err)
		}
		return block, nil

	default:
		// Return unknown block for forward compatibility with new CLI versions
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("parse unknown content block data: %w", err)
		}
		return UnknownContentBlock{Type: typeHolder.Type, Data: data}, nil
	}
}
