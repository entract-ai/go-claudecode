package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestHookSpecificOutput_Fields(t *testing.T) {
	t.Run("PreToolUseSpecificOutput has AdditionalContext", func(t *testing.T) {
		output := PreToolUseSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
			AdditionalContext:  "extra info",
		}
		assert.Equal(t, "extra info", output.AdditionalContext)
	})

	t.Run("PostToolUseSpecificOutput has UpdatedMCPToolOutput", func(t *testing.T) {
		output := PostToolUseSpecificOutput{
			HookEventName:        "PostToolUse",
			AdditionalContext:    "context",
			UpdatedMCPToolOutput: map[string]any{"modified": true},
		}
		assert.Equal(t, "context", output.AdditionalContext)
		assert.Equal(t, map[string]any{"modified": true}, output.UpdatedMCPToolOutput)
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
			"tool_use_id":     "tu_123",
		}

		result := parseHookInput(input)
		preToolUse, ok := result.(PreToolUseInput)
		assert.True(t, ok)
		assert.Equal(t, "PreToolUse", preToolUse.HookEventName)
		assert.Equal(t, "sess_123", preToolUse.SessionID)
		assert.Equal(t, "Bash", preToolUse.ToolName)
		assert.Equal(t, "ls", preToolUse.ToolInput["command"])
		assert.Equal(t, "tu_123", preToolUse.ToolUseID)
		assert.Nil(t, preToolUse.AgentID, "AgentID should be nil when absent")
		assert.Nil(t, preToolUse.AgentType, "AgentType should be nil when absent")
	})

	t.Run("PreToolUse with agent_id and agent_type", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "PreToolUse",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"tool_name":       "Bash",
			"tool_input":      map[string]any{"command": "echo hello"},
			"tool_use_id":     "toolu_abc123",
			"agent_id":        "agent-42",
			"agent_type":      "researcher",
		}

		result := parseHookInput(input)
		preToolUse, ok := result.(PreToolUseInput)
		require.True(t, ok)
		require.NotNil(t, preToolUse.AgentID)
		assert.Equal(t, "agent-42", *preToolUse.AgentID)
		require.NotNil(t, preToolUse.AgentType)
		assert.Equal(t, "researcher", *preToolUse.AgentType)
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
			"tool_use_id":     "tu_456",
		}

		result := parseHookInput(input)
		postToolUse, ok := result.(PostToolUseInput)
		assert.True(t, ok)
		assert.Equal(t, "PostToolUse", postToolUse.HookEventName)
		assert.Equal(t, "Bash", postToolUse.ToolName)
		assert.Equal(t, "file1.txt\nfile2.txt", postToolUse.ToolResponse)
		assert.Equal(t, "tu_456", postToolUse.ToolUseID)
		assert.Nil(t, postToolUse.AgentID, "AgentID should be nil when absent")
		assert.Nil(t, postToolUse.AgentType, "AgentType should be nil when absent")
	})

	t.Run("PostToolUse with agent_id", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "PostToolUse",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"tool_name":       "Bash",
			"tool_input":      map[string]any{"command": "echo hello"},
			"tool_response":   map[string]any{"content": []any{map[string]any{"type": "text", "text": "hello"}}},
			"tool_use_id":     "toolu_abc123",
			"agent_id":        "agent-42",
		}

		result := parseHookInput(input)
		postToolUse, ok := result.(PostToolUseInput)
		require.True(t, ok)
		require.NotNil(t, postToolUse.AgentID)
		assert.Equal(t, "agent-42", *postToolUse.AgentID)
		assert.Nil(t, postToolUse.AgentType, "AgentType should be nil when only agent_id is present")
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

	t.Run("PostToolUseFailure", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "PostToolUseFailure",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"tool_name":       "Bash",
			"tool_input":      map[string]any{"command": "rm -rf /"},
			"tool_use_id":     "tu_456",
			"error":           "permission denied",
			"is_interrupt":    true,
		}

		result := parseHookInput(input)
		failure, ok := result.(PostToolUseFailureInput)
		assert.True(t, ok)
		assert.Equal(t, "PostToolUseFailure", failure.HookEventName)
		assert.Equal(t, "Bash", failure.ToolName)
		assert.Equal(t, "tu_456", failure.ToolUseID)
		assert.Equal(t, "permission denied", failure.Error)
		assert.True(t, failure.IsInterrupt)
		assert.Nil(t, failure.AgentID, "AgentID should be nil when absent")
		assert.Nil(t, failure.AgentType, "AgentType should be nil when absent")
	})

	t.Run("PostToolUseFailure with agent_id and agent_type", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "PostToolUseFailure",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"tool_name":       "Bash",
			"tool_input":      map[string]any{"command": "rm -rf /"},
			"tool_use_id":     "tu_789",
			"error":           "permission denied",
			"is_interrupt":    false,
			"agent_id":        "agent-99",
			"agent_type":      "code-reviewer",
		}

		result := parseHookInput(input)
		failure, ok := result.(PostToolUseFailureInput)
		require.True(t, ok)
		require.NotNil(t, failure.AgentID)
		assert.Equal(t, "agent-99", *failure.AgentID)
		require.NotNil(t, failure.AgentType)
		assert.Equal(t, "code-reviewer", *failure.AgentType)
	})

	t.Run("Notification", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name":   "Notification",
			"session_id":        "sess_123",
			"transcript_path":   "/path/to/transcript",
			"cwd":               "/home/user",
			"message":           "Task completed",
			"title":             "Done",
			"notification_type": "info",
		}

		result := parseHookInput(input)
		notification, ok := result.(NotificationInput)
		assert.True(t, ok)
		assert.Equal(t, "Notification", notification.HookEventName)
		assert.Equal(t, "Task completed", notification.Message)
		assert.Equal(t, "Done", notification.Title)
		assert.Equal(t, "info", notification.NotificationType)
	})

	t.Run("SubagentStop", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name":       "SubagentStop",
			"session_id":            "sess_123",
			"transcript_path":       "/path/to/transcript",
			"cwd":                   "/home/user",
			"stop_hook_active":      false,
			"agent_id":              "agent_789",
			"agent_transcript_path": "/path/to/agent/transcript",
			"agent_type":            "researcher",
		}

		result := parseHookInput(input)
		subagentStop, ok := result.(SubagentStopInput)
		assert.True(t, ok)
		assert.Equal(t, "SubagentStop", subagentStop.HookEventName)
		assert.False(t, subagentStop.StopHookActive)
		assert.Equal(t, "agent_789", subagentStop.AgentID)
		assert.Equal(t, "/path/to/agent/transcript", subagentStop.AgentTranscriptPath)
		assert.Equal(t, "researcher", subagentStop.AgentType)
	})

	t.Run("SubagentStart", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name": "SubagentStart",
			"session_id":      "sess_123",
			"transcript_path": "/path/to/transcript",
			"cwd":             "/home/user",
			"agent_id":        "agent_456",
			"agent_type":      "coder",
		}

		result := parseHookInput(input)
		subagentStart, ok := result.(SubagentStartInput)
		assert.True(t, ok)
		assert.Equal(t, "SubagentStart", subagentStart.HookEventName)
		assert.Equal(t, "agent_456", subagentStart.AgentID)
		assert.Equal(t, "coder", subagentStart.AgentType)
	})

	t.Run("PermissionRequest", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name":        "PermissionRequest",
			"session_id":             "sess_123",
			"transcript_path":        "/path/to/transcript",
			"cwd":                    "/home/user",
			"tool_name":              "Write",
			"tool_input":             map[string]any{"path": "/etc/passwd"},
			"permission_suggestions": []any{"allow"},
		}

		result := parseHookInput(input)
		permReq, ok := result.(PermissionRequestInput)
		assert.True(t, ok)
		assert.Equal(t, "PermissionRequest", permReq.HookEventName)
		assert.Equal(t, "Write", permReq.ToolName)
		assert.Equal(t, "/etc/passwd", permReq.ToolInput["path"])
		assert.Equal(t, []any{"allow"}, permReq.PermissionSuggestions)
		assert.Nil(t, permReq.AgentID, "AgentID should be nil when absent")
		assert.Nil(t, permReq.AgentType, "AgentType should be nil when absent")
	})

	t.Run("PermissionRequest with agent_id and agent_type", func(t *testing.T) {
		input := map[string]any{
			"hook_event_name":        "PermissionRequest",
			"session_id":             "sess_123",
			"transcript_path":        "/path/to/transcript",
			"cwd":                    "/home/user",
			"tool_name":              "Write",
			"tool_input":             map[string]any{"path": "/tmp/out.txt"},
			"permission_suggestions": []any{},
			"agent_id":               "agent-77",
			"agent_type":             "general-purpose",
		}

		result := parseHookInput(input)
		permReq, ok := result.(PermissionRequestInput)
		require.True(t, ok)
		require.NotNil(t, permReq.AgentID)
		assert.Equal(t, "agent-77", *permReq.AgentID)
		require.NotNil(t, permReq.AgentType)
		assert.Equal(t, "general-purpose", *permReq.AgentType)
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

func TestHookInputAgentFields_JSONRoundTrip(t *testing.T) {
	agentID := "agent-42"
	agentType := "researcher"

	t.Run("PreToolUseInput with agent fields serializes correctly", func(t *testing.T) {
		input := PreToolUseInput{
			BaseHookInput: BaseHookInput{
				HookEventName: "PreToolUse",
				SessionID:     "sess_1",
			},
			ToolName:  "Bash",
			ToolInput: map[string]any{"command": "ls"},
			ToolUseID: "tu_1",
			AgentID:   &agentID,
			AgentType: &agentType,
		}

		data, err := json.Marshal(input)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		assert.Equal(t, "agent-42", m["agent_id"])
		assert.Equal(t, "researcher", m["agent_type"])
	})

	t.Run("PreToolUseInput without agent fields omits them in JSON", func(t *testing.T) {
		input := PreToolUseInput{
			BaseHookInput: BaseHookInput{
				HookEventName: "PreToolUse",
				SessionID:     "sess_1",
			},
			ToolName:  "Bash",
			ToolInput: map[string]any{"command": "ls"},
			ToolUseID: "tu_1",
		}

		data, err := json.Marshal(input)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		_, hasAgentID := m["agent_id"]
		_, hasAgentType := m["agent_type"]
		assert.False(t, hasAgentID, "agent_id should be omitted when nil")
		assert.False(t, hasAgentType, "agent_type should be omitted when nil")
	})

	t.Run("JSON deserialization into PreToolUseInput", func(t *testing.T) {
		jsonData := `{
			"hook_event_name": "PreToolUse",
			"session_id": "sess_1",
			"transcript_path": "/tmp/t",
			"cwd": "/home/user",
			"tool_name": "Bash",
			"tool_input": {"command": "ls"},
			"tool_use_id": "tu_1",
			"agent_id": "agent-42",
			"agent_type": "researcher"
		}`

		var input PreToolUseInput
		require.NoError(t, json.Unmarshal([]byte(jsonData), &input))
		require.NotNil(t, input.AgentID)
		assert.Equal(t, "agent-42", *input.AgentID)
		require.NotNil(t, input.AgentType)
		assert.Equal(t, "researcher", *input.AgentType)
	})

	t.Run("JSON deserialization without agent fields", func(t *testing.T) {
		jsonData := `{
			"hook_event_name": "PreToolUse",
			"session_id": "sess_1",
			"transcript_path": "/tmp/t",
			"cwd": "/home/user",
			"tool_name": "Bash",
			"tool_input": {"command": "ls"},
			"tool_use_id": "tu_1"
		}`

		var input PreToolUseInput
		require.NoError(t, json.Unmarshal([]byte(jsonData), &input))
		assert.Nil(t, input.AgentID, "AgentID should be nil when not in JSON")
		assert.Nil(t, input.AgentType, "AgentType should be nil when not in JSON")
	})
}
