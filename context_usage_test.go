package claudecode

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContextUsageCategory_JSONRoundTrip verifies JSON serialization of a
// ContextUsageCategory, including the optional IsDeferred field.
func TestContextUsageCategory_JSONRoundTrip(t *testing.T) {
	cat := ContextUsageCategory{
		Name:   "System prompt",
		Tokens: 3200,
		Color:  "#abc",
	}

	data, err := json.Marshal(cat)
	require.NoError(t, err)

	var decoded ContextUsageCategory
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "System prompt", decoded.Name)
	assert.Equal(t, 3200, decoded.Tokens)
	assert.Equal(t, "#abc", decoded.Color)
	assert.Nil(t, decoded.IsDeferred)
}

// TestContextUsageCategory_WithDeferred verifies IsDeferred is round-tripped.
func TestContextUsageCategory_WithDeferred(t *testing.T) {
	v := true
	cat := ContextUsageCategory{
		Name:       "Deferred tools",
		Tokens:     500,
		Color:      "#def",
		IsDeferred: &v,
	}

	data, err := json.Marshal(cat)
	require.NoError(t, err)

	var decoded ContextUsageCategory
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.NotNil(t, decoded.IsDeferred)
	assert.True(t, *decoded.IsDeferred)
}

// TestContextUsageResponse_Deserialization verifies deserialization of a full
// ContextUsageResponse from the CLI.
func TestContextUsageResponse_Deserialization(t *testing.T) {
	rawJSON := `{
		"categories": [
			{"name": "System prompt", "tokens": 3200, "color": "#abc"},
			{"name": "Messages", "tokens": 61400, "color": "#def"},
			{"name": "Tools", "tokens": 12000, "color": "#ghi", "isDeferred": true}
		],
		"totalTokens": 98200,
		"maxTokens": 155000,
		"rawMaxTokens": 200000,
		"percentage": 49.1,
		"model": "claude-sonnet-4-5",
		"isAutoCompactEnabled": true,
		"memoryFiles": [
			{"path": "CLAUDE.md", "type": "project", "tokens": 512}
		],
		"mcpTools": [
			{"name": "search", "serverName": "ref", "tokens": 164, "isLoaded": true}
		],
		"agents": [
			{"agentType": "coder", "source": "sdk", "tokens": 299}
		],
		"gridRows": [
			[{"name": "row1", "value": 100}]
		],
		"autoCompactThreshold": 120000,
		"apiUsage": null
	}`

	var resp ContextUsageResponse
	err := json.Unmarshal([]byte(rawJSON), &resp)
	require.NoError(t, err)

	// Required fields
	require.Len(t, resp.Categories, 3)
	assert.Equal(t, "System prompt", resp.Categories[0].Name)
	assert.Equal(t, 3200, resp.Categories[0].Tokens)
	assert.Equal(t, "#abc", resp.Categories[0].Color)
	assert.Nil(t, resp.Categories[0].IsDeferred)

	assert.Equal(t, "Messages", resp.Categories[1].Name)
	assert.Equal(t, 61400, resp.Categories[1].Tokens)

	require.NotNil(t, resp.Categories[2].IsDeferred)
	assert.True(t, *resp.Categories[2].IsDeferred)

	assert.Equal(t, 98200, resp.TotalTokens)
	assert.Equal(t, 155000, resp.MaxTokens)
	assert.Equal(t, 200000, resp.RawMaxTokens)
	assert.InDelta(t, 49.1, resp.Percentage, 0.001)
	assert.Equal(t, "claude-sonnet-4-5", resp.Model)
	assert.True(t, resp.IsAutoCompactEnabled)

	require.Len(t, resp.MemoryFiles, 1)
	assert.Equal(t, "CLAUDE.md", resp.MemoryFiles[0]["path"])
	assert.Equal(t, "project", resp.MemoryFiles[0]["type"])

	require.Len(t, resp.McpTools, 1)
	assert.Equal(t, "search", resp.McpTools[0]["name"])
	assert.Equal(t, "ref", resp.McpTools[0]["serverName"])

	require.Len(t, resp.Agents, 1)
	assert.Equal(t, "coder", resp.Agents[0]["agentType"])
	assert.Equal(t, float64(299), resp.Agents[0]["tokens"])

	require.Len(t, resp.GridRows, 1)
	require.Len(t, resp.GridRows[0], 1)
	assert.Equal(t, "row1", resp.GridRows[0][0]["name"])

	// Optional fields
	require.NotNil(t, resp.AutoCompactThreshold)
	assert.Equal(t, 120000, *resp.AutoCompactThreshold)
}

// TestContextUsageResponse_Minimal verifies that a response with only required
// fields deserializes correctly.
func TestContextUsageResponse_Minimal(t *testing.T) {
	rawJSON := `{
		"categories": [],
		"totalTokens": 0,
		"maxTokens": 200000,
		"rawMaxTokens": 200000,
		"percentage": 0.0,
		"model": "claude-sonnet-4-5",
		"isAutoCompactEnabled": false,
		"memoryFiles": [],
		"mcpTools": [],
		"agents": [],
		"gridRows": []
	}`

	var resp ContextUsageResponse
	err := json.Unmarshal([]byte(rawJSON), &resp)
	require.NoError(t, err)

	assert.Empty(t, resp.Categories)
	assert.Equal(t, 0, resp.TotalTokens)
	assert.Equal(t, 200000, resp.MaxTokens)
	assert.InDelta(t, 0.0, resp.Percentage, 0.001)
	assert.False(t, resp.IsAutoCompactEnabled)

	// Optional fields should be nil
	assert.Nil(t, resp.AutoCompactThreshold)
	assert.Nil(t, resp.DeferredBuiltinTools)
	assert.Nil(t, resp.SystemTools)
	assert.Nil(t, resp.SystemPromptSections)
	assert.Nil(t, resp.SlashCommands)
	assert.Nil(t, resp.Skills)
	assert.Nil(t, resp.MessageBreakdown)
	assert.Nil(t, resp.APIUsage)
}

// TestGetContextUsage_TypedResponse verifies that GetContextUsage returns a
// typed ContextUsageResponse instead of raw map[string]any.
func TestGetContextUsage_TypedResponse(t *testing.T) {
	transport := newControlCapturingTransport()
	router := NewControlRouter(transport, &Options{})

	done := make(chan struct{})
	var result ContextUsageResponse
	var resultErr error

	go func() {
		defer close(done)
		result, resultErr = router.GetContextUsage(context.Background())
	}()

	require.Eventually(t, func() bool {
		return len(transport.getWrites()) > 0
	}, time.Second, 10*time.Millisecond)

	writes := transport.getWrites()
	require.Len(t, writes, 1)

	var req map[string]any
	err := json.Unmarshal([]byte(writes[0]), &req)
	require.NoError(t, err)

	// Verify control request shape
	assert.Equal(t, "control_request", req["type"])
	request, ok := req["request"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "get_context_usage", request["subtype"])

	requestID, ok := req["request_id"].(string)
	require.True(t, ok)

	// Simulate the CLI response
	responsePayload := map[string]any{
		"categories": []any{
			map[string]any{"name": "System prompt", "tokens": float64(3200), "color": "#abc"},
			map[string]any{"name": "Messages", "tokens": float64(61400), "color": "#def"},
		},
		"totalTokens":          float64(98200),
		"maxTokens":            float64(155000),
		"rawMaxTokens":         float64(200000),
		"percentage":           49.1,
		"model":                "claude-sonnet-4-5",
		"isAutoCompactEnabled": true,
		"memoryFiles":          []any{map[string]any{"path": "CLAUDE.md", "type": "project", "tokens": float64(512)}},
		"mcpTools":             []any{map[string]any{"name": "search", "serverName": "ref", "tokens": float64(164), "isLoaded": true}},
		"agents":               []any{map[string]any{"agentType": "coder", "source": "sdk", "tokens": float64(299)}},
		"gridRows":             []any{},
	}
	responseBytes, err := json.Marshal(responsePayload)
	require.NoError(t, err)

	responseRaw := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + requestID + `","response":` + string(responseBytes) + `}}`)
	err = router.handleControlResponse(&ControlResponse{}, responseRaw)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GetContextUsage did not complete")
	}

	require.NoError(t, resultErr)
	require.Len(t, result.Categories, 2)

	assert.Equal(t, "System prompt", result.Categories[0].Name)
	assert.Equal(t, 3200, result.Categories[0].Tokens)
	assert.Equal(t, "#abc", result.Categories[0].Color)
	assert.Equal(t, "Messages", result.Categories[1].Name)
	assert.Equal(t, 61400, result.Categories[1].Tokens)

	assert.Equal(t, 98200, result.TotalTokens)
	assert.Equal(t, 155000, result.MaxTokens)
	assert.Equal(t, 200000, result.RawMaxTokens)
	assert.InDelta(t, 49.1, result.Percentage, 0.001)
	assert.Equal(t, "claude-sonnet-4-5", result.Model)
	assert.True(t, result.IsAutoCompactEnabled)

	require.Len(t, result.McpTools, 1)
	assert.Equal(t, "search", result.McpTools[0]["name"])
	assert.Equal(t, "ref", result.McpTools[0]["serverName"])

	require.Len(t, result.Agents, 1)
	assert.Equal(t, "coder", result.Agents[0]["agentType"])
}

// TestClient_GetContextUsage_NotConnected verifies the error when not connected.
func TestClient_GetContextUsage_NotConnected(t *testing.T) {
	client := NewClient()
	_, err := client.GetContextUsage(context.Background())
	assert.ErrorIs(t, err, ErrNotConnected)
}
