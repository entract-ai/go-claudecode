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
