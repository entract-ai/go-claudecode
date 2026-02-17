package claudecode

import (
	"context"
	"time"
)

// HookEvent represents the type of hook event.
type HookEvent string

const (
	HookPreToolUse       HookEvent = "PreToolUse"
	HookPostToolUse      HookEvent = "PostToolUse"
	HookUserPromptSubmit HookEvent = "UserPromptSubmit"
	HookStop             HookEvent = "Stop"
	HookSubagentStop     HookEvent = "SubagentStop"
	HookPreCompact       HookEvent = "PreCompact"
)

// HookCallback is the function signature for hook callbacks.
// toolUseID is nil for non-tool hooks, non-nil for tool hooks (may be empty string).
type HookCallback func(ctx context.Context, input HookInput, toolUseID *string) (HookOutput, error)

// HookMatcher configures when and how hooks are invoked.
type HookMatcher struct {
	// Matcher is a tool name pattern (e.g., "Bash", "Write|MultiEdit|Edit").
	Matcher string
	// Hooks are the callback functions to invoke.
	Hooks []HookCallback
	// Timeout is the maximum time to wait for the hook (default: 60s).
	Timeout time.Duration
}

// HookInput is a marker interface for all hook input types.
type HookInput interface {
	hookInputMarker()
}

// BaseHookInput contains fields common to all hook inputs.
type BaseHookInput struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode,omitzero"`
}

func (BaseHookInput) hookInputMarker() {}

// PreToolUseInput is the input for PreToolUse hooks.
type PreToolUseInput struct {
	BaseHookInput
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

func (PreToolUseInput) hookInputMarker() {}

// PostToolUseInput is the input for PostToolUse hooks.
type PostToolUseInput struct {
	BaseHookInput
	ToolName     string         `json:"tool_name"`
	ToolInput    map[string]any `json:"tool_input"`
	ToolResponse any            `json:"tool_response"`
}

func (PostToolUseInput) hookInputMarker() {}

// UserPromptSubmitInput is the input for UserPromptSubmit hooks.
type UserPromptSubmitInput struct {
	BaseHookInput
	Prompt string `json:"prompt"`
}

func (UserPromptSubmitInput) hookInputMarker() {}

// StopInput is the input for Stop hooks.
type StopInput struct {
	BaseHookInput
	StopHookActive bool `json:"stop_hook_active"`
}

func (StopInput) hookInputMarker() {}

// SubagentStopInput is the input for SubagentStop hooks.
type SubagentStopInput struct {
	BaseHookInput
	StopHookActive bool `json:"stop_hook_active"`
}

func (SubagentStopInput) hookInputMarker() {}

// PreCompactInput is the input for PreCompact hooks.
type PreCompactInput struct {
	BaseHookInput
	Trigger            string `json:"trigger"` // "manual" or "auto"
	CustomInstructions string `json:"custom_instructions,omitzero"`
}

func (PreCompactInput) hookInputMarker() {}

// HookOutput is the response from a hook callback.
//
// The Continue field uses *bool because the Claude Code CLI protocol specifies that
// when the "continue" field is omitted (nil), execution should continue (equivalent
// to continue: true). This is NOT fallback behavior - it's the protocol specification.
// Using *bool allows us to distinguish between:
//   - nil: use protocol default (continue)
//   - true: explicitly continue
//   - false: explicitly block
type HookOutput struct {
	// Async execution (mutually exclusive with sync fields)
	Async        bool `json:"async,omitzero"`
	AsyncTimeout int  `json:"asyncTimeout,omitzero"` // milliseconds

	// Sync control fields
	// Continue is nil to use protocol default (true), or explicitly set to control flow.
	// The protocol specifies that omitting this field means "continue".
	Continue       *bool  `json:"continue,omitzero"`
	SuppressOutput bool   `json:"suppressOutput,omitzero"`
	StopReason     string `json:"stopReason,omitzero"`

	// Decision fields
	Decision      string `json:"decision,omitzero"` // "block"
	SystemMessage string `json:"systemMessage,omitzero"`
	Reason        string `json:"reason,omitzero"`

	// Event-specific output
	HookSpecificOutput any `json:"hookSpecificOutput,omitzero"`
}

// PreToolUseSpecificOutput is hook-specific output for PreToolUse.
type PreToolUseSpecificOutput struct {
	HookEventName            string         `json:"hookEventName"`               // "PreToolUse"
	PermissionDecision       string         `json:"permissionDecision,omitzero"` // "allow", "deny", "ask"
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitzero"`
	UpdatedInput             map[string]any `json:"updatedInput,omitzero"`
}

// PostToolUseSpecificOutput is hook-specific output for PostToolUse.
type PostToolUseSpecificOutput struct {
	HookEventName     string `json:"hookEventName"` // "PostToolUse"
	AdditionalContext string `json:"additionalContext,omitzero"`
}

// UserPromptSubmitSpecificOutput is hook-specific output for UserPromptSubmit.
type UserPromptSubmitSpecificOutput struct {
	HookEventName     string `json:"hookEventName"` // "UserPromptSubmit"
	AdditionalContext string `json:"additionalContext,omitzero"`
}

// NewHookOutputContinue creates a hook output that allows execution to continue.
func NewHookOutputContinue() HookOutput {
	t := true
	return HookOutput{Continue: &t}
}

// NewHookOutputBlock creates a hook output that blocks execution.
func NewHookOutputBlock(reason string) HookOutput {
	f := false
	return HookOutput{
		Continue: &f,
		Decision: "block",
		Reason:   reason,
	}
}

// NewPreToolUseAllow creates a PreToolUse output that allows the tool to run.
func NewPreToolUseAllow() HookOutput {
	t := true
	return HookOutput{
		Continue: &t,
		HookSpecificOutput: PreToolUseSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
		},
	}
}

// NewPreToolUseDeny creates a PreToolUse output that denies the tool.
func NewPreToolUseDeny(reason string) HookOutput {
	t := true
	return HookOutput{
		Continue: &t,
		HookSpecificOutput: PreToolUseSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		},
	}
}

// NewPreToolUseModify creates a PreToolUse output that modifies the tool input.
func NewPreToolUseModify(updatedInput map[string]any) HookOutput {
	t := true
	return HookOutput{
		Continue: &t,
		HookSpecificOutput: PreToolUseSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
			UpdatedInput:       updatedInput,
		},
	}
}

// NewPostToolUseContext creates a PostToolUse output with additional context.
func NewPostToolUseContext(context string) HookOutput {
	t := true
	return HookOutput{
		Continue: &t,
		HookSpecificOutput: PostToolUseSpecificOutput{
			HookEventName:     "PostToolUse",
			AdditionalContext: context,
		},
	}
}
