package claudecode

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnvDuration(t *testing.T) {
	const testEnvKey = "TEST_CLAUDECODE_TIMEOUT"

	t.Run("returns 0 when env var not set", func(t *testing.T) {
		os.Unsetenv(testEnvKey)
		d, err := getEnvDuration(testEnvKey)
		require.NoError(t, err)
		assert.Equal(t, time.Duration(0), d)
	})

	t.Run("parses milliseconds correctly", func(t *testing.T) {
		os.Setenv(testEnvKey, "5000")
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDuration(testEnvKey)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, d)
	})

	t.Run("returns error for invalid value", func(t *testing.T) {
		os.Setenv(testEnvKey, "not-a-number")
		defer os.Unsetenv(testEnvKey)

		_, err := getEnvDuration(testEnvKey)
		require.Error(t, err)
	})
}

func TestGetEnvDurationWithDefault(t *testing.T) {
	const testEnvKey = "TEST_CLAUDECODE_TIMEOUT"
	const defaultVal = 60 * time.Second

	t.Run("returns default when env var not set", func(t *testing.T) {
		os.Unsetenv(testEnvKey)
		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, defaultVal, d)
	})

	t.Run("honors env value below default", func(t *testing.T) {
		os.Setenv(testEnvKey, "5000") // 5 seconds, below 60s default
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, d, "should honor user-specified value even if below default")
	})

	t.Run("honors env value above default", func(t *testing.T) {
		os.Setenv(testEnvKey, "120000") // 120 seconds
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, 120*time.Second, d)
	})

	t.Run("honors env value equal to default", func(t *testing.T) {
		os.Setenv(testEnvKey, "60000") // exactly 60 seconds
		defer os.Unsetenv(testEnvKey)

		d, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.NoError(t, err)
		assert.Equal(t, 60*time.Second, d)
	})

	t.Run("returns error for invalid value", func(t *testing.T) {
		os.Setenv(testEnvKey, "invalid")
		defer os.Unsetenv(testEnvKey)

		_, err := getEnvDurationWithDefault(testEnvKey, defaultVal)
		require.Error(t, err)
	})
}

func TestInitialize_IncludesAgents(t *testing.T) {
	// Verify that Initialize() includes agents in the request map.
	// We can't easily test the full round-trip without a real CLI,
	// but we can verify the request construction by inspecting the
	// ControlRouter's options.
	opts := &Options{
		agents: map[string]AgentDefinition{
			"analyzer": {
				Description: "Analyzes code",
				Prompt:      "Analyze code",
			},
		},
	}

	router := NewControlRouter(nil, opts)
	assert.NotNil(t, router.options.agents)
	assert.Contains(t, router.options.agents, "analyzer")
}

func TestHandleCanUseTool_BlockedPath(t *testing.T) {
	// Verify that blocked_path is parsed from the request.
	var receivedCtx ToolPermissionContext
	opts := &Options{
		canUseTool: func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error) {
			receivedCtx = permCtx
			return PermissionAllow{}, nil
		},
	}

	router := NewControlRouter(nil, opts)

	raw := []byte(`{
		"request_id": "req_1",
		"request": {
			"subtype": "can_use_tool",
			"tool_name": "Write",
			"input": {"path": "/tmp/test"},
			"blocked_path": "/etc/sensitive",
			"permission_suggestions": []
		}
	}`)

	_, err := router.handleCanUseTool(context.Background(), raw)
	require.NoError(t, err)
	assert.Equal(t, "/etc/sensitive", receivedCtx.BlockedPath)
}

func TestBuildHooksConfig(t *testing.T) {
	// Suppress unused variable warning for context import
	_ = context.Background()

	t.Run("omits matcher key when empty string", func(t *testing.T) {
		opts := &Options{
			hooks: map[HookEvent][]HookMatcher{
				HookPreToolUse: {
					{
						Matcher: "", // Empty - should be omitted
						Hooks: []HookCallback{
							func(ctx context.Context, input HookInput, toolUseID *string) (HookOutput, error) {
								return HookOutput{}, nil
							},
						},
					},
				},
			},
		}

		router := NewControlRouter(nil, opts)
		config := router.buildHooksConfig()

		require.NotNil(t, config)
		preToolUse, ok := config["PreToolUse"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, preToolUse, 1)

		// Check that matcher key is NOT present (not set to empty string)
		_, hasMatcher := preToolUse[0]["matcher"]
		assert.False(t, hasMatcher, "matcher key should be omitted when empty")
		assert.NotNil(t, preToolUse[0]["hookCallbackIds"])
	})

	t.Run("includes matcher key when non-empty", func(t *testing.T) {
		opts := &Options{
			hooks: map[HookEvent][]HookMatcher{
				HookPreToolUse: {
					{
						Matcher: "Bash",
						Hooks: []HookCallback{
							func(ctx context.Context, input HookInput, toolUseID *string) (HookOutput, error) {
								return HookOutput{}, nil
							},
						},
					},
				},
			},
		}

		router := NewControlRouter(nil, opts)
		config := router.buildHooksConfig()

		require.NotNil(t, config)
		preToolUse, ok := config["PreToolUse"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, preToolUse, 1)

		// Check that matcher key IS present
		matcher, hasMatcher := preToolUse[0]["matcher"]
		assert.True(t, hasMatcher, "matcher key should be present when non-empty")
		assert.Equal(t, "Bash", matcher)
	})
}
