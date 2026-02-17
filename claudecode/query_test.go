package claudecode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuery_Validation(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects empty prompt", func(t *testing.T) {
		ch := Query(ctx, "")
		msg := <-ch
		assert.Error(t, msg.Err)
		assert.Contains(t, msg.Err.Error(), "non-empty prompt")
	})

	t.Run("rejects hooks in print mode", func(t *testing.T) {
		ch := Query(ctx, "test prompt", WithHook(HookPreToolUse, HookMatcher{
			Matcher: "Bash",
			Hooks:   []HookCallback{},
		}))
		msg := <-ch
		assert.Error(t, msg.Err)
		assert.Contains(t, msg.Err.Error(), "hooks require streaming mode")
	})

	t.Run("rejects can_use_tool in print mode", func(t *testing.T) {
		ch := Query(ctx, "test prompt", WithCanUseTool(func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			return PermissionAllow{}, nil
		}))
		msg := <-ch
		assert.Error(t, msg.Err)
		assert.Contains(t, msg.Err.Error(), "can_use_tool callback requires streaming mode")
	})
}
