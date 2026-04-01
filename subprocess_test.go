package claudecode

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

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

	t.Run("with system prompt file", func(t *testing.T) {
		opts := applyOptions(WithSystemPromptFile("/path/to/prompt.md"))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		// Should emit --system-prompt-file with the path
		assert.Contains(t, args, "--system-prompt-file")
		idx := indexOf(args, "--system-prompt-file")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "/path/to/prompt.md", args[idx+1])

		// Should NOT emit --system-prompt or --append-system-prompt
		assert.NotContains(t, args, "--system-prompt")
		assert.NotContains(t, args, "--append-system-prompt")
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

	t.Run("agents are not passed as CLI args", func(t *testing.T) {
		opts := applyOptions(
			WithAgent("analyzer", AgentDefinition{
				Description: "Analyzes code",
				Prompt:      "Analyze code",
			}),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.NotContains(t, args, "--agents",
			"Agents should be sent via initialize request, not CLI args")
	})

	t.Run("with thinking enabled", func(t *testing.T) {
		opts := applyOptions(WithThinking(ThinkingEnabled{BudgetTokens: 4096}))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--max-thinking-tokens")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "4096", args[idx+1])
	})

	t.Run("with thinking disabled passes zero", func(t *testing.T) {
		opts := applyOptions(WithThinking(ThinkingDisabled{}))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--max-thinking-tokens")
		require.GreaterOrEqual(t, idx, 0, "--max-thinking-tokens should be present for disabled thinking")
		assert.Equal(t, "0", args[idx+1], "disabled thinking should pass 0")
	})

	t.Run("with thinking adaptive defaults to 32000", func(t *testing.T) {
		opts := applyOptions(WithThinking(ThinkingAdaptive{}))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--max-thinking-tokens")
		require.GreaterOrEqual(t, idx, 0, "--max-thinking-tokens should be present for adaptive thinking")
		assert.Equal(t, "32000", args[idx+1], "adaptive thinking should default to 32000")
	})

	t.Run("with thinking adaptive respects explicit max tokens", func(t *testing.T) {
		opts := applyOptions(
			WithMaxThinkingTokens(16000),
			WithThinking(ThinkingAdaptive{}),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--max-thinking-tokens")
		require.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "16000", args[idx+1], "adaptive thinking should respect explicit max tokens")
	})

	t.Run("thinking overrides maxThinkingTokens", func(t *testing.T) {
		opts := applyOptions(
			WithMaxThinkingTokens(8000),
			WithThinking(ThinkingEnabled{BudgetTokens: 2000}),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--max-thinking-tokens")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "2000", args[idx+1])
	})

	t.Run("with effort", func(t *testing.T) {
		opts := applyOptions(WithEffort("high"))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--effort")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "high", args[idx+1])
	})

	t.Run("with output format", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{}}
		opts := applyOptions(WithOutputFormat(map[string]any{
			"type":   "json_schema",
			"schema": schema,
		}))
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		assert.Contains(t, args, "--json-schema")
	})

	t.Run("output format overrides jsonSchemaOutput", func(t *testing.T) {
		oldSchema := map[string]any{"old": true}
		newSchema := map[string]any{"new": true}
		opts := applyOptions(
			WithJSONSchemaOutput(oldSchema),
			WithOutputFormat(map[string]any{
				"type":   "json_schema",
				"schema": newSchema,
			}),
		)
		transport := NewSubprocessTransport(opts)
		args, err := transport.buildArgs()
		require.NoError(t, err)

		idx := indexOf(args, "--json-schema")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Contains(t, args[idx+1], `"new":true`)
		assert.NotContains(t, args[idx+1], `"old":true`)
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

func TestBuildArgs_NoPrintFlag(t *testing.T) {
	// After the Query rewrite, --print should never be emitted by buildArgs.
	opts := applyOptions()
	transport := NewSubprocessTransport(opts)
	args, err := transport.buildArgs()
	require.NoError(t, err)
	assert.NotContains(t, args, "--print", "buildArgs should never emit --print after Query rewrite")
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

// envToMap converts a []string env list (KEY=VALUE entries) to a map.
// Later entries override earlier ones, matching exec.Cmd behavior.
func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok {
			m[k] = v
		}
	}
	return m
}

func TestSubprocessTransport_buildEnv(t *testing.T) {
	t.Run("CLAUDE_CODE_ENTRYPOINT defaults to sdk-go", func(t *testing.T) {
		opts := applyOptions()
		transport := NewSubprocessTransport(opts)
		baseEnv := []string{"PATH=/usr/bin"}
		env := transport.buildEnv(baseEnv)
		m := envToMap(env)

		assert.Equal(t, "sdk-go", m["CLAUDE_CODE_ENTRYPOINT"])
	})

	t.Run("caller can override CLAUDE_CODE_ENTRYPOINT via options env", func(t *testing.T) {
		opts := applyOptions(WithEnv("CLAUDE_CODE_ENTRYPOINT", "custom-caller"))
		transport := NewSubprocessTransport(opts)
		baseEnv := []string{"PATH=/usr/bin"}
		env := transport.buildEnv(baseEnv)
		m := envToMap(env)

		assert.Equal(t, "custom-caller", m["CLAUDE_CODE_ENTRYPOINT"],
			"user-provided CLAUDE_CODE_ENTRYPOINT should override the sdk-go default")
	})

	t.Run("CLAUDE_AGENT_SDK_VERSION cannot be overridden by user env", func(t *testing.T) {
		opts := applyOptions(WithEnv("CLAUDE_AGENT_SDK_VERSION", "user-hacked-version"))
		transport := NewSubprocessTransport(opts)
		baseEnv := []string{"PATH=/usr/bin"}
		env := transport.buildEnv(baseEnv)
		m := envToMap(env)

		assert.Equal(t, SDKVersion, m["CLAUDE_AGENT_SDK_VERSION"],
			"CLAUDE_AGENT_SDK_VERSION must always reflect the actual SDK version")
	})

	t.Run("user env vars are passed through", func(t *testing.T) {
		opts := applyOptions(WithEnv("MY_CUSTOM_VAR", "hello"))
		transport := NewSubprocessTransport(opts)
		baseEnv := []string{"PATH=/usr/bin"}
		env := transport.buildEnv(baseEnv)
		m := envToMap(env)

		assert.Equal(t, "hello", m["MY_CUSTOM_VAR"])
	})

	t.Run("cwd sets PWD", func(t *testing.T) {
		opts := applyOptions(WithCWD("/some/dir"))
		transport := NewSubprocessTransport(opts)
		baseEnv := []string{"PATH=/usr/bin"}
		env := transport.buildEnv(baseEnv)
		m := envToMap(env)

		assert.Equal(t, "/some/dir", m["PWD"])
	})

	t.Run("enableFileCheckpointing sets env var", func(t *testing.T) {
		opts := applyOptions(WithEnableFileCheckpointing())
		transport := NewSubprocessTransport(opts)
		baseEnv := []string{"PATH=/usr/bin"}
		env := transport.buildEnv(baseEnv)
		m := envToMap(env)

		assert.Equal(t, "true", m["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"])
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

// setupTransportWithProcess creates a SubprocessTransport with a real exec.Cmd
// already started. This simulates the state after Connect() without needing
// the full CLI version check / argument building.
func setupTransportWithProcess(t *testing.T, cmd *exec.Cmd) *SubprocessTransport {
	t.Helper()

	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)

	require.NoError(t, cmd.Start())

	transport := &SubprocessTransport{
		options:    applyOptions(),
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		ready:      true,
		stderrDone: make(chan struct{}),
	}
	// stderr not piped through callback, so mark done immediately
	close(transport.stderrDone)
	return transport
}

func TestSubprocessTransport_Close_GracefulExit(t *testing.T) {
	// "cat" reads from stdin and exits when stdin is closed.
	// With a generous grace period, the process should exit on its own
	// and Close should NOT need to send any signal.
	cmd := exec.Command("cat")

	transport := setupTransportWithProcess(t, cmd)
	transport.shutdownGracePeriod = 5 * time.Second

	start := time.Now()
	err := transport.Close(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)

	// The process should exit very quickly after stdin is closed
	// (well under the 5s grace period).
	assert.Less(t, elapsed, 2*time.Second,
		"graceful exit should complete quickly, not wait the full grace period")

	// Verify the process actually exited
	assert.True(t, transport.waited || transport.cmd == nil)
}

func TestSubprocessTransport_Close_TimeoutSendsSIGTERM(t *testing.T) {
	// "sleep 999" ignores stdin close and will hang indefinitely.
	// With a short grace period, Close should time out and send SIGTERM.
	cmd := exec.Command("sleep", "999")

	transport := setupTransportWithProcess(t, cmd)
	// Use a very short grace period so the test is fast
	transport.shutdownGracePeriod = 100 * time.Millisecond

	pid := cmd.Process.Pid

	start := time.Now()
	err := transport.Close(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)

	// Should complete in roughly the grace period (plus some slack for cleanup)
	assert.Less(t, elapsed, 3*time.Second,
		"should not hang much beyond the grace period")
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond,
		"should have waited at least the grace period before signaling")

	// Verify process is no longer running. Sending signal 0 to a dead
	// process returns an error.
	err = syscall.Kill(pid, 0)
	assert.Error(t, err, "process should no longer be running after Close")
}

func TestSubprocessTransport_Close_AlreadyExited(t *testing.T) {
	// "true" exits immediately with code 0. By the time Close runs,
	// the process has already exited.
	cmd := exec.Command("true")

	transport := setupTransportWithProcess(t, cmd)
	transport.shutdownGracePeriod = 5 * time.Second

	// Wait a moment for the "true" command to finish
	time.Sleep(50 * time.Millisecond)

	// Call cmd.Wait() to reap the process before Close, simulating the
	// case where ReadMessages already reaped it.
	_ = cmd.Wait()
	transport.waited = true

	start := time.Now()
	err := transport.Close(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)

	// Should be nearly instant -- no grace period wait for already-exited process
	assert.Less(t, elapsed, 500*time.Millisecond,
		"should not wait for an already-exited process")
}

func TestSubprocessTransport_Close_ProcessExitedButNotYetWaited(t *testing.T) {
	// "true" exits immediately, but we do NOT call cmd.Wait() first.
	// The process has exited at the OS level but Close hasn't reaped it yet.
	// Close should detect this quickly via the grace period wait returning
	// immediately (since the process is already dead).
	cmd := exec.Command("true")

	transport := setupTransportWithProcess(t, cmd)
	transport.shutdownGracePeriod = 5 * time.Second

	// Wait a moment for "true" to actually exit
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	err := transport.Close(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)

	// Should be nearly instant because cmd.Wait() returns immediately
	// for a process that has already exited
	assert.Less(t, elapsed, 500*time.Millisecond,
		"should detect already-exited process quickly via Wait()")
}

func TestDefaultShutdownGracePeriod(t *testing.T) {
	// Verify the default grace period is 5 seconds as specified by upstream.
	assert.Equal(t, 5*time.Second, DefaultShutdownGracePeriod)
}

func TestSubprocessTransport_Close_DefaultGracePeriod(t *testing.T) {
	// Verify that a newly created transport uses the default grace period.
	opts := applyOptions()
	transport := NewSubprocessTransport(opts)
	assert.Equal(t, time.Duration(0), transport.shutdownGracePeriod,
		"zero value means use default")
}

