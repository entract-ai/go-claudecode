package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Client provides bidirectional, interactive conversations with Claude Code.
type Client struct {
	opts *Options

	mu        sync.Mutex
	transport Transport
	router    *ControlRouter
	msgCh     <-chan MessageOrError // raw messages from transport
	routedCh  chan MessageOrError   // buffered channel for non-control messages
	connected bool
	closed    bool
	sessionID string

	// For input streaming errors
	streamErr   error
	streamErrMu sync.Mutex
}

// NewClient creates a new client with the given options.
func NewClient(opts ...Option) *Client {
	return &Client{
		opts: applyOptions(opts...),
	}
}

// NewClientWithTransport creates a client with a custom transport.
func NewClientWithTransport(transport Transport, opts ...Option) *Client {
	c := NewClient(opts...)
	c.transport = transport
	return c
}

// Connect starts the CLI and prepares for interactive communication.
// This opens a connection without sending an initial prompt.
// Use Query or QueryStream to send messages after connecting.
func (c *Client) Connect(ctx context.Context) error {
	return c.connectInternal(ctx, nil)
}

// ConnectWithPrompt starts the CLI and sends an initial string prompt.
// Use ReceiveResponse to get the response messages.
func (c *Client) ConnectWithPrompt(ctx context.Context, prompt string) error {
	return c.connectInternal(ctx, prompt)
}

// ConnectWithStream starts the CLI with a streaming input channel.
// Messages from the input channel are forwarded to the CLI.
func (c *Client) ConnectWithStream(ctx context.Context, input <-chan InputMessage) error {
	return c.connectInternal(ctx, input)
}

// connectInternal implements the connection logic.
// prompt can be nil, string, or <-chan InputMessage.
func (c *Client) connectInternal(ctx context.Context, prompt any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ErrClientClosed
	}

	if c.connected {
		return nil
	}

	c.opts.streamingMode = true

	// Validate can_use_tool requirements
	if c.opts.canUseTool != nil {
		if c.opts.permissionPromptToolName != "" {
			return fmt.Errorf("can_use_tool callback cannot be used with permission_prompt_tool_name")
		}
		c.opts.permissionPromptToolName = "stdio"
	}

	// Create transport if not provided
	if c.transport == nil {
		c.transport = NewSubprocessTransport(c.opts)
	}

	if err := c.transport.Connect(ctx); err != nil {
		return fmt.Errorf("connect transport: %w", err)
	}

	// Create control router
	c.router = NewControlRouter(c.transport, c.opts)

	// Start reading messages from transport
	c.msgCh = c.transport.ReadMessages(ctx)

	// Create buffered channel for non-control messages
	c.routedCh = make(chan MessageOrError, 100)

	// Start message routing goroutine BEFORE Initialize so control responses can be delivered
	go c.routeMessages(ctx)

	// Initialize the control protocol
	if _, err := c.router.Initialize(ctx); err != nil {
		c.transport.Close(ctx)
		return fmt.Errorf("initialize: %w", err)
	}

	c.connected = true

	// Handle initial prompt if provided
	if prompt != nil {
		switch p := prompt.(type) {
		case string:
			// Send string prompt
			return c.sendQuery(ctx, p, "default")
		case <-chan InputMessage:
			// Start streaming input in background
			go c.streamInput(ctx, p)
		}
	}

	return nil
}

func (c *Client) streamInput(ctx context.Context, ch <-chan InputMessage) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				c.setStreamError(fmt.Errorf("marshal input message: %w", err))
				return
			}
			if err := c.transport.Write(ctx, string(data)+"\n"); err != nil {
				c.setStreamError(fmt.Errorf("write input message: %w", err))
				return
			}
		}
	}
}

// routeMessages reads from transport and routes control messages to the router.
// Non-control messages are forwarded to routedCh for ReceiveMessages to consume.
// This runs in a goroutine started BEFORE Initialize so control responses can be delivered.
func (c *Client) routeMessages(ctx context.Context) {
	defer close(c.routedCh)

	for {
		select {
		case <-ctx.Done():
			c.routedCh <- MessageOrError{Err: ctx.Err()}
			return
		case msg, ok := <-c.msgCh:
			if !ok {
				return
			}

			if msg.Err != nil {
				c.routedCh <- msg
				continue
			}

			// Route control messages to the router
			handled, err := c.router.HandleMessage(ctx, msg.Message, msg.Raw)
			if err != nil {
				c.routedCh <- MessageOrError{Err: err}
				continue
			}
			if handled {
				continue
			}

			// Capture SessionID from ResultMessage
			if result, ok := msg.Message.(*ResultMessage); ok {
				c.setSessionID(result.SessionID)
			}

			// Forward non-control messages
			c.routedCh <- msg
		}
	}
}

func (c *Client) isConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *Client) setSessionID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
}

func (c *Client) setStreamError(err error) {
	c.streamErrMu.Lock()
	defer c.streamErrMu.Unlock()
	if c.streamErr == nil {
		c.streamErr = err
	}
}

// StreamError returns any error that occurred during input streaming.
// Returns nil if no error has occurred.
func (c *Client) StreamError() error {
	c.streamErrMu.Lock()
	defer c.streamErrMu.Unlock()
	return c.streamErr
}

// Query sends a string prompt to Claude.
// sessionID identifies the conversation session (use "default" for the default session).
func (c *Client) Query(ctx context.Context, prompt, sessionID string) error {
	if !c.isConnected() {
		return ErrNotConnected
	}
	return c.sendQuery(ctx, prompt, sessionID)
}

// QueryStream sends messages from a channel to Claude.
// sessionID identifies the conversation session (use "default" for the default session).
func (c *Client) QueryStream(ctx context.Context, input <-chan InputMessage, sessionID string) error {
	if !c.isConnected() {
		return ErrNotConnected
	}
	for msg := range input {
		if msg.SessionID == "" {
			msg.SessionID = sessionID
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal message: %w", err)
		}
		if err := c.transport.Write(ctx, string(data)+"\n"); err != nil {
			return fmt.Errorf("write message: %w", err)
		}
	}
	return nil
}

func (c *Client) sendQuery(ctx context.Context, prompt string, sessionID string) error {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
		"parent_tool_use_id": nil,
		"session_id":         sessionID,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal query: %w", err)
	}

	return c.transport.Write(ctx, string(data)+"\n")
}

// ReceiveMessages returns all messages from the CLI.
// Control messages are already routed by the internal routeMessages goroutine.
func (c *Client) ReceiveMessages(ctx context.Context) <-chan MessageOrError {
	ch := make(chan MessageOrError, 100)

	go func() {
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				ch <- MessageOrError{Err: ctx.Err()}
				return
			case msg, ok := <-c.routedCh:
				if !ok {
					return
				}
				ch <- msg
			}
		}
	}()

	return ch
}

// ReceiveResponse returns messages until and including a ResultMessage.
// Unlike ReceiveMessages, this reads directly from the internal channel
// to avoid leaking goroutines when stopping early at ResultMessage.
func (c *Client) ReceiveResponse(ctx context.Context) <-chan MessageOrError {
	ch := make(chan MessageOrError, 100)

	go func() {
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				ch <- MessageOrError{Err: ctx.Err()}
				// Drain remaining messages from routedCh to prevent routeMessages
				// from blocking on send. Use a timeout to avoid hanging forever
				// if messages keep arriving.
				go c.drainRoutedCh()
				return
			case msg, ok := <-c.routedCh:
				if !ok {
					return
				}
				ch <- msg
				if msg.Err != nil {
					continue
				}
				if _, ok := msg.Message.(*ResultMessage); ok {
					return
				}
			}
		}
	}()

	return ch
}

// drainRoutedCh drains remaining messages from routedCh to prevent
// routeMessages from blocking. This runs when ReceiveResponse times out.
func (c *Client) drainRoutedCh() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-c.routedCh:
			if !ok {
				return
			}
			// Discard the message
		}
	}
}

// getRouterIfConnected returns the router if connected, or ErrNotConnected.
func (c *Client) getRouterIfConnected() (*ControlRouter, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		return nil, ErrNotConnected
	}
	return c.router, nil
}

// Interrupt sends an interrupt signal.
func (c *Client) Interrupt(ctx context.Context) error {
	router, err := c.getRouterIfConnected()
	if err != nil {
		return err
	}
	return router.Interrupt(ctx)
}

// SetPermissionMode changes the permission mode.
func (c *Client) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	router, err := c.getRouterIfConnected()
	if err != nil {
		return err
	}
	return router.SetPermissionMode(ctx, mode)
}

// SetModel changes the AI model.
func (c *Client) SetModel(ctx context.Context, model string) error {
	router, err := c.getRouterIfConnected()
	if err != nil {
		return err
	}
	return router.SetModel(ctx, model)
}

// GetMCPStatus returns the MCP server connection status.
func (c *Client) GetMCPStatus(ctx context.Context) (map[string]any, error) {
	router, err := c.getRouterIfConnected()
	if err != nil {
		return nil, err
	}
	return router.GetMCPStatus(ctx)
}

// GetServerInfo returns the cached initialization result.
func (c *Client) GetServerInfo() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.router == nil {
		return nil
	}
	return c.router.GetServerInfo()
}

// RewindFiles rewinds tracked files to a specific user message.
func (c *Client) RewindFiles(ctx context.Context, userMessageUUID string) error {
	router, err := c.getRouterIfConnected()
	if err != nil {
		return err
	}
	return router.RewindFiles(ctx, userMessageUUID)
}

// SessionID returns the current session ID.
func (c *Client) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

// Close closes the client and releases resources.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	c.connected = false

	if c.transport != nil {
		return c.transport.Close(ctx)
	}

	return nil
}
