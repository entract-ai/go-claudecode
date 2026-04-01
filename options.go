package claudecode

import (
	"context"

	"github.com/bpowers/go-claudecode/chat"
	"github.com/bpowers/go-claudecode/sandbox"
)

// PermissionMode represents the permission mode for Claude Code.
type PermissionMode string

const (
	PermissionDefault           PermissionMode = "default"
	PermissionAcceptEdits       PermissionMode = "acceptEdits"
	PermissionPlan              PermissionMode = "plan"
	PermissionBypassPermissions PermissionMode = "bypassPermissions"
)

// Beta represents a beta feature flag.
type Beta string

// SettingSource represents a settings source.
type SettingSource string

const (
	SettingSourceUser    SettingSource = "user"
	SettingSourceProject SettingSource = "project"
	SettingSourceLocal   SettingSource = "local"
)

// AgentDefinition defines a custom agent.
type AgentDefinition struct {
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools,omitzero"`
	Model       string   `json:"model,omitzero"` // "sonnet", "opus", "haiku", "inherit"
}

// SandboxSettings configures the sandbox for bash commands.
type SandboxSettings struct {
	Enabled                   bool                     `json:"enabled,omitzero"`
	AutoAllowBashIfSandboxed  bool                     `json:"autoAllowBashIfSandboxed,omitzero"`
	ExcludedCommands          []string                 `json:"excludedCommands,omitzero"`
	AllowUnsandboxedCommands  bool                     `json:"allowUnsandboxedCommands,omitzero"`
	Network                   *SandboxNetworkConfig    `json:"network,omitzero"`
	IgnoreViolations          *SandboxIgnoreViolations `json:"ignoreViolations,omitzero"`
	EnableWeakerNestedSandbox bool                     `json:"enableWeakerNestedSandbox,omitzero"`
}

// SandboxNetworkConfig configures network access in sandbox.
type SandboxNetworkConfig struct {
	AllowUnixSockets    []string `json:"allowUnixSockets,omitzero"`
	AllowAllUnixSockets bool     `json:"allowAllUnixSockets,omitzero"`
	AllowLocalBinding   bool     `json:"allowLocalBinding,omitzero"`
	HTTPProxyPort       int      `json:"httpProxyPort,omitzero"`
	SOCKSProxyPort      int      `json:"socksProxyPort,omitzero"`
}

// SandboxIgnoreViolations configures violations to ignore.
type SandboxIgnoreViolations struct {
	File    []string `json:"file,omitzero"`
	Network []string `json:"network,omitzero"`
}

// PluginConfig configures a plugin.
type PluginConfig struct {
	Type string `json:"type"` // "local"
	Path string `json:"path"`
}

// MCPServerConfig is a marker interface for MCP server configurations.
type MCPServerConfig interface {
	mcpServerConfigMarker()
}

// MCPStdioConfig configures an MCP server via stdio.
type MCPStdioConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitzero"`
	Env     map[string]string `json:"env,omitzero"`
}

func (*MCPStdioConfig) mcpServerConfigMarker() {}

// MCPSSEConfig configures an MCP server via SSE.
type MCPSSEConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitzero"`
}

func (*MCPSSEConfig) mcpServerConfigMarker() {}

// MCPHTTPConfig configures an MCP server via HTTP.
type MCPHTTPConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitzero"`
}

func (*MCPHTTPConfig) mcpServerConfigMarker() {}

// MCPSDKConfig configures an in-process MCP server using chat.Tool instances.
type MCPSDKConfig struct {
	Name  string
	Tools []chat.Tool
}

func (*MCPSDKConfig) mcpServerConfigMarker() {}

// McpServerConnectionStatus represents the connection status of an MCP server.
type McpServerConnectionStatus string

const (
	McpStatusConnected McpServerConnectionStatus = "connected"
	McpStatusFailed    McpServerConnectionStatus = "failed"
	McpStatusNeedsAuth McpServerConnectionStatus = "needs-auth"
	McpStatusPending   McpServerConnectionStatus = "pending"
	McpStatusDisabled  McpServerConnectionStatus = "disabled"
)

// McpServerInfo contains server info from the MCP initialize handshake.
// Available when the server status is "connected".
type McpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// McpToolAnnotations describes tool behavior annotations returned in MCP
// server status. Wire format uses camelCase field names.
type McpToolAnnotations struct {
	ReadOnly    *bool `json:"readOnly,omitempty"`
	Destructive *bool `json:"destructive,omitempty"`
	OpenWorld   *bool `json:"openWorld,omitempty"`
}

// McpToolInfo describes a tool provided by an MCP server.
type McpToolInfo struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Annotations *McpToolAnnotations `json:"annotations,omitempty"`
}

// McpServerStatus contains the status of an MCP server connection.
// Returned by Client.GetMCPStatus in the McpServers list.
type McpServerStatus struct {
	Name       string                    `json:"name"`
	Status     McpServerConnectionStatus `json:"status"`
	ServerInfo *McpServerInfo            `json:"serverInfo,omitempty"`
	Error      string                    `json:"error,omitempty"`
	Config     map[string]any            `json:"config,omitempty"`
	Scope      string                    `json:"scope,omitempty"`
	Tools      []McpToolInfo             `json:"tools,omitempty"`
}

// McpStatusResponse is the response from Client.GetMCPStatus.
// It wraps the list of server statuses under the McpServers key, matching
// the wire-format response shape.
type McpStatusResponse struct {
	McpServers []McpServerStatus `json:"mcpServers"`
}

// CanUseToolFunc is the callback type for tool permission decisions.
type CanUseToolFunc func(ctx context.Context, toolName string, input map[string]any, permCtx ToolPermissionContext) (PermissionResult, error)

// ToolPermissionContext provides context for permission decisions.
type ToolPermissionContext struct {
	Suggestions []PermissionUpdate
	BlockedPath string // path that triggered the permission check
}

// PermissionResult is a marker interface for permission decisions.
type PermissionResult interface {
	permissionResultMarker()
}

// PermissionAllow allows tool execution, optionally with modifications.
type PermissionAllow struct {
	UpdatedInput       map[string]any
	UpdatedPermissions []PermissionUpdate
}

func (PermissionAllow) permissionResultMarker() {}

// PermissionDeny denies tool execution.
type PermissionDeny struct {
	Message   string
	Interrupt bool
}

func (PermissionDeny) permissionResultMarker() {}

// PermissionUpdate represents a permission update request.
type PermissionUpdate struct {
	Type        string // "addRules", "replaceRules", "removeRules", "setMode", "addDirectories", "removeDirectories"
	Rules       []PermissionRuleValue
	Behavior    string // "allow", "deny", "ask"
	Mode        PermissionMode
	Directories []string
	Destination string // "userSettings", "projectSettings", "localSettings", "session"
}

// PermissionRuleValue represents a permission rule.
type PermissionRuleValue struct {
	ToolName    string
	RuleContent string
}

// ToMap converts PermissionUpdate to a map with correct camelCase JSON keys.
func (p PermissionUpdate) ToMap() map[string]any {
	result := map[string]any{"type": p.Type}
	if p.Destination != "" {
		result["destination"] = p.Destination
	}
	// Rules-based variants
	if p.Type == "addRules" || p.Type == "replaceRules" || p.Type == "removeRules" {
		if len(p.Rules) > 0 {
			rules := make([]map[string]any, len(p.Rules))
			for i, r := range p.Rules {
				rules[i] = map[string]any{
					"toolName":    r.ToolName,
					"ruleContent": r.RuleContent,
				}
			}
			result["rules"] = rules
		}
		if p.Behavior != "" {
			result["behavior"] = p.Behavior
		}
	} else if p.Type == "setMode" {
		if p.Mode != "" {
			result["mode"] = p.Mode
		}
	} else if p.Type == "addDirectories" || p.Type == "removeDirectories" {
		if len(p.Directories) > 0 {
			result["directories"] = p.Directories
		}
	}
	return result
}

// parsePermissionSuggestions parses raw permission suggestions from the CLI.
func parsePermissionSuggestions(raw []any) []PermissionUpdate {
	if len(raw) == 0 {
		return nil
	}
	suggestions := make([]PermissionUpdate, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pu := PermissionUpdate{
			Type:        getStringFromMap(m, "type"),
			Behavior:    getStringFromMap(m, "behavior"),
			Destination: getStringFromMap(m, "destination"),
		}
		if modeStr := getStringFromMap(m, "mode"); modeStr != "" {
			pu.Mode = PermissionMode(modeStr)
		}
		if rules, ok := m["rules"].([]any); ok {
			for _, r := range rules {
				rm, ok := r.(map[string]any)
				if !ok {
					continue
				}
				pu.Rules = append(pu.Rules, PermissionRuleValue{
					ToolName:    getStringFromMap(rm, "toolName"),
					RuleContent: getStringFromMap(rm, "ruleContent"),
				})
			}
		}
		if dirs, ok := m["directories"].([]any); ok {
			for _, d := range dirs {
				if s, ok := d.(string); ok {
					pu.Directories = append(pu.Directories, s)
				}
			}
		}
		suggestions = append(suggestions, pu)
	}
	return suggestions
}

func getStringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// ThinkingConfig configures the model's thinking behavior.
type ThinkingConfig interface {
	thinkingConfigMarker()
}

// ThinkingAdaptive lets the model decide when to think.
type ThinkingAdaptive struct{}

func (ThinkingAdaptive) thinkingConfigMarker() {}

// ThinkingEnabled enables thinking with a token budget.
type ThinkingEnabled struct {
	BudgetTokens int
}

func (ThinkingEnabled) thinkingConfigMarker() {}

// ThinkingDisabled disables thinking.
type ThinkingDisabled struct{}

func (ThinkingDisabled) thinkingConfigMarker() {}

// Options holds all configuration for a Claude Code session.
type Options struct {
	// Tool configuration
	tools           *[]string // nil = not set, empty slice = --tools ""
	toolsPreset     bool      // --tools default
	allowedTools    []string
	disallowedTools []string

	// System prompt
	systemPrompt       *string // nil = use --system-prompt ""
	systemPromptAppend string  // --append-system-prompt

	// MCP servers
	mcpServers     map[string]MCPServerConfig
	mcpServersPath string
	mcpServersJSON string
	sdkMCPServers  map[string]*MCPSDKConfig

	// Permission
	permissionMode           PermissionMode
	permissionPromptToolName string
	canUseTool               CanUseToolFunc

	// Session
	continueConversation bool
	resume               string
	forkSession          bool

	// Limits
	maxTurns          int
	maxBudgetUSD      float64
	maxThinkingTokens *int

	// Model
	model         string
	fallbackModel string
	betas         []string
	thinking      ThinkingConfig
	effort        string

	// Output
	jsonSchemaOutput       map[string]any
	outputFormat           map[string]any
	includePartialMessages bool

	// Environment
	cwd            string
	cliPath        string
	env            map[string]string
	addDirs        []string
	settings       string
	settingSources []string

	// Extra args
	extraArgs map[string]*string

	// Hooks
	hooks map[HookEvent][]HookMatcher

	// Agents
	agents map[string]AgentDefinition

	// Sandbox
	sandbox *SandboxSettings

	// OS-level sandbox policy (distinct from CLI's SandboxSettings)
	osSandboxPolicy *sandbox.Policy

	// Plugins
	plugins []PluginConfig

	// Misc
	enableFileCheckpointing bool
	stderrCallback          func(string)
	streamingMode           bool

}

// Option is a functional option for configuring a Claude Code session.
type Option func(*Options)

// WithTools sets explicit tool list. Empty list means --tools "".
func WithTools(tools ...string) Option {
	return func(o *Options) {
		o.tools = &tools
	}
}

// WithToolsPreset uses the default tools preset (--tools default).
func WithToolsPreset() Option {
	return func(o *Options) {
		o.toolsPreset = true
	}
}

// WithAllowedTools sets the allowed tools.
func WithAllowedTools(tools ...string) Option {
	return func(o *Options) {
		o.allowedTools = tools
	}
}

// WithDisallowedTools sets the disallowed tools.
func WithDisallowedTools(tools ...string) Option {
	return func(o *Options) {
		o.disallowedTools = tools
	}
}

// WithSystemPrompt sets a custom system prompt.
func WithSystemPrompt(prompt string) Option {
	return func(o *Options) {
		o.systemPrompt = &prompt
	}
}

// WithSystemPromptPreset uses the default system prompt and appends additional text.
func WithSystemPromptPreset(append string) Option {
	return func(o *Options) {
		o.systemPromptAppend = append
	}
}

// WithMCPServer adds an MCP server configuration.
func WithMCPServer(name string, config MCPServerConfig) Option {
	return func(o *Options) {
		if o.mcpServers == nil {
			o.mcpServers = make(map[string]MCPServerConfig)
		}
		o.mcpServers[name] = config
	}
}

// WithMCPServersPath sets the path to an MCP servers JSON file.
func WithMCPServersPath(path string) Option {
	return func(o *Options) {
		o.mcpServersPath = path
	}
}

// WithMCPServersJSON sets the MCP servers as a JSON string.
func WithMCPServersJSON(jsonStr string) Option {
	return func(o *Options) {
		o.mcpServersJSON = jsonStr
	}
}

// WithSDKMCPServer adds an in-process MCP server from chat.Tool instances.
func WithSDKMCPServer(name string, tools ...chat.Tool) Option {
	return func(o *Options) {
		if o.sdkMCPServers == nil {
			o.sdkMCPServers = make(map[string]*MCPSDKConfig)
		}
		o.sdkMCPServers[name] = &MCPSDKConfig{
			Name:  name,
			Tools: tools,
		}
		// Also add to mcpServers for CLI serialization
		if o.mcpServers == nil {
			o.mcpServers = make(map[string]MCPServerConfig)
		}
		o.mcpServers[name] = o.sdkMCPServers[name]
	}
}

// WithPermissionMode sets the permission mode.
func WithPermissionMode(mode PermissionMode) Option {
	return func(o *Options) {
		o.permissionMode = mode
	}
}

// WithPermissionPromptToolName sets the permission prompt tool name.
func WithPermissionPromptToolName(name string) Option {
	return func(o *Options) {
		o.permissionPromptToolName = name
	}
}

// WithCanUseTool sets the tool permission callback (requires streaming mode).
func WithCanUseTool(fn CanUseToolFunc) Option {
	return func(o *Options) {
		o.canUseTool = fn
	}
}

// WithContinueConversation enables continuing the previous conversation.
func WithContinueConversation() Option {
	return func(o *Options) {
		o.continueConversation = true
	}
}

// WithResume resumes a specific session by ID.
func WithResume(sessionID string) Option {
	return func(o *Options) {
		o.resume = sessionID
	}
}

// WithForkSession forks the resumed session instead of continuing it.
func WithForkSession() Option {
	return func(o *Options) {
		o.forkSession = true
	}
}

// WithMaxTurns sets the maximum number of conversation turns.
func WithMaxTurns(turns int) Option {
	return func(o *Options) {
		o.maxTurns = turns
	}
}

// WithMaxBudgetUSD sets the maximum budget in USD.
func WithMaxBudgetUSD(budget float64) Option {
	return func(o *Options) {
		o.maxBudgetUSD = budget
	}
}

// WithMaxThinkingTokens sets the maximum thinking tokens.
func WithMaxThinkingTokens(tokens int) Option {
	return func(o *Options) {
		o.maxThinkingTokens = &tokens
	}
}

// WithThinking configures the model's thinking behavior.
// When set, this takes precedence over WithMaxThinkingTokens.
func WithThinking(config ThinkingConfig) Option {
	return func(o *Options) {
		o.thinking = config
	}
}

// WithEffort sets the effort level ("low", "medium", "high", "max").
func WithEffort(level string) Option {
	return func(o *Options) {
		o.effort = level
	}
}

// WithOutputFormat sets the output format configuration.
// The format should follow the structure {"type":"json_schema","schema":{...}}.
// When set, this takes precedence over WithJSONSchemaOutput for the CLI arg.
func WithOutputFormat(format map[string]any) Option {
	return func(o *Options) {
		o.outputFormat = format
	}
}

// WithModel sets the model to use.
func WithModel(model string) Option {
	return func(o *Options) {
		o.model = model
	}
}

// WithFallbackModel sets the fallback model.
func WithFallbackModel(model string) Option {
	return func(o *Options) {
		o.fallbackModel = model
	}
}

// WithBetas enables beta features.
func WithBetas(betas ...Beta) Option {
	return func(o *Options) {
		for _, b := range betas {
			o.betas = append(o.betas, string(b))
		}
	}
}

// WithJSONSchemaOutput sets the JSON schema for structured output.
func WithJSONSchemaOutput(schema map[string]any) Option {
	return func(o *Options) {
		o.jsonSchemaOutput = schema
	}
}

// WithIncludePartialMessages includes partial messages in the stream.
func WithIncludePartialMessages() Option {
	return func(o *Options) {
		o.includePartialMessages = true
	}
}

// WithCWD sets the working directory.
func WithCWD(cwd string) Option {
	return func(o *Options) {
		o.cwd = cwd
	}
}

// WithCLIPath sets the path to the Claude CLI.
func WithCLIPath(path string) Option {
	return func(o *Options) {
		o.cliPath = path
	}
}

// WithEnv adds an environment variable.
func WithEnv(key, value string) Option {
	return func(o *Options) {
		if o.env == nil {
			o.env = make(map[string]string)
		}
		o.env[key] = value
	}
}

// WithAddDirs adds directories to the allowed paths.
func WithAddDirs(dirs ...string) Option {
	return func(o *Options) {
		o.addDirs = append(o.addDirs, dirs...)
	}
}

// WithSettings sets the settings JSON string or file path.
func WithSettings(settings string) Option {
	return func(o *Options) {
		o.settings = settings
	}
}

// WithSettingSources sets which setting sources to load.
func WithSettingSources(sources ...SettingSource) Option {
	return func(o *Options) {
		for _, s := range sources {
			o.settingSources = append(o.settingSources, string(s))
		}
	}
}

// WithExtraArg adds an extra CLI argument.
// value=nil means --flag, value="" means --flag "", value="x" means --flag x.
func WithExtraArg(flag string, value *string) Option {
	return func(o *Options) {
		if o.extraArgs == nil {
			o.extraArgs = make(map[string]*string)
		}
		o.extraArgs[flag] = value
	}
}

// WithHook registers a hook for the given event.
func WithHook(event HookEvent, matcher HookMatcher) Option {
	return func(o *Options) {
		if o.hooks == nil {
			o.hooks = make(map[HookEvent][]HookMatcher)
		}
		o.hooks[event] = append(o.hooks[event], matcher)
	}
}

// WithAgent adds a custom agent definition.
func WithAgent(name string, def AgentDefinition) Option {
	return func(o *Options) {
		if o.agents == nil {
			o.agents = make(map[string]AgentDefinition)
		}
		o.agents[name] = def
	}
}

// WithSandbox configures sandbox settings.
func WithSandbox(settings SandboxSettings) Option {
	return func(o *Options) {
		o.sandbox = &settings
	}
}

// WithOSSandboxPolicy sets an OS-level sandbox policy for the Claude Code CLI process.
// This restricts the CLI's filesystem access at the operating system level.
func WithOSSandboxPolicy(policy *sandbox.Policy) Option {
	return func(o *Options) {
		o.osSandboxPolicy = policy
	}
}

// WithPlugin adds a plugin configuration.
func WithPlugin(config PluginConfig) Option {
	return func(o *Options) {
		o.plugins = append(o.plugins, config)
	}
}

// WithEnableFileCheckpointing enables file change tracking.
func WithEnableFileCheckpointing() Option {
	return func(o *Options) {
		o.enableFileCheckpointing = true
	}
}

// WithStderr sets a callback for stderr output from the CLI.
// By default, stderr is written to os.Stderr. Use this to handle it differently.
func WithStderr(fn func(string)) Option {
	return func(o *Options) {
		o.stderrCallback = fn
	}
}

// WithDiscardStderr discards all stderr output from the CLI.
// By default, stderr is written to os.Stderr.
func WithDiscardStderr() Option {
	return func(o *Options) {
		o.stderrCallback = func(string) {} // no-op callback
	}
}

// applyOptions applies all options and returns the resulting Options struct.
func applyOptions(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
