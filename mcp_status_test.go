package claudecode

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMcpServerConnectionStatus_Values verifies the status string constants.
func TestMcpServerConnectionStatus_Values(t *testing.T) {
	assert.Equal(t, McpServerConnectionStatus("connected"), McpStatusConnected)
	assert.Equal(t, McpServerConnectionStatus("failed"), McpStatusFailed)
	assert.Equal(t, McpServerConnectionStatus("needs-auth"), McpStatusNeedsAuth)
	assert.Equal(t, McpServerConnectionStatus("pending"), McpStatusPending)
	assert.Equal(t, McpServerConnectionStatus("disabled"), McpStatusDisabled)
}

// TestMcpServerStatus_JSONRoundTrip_Connected verifies JSON serialization of a
// fully-populated connected server status.
func TestMcpServerStatus_JSONRoundTrip_Connected(t *testing.T) {
	status := McpServerStatus{
		Name:   "my-http-server",
		Status: McpStatusConnected,
		ServerInfo: &McpServerInfo{
			Name:    "my-http-server",
			Version: "1.0.0",
		},
		Config: map[string]any{
			"type": "http",
			"url":  "https://example.com/mcp",
		},
		Scope: "project",
		Tools: []McpToolInfo{
			{
				Name:        "greet",
				Description: "Greet a user",
				Annotations: &McpToolAnnotations{
					ReadOnly:    boolPtr(true),
					Destructive: boolPtr(false),
					OpenWorld:   boolPtr(false),
				},
			},
			{
				Name: "reset",
			},
		},
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)

	var decoded McpServerStatus
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "my-http-server", decoded.Name)
	assert.Equal(t, McpStatusConnected, decoded.Status)
	require.NotNil(t, decoded.ServerInfo)
	assert.Equal(t, "1.0.0", decoded.ServerInfo.Version)
	assert.Equal(t, "project", decoded.Scope)
	require.Len(t, decoded.Tools, 2)
	assert.Equal(t, "greet", decoded.Tools[0].Name)
	assert.Equal(t, "Greet a user", decoded.Tools[0].Description)
	require.NotNil(t, decoded.Tools[0].Annotations)
	assert.Equal(t, true, *decoded.Tools[0].Annotations.ReadOnly)
	assert.Equal(t, "reset", decoded.Tools[1].Name)
	assert.Empty(t, decoded.Tools[1].Description)
	assert.Nil(t, decoded.Tools[1].Annotations)
}

// TestMcpServerStatus_JSONRoundTrip_Minimal verifies a minimal status with only
// required fields.
func TestMcpServerStatus_JSONRoundTrip_Minimal(t *testing.T) {
	status := McpServerStatus{
		Name:   "pending-server",
		Status: McpStatusPending,
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)

	var decoded McpServerStatus
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "pending-server", decoded.Name)
	assert.Equal(t, McpStatusPending, decoded.Status)
	assert.Nil(t, decoded.ServerInfo)
	assert.Empty(t, decoded.Error)
	assert.Nil(t, decoded.Config)
	assert.Empty(t, decoded.Scope)
	assert.Nil(t, decoded.Tools)
}

// TestMcpServerStatus_JSONRoundTrip_FailedWithError verifies a failed server
// status with an error message.
func TestMcpServerStatus_JSONRoundTrip_FailedWithError(t *testing.T) {
	status := McpServerStatus{
		Name:   "broken-server",
		Status: McpStatusFailed,
		Error:  "Connection refused",
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)

	var decoded McpServerStatus
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, McpStatusFailed, decoded.Status)
	assert.Equal(t, "Connection refused", decoded.Error)
}

// TestMcpStatusResponse_Deserialization verifies deserialization of a full
// McpStatusResponse from the CLI.
func TestMcpStatusResponse_Deserialization(t *testing.T) {
	rawJSON := `{
		"mcpServers": [
			{
				"name": "my-http-server",
				"status": "connected",
				"serverInfo": {"name": "my-http-server", "version": "1.0.0"},
				"config": {"type": "http", "url": "https://example.com/mcp"},
				"scope": "project",
				"tools": [
					{"name": "greet", "description": "Greet a user", "annotations": {"readOnly": true}},
					{"name": "reset"}
				]
			},
			{
				"name": "failed-server",
				"status": "failed",
				"error": "Connection refused"
			},
			{
				"name": "proxy-server",
				"status": "needs-auth",
				"config": {"type": "claudeai-proxy", "url": "https://claude.ai/proxy", "id": "proxy-123"}
			}
		]
	}`

	var resp McpStatusResponse
	err := json.Unmarshal([]byte(rawJSON), &resp)
	require.NoError(t, err)

	require.Len(t, resp.McpServers, 3)

	// Connected server with full info
	connected := resp.McpServers[0]
	assert.Equal(t, "my-http-server", connected.Name)
	assert.Equal(t, McpStatusConnected, connected.Status)
	require.NotNil(t, connected.ServerInfo)
	assert.Equal(t, "1.0.0", connected.ServerInfo.Version)
	assert.Equal(t, "project", connected.Scope)
	require.Len(t, connected.Tools, 2)
	assert.Equal(t, "greet", connected.Tools[0].Name)
	assert.Equal(t, "Greet a user", connected.Tools[0].Description)
	require.NotNil(t, connected.Tools[0].Annotations)
	assert.Equal(t, true, *connected.Tools[0].Annotations.ReadOnly)
	assert.Equal(t, "reset", connected.Tools[1].Name)
	assert.Empty(t, connected.Tools[1].Description)

	// Failed server
	failed := resp.McpServers[1]
	assert.Equal(t, "failed-server", failed.Name)
	assert.Equal(t, McpStatusFailed, failed.Status)
	assert.Equal(t, "Connection refused", failed.Error)

	// Server with claudeai-proxy config
	proxy := resp.McpServers[2]
	assert.Equal(t, "proxy-server", proxy.Name)
	assert.Equal(t, McpStatusNeedsAuth, proxy.Status)
	require.NotNil(t, proxy.Config)
	assert.Equal(t, "claudeai-proxy", proxy.Config["type"])
	assert.Equal(t, "proxy-123", proxy.Config["id"])
}

// TestMcpToolAnnotations_OptionalFields verifies that McpToolAnnotations fields
// are all optional.
func TestMcpToolAnnotations_OptionalFields(t *testing.T) {
	// All fields present
	rawJSON := `{"readOnly": true, "destructive": false, "openWorld": true}`
	var annotations McpToolAnnotations
	err := json.Unmarshal([]byte(rawJSON), &annotations)
	require.NoError(t, err)
	require.NotNil(t, annotations.ReadOnly)
	assert.True(t, *annotations.ReadOnly)
	require.NotNil(t, annotations.Destructive)
	assert.False(t, *annotations.Destructive)
	require.NotNil(t, annotations.OpenWorld)
	assert.True(t, *annotations.OpenWorld)

	// Partial fields
	rawJSON = `{"readOnly": true}`
	var partial McpToolAnnotations
	err = json.Unmarshal([]byte(rawJSON), &partial)
	require.NoError(t, err)
	require.NotNil(t, partial.ReadOnly)
	assert.True(t, *partial.ReadOnly)
	assert.Nil(t, partial.Destructive)
	assert.Nil(t, partial.OpenWorld)

	// Empty
	rawJSON = `{}`
	var empty McpToolAnnotations
	err = json.Unmarshal([]byte(rawJSON), &empty)
	require.NoError(t, err)
	assert.Nil(t, empty.ReadOnly)
	assert.Nil(t, empty.Destructive)
	assert.Nil(t, empty.OpenWorld)
}

// controlCapturingTransport captures all written control requests for inspection.
type controlCapturingTransport struct {
	mu       sync.Mutex
	writes   []string
	closeCh  chan struct{}
	closed   bool
	resultCh chan controlResult
}

func newControlCapturingTransport() *controlCapturingTransport {
	return &controlCapturingTransport{
		closeCh:  make(chan struct{}),
		resultCh: make(chan controlResult, 1),
	}
}

func (t *controlCapturingTransport) Connect(ctx context.Context) error { return nil }
func (t *controlCapturingTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	ch := make(chan MessageOrError)
	close(ch)
	return ch
}
func (t *controlCapturingTransport) Write(ctx context.Context, data string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writes = append(t.writes, data)
	return nil
}
func (t *controlCapturingTransport) EndInput(ctx context.Context) error { return nil }
func (t *controlCapturingTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.closeCh)
	}
	return nil
}
func (t *controlCapturingTransport) IsReady() bool { return true }

func (t *controlCapturingTransport) getWrites() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]string, len(t.writes))
	copy(result, t.writes)
	return result
}

// TestReconnectMCPServer_ControlRequestJSON verifies the JSON shape of the
// mcp_reconnect control request, including camelCase serverName.
func TestReconnectMCPServer_ControlRequestJSON(t *testing.T) {
	transport := newControlCapturingTransport()
	router := NewControlRouter(transport, &Options{})

	// We send the request in a goroutine since it will block waiting for a response.
	// We'll manually respond via handleControlResponse.
	done := make(chan error, 1)
	go func() {
		done <- router.ReconnectMCPServer(context.Background(), "my-server")
	}()

	// Wait for the write to appear
	require.Eventually(t, func() bool {
		return len(transport.getWrites()) > 0
	}, time.Second, 10*time.Millisecond)

	writes := transport.getWrites()
	require.Len(t, writes, 1)

	var req map[string]any
	err := json.Unmarshal([]byte(writes[0]), &req)
	require.NoError(t, err)

	assert.Equal(t, "control_request", req["type"])
	request, ok := req["request"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "mcp_reconnect", request["subtype"])
	// Wire format uses camelCase serverName
	assert.Equal(t, "my-server", request["serverName"])
	// Verify there is no snake_case variant
	_, hasSnakeCase := request["server_name"]
	assert.False(t, hasSnakeCase, "wire format should use camelCase serverName, not snake_case")

	// Respond to unblock the goroutine
	requestID, ok := req["request_id"].(string)
	require.True(t, ok)
	responseRaw := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + requestID + `","response":{}}}`)
	err = router.handleControlResponse(&ControlResponse{}, responseRaw)
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("ReconnectMCPServer did not complete")
	}
}

// TestToggleMCPServer_ControlRequestJSON_Disable verifies the JSON shape of the
// mcp_toggle control request with enabled=false, including camelCase serverName.
func TestToggleMCPServer_ControlRequestJSON_Disable(t *testing.T) {
	transport := newControlCapturingTransport()
	router := NewControlRouter(transport, &Options{})

	done := make(chan error, 1)
	go func() {
		done <- router.ToggleMCPServer(context.Background(), "my-server", false)
	}()

	require.Eventually(t, func() bool {
		return len(transport.getWrites()) > 0
	}, time.Second, 10*time.Millisecond)

	writes := transport.getWrites()
	require.Len(t, writes, 1)

	var req map[string]any
	err := json.Unmarshal([]byte(writes[0]), &req)
	require.NoError(t, err)

	assert.Equal(t, "control_request", req["type"])
	request, ok := req["request"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "mcp_toggle", request["subtype"])
	assert.Equal(t, "my-server", request["serverName"])
	assert.Equal(t, false, request["enabled"])
	_, hasSnakeCase := request["server_name"]
	assert.False(t, hasSnakeCase, "wire format should use camelCase serverName, not snake_case")

	requestID, ok := req["request_id"].(string)
	require.True(t, ok)
	responseRaw := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + requestID + `","response":{}}}`)
	err = router.handleControlResponse(&ControlResponse{}, responseRaw)
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("ToggleMCPServer did not complete")
	}
}

// TestToggleMCPServer_ControlRequestJSON_Enable verifies enabled=true is sent.
func TestToggleMCPServer_ControlRequestJSON_Enable(t *testing.T) {
	transport := newControlCapturingTransport()
	router := NewControlRouter(transport, &Options{})

	done := make(chan error, 1)
	go func() {
		done <- router.ToggleMCPServer(context.Background(), "other-server", true)
	}()

	require.Eventually(t, func() bool {
		return len(transport.getWrites()) > 0
	}, time.Second, 10*time.Millisecond)

	writes := transport.getWrites()
	require.Len(t, writes, 1)

	var req map[string]any
	err := json.Unmarshal([]byte(writes[0]), &req)
	require.NoError(t, err)

	request, ok := req["request"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "mcp_toggle", request["subtype"])
	assert.Equal(t, "other-server", request["serverName"])
	assert.Equal(t, true, request["enabled"])

	requestID, ok := req["request_id"].(string)
	require.True(t, ok)
	responseRaw := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + requestID + `","response":{}}}`)
	err = router.handleControlResponse(&ControlResponse{}, responseRaw)
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("ToggleMCPServer did not complete")
	}
}

// TestStopTask_ControlRequestJSON verifies the JSON shape of the stop_task
// control request, including snake_case task_id.
func TestStopTask_ControlRequestJSON(t *testing.T) {
	transport := newControlCapturingTransport()
	router := NewControlRouter(transport, &Options{})

	done := make(chan error, 1)
	go func() {
		done <- router.StopTask(context.Background(), "task-abc123")
	}()

	require.Eventually(t, func() bool {
		return len(transport.getWrites()) > 0
	}, time.Second, 10*time.Millisecond)

	writes := transport.getWrites()
	require.Len(t, writes, 1)

	var req map[string]any
	err := json.Unmarshal([]byte(writes[0]), &req)
	require.NoError(t, err)

	assert.Equal(t, "control_request", req["type"])
	request, ok := req["request"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "stop_task", request["subtype"])
	// Wire format uses snake_case task_id
	assert.Equal(t, "task-abc123", request["task_id"])

	requestID, ok := req["request_id"].(string)
	require.True(t, ok)
	responseRaw := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + requestID + `","response":{}}}`)
	err = router.handleControlResponse(&ControlResponse{}, responseRaw)
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("StopTask did not complete")
	}
}

// TestClient_ReconnectMCPServer_NotConnected verifies the error when not connected.
func TestClient_ReconnectMCPServer_NotConnected(t *testing.T) {
	client := NewClient()
	err := client.ReconnectMCPServer(context.Background(), "my-server")
	assert.ErrorIs(t, err, ErrNotConnected)
}

// TestClient_ToggleMCPServer_NotConnected verifies the error when not connected.
func TestClient_ToggleMCPServer_NotConnected(t *testing.T) {
	client := NewClient()
	err := client.ToggleMCPServer(context.Background(), "my-server", true)
	assert.ErrorIs(t, err, ErrNotConnected)
}

// TestClient_StopTask_NotConnected verifies the error when not connected.
func TestClient_StopTask_NotConnected(t *testing.T) {
	client := NewClient()
	err := client.StopTask(context.Background(), "task-abc123")
	assert.ErrorIs(t, err, ErrNotConnected)
}

// TestGetMCPStatus_TypedResponse verifies that GetMCPStatus returns a typed
// McpStatusResponse instead of raw map[string]any.
func TestGetMCPStatus_TypedResponse(t *testing.T) {
	transport := newControlCapturingTransport()
	router := NewControlRouter(transport, &Options{})

	done := make(chan struct{})
	var result McpStatusResponse
	var resultErr error

	go func() {
		defer close(done)
		result, resultErr = router.GetMCPStatus(context.Background())
	}()

	require.Eventually(t, func() bool {
		return len(transport.getWrites()) > 0
	}, time.Second, 10*time.Millisecond)

	writes := transport.getWrites()
	require.Len(t, writes, 1)

	var req map[string]any
	err := json.Unmarshal([]byte(writes[0]), &req)
	require.NoError(t, err)

	requestID, ok := req["request_id"].(string)
	require.True(t, ok)

	// Simulate the CLI response with typed server statuses
	responseRaw := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + requestID + `","response":{"mcpServers":[{"name":"test-server","status":"connected","serverInfo":{"name":"test","version":"2.0"},"tools":[{"name":"tool1","description":"A tool"}]},{"name":"off-server","status":"disabled"}]}}}`)
	err = router.handleControlResponse(&ControlResponse{}, responseRaw)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GetMCPStatus did not complete")
	}

	require.NoError(t, resultErr)
	require.Len(t, result.McpServers, 2)

	assert.Equal(t, "test-server", result.McpServers[0].Name)
	assert.Equal(t, McpStatusConnected, result.McpServers[0].Status)
	require.NotNil(t, result.McpServers[0].ServerInfo)
	assert.Equal(t, "2.0", result.McpServers[0].ServerInfo.Version)
	require.Len(t, result.McpServers[0].Tools, 1)
	assert.Equal(t, "tool1", result.McpServers[0].Tools[0].Name)
	assert.Equal(t, "A tool", result.McpServers[0].Tools[0].Description)

	assert.Equal(t, "off-server", result.McpServers[1].Name)
	assert.Equal(t, McpStatusDisabled, result.McpServers[1].Status)
}

func boolPtr(b bool) *bool { return &b }
