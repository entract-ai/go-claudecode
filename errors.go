// Package claudecode provides a Go SDK for interacting with Claude Code CLI.
package claudecode

import (
	"errors"
	"fmt"
)

// Sentinel errors for the claudecode package.
var (
	// ErrCLINotFound indicates the Claude Code CLI binary was not found.
	ErrCLINotFound = errors.New("claude code CLI not found")
	// ErrConnection indicates a connection failure to Claude Code.
	ErrConnection = errors.New("connection to claude code failed")
	// ErrProcess indicates the Claude Code process encountered an error.
	ErrProcess = errors.New("claude code process error")
	// ErrJSONDecode indicates a failure to decode JSON from the CLI.
	ErrJSONDecode = errors.New("failed to decode JSON from CLI")
	// ErrProtocol indicates a control protocol error.
	ErrProtocol = errors.New("control protocol error")
	// ErrTimeout indicates an operation timed out.
	ErrTimeout = errors.New("operation timed out")
	// ErrNotConnected indicates the client is not connected.
	ErrNotConnected = errors.New("client not connected")
	// ErrStreamingRequired indicates an operation requires streaming mode.
	ErrStreamingRequired = errors.New("operation requires streaming mode")
	// ErrClientClosed indicates the client has been closed and cannot be reused.
	ErrClientClosed = errors.New("client has been closed and cannot be reused")
	// ErrUnknownMessageType indicates an unrecognized message type in a transcript.
	//
	// Deprecated: Unknown message types are now silently skipped for forward
	// compatibility. This sentinel is retained for backward compatibility but
	// is no longer returned by any function in this package.
	ErrUnknownMessageType = errors.New("unknown message type")
)

// ProcessError represents an error from the Claude Code CLI process.
type ProcessError struct {
	ExitCode int
	Stderr   string
}

// Error implements the error interface.
func (e *ProcessError) Error() string {
	msg := fmt.Sprintf("claude code process error (exit code: %d)", e.ExitCode)
	if e.Stderr != "" {
		msg += fmt.Sprintf("\nstderr: %s", e.Stderr)
	}
	return msg
}

// Unwrap returns the underlying error.
func (e *ProcessError) Unwrap() error {
	return ErrProcess
}

// JSONDecodeError represents a JSON decoding error from CLI output.
type JSONDecodeError struct {
	Line          string
	OriginalError error
}

// Error implements the error interface.
func (e *JSONDecodeError) Error() string {
	truncated := e.Line
	if len(truncated) > 100 {
		truncated = truncated[:100] + "..."
	}
	return fmt.Sprintf("failed to decode JSON: %s (original: %v)", truncated, e.OriginalError)
}

// Unwrap returns the underlying error.
func (e *JSONDecodeError) Unwrap() error {
	return ErrJSONDecode
}

// MessageParseError represents an error parsing a message from CLI output.
type MessageParseError struct {
	Message string
	Data    map[string]any
}

// Error implements the error interface.
func (e *MessageParseError) Error() string {
	return e.Message
}
