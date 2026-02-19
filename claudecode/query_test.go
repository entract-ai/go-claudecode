package claudecode

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuery_Validation(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects empty prompt", func(t *testing.T) {
		ch := Query(ctx, "")
		msg := <-ch
		assert.Error(t, msg.Err)
		assert.Contains(t, msg.Err.Error(), "requires a non-empty prompt")
	})

	t.Run("accepts hooks", func(t *testing.T) {
		// Query now delegates to QueryWithInput (streaming), so hooks are supported.
		// We can't fully exercise it without a CLI, but we verify no upfront rejection.
		ch := Query(ctx, "test prompt", WithHook(HookPreToolUse, HookMatcher{
			Matcher: "Bash",
			Hooks:   []HookCallback{},
		}))
		msg := <-ch
		// The error should be about connecting (no CLI), not about hooks being rejected.
		require.Error(t, msg.Err)
		assert.NotContains(t, msg.Err.Error(), "hooks require streaming mode")
	})

	t.Run("accepts can_use_tool", func(t *testing.T) {
		// Query now delegates to QueryWithInput (streaming), so canUseTool is supported.
		ch := Query(ctx, "test prompt", WithCanUseTool(func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			return PermissionAllow{}, nil
		}))
		msg := <-ch
		require.Error(t, msg.Err)
		assert.NotContains(t, msg.Err.Error(), "can_use_tool callback requires streaming mode")
	})
}

func TestErrorsJoin_PreservesAllErrors(t *testing.T) {
	// Verify errors.Join semantics that QuerySync relies on
	err1 := fmt.Errorf("first error")
	err2 := fmt.Errorf("second error")
	joined := errors.Join(err1, err2)
	require.Error(t, joined)
	assert.ErrorIs(t, joined, err1)
	assert.ErrorIs(t, joined, err2)
	assert.Contains(t, joined.Error(), "first error")
	assert.Contains(t, joined.Error(), "second error")
}

func TestErrorsJoin_NilOnEmpty(t *testing.T) {
	// Verify errors.Join returns nil when no errors are passed,
	// which is the behavior QuerySync relies on for the success case
	assert.Nil(t, errors.Join())
}
