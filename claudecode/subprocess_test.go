package claudecode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubprocessTransport_buildArgs(t *testing.T) {
	t.Run("basic args", func(t *testing.T) {
		opts := applyOptions()
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--output-format")
		assert.Contains(t, args, "stream-json")
		assert.Contains(t, args, "--verbose")
		assert.Contains(t, args, "--system-prompt")
		assert.Contains(t, args, "--setting-sources")
	})

	t.Run("with tools", func(t *testing.T) {
		opts := applyOptions(WithTools("Bash", "Read"))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--tools")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "Bash,Read", args[idx+1])
	})

	t.Run("with empty tools", func(t *testing.T) {
		opts := applyOptions(WithTools())
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--tools")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "", args[idx+1])
	})

	t.Run("with tools preset", func(t *testing.T) {
		opts := applyOptions(WithToolsPreset())
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--tools")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "default", args[idx+1])
	})

	t.Run("with system prompt", func(t *testing.T) {
		opts := applyOptions(WithSystemPrompt("Custom prompt"))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--system-prompt")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "Custom prompt", args[idx+1])
	})

	t.Run("with system prompt append", func(t *testing.T) {
		opts := applyOptions(WithSystemPromptPreset("Extra context"))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--append-system-prompt")
		idx := indexOf(args, "--append-system-prompt")
		assert.Equal(t, "Extra context", args[idx+1])
	})

	t.Run("with model", func(t *testing.T) {
		opts := applyOptions(WithModel("claude-sonnet-4-5"))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--model")
		idx := indexOf(args, "--model")
		assert.Equal(t, "claude-sonnet-4-5", args[idx+1])
	})

	t.Run("with permission mode", func(t *testing.T) {
		opts := applyOptions(WithPermissionMode(PermissionBypassPermissions))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--permission-mode")
		idx := indexOf(args, "--permission-mode")
		assert.Equal(t, "bypassPermissions", args[idx+1])
	})

	t.Run("with session options", func(t *testing.T) {
		opts := applyOptions(
			WithContinueConversation(),
			WithResume("session_123"),
			WithForkSession(),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--continue")
		assert.Contains(t, args, "--resume")
		assert.Contains(t, args, "--fork-session")

		idx := indexOf(args, "--resume")
		assert.Equal(t, "session_123", args[idx+1])
	})

	t.Run("with limits", func(t *testing.T) {
		opts := applyOptions(
			WithMaxTurns(10),
			WithMaxBudgetUSD(5.0),
			WithMaxThinkingTokens(8000),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--max-turns")
		assert.Contains(t, args, "--max-budget-usd")
		assert.Contains(t, args, "--max-thinking-tokens")
	})

	t.Run("with allowed and disallowed tools", func(t *testing.T) {
		opts := applyOptions(
			WithAllowedTools("mcp__calc__add"),
			WithDisallowedTools("Bash"),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--allowedTools")
		assert.Contains(t, args, "--disallowedTools")
	})

	t.Run("with streaming mode", func(t *testing.T) {
		opts := applyOptions()
		opts.streamingMode = true
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--input-format")
		idx := indexOf(args, "--input-format")
		assert.Equal(t, "stream-json", args[idx+1])
	})

	t.Run("with extra args", func(t *testing.T) {
		flagOnly := (*string)(nil)
		someValue := "value"

		opts := applyOptions(
			WithExtraArg("debug", flagOnly),
			WithExtraArg("config", &someValue),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--debug")
		assert.Contains(t, args, "--config")
		idx := indexOf(args, "--config")
		assert.Equal(t, "value", args[idx+1])
	})

	t.Run("with agents", func(t *testing.T) {
		opts := applyOptions(
			WithAgent("analyzer", AgentDefinition{
				Description: "Analyzes code",
				Prompt:      "Analyze code",
			}),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--agents")
	})

	t.Run("with include partial messages", func(t *testing.T) {
		opts := applyOptions(WithIncludePartialMessages())
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--include-partial-messages")
	})

	t.Run("with setting sources", func(t *testing.T) {
		opts := applyOptions(WithSettingSources(SettingSourceUser, SettingSourceProject))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--setting-sources")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "user,project", args[idx+1])
	})

	t.Run("with empty setting sources", func(t *testing.T) {
		opts := applyOptions(WithSettingSources())
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--setting-sources")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "", args[idx+1])
	})
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.9.9", "2.0.0", -1},
		{"1.10.0", "1.9.0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildMCPConfig(t *testing.T) {
	t.Run("stdio config", func(t *testing.T) {
		opts := applyOptions(
			WithMCPServer("test-server", &MCPStdioConfig{
				Command: "node",
				Args:    []string{"server.js"},
				Env:     map[string]string{"DEBUG": "true"},
			}),
		)
		transport := NewSubprocessTransport(opts)
		config, err := transport.buildMCPConfig()
		require.NoError(t, err)

		assert.Contains(t, config, "mcpServers")
		assert.Contains(t, config, "test-server")
		assert.Contains(t, config, "stdio")
		assert.Contains(t, config, "node")
	})

	t.Run("sdk config", func(t *testing.T) {
		opts := applyOptions()
		opts.mcpServers = map[string]MCPServerConfig{
			"sdk-server": &MCPSDKConfig{Name: "sdk-server"},
		}
		transport := NewSubprocessTransport(opts)
		config, err := transport.buildMCPConfig()
		require.NoError(t, err)

		assert.Contains(t, config, "sdk-server")
		assert.Contains(t, config, `"type":"sdk"`)
	})
}

func TestBuildSettingsValue(t *testing.T) {
	t.Run("no settings", func(t *testing.T) {
		opts := applyOptions()
		transport := NewSubprocessTransport(opts)
		value, err := transport.buildSettingsValue()
		require.NoError(t, err)
		assert.Empty(t, value)
	})

	t.Run("settings only", func(t *testing.T) {
		opts := applyOptions(WithSettings("/path/to/settings.json"))
		transport := NewSubprocessTransport(opts)
		value, err := transport.buildSettingsValue()
		require.NoError(t, err)
		assert.Equal(t, "/path/to/settings.json", value)
	})

	t.Run("settings json string", func(t *testing.T) {
		opts := applyOptions(WithSettings(`{"key": "value"}`))
		transport := NewSubprocessTransport(opts)
		value, err := transport.buildSettingsValue()
		require.NoError(t, err)
		assert.Equal(t, `{"key": "value"}`, value)
	})

	t.Run("sandbox only", func(t *testing.T) {
		opts := applyOptions(WithSandbox(SandboxSettings{
			Enabled: true,
		}))
		transport := NewSubprocessTransport(opts)
		value, err := transport.buildSettingsValue()
		require.NoError(t, err)
		assert.Contains(t, value, `"sandbox"`)
		assert.Contains(t, value, `"enabled":true`)
	})

	t.Run("settings and sandbox merged", func(t *testing.T) {
		opts := applyOptions(
			WithSettings(`{"other": "value"}`),
			WithSandbox(SandboxSettings{Enabled: true}),
		)
		transport := NewSubprocessTransport(opts)
		value, err := transport.buildSettingsValue()
		require.NoError(t, err)
		assert.Contains(t, value, `"other":"value"`)
		assert.Contains(t, value, `"sandbox"`)
	})

	t.Run("returns error for invalid settings JSON", func(t *testing.T) {
		opts := applyOptions(
			WithSettings(`{invalid json}`),
			WithSandbox(SandboxSettings{Enabled: true}),
		)
		transport := NewSubprocessTransport(opts)
		_, err := transport.buildSettingsValue()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse settings JSON")
	})

	t.Run("returns error for missing settings file with sandbox", func(t *testing.T) {
		opts := applyOptions(
			WithSettings("/nonexistent/path/settings.json"),
			WithSandbox(SandboxSettings{Enabled: true}),
		)
		transport := NewSubprocessTransport(opts)
		_, err := transport.buildSettingsValue()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read settings file")
	})
}

func TestSubprocessTransport_findCLI(t *testing.T) {
	t.Run("returns configured path if provided", func(t *testing.T) {
		opts := applyOptions(WithCLIPath("/custom/path/claude"))
		transport := NewSubprocessTransport(opts)

		path, err := transport.findCLI()
		require.NoError(t, err)
		assert.Equal(t, "/custom/path/claude", path)
	})

	t.Run("returns error or valid path", func(t *testing.T) {
		opts := applyOptions()
		transport := NewSubprocessTransport(opts)

		path, err := transport.findCLI()
		if err != nil {
			// If error is returned, it must be ErrCLINotFound
			require.ErrorIs(t, err, ErrCLINotFound)
			assert.Empty(t, path)
		} else {
			// If no error, a valid path must be returned
			assert.NotEmpty(t, path)
		}
	})
}

func TestSubprocessTransport_buildArgs_PluginTypes(t *testing.T) {
	t.Run("local plugin type is supported", func(t *testing.T) {
		opts := applyOptions(WithPlugin(PluginConfig{
			Type: "local",
			Path: "/path/to/plugin",
		}))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)
		assert.Contains(t, args, "--plugin-dir")
	})

	t.Run("unsupported plugin type returns error", func(t *testing.T) {
		opts := applyOptions(WithPlugin(PluginConfig{
			Type: "remote", // Not a supported type
			Path: "/path/to/plugin",
		}))
		transport := NewSubprocessTransport(opts)
		_, err := transport.buildArgs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported plugin type")
	})
}

func indexOf(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}
