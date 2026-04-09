package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Query performs a one-shot query with a string prompt.
// Internally delegates to QueryWithInput using the streaming control protocol,
// which means hooks, SDK MCP servers, and canUseTool callbacks are all supported.
//
// Because this delegates to QueryWithInput, both string prompts and streaming
// prompts share the same stdin lifecycle: when SDK MCP servers or hooks are
// configured, stdin stays open until the first result arrives. This avoids
// a bug class where closing stdin too early prevents MCP server initialization.
// See upstream Python SDK commit 6119fd4 for the equivalent fix.
func Query(ctx context.Context, prompt string, opts ...Option) <-chan MessageOrError {
	ch := make(chan MessageOrError, 100)

	go func() {
		defer close(ch)

		if prompt == "" {
			ch <- MessageOrError{Err: fmt.Errorf("query requires a non-empty prompt")}
			return
		}

		input := make(chan InputMessage, 1)
		input <- NewUserInput(prompt)
		close(input)

		for msg := range QueryWithInput(ctx, input, opts...) {
			ch <- msg
		}
	}()

	return ch
}

// QuerySync blocks until the query completes and returns all messages.
func QuerySync(ctx context.Context, prompt string, opts ...Option) ([]Message, error) {
	var messages []Message
	var errs []error

	for msg := range Query(ctx, prompt, opts...) {
		if msg.Err != nil {
			errs = append(errs, msg.Err)
			continue
		}
		messages = append(messages, msg.Message)
	}

	return messages, errors.Join(errs...)
}

// QueryWithInput performs a query with a streaming input channel.
// This opens a bidirectional connection that supports the full control protocol,
// enabling multi-turn conversations, hooks, SDK MCP servers, and can_use_tool callbacks.
// Query delegates to this function internally.
func QueryWithInput(ctx context.Context, input <-chan InputMessage, opts ...Option) <-chan MessageOrError {
	ch := make(chan MessageOrError, 100)

	go func() {
		defer close(ch)

		options := applyOptions(opts...)
		options.streamingMode = true

		// Validate can_use_tool requirements
		if options.canUseTool != nil {
			if options.permissionPromptToolName != "" {
				ch <- MessageOrError{Err: fmt.Errorf("can_use_tool callback cannot be used with permission_prompt_tool_name")}
				return
			}
			options.permissionPromptToolName = "stdio"
		}

		transport := NewSubprocessTransport(options)

		if err := transport.Connect(ctx); err != nil {
			ch <- MessageOrError{Err: fmt.Errorf("connect: %w", err)}
			return
		}

		router := NewControlRouter(transport, options)

		// Start message reader
		var wg sync.WaitGroup
		msgCh := transport.ReadMessages(ctx)

		// Track first result for proper stream closure
		firstResultReceived := make(chan struct{})
		var firstResultOnce sync.Once

		// Channel for routed (non-control) messages
		routedCh := make(chan MessageOrError, 100)

		// Signal when reader goroutine ends (for early exit)
		readerDone := make(chan struct{})

		// Start message routing goroutine BEFORE Initialize so control responses can be delivered
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(routedCh)
			defer close(readerDone)

			// Track the last result message we saw so we can surface its
			// error text if the CLI exits before a pending control request
			// gets a response (e.g. a startup error that Claude Code emits
			// as a single `result` message before closing stdout).
			var lastResult *ResultMessage

			// Ensure any in-flight control requests (most importantly the
			// initialize handshake) wake up as soon as the reader drains,
			// instead of hanging until DefaultInitializeTimeout.
			defer func() {
				var abortErr error
				switch {
				case lastResult != nil && lastResult.IsError:
					msg := lastResult.Result
					if msg == "" {
						msg = "unknown error"
					}
					abortErr = fmt.Errorf("cli exited before control response: %s", msg)
				case lastResult != nil:
					abortErr = errors.New("cli exited before control response (terminal result received)")
				default:
					abortErr = errors.New("cli exited before control response")
				}
				router.AbortPending(abortErr)
			}()

			for msg := range msgCh {
				if msg.Err != nil {
					routedCh <- msg
					continue
				}

				// Route control messages
				handled, err := router.HandleMessage(ctx, msg.Message, msg.Raw)
				if err != nil {
					routedCh <- MessageOrError{Err: err}
					continue
				}
				if handled {
					continue
				}

				// Track first result and remember the latest one for
				// AbortPending's error propagation.
				if rm, ok := msg.Message.(*ResultMessage); ok {
					lastResult = rm
					firstResultOnce.Do(func() {
						close(firstResultReceived)
					})
				}

				routedCh <- msg
			}
		}()

		// Initialize the control protocol (now safe - routing goroutine is active)
		if _, err := router.Initialize(ctx); err != nil {
			transport.Close(ctx)
			ch <- MessageOrError{Err: fmt.Errorf("initialize: %w", err)}
			return
		}

		// Start streaming input in background
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				// Wait for first result if we have SDK MCP servers, hooks, or permission callbacks.
				// These all require bidirectional communication so stdin must stay open.
				hasSDKMCP := len(options.sdkMCPServers) > 0
				hasHooks := len(options.hooks) > 0
				hasCanUseTool := options.canUseTool != nil

				if hasSDKMCP || hasHooks || hasCanUseTool {
					// Wait unconditionally for the first result. The control
					// protocol requires stdin to remain open for the entire
					// conversation when hooks or SDK MCP servers are active,
					// so no timeout is applied. The event is guaranteed to
					// fire: either when the result message arrives, or when
					// the reader goroutine exits (e.g. process exit/crash).
					select {
					case <-firstResultReceived:
						// Normal path - result received
					case <-readerDone:
						// Reader ended early (e.g., CLI failure) - don't wait further
					case <-ctx.Done():
						// Context cancelled
					}

					// Wait for in-flight control request handlers to
					// complete before closing stdin. This ensures MCP
					// init responses and hook responses are written
					// while the transport is still accepting writes.
					router.WaitInflight()
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
						ch <- MessageOrError{Err: fmt.Errorf("marshal input message: %w", err)}
						return
					}
					if err := transport.Write(ctx, string(data)+"\n"); err != nil {
						ch <- MessageOrError{Err: fmt.Errorf("write input message: %w", err)}
						return
					}
				}
			}
		}()

		// Forward routed messages to output channel
		for msg := range routedCh {
			ch <- msg
		}

		// Wait for in-flight control request handlers (e.g. hook callbacks)
		// to finish before closing the transport.
		router.WaitInflight()

		// Wait for input streaming to complete
		wg.Wait()
		transport.Close(ctx)
	}()

	return ch
}

// InputMessage represents a message to send to Claude.
type InputMessage struct {
	Type            string       `json:"type"`
	Message         InputContent `json:"message"`
	ParentToolUseID string       `json:"parent_tool_use_id,omitzero"`
	SessionID       string       `json:"session_id,omitzero"`
}

// InputContent represents the content of an input message.
type InputContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NewUserInput creates a new user input message.
func NewUserInput(content string) InputMessage {
	return InputMessage{
		Type: "user",
		Message: InputContent{
			Role:    "user",
			Content: content,
		},
	}
}

// WithSessionID sets the session ID for the input message.
func (m InputMessage) WithSessionID(sessionID string) InputMessage {
	m.SessionID = sessionID
	return m
}

// WithParentToolUseID sets the parent tool use ID for the input message.
func (m InputMessage) WithParentToolUseID(toolUseID string) InputMessage {
	m.ParentToolUseID = toolUseID
	return m
}
