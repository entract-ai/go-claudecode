package claudecode

import (
	"context"
)

// Transport is the low-level I/O interface for Claude communication.
// Exposed for custom transport implementations (e.g., remote connections).
type Transport interface {
	// Connect establishes the connection to Claude Code.
	Connect(ctx context.Context) error

	// Write sends data to the transport.
	Write(ctx context.Context, data string) error

	// ReadMessages returns a channel that yields messages from the transport.
	// The channel is closed when the transport is closed or an error occurs.
	ReadMessages(ctx context.Context) <-chan MessageOrError

	// EndInput signals the end of input (closes stdin for process transports).
	EndInput(ctx context.Context) error

	// Close closes the transport and releases resources.
	Close(ctx context.Context) error

	// IsReady returns true if the transport is ready for communication.
	IsReady() bool
}

// MessageOrError represents either a message or an error from the transport.
type MessageOrError struct {
	Message Message
	Raw     []byte
	Err     error
}
