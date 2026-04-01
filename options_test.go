package claudecode

import (
	"encoding/json"
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

	t.Run("with system prompt file", func(t *testing.T) {
		opts := applyOptions(WithSystemPromptFile("/path/to/prompt.md"))
		assert.Equal(t, "/path/to/prompt.md", opts.systemPromptFile)
	})

	t.Run("with permission mode", func(t *testing.T) {
		opts := applyOptions(WithPermissionMode(PermissionBypassPermissions))
		assert.Equal(t, PermissionBypassPermissions, opts.permissionMode)
	})

	t.Run("with dontAsk permission mode", func(t *testing.T) {
		opts := applyOptions(WithPermissionMode(PermissionDontAsk))
		assert.Equal(t, PermissionDontAsk, opts.permissionMode)
		assert.Equal(t, PermissionMode("dontAsk"), opts.permissionMode)
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

	t.Run("with task budget", func(t *testing.T) {
		opts := applyOptions(WithTaskBudget(100000))
		require.NotNil(t, opts.taskBudget)
		assert.Equal(t, 100000, *opts.taskBudget)
	})

	t.Run("without task budget", func(t *testing.T) {
		opts := applyOptions()
		assert.Nil(t, opts.taskBudget)
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

func TestAgentDefinitionJSON(t *testing.T) {
	t.Run("minimal definition omits unset fields", func(t *testing.T) {
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "You are a test",
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, "test", m["description"])
		assert.Equal(t, "You are a test", m["prompt"])
		// Zero-value optional fields must not appear in the JSON.
		assert.NotContains(t, m, "tools")
		assert.NotContains(t, m, "model")
		assert.NotContains(t, m, "skills")
		assert.NotContains(t, m, "memory")
		assert.NotContains(t, m, "mcpServers")
	})

	t.Run("skills and memory serialize correctly", func(t *testing.T) {
		memory := "project"
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
			Skills:      []string{"skill-a", "skill-b"},
			Memory:      &memory,
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		skills, ok := m["skills"].([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"skill-a", "skill-b"}, skills)
		assert.Equal(t, "project", m["memory"])
	})

	t.Run("disallowedTools and maxTurns serialize as camelCase", func(t *testing.T) {
		maxTurns := 10
		agent := AgentDefinition{
			Description:     "test",
			Prompt:          "p",
			DisallowedTools: []string{"Bash", "Write"},
			MaxTurns:        &maxTurns,
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		tools, ok := m["disallowedTools"].([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"Bash", "Write"}, tools)
		assert.NotContains(t, m, "disallowed_tools")

		assert.Equal(t, float64(10), m["maxTurns"])
		assert.NotContains(t, m, "max_turns")
	})

	t.Run("initialPrompt serializes as camelCase", func(t *testing.T) {
		initialPrompt := "/review-pr 123"
		agent := AgentDefinition{
			Description:   "test",
			Prompt:        "p",
			InitialPrompt: &initialPrompt,
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, "/review-pr 123", m["initialPrompt"])
		assert.NotContains(t, m, "initial_prompt")
	})

	t.Run("model accepts full model IDs", func(t *testing.T) {
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
			Model:       "claude-opus-4-5",
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, "claude-opus-4-5", m["model"])
	})

	t.Run("new optional fields omitted when unset", func(t *testing.T) {
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.NotContains(t, m, "disallowedTools")
		assert.NotContains(t, m, "maxTurns")
		assert.NotContains(t, m, "initialPrompt")
	})

	t.Run("round-trip preserves all new fields", func(t *testing.T) {
		maxTurns := 5
		initialPrompt := "hello"
		original := AgentDefinition{
			Description:     "desc",
			Prompt:          "prompt",
			DisallowedTools: []string{"Bash"},
			MaxTurns:        &maxTurns,
			InitialPrompt:   &initialPrompt,
			Model:           "claude-opus-4-5",
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded AgentDefinition
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, original.Description, decoded.Description)
		assert.Equal(t, original.Prompt, decoded.Prompt)
		assert.Equal(t, original.DisallowedTools, decoded.DisallowedTools)
		require.NotNil(t, decoded.MaxTurns)
		assert.Equal(t, 5, *decoded.MaxTurns)
		require.NotNil(t, decoded.InitialPrompt)
		assert.Equal(t, "hello", *decoded.InitialPrompt)
		assert.Equal(t, "claude-opus-4-5", decoded.Model)
	})

	t.Run("background serializes correctly", func(t *testing.T) {
		bg := true
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
			Background:  &bg,
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, true, m["background"])
	})

	t.Run("effort accepts named level", func(t *testing.T) {
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
			Effort:      "high",
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, "high", m["effort"])
	})

	t.Run("effort accepts integer", func(t *testing.T) {
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
			Effort:      32000,
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, float64(32000), m["effort"])
	})

	t.Run("permissionMode serializes as camelCase", func(t *testing.T) {
		mode := "bypassPermissions"
		agent := AgentDefinition{
			Description:         "test",
			Prompt:              "p",
			AgentPermissionMode: &mode,
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.Equal(t, "bypassPermissions", m["permissionMode"])
		assert.NotContains(t, m, "permission_mode")
	})

	t.Run("background effort permissionMode omitted when unset", func(t *testing.T) {
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		assert.NotContains(t, m, "background")
		assert.NotContains(t, m, "effort")
		assert.NotContains(t, m, "permissionMode")
	})

	t.Run("round-trip preserves background effort and permissionMode", func(t *testing.T) {
		bg := true
		mode := "dontAsk"
		original := AgentDefinition{
			Description:         "round-trip test",
			Prompt:              "prompt",
			Background:          &bg,
			Effort:              "high",
			AgentPermissionMode: &mode,
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded AgentDefinition
		require.NoError(t, json.Unmarshal(data, &decoded))

		require.NotNil(t, decoded.Background)
		assert.Equal(t, true, *decoded.Background)
		assert.Equal(t, "high", decoded.Effort)
		require.NotNil(t, decoded.AgentPermissionMode)
		assert.Equal(t, "dontAsk", *decoded.AgentPermissionMode)
	})

	t.Run("round-trip effort as integer produces float64", func(t *testing.T) {
		// JSON unmarshal into any produces float64 for numbers.
		original := AgentDefinition{
			Description: "effort-int test",
			Prompt:      "prompt",
			Effort:      32000,
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded AgentDefinition
		require.NoError(t, json.Unmarshal(data, &decoded))

		// After round-trip through JSON, integer becomes float64.
		assert.Equal(t, float64(32000), decoded.Effort)
	})

	t.Run("mcpServers serializes as camelCase", func(t *testing.T) {
		agent := AgentDefinition{
			Description: "test",
			Prompt:      "p",
			McpServers: []any{
				"slack",
				map[string]any{
					"local": map[string]any{
						"command": "python",
						"args":    []string{"server.py"},
					},
				},
			},
		}
		data, err := json.Marshal(agent)
		require.NoError(t, err)

		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))

		// Must be camelCase, not snake_case.
		assert.Contains(t, m, "mcpServers")
		assert.NotContains(t, m, "mcp_servers")

		servers, ok := m["mcpServers"].([]any)
		require.True(t, ok)
		assert.Equal(t, "slack", servers[0])
		inline, ok := servers[1].(map[string]any)
		require.True(t, ok)
		local, ok := inline["local"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "python", local["command"])
	})
}
