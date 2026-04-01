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

func TestConnectWithPrompt_WritesUserMessageToTransport(t *testing.T) {
	// Verify that ConnectWithPrompt sends the string prompt as a JSON user
	// message to the transport. This is the Go equivalent of Python SDK bug
	// #766 where connect(prompt="...") stored the string but never sent it,
	// causing receive_messages() to hang. The Go SDK handles this correctly
	// via sendQuery in the string case of connectInternal's type switch.

	messages := []MessageOrError{
		{Message: &ResultMessage{SessionID: "test-session"}},
	}
	transport := newControlAwareMockTransport(messages, 0)
	client := NewClientWithTransport(transport)
	ctx := context.Background()

	err := client.ConnectWithPrompt(ctx, "Hello Claude")
	require.NoError(t, err)

	// The transport's writeCh should contain the prompt message.
	// The init request was consumed by ReadMessages; the prompt write
	// remains in the buffered channel.
	select {
	case data := <-transport.writeCh:
		var msg struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			SessionID string `json:"session_id"`
		}
		err := json.Unmarshal([]byte(data), &msg)
		require.NoError(t, err, "prompt should be valid JSON")
		assert.Equal(t, "user", msg.Type)
		assert.Equal(t, "user", msg.Message.Role)
		assert.Equal(t, "Hello Claude", msg.Message.Content)
		assert.Equal(t, "default", msg.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for prompt write -- string prompt was not sent to transport")
	}

	// Clean up
	err = client.Close(ctx)
	require.NoError(t, err)
}

func TestClient_ClosedCannotReconnect(t *testing.T) {
	client := NewClient()

	// Create a mock transport that doesn't require a real CLI
	mockTransport := &mockTransport{}
	client.transport = mockTransport

	ctx := context.Background()

	// Close without connecting (simulates a closed client)
	err := client.Close(ctx)
	require.NoError(t, err)

	// Try to connect - should fail because client is closed
	err = client.Connect(ctx)
	assert.ErrorIs(t, err, ErrClientClosed)
}

// mockTransport is a minimal transport for testing
type mockTransport struct {
	connected bool
	closed    bool
}

func (m *mockTransport) Connect(ctx context.Context) error {
	m.connected = true
	return nil
}

func (m *mockTransport) Write(ctx context.Context, data string) error {
	return nil
}

func (m *mockTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	ch := make(chan MessageOrError)
	close(ch)
	return ch
}

func (m *mockTransport) EndInput(ctx context.Context) error {
	return nil
}

func (m *mockTransport) Close(ctx context.Context) error {
	m.closed = true
	return nil
}

func (m *mockTransport) IsReady() bool {
	return m.connected && !m.closed
}

// controlAwareMockTransport handles the control protocol initialization
// and then sends test messages. Used to test timeout/drain behavior.
type controlAwareMockTransport struct {
	mu        sync.Mutex
	connected bool
	closed    bool

	// Messages to send after initialization
	messages []MessageOrError
	// Delay between each message
	messageDelay time.Duration
	// Channel used for writes (to detect control requests)
	writeCh chan string
	// Channel to signal close
	closeCh chan struct{}
}

func newControlAwareMockTransport(messages []MessageOrError, delay time.Duration) *controlAwareMockTransport {
	return &controlAwareMockTransport{
		messages:     messages,
		messageDelay: delay,
		writeCh:      make(chan string, 100),
		closeCh:      make(chan struct{}),
	}
}

func (m *controlAwareMockTransport) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *controlAwareMockTransport) Write(ctx context.Context, data string) error {
	select {
	case m.writeCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *controlAwareMockTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	ch := make(chan MessageOrError)

	go func() {
		defer close(ch)

		// Wait for the initialize request and respond
		select {
		case <-ctx.Done():
			return
		case data := <-m.writeCh:
			// Parse to extract request_id for the response
			if len(data) > 0 {
				var req struct {
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal([]byte(data), &req); err == nil && req.RequestID != "" {
					// Build raw JSON for the control response
					responseJSON := []byte(`{"type":"control_response","response":{"subtype":"init_response","request_id":"` +
						req.RequestID + `","response":{"capabilities":{}}}}`)
					initResponse := &ControlResponse{
						Type:     "control_response",
						Response: responseJSON,
					}
					select {
					case ch <- MessageOrError{Message: initResponse, Raw: responseJSON}:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		// Now send the test messages
		for _, msg := range m.messages {
			if m.messageDelay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-m.closeCh:
					return
				case <-time.After(m.messageDelay):
				}
			}

			m.mu.Lock()
			closed := m.closed
			m.mu.Unlock()
			if closed {
				return
			}

			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			case <-m.closeCh:
				return
			}
		}
	}()

	return ch
}

func (m *controlAwareMockTransport) EndInput(ctx context.Context) error {
	return nil
}

func (m *controlAwareMockTransport) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.closeCh)
	}
	return nil
}

func (m *controlAwareMockTransport) IsReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func TestReceiveResponse_DrainOnTimeout(t *testing.T) {
	// Create a transport that sends many messages slowly without a ResultMessage
	messages := []MessageOrError{
		{Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "msg1"}}}},
		{Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "msg2"}}}},
		{Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "msg3"}}}},
		{Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "msg4"}}}},
		{Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "msg5"}}}},
		// No ResultMessage - ReceiveResponse will timeout waiting
	}

	transport := newControlAwareMockTransport(messages, 100*time.Millisecond)
	client := NewClientWithTransport(transport)
	ctx := context.Background()

	err := client.Connect(ctx)
	require.NoError(t, err)

	// Very short timeout - will exit before receiving all messages
	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// Consume response (will timeout)
	for range client.ReceiveResponse(timeoutCtx) {
		// Drain the channel
	}

	// The critical assertion: client should still be closeable without deadlock.
	// If drain doesn't work, routeMessages goroutine is blocked on routedCh send,
	// preventing the transport from closing cleanly.
	done := make(chan bool, 1)
	go func() {
		client.Close(context.Background())
		done <- true
	}()

	select {
	case <-done:
		// Success - client closed without deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("Client.Close deadlocked - routeMessages likely blocked on routedCh send")
	}
}

// blockingMockTransport is a mock that blocks forever on sends to simulate
// a transport that continues sending after context cancellation.
type blockingMockTransport struct {
	mu        sync.Mutex
	connected bool
	closed    bool

	writeCh chan string
	// msgCh will not close until transport is closed, simulating a never-ending stream
	msgCh chan MessageOrError
}

func newBlockingMockTransport() *blockingMockTransport {
	return &blockingMockTransport{
		writeCh: make(chan string, 100),
		msgCh:   make(chan MessageOrError),
	}
}

func (m *blockingMockTransport) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *blockingMockTransport) Write(ctx context.Context, data string) error {
	select {
	case m.writeCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *blockingMockTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	outCh := make(chan MessageOrError)

	go func() {
		defer close(outCh)

		// Wait for the initialize request and respond
		select {
		case <-ctx.Done():
			return
		case data := <-m.writeCh:
			if len(data) > 0 {
				var req struct {
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal([]byte(data), &req); err == nil && req.RequestID != "" {
					responseJSON := []byte(`{"type":"control_response","response":{"subtype":"init_response","request_id":"` +
						req.RequestID + `","response":{"capabilities":{}}}}`)
					initResponse := &ControlResponse{
						Type:     "control_response",
						Response: responseJSON,
					}
					select {
					case outCh <- MessageOrError{Message: initResponse, Raw: responseJSON}:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		// Now continuously send messages until closed
		for {
			m.mu.Lock()
			closed := m.closed
			m.mu.Unlock()
			if closed {
				return
			}

			msg := MessageOrError{
				Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "continuous msg"}}},
			}

			select {
			case outCh <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	return outCh
}

func (m *blockingMockTransport) EndInput(ctx context.Context) error {
	return nil
}

func (m *blockingMockTransport) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *blockingMockTransport) IsReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func TestReceiveResponse_DrainOnTimeoutWithContinuousStream(t *testing.T) {
	// This test simulates a more realistic scenario where messages keep coming
	// even after we've given up waiting (context timeout).
	transport := newBlockingMockTransport()
	client := NewClientWithTransport(transport)
	ctx := context.Background()

	err := client.Connect(ctx)
	require.NoError(t, err)

	// Short timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// Consume some responses until timeout
	count := 0
	for range client.ReceiveResponse(timeoutCtx) {
		count++
		if count > 5 {
			// Don't process forever in case drain doesn't work
			break
		}
	}

	// Client should be closeable without deadlock even though transport is still
	// trying to send messages
	done := make(chan bool, 1)
	go func() {
		client.Close(context.Background())
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Client.Close deadlocked - drain mechanism may not be working")
	}
}

// floodingMockTransport sends many messages very quickly to fill up the routedCh buffer.
// It ignores context cancellation for its message loop, simulating a real CLI that
// might continue sending data even after we stop reading.
type floodingMockTransport struct {
	mu        sync.Mutex
	connected bool
	closed    bool
	closeCh   chan struct{}

	writeCh chan string
}

func newFloodingMockTransport() *floodingMockTransport {
	return &floodingMockTransport{
		writeCh: make(chan string, 100),
		closeCh: make(chan struct{}),
	}
}

func (m *floodingMockTransport) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *floodingMockTransport) Write(ctx context.Context, data string) error {
	select {
	case m.writeCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *floodingMockTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	outCh := make(chan MessageOrError)

	go func() {
		defer close(outCh)

		// Wait for the initialize request and respond
		select {
		case <-m.closeCh:
			return
		case data := <-m.writeCh:
			if len(data) > 0 {
				var req struct {
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal([]byte(data), &req); err == nil && req.RequestID != "" {
					responseJSON := []byte(`{"type":"control_response","response":{"subtype":"init_response","request_id":"` +
						req.RequestID + `","response":{"capabilities":{}}}}`)
					initResponse := &ControlResponse{
						Type:     "control_response",
						Response: responseJSON,
					}
					select {
					case outCh <- MessageOrError{Message: initResponse, Raw: responseJSON}:
					case <-m.closeCh:
						return
					}
				}
			}
		}

		// Now flood the channel with many messages as fast as possible.
		// This simulates what happens when the CLI keeps sending data.
		// IMPORTANT: We do NOT respect ctx.Done() here - we only stop on close.
		for i := 0; i < 200; i++ { // More than the 100-message buffer
			m.mu.Lock()
			closed := m.closed
			m.mu.Unlock()
			if closed {
				return
			}

			msg := MessageOrError{
				Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "flood msg"}}},
			}

			select {
			case outCh <- msg:
			case <-m.closeCh:
				return
			}
		}
	}()

	return outCh
}

func (m *floodingMockTransport) EndInput(ctx context.Context) error {
	return nil
}

func (m *floodingMockTransport) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.closeCh)
	}
	return nil
}

func (m *floodingMockTransport) IsReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func TestReceiveResponse_BufferOverflowOnTimeout(t *testing.T) {
	// This test creates a scenario where:
	// 1. Client connects and starts receiving
	// 2. Caller reads a few messages then breaks out of the loop (simulating early exit)
	// 3. Transport continues flooding messages (>100 to exceed buffer)
	// 4. Without drain, routeMessages blocks on routedCh send
	// 5. Close would deadlock because routeMessages can't process the close signal
	transport := newFloodingMockTransport()
	client := NewClientWithTransport(transport)
	ctx := context.Background()

	err := client.Connect(ctx)
	require.NoError(t, err)

	// Use a long timeout - we'll break out early manually
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Consume only a few messages, then STOP READING (break out early)
	// This simulates what happens when an HTTP handler encounters an error
	// and breaks out of the loop before draining the channel
	respCh := client.ReceiveResponse(timeoutCtx)
	count := 0
	for msg := range respCh {
		count++
		if msg.Err != nil {
			break
		}
		if count >= 3 {
			// Simulate breaking out early (like on an error or HTTP client disconnect)
			break
		}
	}
	t.Logf("Received %d messages before breaking out", count)

	// Give the flooding transport time to try filling the buffer
	// The ReceiveResponse goroutine is still running and will receive messages
	// from routedCh, but we've stopped reading from respCh
	time.Sleep(100 * time.Millisecond)

	// Client should be closeable without deadlock
	done := make(chan bool, 1)
	go func() {
		client.Close(context.Background())
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Client.Close deadlocked - routeMessages blocked on full routedCh buffer")
	}
}

// slowFloodingMockTransport is like floodingMockTransport but sends messages slowly,
// allowing the context to cancel while messages are still pending.
type slowFloodingMockTransport struct {
	mu        sync.Mutex
	connected bool
	closed    bool
	closeCh   chan struct{}

	writeCh      chan string
	messageDelay time.Duration
}

func newSlowFloodingMockTransport(delay time.Duration) *slowFloodingMockTransport {
	return &slowFloodingMockTransport{
		writeCh:      make(chan string, 100),
		closeCh:      make(chan struct{}),
		messageDelay: delay,
	}
}

func (m *slowFloodingMockTransport) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *slowFloodingMockTransport) Write(ctx context.Context, data string) error {
	select {
	case m.writeCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *slowFloodingMockTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	outCh := make(chan MessageOrError)

	go func() {
		defer close(outCh)

		// Wait for the initialize request and respond
		select {
		case <-m.closeCh:
			return
		case data := <-m.writeCh:
			if len(data) > 0 {
				var req struct {
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal([]byte(data), &req); err == nil && req.RequestID != "" {
					responseJSON := []byte(`{"type":"control_response","response":{"subtype":"init_response","request_id":"` +
						req.RequestID + `","response":{"capabilities":{}}}}`)
					initResponse := &ControlResponse{
						Type:     "control_response",
						Response: responseJSON,
					}
					select {
					case outCh <- MessageOrError{Message: initResponse, Raw: responseJSON}:
					case <-m.closeCh:
						return
					}
				}
			}
		}

		// Send 200 messages slowly - enough to exceed the 100-message buffer
		// if ReceiveResponse stops draining routedCh
		for i := 0; i < 200; i++ {
			select {
			case <-m.closeCh:
				return
			case <-time.After(m.messageDelay):
			}

			m.mu.Lock()
			closed := m.closed
			m.mu.Unlock()
			if closed {
				return
			}

			msg := MessageOrError{
				Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "slow flood msg"}}},
			}

			select {
			case outCh <- msg:
			case <-m.closeCh:
				return
			}
		}
	}()

	return outCh
}

func (m *slowFloodingMockTransport) EndInput(ctx context.Context) error {
	return nil
}

func (m *slowFloodingMockTransport) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.closeCh)
	}
	return nil
}

func (m *slowFloodingMockTransport) IsReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func TestReceiveResponse_ContextCancelWhileMessagesFlowing(t *testing.T) {
	// This test simulates:
	// 1. Client connects
	// 2. Messages flow slowly from transport
	// 3. Context is cancelled while messages are still flowing
	// 4. ReceiveResponse goroutine exits, but routeMessages keeps trying to send
	// 5. If routedCh fills up (100 msgs), routeMessages blocks
	// 6. Close needs to work despite this

	transport := newSlowFloodingMockTransport(5 * time.Millisecond) // 200 msgs * 5ms = 1s total
	client := NewClientWithTransport(transport)
	ctx := context.Background()

	err := client.Connect(ctx)
	require.NoError(t, err)

	// Short timeout that will fire while messages are still being sent
	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// Start receiving - the goroutine will exit when timeout fires
	respCh := client.ReceiveResponse(timeoutCtx)

	// Read messages until timeout
	count := 0
	for msg := range respCh {
		if msg.Err != nil {
			t.Logf("Got error after %d messages: %v", count, msg.Err)
			break
		}
		count++
	}
	t.Logf("Received %d messages before context cancelled", count)

	// At this point:
	// - ReceiveResponse goroutine has exited (ctx cancelled)
	// - Transport is still sending messages
	// - routeMessages is still running and trying to send to routedCh
	// - If we wait long enough, routedCh will fill up

	// Wait for messages to accumulate (but not forever)
	time.Sleep(200 * time.Millisecond)

	// Client should be closeable without deadlock
	done := make(chan bool, 1)
	go func() {
		client.Close(context.Background())
		done <- true
	}()

	select {
	case <-done:
		// Success - client closed without deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("Client.Close deadlocked - routeMessages likely blocked on routedCh send")
	}
}

// blockingOnCloseTransport simulates a transport where messages continue
// flowing even after Close is called, blocking the routedCh buffer.
type blockingOnCloseTransport struct {
	mu        sync.Mutex
	connected bool
	closed    bool

	writeCh chan string
	// msgCh sends messages continuously
	msgCh chan MessageOrError
	// closeStartedCh signals when Close has been called
	closeStartedCh chan struct{}
	// closeCompleteCh signals when Close should return
	closeCompleteCh chan struct{}
}

func newBlockingOnCloseTransport() *blockingOnCloseTransport {
	return &blockingOnCloseTransport{
		writeCh:         make(chan string, 100),
		msgCh:           make(chan MessageOrError, 300), // Large buffer to fill routedCh
		closeStartedCh:  make(chan struct{}),
		closeCompleteCh: make(chan struct{}),
	}
}

func (m *blockingOnCloseTransport) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *blockingOnCloseTransport) Write(ctx context.Context, data string) error {
	select {
	case m.writeCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *blockingOnCloseTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	outCh := make(chan MessageOrError)

	go func() {
		defer close(outCh)

		// Wait for the initialize request and respond
		select {
		case <-ctx.Done():
			return
		case data := <-m.writeCh:
			if len(data) > 0 {
				var req struct {
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal([]byte(data), &req); err == nil && req.RequestID != "" {
					responseJSON := []byte(`{"type":"control_response","response":{"subtype":"init_response","request_id":"` +
						req.RequestID + `","response":{"capabilities":{}}}}`)
					initResponse := &ControlResponse{
						Type:     "control_response",
						Response: responseJSON,
					}
					select {
					case outCh <- MessageOrError{Message: initResponse, Raw: responseJSON}:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		// Pre-fill msgCh with messages
		for i := 0; i < 200; i++ {
			m.msgCh <- MessageOrError{
				Message: &AssistantMessage{Content: []ContentBlock{TextBlock{Text: "prefilled msg"}}},
			}
		}

		// Forward messages from msgCh to outCh
		// This will block when outCh backs up (because routedCh is full)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-m.msgCh:
				if !ok {
					return
				}
				select {
				case outCh <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return outCh
}

func (m *blockingOnCloseTransport) EndInput(ctx context.Context) error {
	return nil
}

func (m *blockingOnCloseTransport) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	// Signal that Close was called
	close(m.closeStartedCh)

	// Wait for test to signal we can return
	// This simulates a transport that takes time to close (like a subprocess)
	<-m.closeCompleteCh
	close(m.msgCh)
	return nil
}

func (m *blockingOnCloseTransport) IsReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func TestReceiveResponse_RouteMessagesBlockedOnSend(t *testing.T) {
	// This test demonstrates a potential deadlock scenario:
	// 1. ReceiveResponse's context times out, its goroutine exits
	// 2. Transport keeps sending messages
	// 3. routedCh buffer (100 msgs) fills up
	// 4. routeMessages blocks on `c.routedCh <- msg` (line 174)
	// 5. Close() is called, but routeMessages can't exit because it's blocked
	//
	// The key insight is that the send at line 174 is INSIDE the select case,
	// so once we're there, we can't check ctx.Done() or see msgCh close
	// until the send completes.
	//
	// However, Close() eventually closes the transport which closes msgCh.
	// Since routeMessages is blocked on send (not receive), msgCh closure
	// won't immediately unblock it. The transport needs to stop sending
	// to let routeMessages loop back and see the closure.

	transport := newBlockingOnCloseTransport()
	client := NewClientWithTransport(transport)
	ctx := context.Background()

	err := client.Connect(ctx)
	require.NoError(t, err)

	// Very short timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	// Start receiving - will quickly timeout
	respCh := client.ReceiveResponse(timeoutCtx)

	// Consume just a few messages, then the channel will close
	count := 0
	for range respCh {
		count++
	}
	t.Logf("Received %d messages before timeout", count)

	// At this point, ReceiveResponse's goroutine has exited.
	// The transport has 200 messages pre-queued.
	// routeMessages will try to forward them all to routedCh.
	// After 100 messages, routedCh is full and routeMessages blocks.

	// Wait to ensure routeMessages is blocked
	time.Sleep(50 * time.Millisecond)

	// Now try to close - this needs to unblock routeMessages
	done := make(chan bool, 1)
	go func() {
		// Signal that we can complete Close
		go func() {
			<-transport.closeStartedCh
			time.Sleep(10 * time.Millisecond)
			close(transport.closeCompleteCh)
		}()
		client.Close(context.Background())
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Fatal("Client.Close deadlocked - routeMessages blocked on full routedCh")
	}
}
