package claudecode

import (
	"context"
	"encoding/json"

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
			tools = append(tools, map[string]any{
				"name":        schema["name"],
				"description": schema["description"],
				"inputSchema": schema["inputSchema"],
			})
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

		// Parse output to check for errors
		var structured map[string]any
		isError := false
		if err := json.Unmarshal([]byte(output), &structured); err == nil {
			if errField, ok := structured["error"]; ok && errField != nil && errField != "" {
				isError = true
			}
		}

		result := map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": output,
				},
			},
		}
		if isError {
			result["isError"] = true
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
