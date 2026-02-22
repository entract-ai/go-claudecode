package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bpowers/go-claudecode/chat"
)

// ClaudeCodeProjectPath computes the path to Claude Code's project directory
// for a given working directory. Claude Code escapes paths by replacing "/" with "-".
// For example, "/Users/bob/myproject" becomes "~/.claude/projects/-Users-bob-myproject".
func ClaudeCodeProjectPath(projectDir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	// Ensure absolute path
	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("get absolute path: %w", err)
	}

	// Claude Code escapes the project path by replacing "/" with "-"
	escapedPath := strings.ReplaceAll(absPath, "/", "-")

	return filepath.Join(home, ".claude", "projects", escapedPath), nil
}

// TranscriptPath returns the path to a Claude Code transcript file
// for a given project directory and session ID.
func TranscriptPath(projectDir, sessionID string) (string, error) {
	projectPath, err := ClaudeCodeProjectPath(projectDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(projectPath, sessionID+".jsonl"), nil
}

// GetSessionMessagesFromTranscript reads a Claude Code transcript and converts
// it to chat.Message format. This reads directly from Claude Code's storage,
// making Claude Code the source of truth for conversation history.
func GetSessionMessagesFromTranscript(projectDir, sessionID string) ([]chat.Message, error) {
	transcriptPath, err := TranscriptPath(projectDir, sessionID)
	if err != nil {
		return nil, fmt.Errorf("compute transcript path: %w", err)
	}

	// Check if transcript exists
	if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
		// No transcript yet - return empty messages (new session)
		return nil, nil
	}

	// Parse the transcript
	messages, err := ParseTranscript(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("parse transcript: %w", err)
	}

	// Convert to chat.Message format
	chatMessages, err := ToChatMessages(messages)
	if err != nil {
		return nil, fmt.Errorf("convert to chat messages: %w", err)
	}

	return chatMessages, nil
}
