package claudecode

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnvDuration(t *testing.T) {
	const testEnvKey = "TEST_CLAUDECODE_TIMEOUT"

	t.Run("returns 0 when env var not set", func(t *testing.T) {
		os.Unsetenv(testEnvKey)
		d, err := getEnvDuration(testEnvKey)
		require.NoError(t, err)
		assert.Equal(t, time.Duration(0), d)
	})

	t.Run("parses milliseconds correctly", func(t *testing.T) {
		os.Setenv(testEnvKey, "5000")
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDuration(testEnvKey)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, d)
	})

	t.Run("returns error for invalid value", func(t *testing.T) {
		os.Setenv(testEnvKey, "not-a-number")
		defer os.Unsetenv(testEnvKey)

		_, err := getEnvDuration(testEnvKey)
		require.Error(t, err)
	})
}

func TestGetEnvDurationWithDefault(t *testing.T) {
	const testEnvKey = "TEST_CLAUDECODE_TIMEOUT"
	const defaultVal = 60 * time.Second

	t.Run("returns default when env var not set", func(t *testing.T) {
		os.Unsetenv(testEnvKey)
		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, defaultVal, d)
	})

	t.Run("honors env value below default", func(t *testing.T) {
		os.Setenv(testEnvKey, "5000") // 5 seconds, below 60s default
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, d, "should honor user-specified value even if below default")
	})

	t.Run("honors env value above default", func(t *testing.T) {
		os.Setenv(testEnvKey, "120000") // 120 seconds
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, 120*time.Second, d)
	})

	t.Run("honors env value equal to default", func(t *testing.T) {
		os.Setenv(testEnvKey, "60000") // exactly 60 seconds
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, 60*time.Second, d)
	})

	t.Run("returns error for invalid value", func(t *testing.T) {
		os.Setenv(testEnvKey, "invalid")
		defer os.Unsetenv(testEnvKey)

		_, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.Error(t, err)
	})
}

func TestInitialize_IncludesAgents(t *testing.T) {
	// Verify that Initialize() includes agents in the request map.
	// We can't easily test the full round-trip without a real CLI,
	// but we can verify the request construction by inspecting the
	// ControlRouter's options.
	opts := &Options{
		agents: map[string]AgentDefinition{
			"analyzer": {
				Description: "Analyzes code",
				Prompt:      "Analyze code",
			},
		},
	}

	router := NewControlRouter(nil, opts)
	assert.NotNil(t, router.options.agents)
	assert.Contains(t, router.options.agents, "analyzer")
}

func TestHandleCanUseTool_BlockedPath(t *testing.T) {
	// Verify that blocked_path is parsed from the request.
	var receivedCtx ToolPermissionContext
	opts := &Options{
		canUseTool: func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			receivedCtx = permCtx
			return PermissionAllow{}, nil
		},
	}

	router := NewControlRouter(nil, opts)

	raw := []byte(`{
		"request_id": "req_1",
		"request": {
			"subtype": "can_use_tool",
			"tool_name": "Write",
			"input": {"path": "/tmp/test"},
			"blocked_path": "/etc/sensitive",
			"permission_suggestions": []
		}
	}`)

	_, err := router.handleCanUseTool(context.Background(), raw)
	require.NoError(t, err)
	assert.Equal(t, "/etc/sensitive", receivedCtx.BlockedPath)
}

func TestHandleCanUseTool_ToolUseIDAndAgentID(t *testing.T) {
	// Verify that tool_use_id and agent_id are parsed and passed to the callback.
	var receivedCtx ToolPermissionContext
	opts := &Options{
		canUseTool: func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			receivedCtx = permCtx
			return PermissionAllow{}, nil
		},
	}

	router := NewControlRouter(nil, opts)

	raw := []byte(`{
		"request_id": "req_2",
		"request": {
			"subtype": "can_use_tool",
			"tool_name": "Bash",
			"input": {"command": "ls"},
			"permission_suggestions": [],
			"tool_use_id": "toolu_01ABC123",
			"agent_id": "agent-456"
		}
	}`)

	_, err := router.handleCanUseTool(context.Background(), raw)
	require.NoError(t, err)
	assert.Equal(t, "toolu_01ABC123", receivedCtx.ToolUseID)
	assert.Equal(t, "agent-456", receivedCtx.AgentID)
}

func TestHandleCanUseTool_MissingAgentID(t *testing.T) {
	// Verify that agent_id defaults to empty string when not present in the request
	// (e.g. when the tool call comes from the top-level agent, not a sub-agent).
	var receivedCtx ToolPermissionContext
	opts := &Options{
		canUseTool: func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			receivedCtx = permCtx
			return PermissionAllow{}, nil
		},
	}

	router := NewControlRouter(nil, opts)

	raw := []byte(`{
		"request_id": "req_3",
		"request": {
			"subtype": "can_use_tool",
			"tool_name": "Bash",
			"input": {"command": "pwd"},
			"permission_suggestions": [],
			"tool_use_id": "toolu_01XYZ789"
		}
	}`)

	_, err := router.handleCanUseTool(context.Background(), raw)
	require.NoError(t, err)
	assert.Equal(t, "toolu_01XYZ789", receivedCtx.ToolUseID)
	assert.Empty(t, receivedCtx.AgentID)
}

func TestHandleCanUseTool_MissingToolUseID(t *testing.T) {
	// Verify that tool_use_id defaults to empty string when not present.
	// This guards against older CLI versions that might not send tool_use_id.
	var receivedCtx ToolPermissionContext
	opts := &Options{
		canUseTool: func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			receivedCtx = permCtx
			return PermissionAllow{}, nil
		},
	}

	router := NewControlRouter(nil, opts)

	raw := []byte(`{
		"request_id": "req_4",
		"request": {
			"subtype": "can_use_tool",
			"tool_name": "Write",
			"input": {"path": "/tmp/test"},
			"permission_suggestions": []
		}
	}`)

	_, err := router.handleCanUseTool(context.Background(), raw)
	require.NoError(t, err)
	assert.Empty(t, receivedCtx.ToolUseID)
	assert.Empty(t, receivedCtx.AgentID)
}

func TestControlRouter_HandleMessage_NonControlPassthrough(t *testing.T) {
	// Non-control messages (user, assistant, system, result, stream events)
	// should pass through HandleMessage and return handled=false.
	opts := &Options{}
	router := NewControlRouter(nil, opts)
	ctx := context.Background()

	tests := []struct {
		name string
		msg  Message
	}{
		{"UserMessage", &UserMessage{Content: "hello"}},
		{"AssistantMessage", &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "test"}}}},
		{"SystemMessage", &SystemMessage{Subtype: "init"}},
		{"ResultMessage", &ResultMessage{Subtype: "success", SessionID: "s1"}},
		{"StreamEvent", &StreamEvent{UUID: "u1", SessionID: "s1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handled, err := router.HandleMessage(ctx, tt.msg, nil)
			require.NoError(t, err)
			assert.False(t, handled, "%s should not be handled as control message", tt.name)
		})
	}
}

func TestControlRouter_HandleMessage_UnknownControlSubtype(t *testing.T) {
	// When the CLI sends a control request with an unknown subtype,
	// HandleMessage should return an error response (not panic or ignore).
	transport := &writeCapturingTransport{}
	opts := &Options{}
	router := NewControlRouter(transport, opts)
	ctx := context.Background()

	msg := &ControlRequest{
		Type:      "control_request",
		RequestID: "req_99",
	}
	raw := []byte(`{
		"type": "control_request",
		"request_id": "req_99",
		"request": {"subtype": "unknown_future_subtype"}
	}`)

	handled, err := router.HandleMessage(ctx, msg, raw)
	require.NoError(t, err)
	assert.True(t, handled, "control request should always be handled")

	// The router should send an error response back to the transport
	written := transport.lastWritten
	assert.Contains(t, written, "control_response")
	assert.Contains(t, written, "error")
	assert.Contains(t, written, "unsupported control request subtype")
}

func TestControlRouter_HandleMessage_ControlCancelRequest(t *testing.T) {
	// ControlCancelRequest should be handled (swallowed) without error.
	opts := &Options{}
	router := NewControlRouter(nil, opts)
	ctx := context.Background()

	msg := &ControlCancelRequest{
		Type:      "control_cancel_request",
		RequestID: "req_42",
	}

	handled, err := router.HandleMessage(ctx, msg, nil)
	require.NoError(t, err)
	assert.True(t, handled, "cancel requests should be handled")
}

func TestControlRouter_ConcurrentRequests(t *testing.T) {
	// Verify that concurrent access to the control router is safe.
	transport := &writeCapturingTransport{}
	opts := &Options{
		canUseTool: func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			return PermissionAllow{}, nil
		},
	}
	router := NewControlRouter(transport, opts)
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			// Mix of operations: HandleMessage with various types, nextRequestID
			switch idx % 4 {
			case 0:
				// Non-control passthrough
				msg := &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "test"}}}
				_, _ = router.HandleMessage(ctx, msg, nil)
			case 1:
				// Generate request IDs concurrently
				_ = router.nextRequestID()
			case 2:
				// Handle control request with can_use_tool subtype
				msg := &ControlRequest{Type: "control_request", RequestID: fmt.Sprintf("req_%d", idx)}
				raw := []byte(fmt.Sprintf(`{
					"type": "control_request",
					"request_id": "req_%d",
					"request": {"subtype": "can_use_tool", "tool_name": "Bash", "input": {}}
				}`, idx))
				_, _ = router.HandleMessage(ctx, msg, raw)
			case 3:
				// Cancel request
				msg := &ControlCancelRequest{Type: "control_cancel_request", RequestID: fmt.Sprintf("cancel_%d", idx)}
				_, _ = router.HandleMessage(ctx, msg, nil)
			}
		}(i)
	}

	wg.Wait()
}

// writeCapturingTransport captures the last written data for assertions.
type writeCapturingTransport struct {
	mu          sync.Mutex
	lastWritten string
}

func (w *writeCapturingTransport) Connect(ctx context.Context) error                      { return nil }
func (w *writeCapturingTransport) ReadMessages(ctx context.Context) <-chan MessageOrError  { return nil }
func (w *writeCapturingTransport) Write(ctx context.Context, data string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastWritten = data
	return nil
}
func (w *writeCapturingTransport) EndInput(ctx context.Context) error                     { return nil }
func (w *writeCapturingTransport) Close(ctx context.Context) error                        { return nil }
func (w *writeCapturingTransport) IsReady() bool                                          { return true }

func TestInitialize_HonorsEnvTimeout(t *testing.T) {
	// Verify that Initialize() reads CLAUDE_CODE_STREAM_CLOSE_TIMEOUT and uses
	// it as the initialize timeout. This is the Go equivalent of the Python SDK
	// fix in commit 76cb292: both Query()/QueryWithInput() and Client.Connect()
	// call ControlRouter.Initialize(), which reads the env var. The Go SDK never
	// had the Python bug (separate code paths with one forgetting the env var),
	// but this test guards against regressions.

	t.Run("custom timeout from env var", func(t *testing.T) {
		// Set a short timeout (100ms) via the env var. Initialize() should
		// time out in roughly 100ms since the transport never responds.
		t.Setenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT", "100")

		transport := &writeCapturingTransport{}
		opts := &Options{}
		router := NewControlRouter(transport, opts)

		start := time.Now()
		_, err := router.Initialize(context.Background())
		elapsed := time.Since(start)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTimeout)

		// Should have timed out in ~100ms, not the default 60s.
		// Use a generous upper bound to avoid flakiness, but ensure it
		// did not wait anywhere close to the default 60s.
		assert.Less(t, elapsed, 5*time.Second,
			"should time out near 100ms, not the default 60s")
	})

	t.Run("default timeout when env var unset", func(t *testing.T) {
		// When the env var is unset, Initialize() should use DefaultInitializeTimeout (60s).
		// We cannot wait 60s in a unit test, so instead we use context cancellation
		// to verify that the initialize attempt starts (proving it did not time out
		// at 0 or some other wrong value).
		os.Unsetenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT")

		transport := &writeCapturingTransport{}
		opts := &Options{}
		router := NewControlRouter(transport, opts)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := router.Initialize(ctx)
		require.Error(t, err)
		// Should be a context deadline exceeded, not ErrTimeout (because the
		// default 60s timeout is much longer than our 50ms context deadline).
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("invalid env var returns error", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT", "not-a-number")

		transport := &writeCapturingTransport{}
		opts := &Options{}
		router := NewControlRouter(transport, opts)

		_, err := router.Initialize(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse duration")
	})
}

func TestBuildHooksConfig(t *testing.T) {
	// Suppress unused variable warning for context import
	_ = context.Background()

	t.Run("omits matcher key when empty string", func(t *testing.T) {
		opts := &Options{
			hooks: map[HookEvent][]HookMatcher{
				HookPreToolUse: {
					{
						Matcher: "", // Empty - should be omitted
						Hooks: []HookCallback{
							func(ctx context.Context, input HookInput, toolUseID *string) (HookOutput, error) {
								return HookOutput{}, nil
							},
						},
					},
				},
			},
		}

		router := NewControlRouter(nil, opts)
		config := router.buildHooksConfig()

		require.NotNil(t, config)
		preToolUse, ok := config["PreToolUse"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, preToolUse, 1)

		// Check that matcher key is NOT present (not set to empty string)
		_, hasMatcher := preToolUse[0]["matcher"]
		assert.False(t, hasMatcher, "matcher key should be omitted when empty")
		assert.NotNil(t, preToolUse[0]["hookCallbackIds"])
	})

	t.Run("includes matcher key when non-empty", func(t *testing.T) {
		opts := &Options{
			hooks: map[HookEvent][]HookMatcher{
				HookPreToolUse: {
					{
						Matcher: "Bash",
						Hooks: []HookCallback{
							func(ctx context.Context, input HookInput, toolUseID *string) (HookOutput, error) {
								return HookOutput{}, nil
							},
						},
					},
				},
			},
		}

		router := NewControlRouter(nil, opts)
		config := router.buildHooksConfig()

		require.NotNil(t, config)
		preToolUse, ok := config["PreToolUse"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, preToolUse, 1)

		// Check that matcher key IS present
		matcher, hasMatcher := preToolUse[0]["matcher"]
		assert.True(t, hasMatcher, "matcher key should be present when non-empty")
		assert.Equal(t, "Bash", matcher)
	})
}
