package claudecode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHookOutput_Builders(t *testing.T) {
	t.Run("NewHookOutputContinue", func(t *testing.T) {
		output := NewHookOutputContinue()
		assert.NotNil(t, output.Continue)
		assert.True(t, *output.Continue)
		assert.False(t, output.Async)
		assert.Empty(t, output.Decision)
	})

	t.Run("NewHookOutputBlock", func(t *testing.T) {
		output := NewHookOutputBlock("dangerous operation")
		assert.NotNil(t, output.Continue)
		assert.False(t, *output.Continue)
		assert.Equal(t, "block", output.Decision)
		assert.Equal(t, "dangerous operation", output.Reason)
	})

	t.Run("NewPreToolUseAllow", func(t *testing.T) {
		output := NewPreToolUseAllow()
		assert.NotNil(t, output.Continue)
		assert.True(t, *output.Continue)

		specific, ok := output.HookSpecificOutput.(PreToolUseSpecificOutput)
		assert.True(t, ok)
		assert.Equal(t, "PreToolUse", specific.HookEventName)
		assert.Equal(t, "allow", specific.PermissionDecision)
	})

	t.Run("NewPreToolUseDeny", func(t *testing.T) {
		output := NewPreToolUseDeny("command not allowed")
		assert.NotNil(t, output.Continue)
		assert.True(t, *output.Continue)

		specific, ok := output.HookSpecificOutput.(PreToolUseSpecificOutput)
		assert.True(t, ok)
		assert.Equal(t, "PreToolUse", specific.HookEventName)
		assert.Equal(t, "deny", specific.PermissionDecision)
		assert.Equal(t, "command not allowed", specific.PermissionDecisionReason)
	})

	t.Run("NewPreToolUseModify", func(t *testing.T) {
		modified := map[string]any{"command": "ls -la"}
		output := NewPreToolUseModify(modified)
		assert.NotNil(t, output.Continue)
		assert.True(t, *output.Continue)

		specific, ok := output.HookSpecificOutput.(PreToolUseSpecificOutput)
		assert.True(t, ok)
		assert.Equal(t, "PreToolUse", specific.HookEventName)
		assert.Equal(t, "allow", specific.PermissionDecision)
		assert.Equal(t, modified, specific.UpdatedInput)
	})

	t.Run("NewPostToolUseContext", func(t *testing.T) {
		output := NewPostToolUseContext("Additional context for Claude")
		assert.NotNil(t, output.Continue)
		assert.True(t, *output.Continue)

		specific, ok := output.HookSpecificOutput.(PostToolUseSpecificOutput)
		assert.True(t, ok)
		assert.Equal(t, "PostToolUse", specific.HookEventName)
		assert.Equal(t, "Additional context for Claude", specific.AdditionalContext)
	})
}

func TestParseHookInput(t *testing.T) {
	t.Run("PreToolUse", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "PreToolUse",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"permission_mode": "default",
			"tool_name":       "Bash",
			"tool_input":      map[string]any{"command": "ls"},
		}

		result := parseHookInput(input)
		preToolUse, ok := result.(PreToolUseInput)
		assert.True(t, ok)
		assert.Equal(t, "PreToolUse", preToolUse.HookEventName)
		assert.Equal(t, "sess_123", preToolUse.SessionID)
		assert.Equal(t, "Bash", preToolUse.ToolName)
		assert.Equal(t, "ls", preToolUse.ToolInput["command"])
	})

	t.Run("PostToolUse", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "PostToolUse",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"tool_name":       "Bash",
			"tool_input":      map[string]any{"command": "ls"},
			"tool_response":   "file1.txt\nfile2.txt",
		}

		result := parseHookInput(input)
		postToolUse, ok := result.(PostToolUseInput)
		assert.True(t, ok)
		assert.Equal(t, "PostToolUse", postToolUse.HookEventName)
		assert.Equal(t, "Bash", postToolUse.ToolName)
		assert.Equal(t, "file1.txt\nfile2.txt", postToolUse.ToolResponse)
	})

	t.Run("UserPromptSubmit", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "UserPromptSubmit",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"prompt":          "Help me with this code",
		}

		result := parseHookInput(input)
		promptSubmit, ok := result.(UserPromptSubmitInput)
		assert.True(t, ok)
		assert.Equal(t, "UserPromptSubmit", promptSubmit.HookEventName)
		assert.Equal(t, "Help me with this code", promptSubmit.Prompt)
	})

	t.Run("Stop", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name":  "Stop",
			"session_id":       "sess_123",
			"transcript_path":  "/path/to/transcript",
			"cwd":              "/home/user",
			"stop_hook_active": true,
		}

		result := parseHookInput(input)
		stopInput, ok := result.(StopInput)
		assert.True(t, ok)
		assert.Equal(t, "Stop", stopInput.HookEventName)
		assert.True(t, stopInput.StopHookActive)
	})

	t.Run("SubagentStop", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name":  "SubagentStop",
			"session_id":       "sess_123",
			"transcript_path":  "/path/to/transcript",
			"cwd":              "/home/user",
			"stop_hook_active": false,
		}

		result := parseHookInput(input)
		subagentStop, ok := result.(SubagentStopInput)
		assert.True(t, ok)
		assert.Equal(t, "SubagentStop", subagentStop.HookEventName)
		assert.False(t, subagentStop.StopHookActive)
	})

	t.Run("PreCompact", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name":     "PreCompact",
			"session_id":          "sess_123",
			"transcript_path":     "/path/to/transcript",
			"cwd":                 "/home/user",
			"trigger":             "auto",
			"custom_instructions": "Keep important context",
		}

		result := parseHookInput(input)
		preCompact, ok := result.(PreCompactInput)
		assert.True(t, ok)
		assert.Equal(t, "PreCompact", preCompact.HookEventName)
		assert.Equal(t, "auto", preCompact.Trigger)
		assert.Equal(t, "Keep important context", preCompact.CustomInstructions)
	})

	t.Run("Unknown event", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "Unknown",
			"session_id":      "sess_123",
		}

		result := parseHookInput(input)
		base, ok := result.(BaseHookInput)
		assert.True(t, ok)
		assert.Equal(t, "Unknown", base.HookEventName)
	})
}
