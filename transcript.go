package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ParseTranscript reads a JSONL transcript file and returns parsed messages.
// Empty lines are skipped. Returns an error with line number for malformed JSON.
func ParseTranscript(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript file: %w", err)
	}
	defer f.Close()

	var messages []Message
	scanner := bufio.NewScanner(f)
	// Increase buffer size to handle large JSONL lines (e.g., file reads, Display outputs)
	const maxScannerBufferSize = 4 * 1024 * 1024 // 4MB
	scanner.Buffer(make([]byte, 64*1024), maxScannerBufferSize)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		msg, err := parseMessage(json.RawMessage(line))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		// Skip unknown message types (parseMessage returns nil, nil).
		// Transcripts often contain internal/diagnostic messages like
		// queue-operation, saved_hook_context, rate_limit_event, etc.
		if msg == nil {
			continue
		}
		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading transcript: %w", err)
	}

	return messages, nil
}
