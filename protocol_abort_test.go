package claudecode

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// abortTransport is a minimal Transport stub for exercising ControlRouter's
// AbortPending behavior. Write succeeds silently; ReadMessages returns a
// channel the test controls; Connect/Close/EndInput are no-ops.
type abortTransport struct {
	msgCh chan MessageOrError
}

func (t *abortTransport) Connect(ctx context.Context) error              { return nil }
func (t *abortTransport) Write(ctx context.Context, data string) error   { return nil }
func (t *abortTransport) EndInput(ctx context.Context) error             { return nil }
func (t *abortTransport) Close(ctx context.Context) error                { return nil }
func (t *abortTransport) IsReady() bool                                  { return true }
func (t *abortTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	return t.msgCh
}

// TestAbortPendingWakesSendControlRequest verifies that a sendControlRequest
// call that is blocked waiting for the CLI to respond wakes immediately when
// AbortPending is called, rather than hanging until the full timeout.
//
// This is the regression test for the "control request timeout for initialize"
// bug: previously the SDK would block for DefaultInitializeTimeout (60s)
// whenever the CLI exited before writing a control_response, because
// sendControlRequest only selected on the pending channel, a timer, and ctx.
func TestAbortPendingWakesSendControlRequest(t *testing.T) {
	t.Parallel()

	router := NewControlRouter(&abortTransport{msgCh: make(chan MessageOrError)}, applyOptions())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type result struct {
		resp map[string]any
		err  error
	}
	done := make(chan result, 1)

	go func() {
		resp, err := router.sendControlRequest(ctx, map[string]any{"subtype": "initialize"}, 60*time.Second)
		done <- result{resp: resp, err: err}
	}()

	// Give the goroutine a moment to register its pending request, then
	// simulate the reader goroutine noticing the CLI has exited.
	time.Sleep(20 * time.Millisecond)
	abortErr := errors.New("cli exited before control response: EPERM /tmp/foo")
	router.AbortPending(abortErr)

	select {
	case r := <-done:
		require.Error(t, r.err)
		assert.True(t, errors.Is(r.err, abortErr) || strings.Contains(r.err.Error(), "EPERM"),
			"expected the abort error to be surfaced, got: %v", r.err)
	case <-time.After(2 * time.Second):
		t.Fatal("sendControlRequest did not wake on AbortPending within 2s")
	}
}

// TestAbortPendingAfterClosedReturnsImmediately verifies that once AbortPending
// has been called, subsequent sendControlRequest calls fail fast instead of
// proceeding to write and block on the (now-dead) transport.
func TestAbortPendingAfterClosedReturnsImmediately(t *testing.T) {
	t.Parallel()

	router := NewControlRouter(&abortTransport{msgCh: make(chan MessageOrError)}, applyOptions())

	router.AbortPending(errors.New("reader drained"))

	start := time.Now()
	_, err := router.sendControlRequest(context.Background(), map[string]any{"subtype": "initialize"}, 60*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "router closed")
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}
