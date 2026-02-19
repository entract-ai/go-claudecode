package claudecode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOptions(t *testing.T) {
	t.Run("empty options", func(t *testing.T) {
		opts := applyOptions()
		assert.Nil(t, opts.tools)
		assert.False(t, opts.toolsPreset)
		assert.Empty(t, opts.allowedTools)
		assert.Nil(t, opts.systemPrompt)
	})

	t.Run("with tools", func(t *testing.T) {
		opts := applyOptions(WithTools("Bash", "Read"))
		assert.NotNil(t, opts.tools)
		assert.Equal(t, []string{"Bash", "Read"}, *opts.tools)
	})

	t.Run("with empty tools", func(t *testing.T) {
		opts := applyOptions(WithTools())
		assert.NotNil(t, opts.tools)
		assert.Empty(t, *opts.tools)
	})

	t.Run("with tools preset", func(t *testing.T) {
		opts := applyOptions(WithToolsPreset())
		assert.True(t, opts.toolsPreset)
	})

	t.Run("with allowed tools", func(t *testing.T) {
		opts := applyOptions(WithAllowedTools("mcp__calc__add", "mcp__calc__sub"))
		assert.Equal(t, []string{"mcp__calc__add", "mcp__calc__sub"}, opts.allowedTools)
	})

	t.Run("with disallowed tools", func(t *testing.T) {
		opts := applyOptions(WithDisallowedTools("Bash"))
		assert.Equal(t, []string{"Bash"}, opts.disallowedTools)
	})

	t.Run("with system prompt", func(t *testing.T) {
		opts := applyOptions(WithSystemPrompt("You are a helpful assistant"))
		assert.NotNil(t, opts.systemPrompt)
		assert.Equal(t, "You are a helpful assistant", *opts.systemPrompt)
	})

	t.Run("with system prompt preset", func(t *testing.T) {
		opts := applyOptions(WithSystemPromptPreset("Additional context"))
		assert.Equal(t, "Additional context", opts.systemPromptAppend)
	})

	t.Run("with permission mode", func(t *testing.T) {
		opts := applyOptions(WithPermissionMode(PermissionBypassPermissions))
		assert.Equal(t, PermissionBypassPermissions, opts.permissionMode)
	})

	t.Run("with session options", func(t *testing.T) {
		opts := applyOptions(
			WithContinueConversation(),
			WithResume("session_123"),
			WithForkSession(),
		)
		assert.True(t, opts.continueConversation)
		assert.Equal(t, "session_123", opts.resume)
		assert.True(t, opts.forkSession)
	})

	t.Run("with limits", func(t *testing.T) {
		opts := applyOptions(
			WithMaxTurns(10),
			WithMaxBudgetUSD(1.0),
			WithMaxThinkingTokens(5000),
		)
		assert.Equal(t, 10, opts.maxTurns)
		assert.Equal(t, 1.0, opts.maxBudgetUSD)
		require.NotNil(t, opts.maxThinkingTokens)
		assert.Equal(t, 5000, *opts.maxThinkingTokens)
	})

	t.Run("with model", func(t *testing.T) {
		opts := applyOptions(
			WithModel("claude-sonnet-4-5"),
			WithFallbackModel("claude-haiku"),
		)
		assert.Equal(t, "claude-sonnet-4-5", opts.model)
		assert.Equal(t, "claude-haiku", opts.fallbackModel)
	})

	t.Run("with environment", func(t *testing.T) {
		opts := applyOptions(
			WithCWD("/path/to/project"),
			WithCLIPath("/custom/path/claude"),
			WithEnv("CUSTOM_VAR", "value"),
			WithAddDirs("/extra/dir1", "/extra/dir2"),
		)
		assert.Equal(t, "/path/to/project", opts.cwd)
		assert.Equal(t, "/custom/path/claude", opts.cliPath)
		assert.Equal(t, "value", opts.env["CUSTOM_VAR"])
		assert.Equal(t, []string{"/extra/dir1", "/extra/dir2"}, opts.addDirs)
	})

	t.Run("with settings", func(t *testing.T) {
		opts := applyOptions(
			WithSettings(`{"key": "value"}`),
			WithSettingSources(SettingSourceUser, SettingSourceProject),
		)
		assert.Equal(t, `{"key": "value"}`, opts.settings)
		assert.Equal(t, []string{"user", "project"}, opts.settingSources)
	})

	t.Run("with extra args", func(t *testing.T) {
		flagOnly := (*string)(nil)
		emptyValue := ""
		someValue := "foo"

		opts := applyOptions(
			WithExtraArg("verbose", flagOnly),
			WithExtraArg("output", &emptyValue),
			WithExtraArg("config", &someValue),
		)

		assert.Nil(t, opts.extraArgs["verbose"])
		assert.Equal(t, "", *opts.extraArgs["output"])
		assert.Equal(t, "foo", *opts.extraArgs["config"])
	})

	t.Run("with agents", func(t *testing.T) {
		opts := applyOptions(
			WithAgent("analyzer", AgentDefinition{
				Description: "Analyzes code",
				Prompt:      "Analyze the following code",
				Tools:       []string{"Read", "Grep"},
				Model:       "sonnet",
			}),
		)
		assert.Len(t, opts.agents, 1)
		assert.Equal(t, "Analyzes code", opts.agents["analyzer"].Description)
	})

	t.Run("with sandbox", func(t *testing.T) {
		opts := applyOptions(
			WithSandbox(SandboxSettings{
				Enabled:                  true,
				AutoAllowBashIfSandboxed: true,
				ExcludedCommands:         []string{"docker"},
			}),
		)
		assert.NotNil(t, opts.sandbox)
		assert.True(t, opts.sandbox.Enabled)
		assert.Equal(t, []string{"docker"}, opts.sandbox.ExcludedCommands)
	})

	t.Run("with plugins", func(t *testing.T) {
		opts := applyOptions(
			WithPlugin(PluginConfig{Type: "local", Path: "/path/to/plugin"}),
		)
		assert.Len(t, opts.plugins, 1)
		assert.Equal(t, "local", opts.plugins[0].Type)
	})

	t.Run("with misc options", func(t *testing.T) {
		called := false
		opts := applyOptions(
			WithEnableFileCheckpointing(),
			WithIncludePartialMessages(),
			WithStderr(func(s string) { called = true }),
		)
		assert.True(t, opts.enableFileCheckpointing)
		assert.True(t, opts.includePartialMessages)
		assert.NotNil(t, opts.stderrCallback)
		opts.stderrCallback("test")
		assert.True(t, called)
	})

	t.Run("with json schema output", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		}
		opts := applyOptions(WithJSONSchemaOutput(schema))
		assert.Equal(t, schema, opts.jsonSchemaOutput)
	})
}

func TestMCPServerOptions(t *testing.T) {
	t.Run("stdio config", func(t *testing.T) {
		opts := applyOptions(
			WithMCPServer("my-server", &MCPStdioConfig{
				Command: "node",
				Args:    []string{"server.js"},
				Env:     map[string]string{"DEBUG": "true"},
			}),
		)
		assert.Len(t, opts.mcpServers, 1)
		config, ok := opts.mcpServers["my-server"].(*MCPStdioConfig)
		assert.True(t, ok)
		assert.Equal(t, "node", config.Command)
	})

	t.Run("sse config", func(t *testing.T) {
		opts := applyOptions(
			WithMCPServer("sse-server", &MCPSSEConfig{
				URL:     "https://api.example.com/mcp",
				Headers: map[string]string{"Authorization": "Bearer token"},
			}),
		)
		config, ok := opts.mcpServers["sse-server"].(*MCPSSEConfig)
		assert.True(t, ok)
		assert.Equal(t, "https://api.example.com/mcp", config.URL)
	})

	t.Run("http config", func(t *testing.T) {
		opts := applyOptions(
			WithMCPServer("http-server", &MCPHTTPConfig{
				URL: "https://api.example.com/mcp",
			}),
		)
		config, ok := opts.mcpServers["http-server"].(*MCPHTTPConfig)
		assert.True(t, ok)
		assert.Equal(t, "https://api.example.com/mcp", config.URL)
	})

	t.Run("mcp servers path", func(t *testing.T) {
		opts := applyOptions(WithMCPServersPath("/path/to/servers.json"))
		assert.Equal(t, "/path/to/servers.json", opts.mcpServersPath)
	})

	t.Run("mcp servers json", func(t *testing.T) {
		json := `{"mcpServers": {}}`
		opts := applyOptions(WithMCPServersJSON(json))
		assert.Equal(t, json, opts.mcpServersJSON)
	})
}

func TestHookOptions(t *testing.T) {
	callCount := 0
	callback := func(_ HookCallback) {
		callCount++
	}
	_ = callback

	opts := applyOptions(
		WithHook(HookPreToolUse, HookMatcher{
			Matcher: "Bash",
			Hooks:   []HookCallback{},
		}),
		WithHook(HookPostToolUse, HookMatcher{
			Matcher: "Write|Edit",
		}),
	)

	assert.Len(t, opts.hooks, 2)
	assert.Len(t, opts.hooks[HookPreToolUse], 1)
	assert.Len(t, opts.hooks[HookPostToolUse], 1)
}
