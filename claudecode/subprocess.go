package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// MinimumCLIVersion is the minimum supported Claude Code CLI version.
	MinimumCLIVersion = "2.0.0"

	// DefaultStreamCloseTimeout is the default timeout for closing stdin.
	DefaultStreamCloseTimeout = 60 * time.Second

	// SDKVersion is the version of this SDK.
	SDKVersion = "0.1.0"
)

// SubprocessTransport implements Transport using the Claude Code CLI subprocess.
type SubprocessTransport struct {
	options *Options

	mu            sync.Mutex
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	ready         bool
	closed        bool
	stdinClosed   bool
	stderrDone    chan struct{}
	messageReader *jsontext.Decoder
	exitError     error // Tracks process exit error
	waited        bool  // Tracks if we've already waited
}

// NewSubprocessTransport creates a new subprocess transport with the given options.
func NewSubprocessTransport(opts *Options) *SubprocessTransport {
	return &SubprocessTransport{
		options:    opts,
		stderrDone: make(chan struct{}),
	}
}

// Connect starts the Claude Code CLI subprocess.
func (t *SubprocessTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ready {
		return nil
	}

	cliPath, err := t.findCLI()
	if err != nil {
		return fmt.Errorf("findCLI: %w", err)
	}

	// Check version unless skipped
	if os.Getenv("CLAUDE_AGENT_SDK_SKIP_VERSION_CHECK") == "" {
		if err := t.checkVersion(ctx, cliPath); err != nil {
			// Log warning but don't fail
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
	}

	// Build command arguments
	args, err := t.buildArgs()
	if err != nil {
		return fmt.Errorf("build args: %w", err)
	}

	// Create command (optionally sandboxed)
	if t.options.osSandboxPolicy != nil {
		t.cmd, err = t.options.osSandboxPolicy.Command(ctx, cliPath, args...)
		if err != nil {
			return fmt.Errorf("create sandboxed command: %w", err)
		}
	} else {
		t.cmd = exec.CommandContext(ctx, cliPath, args...)
	}

	// Set up environment - preserve sandbox-provided env vars (e.g., TMPDIR) if present
	var env []string
	if t.cmd.Env != nil {
		env = t.cmd.Env
	} else {
		env = os.Environ()
	}
	env = append(env, "CLAUDE_CODE_ENTRYPOINT=sdk-go")
	env = append(env, fmt.Sprintf("CLAUDE_AGENT_SDK_VERSION=%s", SDKVersion))

	if t.options.cwd != "" {
		env = append(env, fmt.Sprintf("PWD=%s", t.options.cwd))
		t.cmd.Dir = t.options.cwd
	}

	if t.options.enableFileCheckpointing {
		env = append(env, "CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true")
	}

	// Add user-provided environment variables
	for k, v := range t.options.env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	t.cmd.Env = env

	// Set up pipes
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	// When a callback is set, pipe stderr through Go so the callback sees
	// each line.  Otherwise wire the child's stderr directly to our stderr
	// so output is never buffered or lost (especially useful under systemd
	// where os.Stderr goes to the journal).
	if t.options.stderrCallback != nil {
		t.stderr, err = t.cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("create stderr pipe: %w", err)
		}
	} else {
		t.cmd.Stderr = os.Stderr
	}

	// Start process
	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	// Set up message reader - jsontext.Decoder handles streaming natively
	t.messageReader = jsontext.NewDecoder(t.stdout)

	// Handle stderr in background (only when piped through callback)
	if t.options.stderrCallback != nil {
		go t.handleStderr()
	} else {
		close(t.stderrDone)
	}

	// Close stdin immediately for non-streaming (print) mode
	if !t.options.streamingMode && t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			return fmt.Errorf("close stdin for print mode: %w", err)
		}
		t.stdinClosed = true
	}

	t.ready = true
	return nil
}

// handleStderr reads stderr line-by-line and forwards to the callback.
// Only called when stderrCallback is set; otherwise stderr goes directly
// to os.Stderr without buffering.
func (t *SubprocessTransport) handleStderr() {
	defer close(t.stderrDone)

	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		t.options.stderrCallback(scanner.Text())
	}
}

// Write sends data to the transport.
func (t *SubprocessTransport) Write(ctx context.Context, data string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.exitError != nil {
		return fmt.Errorf("cannot write: process exited: %w", t.exitError)
	}

	if !t.ready {
		return fmt.Errorf("transport not ready: %w", ErrNotConnected)
	}

	if t.stdinClosed {
		return fmt.Errorf("stdin already closed: %w", ErrConnection)
	}

	if _, err := io.WriteString(t.stdin, data); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}

	return nil
}

// ReadMessages returns a channel that yields messages from the transport.
func (t *SubprocessTransport) ReadMessages(ctx context.Context) <-chan MessageOrError {
	ch := make(chan MessageOrError, 100)

	go func() {
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				ch <- MessageOrError{Err: ctx.Err()}
				return
			default:
			}

			raw, err := t.messageReader.ReadValue()
			if err != nil {
				if errors.Is(err, io.EOF) {
					if exitErr := t.waitForExit(); exitErr != nil {
						ch <- MessageOrError{Err: exitErr}
					}
					return
				}
				ch <- MessageOrError{Err: err}
				return
			}

			// Copy the raw bytes because jsontext.Decoder reuses its internal buffer.
			// Without copying, the next ReadValue call would overwrite this data.
			rawMsg := make(json.RawMessage, len(raw))
			copy(rawMsg, raw)
			msg, err := parseMessage(rawMsg)
			if err != nil {
				// Parse errors are fatal - terminate the stream to match Python behavior.
				// This helps detect protocol changes or corrupted output early.
				ch <- MessageOrError{Err: fmt.Errorf("parse message: %w", err), Raw: rawMsg}
				return
			}

			ch <- MessageOrError{Message: msg, Raw: rawMsg}
		}
	}()

	return ch
}

// waitForExit waits for the process to exit exactly once and returns any exit error.
func (t *SubprocessTransport) waitForExit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.waited && t.cmd != nil {
		t.waited = true
		if waitErr := t.cmd.Wait(); waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				t.exitError = &ProcessError{
					ExitCode: exitErr.ExitCode(),
					Stderr:   "Check stderr output for details",
				}
			} else {
				t.exitError = fmt.Errorf("wait for process: %w", waitErr)
			}
		}
	}
	return t.exitError
}

// EndInput closes stdin to signal end of input.
func (t *SubprocessTransport) EndInput(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stdinClosed {
		return nil
	}

	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			return fmt.Errorf("close stdin: %w", err)
		}
	}

	t.stdinClosed = true
	return nil
}

// Close closes the transport and releases resources.
func (t *SubprocessTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	t.ready = false

	// Close stdin if not already closed
	if !t.stdinClosed && t.stdin != nil {
		t.stdin.Close()
		t.stdinClosed = true
	}

	// Wait for stderr to finish
	select {
	case <-t.stderrDone:
	case <-time.After(time.Second):
	}

	// Kill process if still running, but only wait if not already waited
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		if !t.waited {
			t.waited = true
			t.cmd.Wait() // Ignore error since we're closing anyway
		}
	}

	return nil
}

// IsReady returns true if the transport is ready for communication.
func (t *SubprocessTransport) IsReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready && !t.closed
}

func (t *SubprocessTransport) findCLI() (string, error) {
	// Use configured path if provided
	if t.options.cliPath != "" {
		return t.options.cliPath, nil
	}

	// Check PATH
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}

	// Check common installation locations
	home, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(home, ".npm-global/bin/claude"),
		"/usr/local/bin/claude",
		filepath.Join(home, ".local/bin/claude"),
		filepath.Join(home, "node_modules/.bin/claude"),
		filepath.Join(home, ".yarn/bin/claude"),
		filepath.Join(home, ".local/share/pnpm/claude"),
		filepath.Join(home, ".claude/local/claude"),
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc, nil
		}
	}

	return "", ErrCLINotFound
}

func (t *SubprocessTransport) checkVersion(ctx context.Context, cliPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cliPath, "-v")
	output, err := cmd.Output()
	if err != nil {
		return nil // Silently ignore version check failures
	}

	versionRe := regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+)`)
	match := versionRe.FindStringSubmatch(string(output))
	if len(match) < 2 {
		return nil
	}

	version := match[1]
	if compareVersions(version, MinimumCLIVersion) < 0 {
		return fmt.Errorf("claude code version %s is below minimum required version %s", version, MinimumCLIVersion)
	}

	return nil
}

func (t *SubprocessTransport) buildArgs() ([]string, error) {
	var args []string

	// Always use stream-json output format
	args = append(args, "--output-format", "stream-json")
	args = append(args, "--verbose")

	// System prompt handling
	if t.options.systemPrompt != nil {
		args = append(args, "--system-prompt", *t.options.systemPrompt)
	} else if t.options.systemPromptAppend != "" {
		args = append(args, "--append-system-prompt", t.options.systemPromptAppend)
	} else {
		args = append(args, "--system-prompt", "")
	}

	// Tools handling
	if t.options.tools != nil {
		if len(*t.options.tools) == 0 {
			args = append(args, "--tools", "")
		} else {
			args = append(args, "--tools", strings.Join(*t.options.tools, ","))
		}
	} else if t.options.toolsPreset {
		args = append(args, "--tools", "default")
	}

	if len(t.options.allowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(t.options.allowedTools, ","))
	}

	if len(t.options.disallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(t.options.disallowedTools, ","))
	}

	// Session options
	if t.options.continueConversation {
		args = append(args, "--continue")
	}

	if t.options.resume != "" {
		args = append(args, "--resume", t.options.resume)
	}

	if t.options.forkSession {
		args = append(args, "--fork-session")
	}

	// Limits
	if t.options.maxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", t.options.maxTurns))
	}

	if t.options.maxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%f", t.options.maxBudgetUSD))
	}

	// Resolve thinking tokens: WithThinking takes precedence over WithMaxThinkingTokens
	maxThinkingTokens := t.options.maxThinkingTokens
	if t.options.thinking != nil {
		switch tc := t.options.thinking.(type) {
		case ThinkingEnabled:
			maxThinkingTokens = tc.BudgetTokens
		case ThinkingDisabled:
			maxThinkingTokens = 0
		case ThinkingAdaptive:
			maxThinkingTokens = 0 // omit the flag for adaptive
		}
	}

	if maxThinkingTokens > 0 {
		args = append(args, "--max-thinking-tokens", fmt.Sprintf("%d", maxThinkingTokens))
	}

	if t.options.effort != "" {
		args = append(args, "--effort", t.options.effort)
	}

	// Model
	if t.options.model != "" {
		args = append(args, "--model", t.options.model)
	}

	if t.options.fallbackModel != "" {
		args = append(args, "--fallback-model", t.options.fallbackModel)
	}

	// Betas
	if len(t.options.betas) > 0 {
		args = append(args, "--betas", strings.Join(t.options.betas, ","))
	}

	// Permission
	if t.options.permissionMode != "" {
		args = append(args, "--permission-mode", string(t.options.permissionMode))
	}

	if t.options.permissionPromptToolName != "" {
		args = append(args, "--permission-prompt-tool", t.options.permissionPromptToolName)
	}

	// MCP servers
	if len(t.options.mcpServers) > 0 {
		mcpConfig, err := t.buildMCPConfig()
		if err != nil {
			return nil, fmt.Errorf("buildArgs: %w", err)
		}
		if mcpConfig != "" {
			args = append(args, "--mcp-config", mcpConfig)
		}
	} else if t.options.mcpServersPath != "" {
		args = append(args, "--mcp-config", t.options.mcpServersPath)
	} else if t.options.mcpServersJSON != "" {
		args = append(args, "--mcp-config", t.options.mcpServersJSON)
	}

	// Settings
	settingsValue, err := t.buildSettingsValue()
	if err != nil {
		return nil, fmt.Errorf("buildArgs: %w", err)
	}
	if settingsValue != "" {
		args = append(args, "--settings", settingsValue)
	}

	// Setting sources
	sources := ""
	if t.options.settingSources != nil {
		sources = strings.Join(t.options.settingSources, ",")
	}
	args = append(args, "--setting-sources", sources)

	// Add dirs
	for _, dir := range t.options.addDirs {
		args = append(args, "--add-dir", dir)
	}

	// Include partial messages
	if t.options.includePartialMessages {
		args = append(args, "--include-partial-messages")
	}

	// JSON schema output: outputFormat takes precedence over jsonSchemaOutput
	if t.options.outputFormat != nil {
		if schema, ok := t.options.outputFormat["schema"]; ok {
			schemaJSON, err := json.Marshal(schema)
			if err != nil {
				return nil, fmt.Errorf("marshal output format schema: %w", err)
			}
			args = append(args, "--json-schema", string(schemaJSON))
		}
	} else if t.options.jsonSchemaOutput != nil {
		schemaJSON, err := json.Marshal(t.options.jsonSchemaOutput)
		if err != nil {
			return nil, fmt.Errorf("marshal JSON schema: %w", err)
		}
		args = append(args, "--json-schema", string(schemaJSON))
	}

	// Plugins
	for _, plugin := range t.options.plugins {
		switch plugin.Type {
		case "local":
			args = append(args, "--plugin-dir", plugin.Path)
		default:
			return nil, fmt.Errorf("unsupported plugin type: %q", plugin.Type)
		}
	}

	// Extra args
	for flag, value := range t.options.extraArgs {
		if value == nil {
			// Boolean flag without value
			args = append(args, fmt.Sprintf("--%s", flag))
		} else {
			// Flag with value
			args = append(args, fmt.Sprintf("--%s", flag), *value)
		}
	}

	// Input format for streaming mode
	if t.options.streamingMode {
		args = append(args, "--input-format", "stream-json")
	}

	// Handle print mode - must come last because everything after -- is treated as arguments
	if t.options.printPrompt != nil {
		args = append(args, "--print", "--", *t.options.printPrompt)
	}

	return args, nil
}

func (t *SubprocessTransport) buildMCPConfig() (string, error) {
	servers := make(map[string]any)

	for name, config := range t.options.mcpServers {
		// Serialize each config, omitting the instance field for SDK servers
		serverConfig := make(map[string]any)

		switch c := config.(type) {
		case *MCPStdioConfig:
			serverConfig["type"] = "stdio"
			serverConfig["command"] = c.Command
			if len(c.Args) > 0 {
				serverConfig["args"] = c.Args
			}
			if len(c.Env) > 0 {
				serverConfig["env"] = c.Env
			}
		case *MCPSSEConfig:
			serverConfig["type"] = "sse"
			serverConfig["url"] = c.URL
			if len(c.Headers) > 0 {
				serverConfig["headers"] = c.Headers
			}
		case *MCPHTTPConfig:
			serverConfig["type"] = "http"
			serverConfig["url"] = c.URL
			if len(c.Headers) > 0 {
				serverConfig["headers"] = c.Headers
			}
		case *MCPSDKConfig:
			serverConfig["type"] = "sdk"
			serverConfig["name"] = c.Name
			// Don't include instance - it's for internal use
		default:
			return "", fmt.Errorf("unsupported MCP server config type: %T", config)
		}

		servers[name] = serverConfig
	}

	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return "", fmt.Errorf("marshal MCP config: %w", err)
	}

	return string(data), nil
}

func (t *SubprocessTransport) buildSettingsValue() (string, error) {
	hasSettings := t.options.settings != ""
	hasSandbox := t.options.sandbox != nil

	if !hasSettings && !hasSandbox {
		return "", nil
	}

	// If only settings path and no sandbox, pass through as-is
	if hasSettings && !hasSandbox {
		return t.options.settings, nil
	}

	// Merge settings with sandbox
	var settingsObj map[string]any

	if hasSettings {
		settings := strings.TrimSpace(t.options.settings)
		if strings.HasPrefix(settings, "{") && strings.HasSuffix(settings, "}") {
			// Parse JSON string
			if err := json.Unmarshal([]byte(settings), &settingsObj); err != nil {
				return "", fmt.Errorf("parse settings JSON: %w", err)
			}
		} else {
			// It's a file path - read and parse
			data, err := os.ReadFile(settings)
			if err != nil {
				return "", fmt.Errorf("read settings file %s: %w", settings, err)
			}
			if err := json.Unmarshal(data, &settingsObj); err != nil {
				return "", fmt.Errorf("parse settings file %s: %w", settings, err)
			}
		}
	} else {
		settingsObj = make(map[string]any)
	}

	// Add sandbox
	if hasSandbox {
		settingsObj["sandbox"] = t.options.sandbox
	}

	data, err := json.Marshal(settingsObj)
	if err != nil {
		return "", fmt.Errorf("marshal settings: %w", err)
	}
	return string(data), nil
}

// compareVersions compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	aParts := parseVersion(a)
	bParts := parseVersion(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}

	return 0
}

func parseVersion(v string) [3]int {
	var parts [3]int
	fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
	return parts
}
