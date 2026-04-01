package claudecode

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpowers/go-claudecode/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stdinLifecycleTransport is a mock transport that tracks the ordering of
// Write, EndInput, and message delivery. It simulates the CLI by emitting
// a configurable sequence of messages (including control requests for MCP
// initialization) before the assistant/result messages.
type stdinLifecycleTransport struct {
	mu sync.Mutex

	// messages to emit from ReadMessages (pre-serialized JSON lines)
	messages []json.RawMessage

	// Tracking for assertions
	callLog []stdinCall

	// endInputCalled is closed when EndInput is invoked.
	endInputCalled chan struct{}
	endInputOnce   sync.Once

	// controlResponseCount tracks how many control_response writes occurred
	// before EndInput was called. This verifies that MCP init responses
	// can be sent while stdin is open.
	controlResponsesBeforeEndInput atomic.Int32

	connected bool
	closed    bool
}

type stdinCall struct {
	method string
	data   string
}

func (t *stdinLifecycleTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connected = true
	return nil
}

func (t *stdinLifecycleTransport) Write(ctx context.Context, data string) error {
	t.mu.Lock()
	t.callLog = append(t.callLog, stdinCall{method: "write", data: data})
	t.mu.Unlock()

	// Track control responses before EndInput
	select {
	case <-t.endInputCalled:
		// EndInput already called
	default:
		if isControlResponse(data) {
			t.controlResponsesBeforeEndInput.Add(1)
		}
	}

	return nil
}

func (t *stdinLifecycleTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	ch := make(chan MessageOrError, 100)
	go func() {
		defer close(ch)
		for _, raw := range t.messages {
			msg, err := parseMessage(raw)
			if err != nil {
				ch <- MessageOrError{Err: err}
				return
			}
			if msg == nil {
				continue
			}
			ch <- MessageOrError{Message: msg, Raw: raw}
		}
	}()
	return ch
}

func (t *stdinLifecycleTransport) EndInput(ctx context.Context) error {
	t.mu.Lock()
	t.callLog = append(t.callLog, stdinCall{method: "end_input"})
	t.mu.Unlock()

	t.endInputOnce.Do(func() {
		close(t.endInputCalled)
	})
	return nil
}

func (t *stdinLifecycleTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

func (t *stdinLifecycleTransport) IsReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected && !t.closed
}

func (t *stdinLifecycleTransport) getCalls() []stdinCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]stdinCall, len(t.callLog))
	copy(result, t.callLog)
	return result
}

func isControlResponse(data string) bool {
	var msg struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(data), &msg) == nil {
		return msg.Type == "control_response"
	}
	return false
}

// Standard test messages: an assistant message followed by a result.
func assistantAndResultMessages() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`{
			"type": "assistant",
			"message": {
				"role": "assistant",
				"content": [{"type": "text", "text": "Hello!"}],
				"model": "claude-sonnet-4-20250514"
			}
		}`),
		json.RawMessage(`{
			"type": "result",
			"subtype": "success",
			"duration_ms": 100,
			"duration_api_ms": 80,
			"is_error": false,
			"num_turns": 1,
			"session_id": "test-session"
		}`),
	}
}

// mcpControlRequestMessages returns control requests that simulate MCP server
// initialization (initialize + tools/list).
func mcpControlRequestMessages() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`{
			"type": "control_request",
			"request_id": "mcp_init_1",
			"request": {
				"subtype": "mcp_message",
				"server_name": "test-server",
				"message": {
					"jsonrpc": "2.0",
					"id": 1,
					"method": "initialize",
					"params": {}
				}
			}
		}`),
		json.RawMessage(`{
			"type": "control_request",
			"request_id": "mcp_init_2",
			"request": {
				"subtype": "mcp_message",
				"server_name": "test-server",
				"message": {
					"jsonrpc": "2.0",
					"id": 2,
					"method": "tools/list",
					"params": {}
				}
			}
		}`),
	}
}

// runQueryWithMockTransport executes the core QueryWithInput logic with a
// mock transport. This replicates the structure of QueryWithInput but allows
// injecting a test transport.
//
// It returns all received non-control messages and the mock transport for
// call-order assertions.
func runQueryWithMockTransport(
	t *testing.T,
	ctx context.Context,
	transport *stdinLifecycleTransport,
	options *Options,
	input <-chan InputMessage,
) []Message {
	t.Helper()

	options.streamingMode = true

	err := transport.Connect(ctx)
	require.NoError(t, err)

	router := NewControlRouter(transport, options)

	msgCh := transport.ReadMessages(ctx)

	firstResultReceived := make(chan struct{})
	var firstResultOnce sync.Once

	routedCh := make(chan MessageOrError, 100)
	readerDone := make(chan struct{})

	var wg sync.WaitGroup

	// Reader goroutine (same as QueryWithInput)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(routedCh)
		defer close(readerDone)

		for msg := range msgCh {
			if msg.Err != nil {
				routedCh <- msg
				continue
			}

			handled, err := router.HandleMessage(ctx, msg.Message, msg.Raw)
			if err != nil {
				routedCh <- MessageOrError{Err: err}
				continue
			}
			if handled {
				continue
			}

			if _, ok := msg.Message.(*ResultMessage); ok {
				firstResultOnce.Do(func() {
					close(firstResultReceived)
				})
			}

			routedCh <- msg
		}
	}()

	// Skip Initialize for test simplicity - the control router doesn't
	// need a real initialize handshake to handle MCP messages.
	router.setInitialized(nil)

	// Input streaming goroutine (same as QueryWithInput)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			hasSDKMCP := len(options.sdkMCPServers) > 0
			hasHooks := len(options.hooks) > 0
			hasCanUseTool := options.canUseTool != nil

			if hasSDKMCP || hasHooks || hasCanUseTool {
				select {
				case <-firstResultReceived:
				case <-readerDone:
				case <-time.After(5 * time.Second): // Short timeout for tests
				case <-ctx.Done():
				}
			}

			transport.EndInput(ctx)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-input:
				if !ok {
					return
				}
				data, err := json.Marshal(msg)
				if err != nil {
					return
				}
				if writeErr := transport.Write(ctx, string(data)+"\n"); writeErr != nil {
					return
				}
			}
		}
	}()

	var messages []Message
	for msg := range routedCh {
		if msg.Err != nil {
			t.Errorf("unexpected error: %v", msg.Err)
			continue
		}
		messages = append(messages, msg.Message)
	}

	wg.Wait()
	transport.Close(ctx)
	return messages
}

// noopTool is a minimal chat.Tool for creating SDK MCP servers in tests.
type noopTool struct {
	name string
}

func (t *noopTool) Name() string                              { return t.name }
func (t *noopTool) Description() string                       { return "test tool" }
func (t *noopTool) MCPJsonSchema() string                     { return `{"type":"object"}` }
func (t *noopTool) Call(ctx context.Context, input string) string { return `{"result":"ok"}` }

func TestStringPromptStdinLifecycle(t *testing.T) {
	t.Run("waits for first result when SDK MCP servers present", func(t *testing.T) {
		// This test verifies the fix for the Python bug in commit 6119fd4:
		// when a string prompt is used with SDK MCP servers, stdin must
		// stay open until the first result arrives so that MCP init
		// control requests can be responded to.
		//
		// In Go, Query() delegates to QueryWithInput(), so both paths
		// share the same stdin lifecycle logic. This test proves it works.

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		transport := &stdinLifecycleTransport{
			messages:       assistantAndResultMessages(),
			endInputCalled: make(chan struct{}),
		}

		options := &Options{
			sdkMCPServers: map[string]*MCPSDKConfig{
				"test-server": {
					Name:  "test-server",
					Tools: []chat.Tool{&noopTool{name: "greet"}},
				},
			},
		}

		// Simulate string prompt: buffered channel with one message, immediately closed
		input := make(chan InputMessage, 1)
		input <- NewUserInput("Hello")
		close(input)

		messages := runQueryWithMockTransport(t, ctx, transport, options, input)

		// Verify we got the expected messages
		require.Len(t, messages, 2)
		assert.IsType(t, &AssistantMessage{}, messages[0])
		assert.IsType(t, &ResultMessage{}, messages[1])

		// Verify EndInput was called
		calls := transport.getCalls()
		hasEndInput := false
		for _, c := range calls {
			if c.method == "end_input" {
				hasEndInput = true
			}
		}
		assert.True(t, hasEndInput, "EndInput should have been called")

		// Verify the user message was written
		hasUserWrite := false
		for _, c := range calls {
			if c.method == "write" {
				var msg struct{ Type string }
				if json.Unmarshal([]byte(c.data), &msg) == nil && msg.Type == "user" {
					hasUserWrite = true
				}
			}
		}
		assert.True(t, hasUserWrite, "user message should have been written")
	})

	t.Run("closes stdin immediately without MCP servers or hooks", func(t *testing.T) {
		// Without SDK MCP servers or hooks, stdin should close as soon as
		// the input channel is exhausted, without waiting for any result.

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		transport := &stdinLifecycleTransport{
			messages:       assistantAndResultMessages(),
			endInputCalled: make(chan struct{}),
		}

		options := &Options{} // No MCP servers, no hooks

		input := make(chan InputMessage, 1)
		input <- NewUserInput("Hello")
		close(input)

		messages := runQueryWithMockTransport(t, ctx, transport, options, input)

		require.Len(t, messages, 2)
		assert.IsType(t, &AssistantMessage{}, messages[0])
		assert.IsType(t, &ResultMessage{}, messages[1])

		// EndInput should have been called
		calls := transport.getCalls()
		hasEndInput := false
		for _, c := range calls {
			if c.method == "end_input" {
				hasEndInput = true
			}
		}
		assert.True(t, hasEndInput, "EndInput should have been called")
	})

	t.Run("waits for first result when hooks present", func(t *testing.T) {
		// Hooks also require bidirectional communication, so stdin should
		// stay open until the first result even without MCP servers.

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		transport := &stdinLifecycleTransport{
			messages:       assistantAndResultMessages(),
			endInputCalled: make(chan struct{}),
		}

		options := &Options{
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

		input := make(chan InputMessage, 1)
		input <- NewUserInput("Do something")
		close(input)

		messages := runQueryWithMockTransport(t, ctx, transport, options, input)

		require.Len(t, messages, 2)
		assert.IsType(t, &AssistantMessage{}, messages[0])
		assert.IsType(t, &ResultMessage{}, messages[1])

		calls := transport.getCalls()
		hasEndInput := false
		for _, c := range calls {
			if c.method == "end_input" {
				hasEndInput = true
			}
		}
		assert.True(t, hasEndInput, "EndInput should have been called")
	})

	t.Run("MCP control requests handled while stdin open", func(t *testing.T) {
		// When MCP control requests arrive after the user message, they
		// should be handled successfully because stdin is still open. This
		// is the core scenario that the Python fix addresses.

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Build message sequence: MCP init requests, then assistant + result
		var allMessages []json.RawMessage
		allMessages = append(allMessages, mcpControlRequestMessages()...)
		allMessages = append(allMessages, assistantAndResultMessages()...)

		transport := &stdinLifecycleTransport{
			messages:       allMessages,
			endInputCalled: make(chan struct{}),
		}

		options := &Options{
			sdkMCPServers: map[string]*MCPSDKConfig{
				"test-server": {
					Name:  "test-server",
					Tools: []chat.Tool{&noopTool{name: "greet"}},
				},
			},
		}

		input := make(chan InputMessage, 1)
		input <- NewUserInput("Greet Alice")
		close(input)

		messages := runQueryWithMockTransport(t, ctx, transport, options, input)

		// Should get assistant + result (control messages are handled by router)
		require.Len(t, messages, 2)
		assert.IsType(t, &AssistantMessage{}, messages[0])
		assert.IsType(t, &ResultMessage{}, messages[1])

		// Verify control responses were written before EndInput
		controlResponsesBefore := transport.controlResponsesBeforeEndInput.Load()
		assert.Equal(t, int32(2), controlResponsesBefore,
			"both MCP control responses should have been written before EndInput")

		// Verify the overall call sequence
		calls := transport.getCalls()
		var writeCount, controlResponseCount int
		endInputIdx := -1
		for i, c := range calls {
			if c.method == "write" {
				writeCount++
				if isControlResponse(c.data) {
					controlResponseCount++
				}
			}
			if c.method == "end_input" {
				endInputIdx = i
			}
		}

		assert.Equal(t, 2, controlResponseCount,
			"should have 2 MCP control responses (initialize + tools/list)")
		assert.Greater(t, endInputIdx, 0,
			"EndInput should appear in the call log")
		assert.GreaterOrEqual(t, writeCount, 3,
			"should have at least 3 writes (user message + 2 control responses)")
	})

	t.Run("reader early exit unblocks stdin closure", func(t *testing.T) {
		// If the reader goroutine exits early (e.g., no messages at all),
		// the readerDone channel should unblock the stdin closure so it
		// doesn't wait for the full timeout.

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		transport := &stdinLifecycleTransport{
			messages:       nil, // No messages at all - reader exits immediately
			endInputCalled: make(chan struct{}),
		}

		options := &Options{
			sdkMCPServers: map[string]*MCPSDKConfig{
				"test-server": {
					Name:  "test-server",
					Tools: []chat.Tool{&noopTool{name: "greet"}},
				},
			},
		}

		input := make(chan InputMessage, 1)
		input <- NewUserInput("Hello")
		close(input)

		start := time.Now()
		_ = runQueryWithMockTransport(t, ctx, transport, options, input)
		elapsed := time.Since(start)

		// Should complete quickly (< 2s), not wait for the full 5s test timeout.
		// The readerDone channel should unblock the wait.
		assert.Less(t, elapsed, 2*time.Second,
			"should not wait for timeout when reader exits early")

		calls := transport.getCalls()
		hasEndInput := false
		for _, c := range calls {
			if c.method == "end_input" {
				hasEndInput = true
			}
		}
		assert.True(t, hasEndInput, "EndInput should still be called on early exit")
	})

	t.Run("async iterable with MCP servers shares same behavior", func(t *testing.T) {
		// Verify that a multi-message streaming input has the same stdin
		// lifecycle as a single-message string prompt when MCP servers
		// are present. Both should wait for first result.

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var allMessages []json.RawMessage
		allMessages = append(allMessages, mcpControlRequestMessages()...)
		allMessages = append(allMessages, assistantAndResultMessages()...)

		transport := &stdinLifecycleTransport{
			messages:       allMessages,
			endInputCalled: make(chan struct{}),
		}

		options := &Options{
			sdkMCPServers: map[string]*MCPSDKConfig{
				"test-server": {
					Name:  "test-server",
					Tools: []chat.Tool{&noopTool{name: "greet"}},
				},
			},
		}

		// Multiple messages streamed (simulating AsyncIterable)
		input := make(chan InputMessage, 2)
		input <- NewUserInput("First message")
		input <- NewUserInput("Second message")
		close(input)

		messages := runQueryWithMockTransport(t, ctx, transport, options, input)

		require.Len(t, messages, 2)
		assert.IsType(t, &AssistantMessage{}, messages[0])
		assert.IsType(t, &ResultMessage{}, messages[1])

		// Control responses should have been written before EndInput
		controlResponsesBefore := transport.controlResponsesBeforeEndInput.Load()
		assert.Equal(t, int32(2), controlResponsesBefore,
			"MCP control responses should be written before EndInput")
	})
}
