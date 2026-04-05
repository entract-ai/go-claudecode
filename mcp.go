package claudecode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/bpowers/go-claudecode/mcp"
)

// handleSDKMCPRequest routes an MCP message to an SDK server.
func handleSDKMCPRequest(ctx context.Context, config *MCPSDKConfig, message map[string]any) map[string]any {
	method, _ := message["method"].(string)
	id := message["id"]
	params, _ := message["params"].(map[string]any)

	switch method {
	case "initialize":
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"protocolVersion": mcp.ProtocolVersion,
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    config.Name,
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		tools := make([]map[string]any, 0, len(config.Tools))
		for _, tool := range config.Tools {
			var schema map[string]any
			if err := json.Unmarshal([]byte(tool.MCPJsonSchema()), &schema); err != nil {
				return map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"error": map[string]any{
						"code":    -32603,
						"message": "Invalid tool schema for " + tool.Name() + ": " + err.Error(),
					},
				}
			}
			toolData := map[string]any{
				"name":        schema["name"],
				"description": schema["description"],
				"inputSchema": schema["inputSchema"],
			}
			if annotations, ok := schema["annotations"]; ok && annotations != nil {
				toolData["annotations"] = annotations
			}
			tools = append(tools, toolData)
		}
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"tools": tools,
			},
		}

	case "tools/call":
		toolName, _ := params["name"].(string)
		arguments, _ := params["arguments"].(map[string]any)

		// Normalize nil arguments to empty object - when MCP clients omit arguments
		// or pass null, tools expecting {} would fail on "null" JSON.
		if arguments == nil {
			arguments = map[string]any{}
		}

		// Find the tool
		var tool interface {
			Call(context.Context, string) string
		}
		for _, t := range config.Tools {
			if t.Name() == toolName {
				tool = t
				break
			}
		}

		if tool == nil {
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]any{
					"code":    -32601,
					"message": "Tool not found: " + toolName,
				},
			}
		}

		// Execute the tool
		argsJSON, _ := json.Marshal(arguments)
		output := tool.Call(ctx, string(argsJSON))

		result := map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": output,
				},
			},
		}

		// Parse structured output when possible to preserve rich content blocks
		// (text/image) and tool-level error semantics.
		var structured map[string]any
		if err := json.Unmarshal([]byte(output), &structured); err == nil && structured != nil {
			if parsedContent := parseToolOutputContent(structured); parsedContent != nil {
				result["content"] = parsedContent
			}
			if hasToolError(structured) {
				result["isError"] = true
			}
		}

		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  result,
		}

	case "notifications/initialized":
		// Notification, no response needed
		return map[string]any{
			"jsonrpc": "2.0",
			"result":  map[string]any{},
		}

	default:
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]any{
				"code":    -32601,
				"message": "Method not found: " + method,
			},
		}
	}
}

func parseToolOutputContent(structured map[string]any) []map[string]any {
	rawContent, ok := structured["content"]
	if !ok {
		return nil
	}
	items, ok := rawContent.([]any)
	if !ok {
		return nil
	}

	content := make([]map[string]any, 0, len(items))
	for _, item := range items {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			text, _ := block["text"].(string)
			content = append(content, map[string]any{
				"type": "text",
				"text": text,
			})
		case "image":
			imageBlock := map[string]any{
				"type": "image",
			}
			if data, ok := block["data"].(string); ok {
				imageBlock["data"] = data
			}
			if mimeType, ok := block["mimeType"].(string); ok {
				imageBlock["mimeType"] = mimeType
			}
			content = append(content, imageBlock)
		case "resource_link":
			var parts []string
			if name, ok := block["name"].(string); ok && name != "" {
				parts = append(parts, name)
			}
			if uri, ok := block["uri"].(string); ok && uri != "" {
				parts = append(parts, uri)
			}
			if desc, ok := block["description"].(string); ok && desc != "" {
				parts = append(parts, desc)
			}
			text := "Resource link"
			if len(parts) > 0 {
				text = strings.Join(parts, "\n")
			}
			content = append(content, map[string]any{
				"type": "text",
				"text": text,
			})
		case "resource":
			resource, _ := block["resource"].(map[string]any)
			if resource == nil {
				slog.Warn("Binary embedded resource cannot be converted to text, skipping")
				continue
			}
			if text, hasText := resource["text"]; hasText {
				textStr, _ := text.(string)
				content = append(content, map[string]any{
					"type": "text",
					"text": textStr,
				})
			} else {
				slog.Warn("Binary embedded resource cannot be converted to text, skipping")
			}
		default:
			slog.Warn("Unsupported content type in tool result, skipping", "type", blockType)
		}
	}

	return content
}

func hasToolError(structured map[string]any) bool {
	if raw, ok := structured["isError"]; ok {
		if v, ok := raw.(bool); ok {
			return v
		}
	}
	if raw, ok := structured["is_error"]; ok {
		if v, ok := raw.(bool); ok {
			return v
		}
	}
	raw, ok := structured["error"]
	if !ok || raw == nil {
		return false
	}
	if s, ok := raw.(string); ok {
		return s != ""
	}
	return true
}
