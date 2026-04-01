package claudecode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bpowers/go-claudecode/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTool implements the chat.Tool interface for testing.
type mockTool struct {
	name        string
	schemaJSON  string
	callHandler func(ctx context.Context, args string) string
}

var _ chat.Tool = (*mockTool)(nil)

func (t *mockTool) Name() string          { return t.name }
func (t *mockTool) Description() string   { return "Mock tool for testing" }
func (t *mockTool) MCPJsonSchema() string { return t.schemaJSON }
func (t *mockTool) Call(ctx context.Context, args string) string {
	if t.callHandler != nil {
		return t.callHandler(ctx, args)
	}
	return `{"result": "ok"}`
}

func TestHandleSDKMCPRequest_ToolsCall_NilArguments(t *testing.T) {
	// Test that nil arguments are normalized to {} instead of "null"
	tool := &mockTool{
		name:       "test_tool",
		schemaJSON: `{"name": "test_tool", "description": "A test tool", "inputSchema": {"type": "object"}}`,
		callHandler: func(ctx context.Context, args string) string {
			// Verify we received {} not "null"
			if args == "null" {
				return `{"error": "received null instead of empty object"}`
			}
			if args != "{}" {
				return `{"error": "expected empty object, got: ` + args + `"}`
			}
			return `{"result": "ok"}`
		},
	}

	config := &MCPSDKConfig{
		Name:  "test-server",
		Tools: []chat.Tool{tool},
	}

	// Test with nil arguments (omitted from params)
	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "test_tool",
			// arguments omitted - will be nil
		},
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	require.NotNil(t, result["result"])
	resultMap, ok := result["result"].(map[string]any)
	require.True(t, ok)

	content, ok := resultMap["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, content, 1)

	text := content[0]["text"].(string)
	assert.Contains(t, text, `"result": "ok"`, "tool should receive {} not null")
}

func TestHandleSDKMCPRequest_ToolsCall_ExplicitNullArguments(t *testing.T) {
	// Test that explicitly null arguments are also normalized to {}
	tool := &mockTool{
		name:       "test_tool",
		schemaJSON: `{"name": "test_tool", "description": "A test tool", "inputSchema": {"type": "object"}}`,
		callHandler: func(ctx context.Context, args string) string {
			if args == "null" {
				return `{"error": "received null instead of empty object"}`
			}
			return `{"result": "ok"}`
		},
	}

	config := &MCPSDKConfig{
		Name:  "test-server",
		Tools: []chat.Tool{tool},
	}

	// Test with explicitly null arguments
	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "test_tool",
			"arguments": nil, // explicit null
		},
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	require.NotNil(t, result["result"])
	resultMap, ok := result["result"].(map[string]any)
	require.True(t, ok)

	content, ok := resultMap["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, content, 1)

	text := content[0]["text"].(string)
	assert.Contains(t, text, `"result": "ok"`, "tool should receive {} not null")
}

func TestHandleSDKMCPRequest_ToolsCall_IsErrorKeyCasing(t *testing.T) {
	// Test that isError uses camelCase per MCP protocol
	tool := &mockTool{
		name:       "test_tool",
		schemaJSON: `{"name": "test_tool", "description": "A test tool", "inputSchema": {"type": "object"}}`,
		callHandler: func(ctx context.Context, args string) string {
			return `{"error": "something went wrong"}`
		},
	}

	config := &MCPSDKConfig{
		Name:  "test-server",
		Tools: []chat.Tool{tool},
	}

	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "test_tool",
			"arguments": map[string]any{},
		},
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	require.NotNil(t, result["result"])
	resultMap, ok := result["result"].(map[string]any)
	require.True(t, ok)

	// Check for camelCase isError, not snake_case is_error
	_, hasSnakeCase := resultMap["is_error"]
	assert.False(t, hasSnakeCase, "should not use snake_case is_error")

	isError, hasCamelCase := resultMap["isError"]
	assert.True(t, hasCamelCase, "should use camelCase isError")
	assert.True(t, isError.(bool), "isError should be true for error response")
}

func TestHandleSDKMCPRequest_ToolsList_SchemaParseError(t *testing.T) {
	// Test that invalid tool schemas return an error
	tool := &mockTool{
		name:       "bad_tool",
		schemaJSON: `{not valid json}`,
	}

	config := &MCPSDKConfig{
		Name:  "test-server",
		Tools: []chat.Tool{tool},
	}

	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	// Should return an error, not a successful response with empty/partial tools
	if result["error"] != nil {
		// Good - it returned an error
		errMap, ok := result["error"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, errMap["message"], "bad_tool")
	} else {
		// If it returned a result, the tools list should indicate the error
		resultMap := result["result"].(map[string]any)
		tools := resultMap["tools"].([]map[string]any)
		// Bad: silently dropped the tool
		t.Errorf("expected error for invalid schema, but got %d tools", len(tools))
	}
}

func TestHandleSDKMCPRequest_Initialize_UsesProtocolVersion(t *testing.T) {
	config := &MCPSDKConfig{
		Name: "test-server",
	}

	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{},
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	require.NotNil(t, result["result"])
	resultMap, ok := result["result"].(map[string]any)
	require.True(t, ok)

	// Check protocol version is set correctly
	version := resultMap["protocolVersion"].(string)
	assert.NotEmpty(t, version)
	assert.NotEqual(t, "1.0.0", version, "should use actual MCP protocol version, not placeholder")
}

func TestHandleSDKMCPRequest_ToolsCall_ValidArguments(t *testing.T) {
	// Test that valid arguments are passed through correctly
	var receivedArgs string
	tool := &mockTool{
		name:       "test_tool",
		schemaJSON: `{"name": "test_tool", "description": "A test tool", "inputSchema": {"type": "object", "properties": {"name": {"type": "string"}}}}`,
		callHandler: func(ctx context.Context, args string) string {
			receivedArgs = args
			return `{"result": "ok"}`
		},
	}

	config := &MCPSDKConfig{
		Name:  "test-server",
		Tools: []chat.Tool{tool},
	}

	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "test_tool",
			"arguments": map[string]any{
				"name": "test-value",
			},
		},
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	require.NotNil(t, result["result"])

	// Parse the received args and check the value
	var args map[string]any
	err := json.Unmarshal([]byte(receivedArgs), &args)
	require.NoError(t, err)
	assert.Equal(t, "test-value", args["name"])
}

func TestHandleSDKMCPRequest_ToolsList_IncludesAnnotations(t *testing.T) {
	tool := &mockTool{
		name: "annotated_tool",
		schemaJSON: `{
			"name": "annotated_tool",
			"description": "An annotated tool",
			"inputSchema": {"type": "object"},
			"annotations": {"readOnlyHint": true}
		}`,
	}

	config := &MCPSDKConfig{
		Name:  "test-server",
		Tools: []chat.Tool{tool},
	}

	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	require.NotNil(t, result["result"])
	resultMap, ok := result["result"].(map[string]any)
	require.True(t, ok)

	tools, ok := resultMap["tools"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	annotations, ok := tools[0]["annotations"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, annotations["readOnlyHint"])
}

func TestHandleSDKMCPRequest_ToolsCall_IsErrorFromStructuredContent(t *testing.T) {
	// Verify that is_error from structured tool output with content blocks
	// is propagated to the MCP result. This is the exact pattern from the
	// Python SDK fix in commit 582cdf7.
	t.Run("is_error true with content blocks", func(t *testing.T) {
		tool := &mockTool{
			name:       "divide",
			schemaJSON: `{"name": "divide", "description": "Divide two numbers", "inputSchema": {"type": "object"}}`,
			callHandler: func(ctx context.Context, args string) string {
				return `{"content": [{"type": "text", "text": "Division by zero"}], "is_error": true}`
			},
		}

		config := &MCPSDKConfig{
			Name:  "test-server",
			Tools: []chat.Tool{tool},
		}

		message := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "divide",
				"arguments": map[string]any{},
			},
		}

		result := handleSDKMCPRequest(context.Background(), config, message)

		require.NotNil(t, result["result"])
		resultMap, ok := result["result"].(map[string]any)
		require.True(t, ok)

		// isError should be propagated
		isError, hasIsError := resultMap["isError"]
		assert.True(t, hasIsError, "isError should be present in result")
		assert.True(t, isError.(bool), "isError should be true")

		// Content should be parsed from the structured output
		content, ok := resultMap["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		assert.Equal(t, "Division by zero", content[0]["text"])
	})

	t.Run("isError true with content blocks (camelCase)", func(t *testing.T) {
		tool := &mockTool{
			name:       "divide",
			schemaJSON: `{"name": "divide", "description": "Divide two numbers", "inputSchema": {"type": "object"}}`,
			callHandler: func(ctx context.Context, args string) string {
				return `{"content": [{"type": "text", "text": "Division by zero"}], "isError": true}`
			},
		}

		config := &MCPSDKConfig{
			Name:  "test-server",
			Tools: []chat.Tool{tool},
		}

		message := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "divide",
				"arguments": map[string]any{},
			},
		}

		result := handleSDKMCPRequest(context.Background(), config, message)

		require.NotNil(t, result["result"])
		resultMap, ok := result["result"].(map[string]any)
		require.True(t, ok)

		isError, hasIsError := resultMap["isError"]
		assert.True(t, hasIsError, "isError should be present in result")
		assert.True(t, isError.(bool), "isError should be true")
	})

	t.Run("success case omits isError", func(t *testing.T) {
		tool := &mockTool{
			name:       "divide",
			schemaJSON: `{"name": "divide", "description": "Divide two numbers", "inputSchema": {"type": "object"}}`,
			callHandler: func(ctx context.Context, args string) string {
				return `{"content": [{"type": "text", "text": "2.0"}]}`
			},
		}

		config := &MCPSDKConfig{
			Name:  "test-server",
			Tools: []chat.Tool{tool},
		}

		message := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "divide",
				"arguments": map[string]any{},
			},
		}

		result := handleSDKMCPRequest(context.Background(), config, message)

		require.NotNil(t, result["result"])
		resultMap, ok := result["result"].(map[string]any)
		require.True(t, ok)

		// isError should not be present for successful results
		_, hasIsError := resultMap["isError"]
		assert.False(t, hasIsError, "isError should not be present for successful results")
	})
}

func TestHandleSDKMCPRequest_ToolsCall_PassesThroughImageContent(t *testing.T) {
	tool := &mockTool{
		name:       "image_tool",
		schemaJSON: `{"name": "image_tool", "description": "An image tool", "inputSchema": {"type": "object"}}`,
		callHandler: func(ctx context.Context, args string) string {
			return `{
				"content": [
					{"type": "text", "text": "Generated chart"},
					{"type": "image", "data": "iVBOR...", "mimeType": "image/png"}
				]
			}`
		},
	}

	config := &MCPSDKConfig{
		Name:  "test-server",
		Tools: []chat.Tool{tool},
	}

	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "image_tool",
			"arguments": map[string]any{},
		},
	}

	result := handleSDKMCPRequest(context.Background(), config, message)

	require.NotNil(t, result["result"])
	resultMap, ok := result["result"].(map[string]any)
	require.True(t, ok)

	content, ok := resultMap["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, content, 2)
	assert.Equal(t, "text", content[0]["type"])
	assert.Equal(t, "Generated chart", content[0]["text"])
	assert.Equal(t, "image", content[1]["type"])
	assert.Equal(t, "iVBOR...", content[1]["data"])
	assert.Equal(t, "image/png", content[1]["mimeType"])
}
