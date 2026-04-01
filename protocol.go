package claudecode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	// DefaultControlTimeout is the default timeout for control requests.
	DefaultControlTimeout = 60 * time.Second
	// DefaultInitializeTimeout is the default timeout for initialize requests.
	DefaultInitializeTimeout = 60 * time.Second
)

// ControlRouter manages the control protocol for bidirectional communication.
type ControlRouter struct {
	mu sync.Mutex

	transport Transport
	options   *Options

	// Pending control requests waiting for responses
	pendingResponses map[string]chan controlResult
	requestCounter   int

	// Hook callback registry
	hookCallbacks  map[string]HookCallback
	nextCallbackID int

	// MCP server registry for SDK servers
	sdkMCPServers map[string]*MCPSDKConfig

	// In-flight control request handlers, keyed by request_id.
	// Each entry holds a cancel function that, when called, cancels the
	// handler's context. Used by control_cancel_request to stop handlers
	// that the CLI has abandoned.
	inflightRequests map[string]context.CancelFunc
	inflightWg       sync.WaitGroup

	// Initialization state
	initialized          bool
	initializationResult map[string]any
}

type controlResult struct {
	response map[string]any
	err      error
}

// NewControlRouter creates a new control router.
func NewControlRouter(transport Transport, opts *Options) *ControlRouter {
	return &ControlRouter{
		transport:        transport,
		options:          opts,
		pendingResponses: make(map[string]chan controlResult),
		hookCallbacks:    make(map[string]HookCallback),
		sdkMCPServers:    opts.sdkMCPServers,
		inflightRequests: make(map[string]context.CancelFunc),
	}
}

// Initialize performs the initialization handshake with the CLI.
func (r *ControlRouter) Initialize(ctx context.Context) (map[string]any, error) {
	if r.initialized {
		return r.initializationResult, nil
	}

	// Build hooks configuration
	hooksConfig := r.buildHooksConfig()

	request := map[string]any{
		"subtype": "initialize",
		"hooks":   hooksConfig,
	}

	if len(r.options.agents) > 0 {
		request["agents"] = r.options.agents
	}

	timeout, err := getEnvDurationWithDefault("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT", DefaultInitializeTimeout)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	response, err := r.sendControlRequest(ctx, request, timeout)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	r.setInitialized(response)
	return response, nil
}

func (r *ControlRouter) setInitialized(result map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initialized = true
	r.initializationResult = result
}

func (r *ControlRouter) buildHooksConfig() map[string]any {
	if len(r.options.hooks) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	hooksConfig := make(map[string]any)

	for event, matchers := range r.options.hooks {
		if len(matchers) == 0 {
			continue
		}

		var matcherConfigs []map[string]any
		for _, matcher := range matchers {
			var callbackIDs []string
			for _, hook := range matcher.Hooks {
				callbackID := fmt.Sprintf("hook_%d", r.nextCallbackID)
				r.nextCallbackID++
				r.hookCallbacks[callbackID] = hook
				callbackIDs = append(callbackIDs, callbackID)
			}

			matcherConfig := map[string]any{
				"hookCallbackIds": callbackIDs,
			}
			// Only include matcher if non-empty; omit to let CLI receive null
			if matcher.Matcher != "" {
				matcherConfig["matcher"] = matcher.Matcher
			}
			if matcher.Timeout > 0 {
				matcherConfig["timeout"] = matcher.Timeout.Seconds()
			}
			matcherConfigs = append(matcherConfigs, matcherConfig)
		}

		hooksConfig[string(event)] = matcherConfigs
	}

	return hooksConfig
}

// HandleMessage processes an incoming message and routes control messages.
// Returns true if the message was a control message and was handled.
func (r *ControlRouter) HandleMessage(ctx context.Context, msg Message, raw []byte) (bool, error) {
	switch m := msg.(type) {
	case *ControlResponse:
		return true, r.handleControlResponse(m, raw)
	case *ControlRequest:
		r.spawnControlRequestHandler(ctx, m, raw)
		return true, nil
	case *ControlCancelRequest:
		r.cancelInflightRequest(m.RequestID)
		return true, nil
	default:
		return false, nil
	}
}

// spawnControlRequestHandler runs handleControlRequest in a goroutine with a
// cancellable context, tracking the request for potential cancellation.
func (r *ControlRouter) spawnControlRequestHandler(ctx context.Context, msg *ControlRequest, raw []byte) {
	reqID := msg.RequestID
	reqCtx, cancel := context.WithCancel(ctx)

	r.mu.Lock()
	r.inflightRequests[reqID] = cancel
	r.mu.Unlock()

	r.inflightWg.Add(1)
	go func() {
		defer r.inflightWg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.inflightRequests, reqID)
			r.mu.Unlock()
			cancel()
		}()
		// handleControlRequest writes the response internally; errors
		// from it are protocol-level (e.g. bad JSON) and not recoverable.
		_ = r.handleControlRequest(reqCtx, msg, raw)
	}()
}

// WaitInflight blocks until all in-flight control request handler goroutines
// have returned. Call this during shutdown after the message loop has ended
// to ensure handler goroutines (especially hook callbacks) have completed or
// been cancelled.
func (r *ControlRouter) WaitInflight() {
	r.inflightWg.Wait()
}

// cancelInflightRequest cancels the handler for the given request_id, if any.
// Unknown request_ids are silently ignored.
func (r *ControlRouter) cancelInflightRequest(reqID string) {
	r.mu.Lock()
	cancel, ok := r.inflightRequests[reqID]
	if ok {
		delete(r.inflightRequests, reqID)
	}
	r.mu.Unlock()

	if ok {
		cancel()
	}
}

func (r *ControlRouter) handleControlResponse(msg *ControlResponse, raw []byte) error {
	var response struct {
		Response struct {
			Subtype   string         `json:"subtype"`
			RequestID string         `json:"request_id"`
			Response  map[string]any `json:"response"`
			Error     string         `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return fmt.Errorf("parse control response: %w", err)
	}

	ch, ok := r.popPendingResponse(response.Response.RequestID)
	if !ok {
		return nil // Unknown request ID, ignore
	}

	if response.Response.Subtype == "error" {
		ch <- controlResult{err: fmt.Errorf("%w: %s", ErrProtocol, response.Response.Error)}
	} else {
		ch <- controlResult{response: response.Response.Response}
	}

	return nil
}

func (r *ControlRouter) handleControlRequest(ctx context.Context, msg *ControlRequest, raw []byte) error {
	var request struct {
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return fmt.Errorf("parse control request: %w", err)
	}

	var responseData map[string]any
	var responseErr error

	switch request.Request.Subtype {
	case "can_use_tool":
		responseData, responseErr = r.handleCanUseTool(ctx, raw)
	case "hook_callback":
		responseData, responseErr = r.handleHookCallback(ctx, raw)
	case "mcp_message":
		responseData, responseErr = r.handleMCPMessage(ctx, raw)
	default:
		responseErr = fmt.Errorf("unsupported control request subtype: %s", request.Request.Subtype)
	}

	// If the context was cancelled (via control_cancel_request), the CLI
	// has already abandoned this request. Don't write a response.
	if ctx.Err() != nil {
		return nil
	}

	return r.sendControlResponse(ctx, request.RequestID, responseData, responseErr)
}

func (r *ControlRouter) handleCanUseTool(ctx context.Context, raw []byte) (map[string]any, error) {
	if r.options.canUseTool == nil {
		return nil, fmt.Errorf("canUseTool callback is not provided")
	}

	var request struct {
		Request struct {
			ToolName              string         `json:"tool_name"`
			Input                 map[string]any `json:"input"`
			PermissionSuggestions []any          `json:"permission_suggestions"`
			BlockedPath           string         `json:"blocked_path"`
			ToolUseID             string         `json:"tool_use_id"`
			AgentID               string         `json:"agent_id"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("parse can_use_tool request: %w", err)
	}

	permCtx := ToolPermissionContext{
		Suggestions: parsePermissionSuggestions(request.Request.PermissionSuggestions),
		BlockedPath: request.Request.BlockedPath,
		ToolUseID:   request.Request.ToolUseID,
		AgentID:     request.Request.AgentID,
	}

	result, err := r.options.canUseTool(ctx, request.Request.ToolName, request.Request.Input, permCtx)
	if err != nil {
		return nil, fmt.Errorf("canUseTool callback error: %w", err)
	}

	switch res := result.(type) {
	case PermissionAllow:
		responseData := map[string]any{
			"behavior": "allow",
		}
		if res.UpdatedInput != nil {
			responseData["updatedInput"] = res.UpdatedInput
		} else {
			responseData["updatedInput"] = request.Request.Input
		}
		if len(res.UpdatedPermissions) > 0 {
			perms := make([]map[string]any, len(res.UpdatedPermissions))
			for i, p := range res.UpdatedPermissions {
				perms[i] = p.ToMap()
			}
			responseData["updatedPermissions"] = perms
		}
		return responseData, nil

	case PermissionDeny:
		responseData := map[string]any{
			"behavior": "deny",
			"message":  res.Message,
		}
		if res.Interrupt {
			responseData["interrupt"] = true
		}
		return responseData, nil

	default:
		return nil, fmt.Errorf("invalid permission result type: %T", result)
	}
}

func (r *ControlRouter) handleHookCallback(ctx context.Context, raw []byte) (map[string]any, error) {
	var request struct {
		Request struct {
			CallbackID string         `json:"callback_id"`
			Input      map[string]any `json:"input"`
			ToolUseID  *string        `json:"tool_use_id"` // pointer to handle null
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("parse hook_callback request: %w", err)
	}

	callback, ok := r.getHookCallback(request.Request.CallbackID)
	if !ok {
		return nil, fmt.Errorf("no hook callback found for ID: %s", request.Request.CallbackID)
	}

	// Parse input into the appropriate HookInput type
	hookInput := parseHookInput(request.Request.Input)

	// Pass the pointer directly - nil for null, non-nil for string
	output, err := callback(ctx, hookInput, request.Request.ToolUseID)
	if err != nil {
		return nil, fmt.Errorf("hook callback error: %w", err)
	}

	// Convert HookOutput to map
	responseData := map[string]any{}
	if output.Async {
		responseData["async"] = true
		if output.AsyncTimeout > 0 {
			responseData["asyncTimeout"] = output.AsyncTimeout
		}
	} else {
		// The Claude Code CLI protocol specification states that the "continue" field
		// must be sent in responses, with a default semantic of true when omitted from
		// the HookOutput. This is protocol-specified behavior, not a fallback.
		// See: HookOutput documentation for the three-state semantics of this field.
		continueVal := true
		if output.Continue != nil {
			continueVal = *output.Continue
		}
		responseData["continue"] = continueVal

		if output.SuppressOutput {
			responseData["suppressOutput"] = true
		}
		if output.StopReason != "" {
			responseData["stopReason"] = output.StopReason
		}
		if output.Decision != "" {
			responseData["decision"] = output.Decision
		}
		if output.SystemMessage != "" {
			responseData["systemMessage"] = output.SystemMessage
		}
		if output.Reason != "" {
			responseData["reason"] = output.Reason
		}
		if output.HookSpecificOutput != nil {
			responseData["hookSpecificOutput"] = output.HookSpecificOutput
		}
	}

	return responseData, nil
}

func parseHookInput(input map[string]any) HookInput {
	eventName, _ := input["hook_event_name"].(string)

	base := BaseHookInput{
		HookEventName:  eventName,
		SessionID:      getString(input, "session_id"),
		TranscriptPath: getString(input, "transcript_path"),
		CWD:            getString(input, "cwd"),
		PermissionMode: getString(input, "permission_mode"),
	}

	switch eventName {
	case "PreToolUse":
		return PreToolUseInput{
			BaseHookInput: base,
			ToolName:      getString(input, "tool_name"),
			ToolInput:     getMap(input, "tool_input"),
			ToolUseID:     getString(input, "tool_use_id"),
			AgentID:       getStringPtr(input, "agent_id"),
			AgentType:     getStringPtr(input, "agent_type"),
		}
	case "PostToolUse":
		return PostToolUseInput{
			BaseHookInput: base,
			ToolName:      getString(input, "tool_name"),
			ToolInput:     getMap(input, "tool_input"),
			ToolResponse:  input["tool_response"],
			ToolUseID:     getString(input, "tool_use_id"),
			AgentID:       getStringPtr(input, "agent_id"),
			AgentType:     getStringPtr(input, "agent_type"),
		}
	case "UserPromptSubmit":
		return UserPromptSubmitInput{
			BaseHookInput: base,
			Prompt:        getString(input, "prompt"),
		}
	case "Stop":
		return StopInput{
			BaseHookInput:  base,
			StopHookActive: getBool(input, "stop_hook_active"),
		}
	case "PostToolUseFailure":
		return PostToolUseFailureInput{
			BaseHookInput: base,
			ToolName:      getString(input, "tool_name"),
			ToolInput:     getMap(input, "tool_input"),
			ToolUseID:     getString(input, "tool_use_id"),
			Error:         getString(input, "error"),
			IsInterrupt:   getBool(input, "is_interrupt"),
			AgentID:       getStringPtr(input, "agent_id"),
			AgentType:     getStringPtr(input, "agent_type"),
		}
	case "Notification":
		return NotificationInput{
			BaseHookInput:    base,
			Message:          getString(input, "message"),
			Title:            getString(input, "title"),
			NotificationType: getString(input, "notification_type"),
		}
	case "SubagentStop":
		return SubagentStopInput{
			BaseHookInput:       base,
			StopHookActive:      getBool(input, "stop_hook_active"),
			AgentID:             getString(input, "agent_id"),
			AgentTranscriptPath: getString(input, "agent_transcript_path"),
			AgentType:           getString(input, "agent_type"),
		}
	case "SubagentStart":
		return SubagentStartInput{
			BaseHookInput: base,
			AgentID:       getString(input, "agent_id"),
			AgentType:     getString(input, "agent_type"),
		}
	case "PermissionRequest":
		return PermissionRequestInput{
			BaseHookInput:         base,
			ToolName:              getString(input, "tool_name"),
			ToolInput:             getMap(input, "tool_input"),
			PermissionSuggestions: getSlice(input, "permission_suggestions"),
			AgentID:               getStringPtr(input, "agent_id"),
			AgentType:             getStringPtr(input, "agent_type"),
		}
	case "PreCompact":
		return PreCompactInput{
			BaseHookInput:      base,
			Trigger:            getString(input, "trigger"),
			CustomInstructions: getString(input, "custom_instructions"),
		}
	default:
		return base
	}
}

func getStringPtr(m map[string]any, key string) *string {
	if v, ok := m[key].(string); ok {
		return &v
	}
	return nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func getSlice(m map[string]any, key string) []any {
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

func (r *ControlRouter) handleMCPMessage(ctx context.Context, raw []byte) (map[string]any, error) {
	var request struct {
		Request struct {
			ServerName string         `json:"server_name"`
			Message    map[string]any `json:"message"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("parse mcp_message request: %w", err)
	}

	server, ok := r.getMCPServer(request.Request.ServerName)
	if !ok {
		return map[string]any{
			"mcp_response": map[string]any{
				"jsonrpc": "2.0",
				"id":      request.Request.Message["id"],
				"error": map[string]any{
					"code":    -32601,
					"message": fmt.Sprintf("Server '%s' not found", request.Request.ServerName),
				},
			},
		}, nil
	}

	mcpResponse := handleSDKMCPRequest(ctx, server, request.Request.Message)
	return map[string]any{"mcp_response": mcpResponse}, nil
}

func (r *ControlRouter) sendControlResponse(ctx context.Context, requestID string, responseData map[string]any, responseErr error) error {
	var response map[string]any

	if responseErr != nil {
		response = map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "error",
				"request_id": requestID,
				"error":      responseErr.Error(),
			},
		}
	} else {
		response = map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response":   responseData,
			},
		}
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal control response: %w", err)
	}

	return r.transport.Write(ctx, string(data)+"\n")
}

// sendControlRequest sends a control request and waits for the response.
func (r *ControlRouter) sendControlRequest(ctx context.Context, request map[string]any, timeout time.Duration) (map[string]any, error) {
	requestID := r.nextRequestID()

	ch := make(chan controlResult, 1)
	r.registerPendingResponse(requestID, ch)
	defer r.deletePendingResponse(requestID)

	controlRequest := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    request,
	}

	data, err := json.Marshal(controlRequest)
	if err != nil {
		return nil, fmt.Errorf("marshal control request: %w", err)
	}

	if err := r.transport.Write(ctx, string(data)+"\n"); err != nil {
		return nil, fmt.Errorf("write control request: %w", err)
	}

	select {
	case result := <-ch:
		if result.err != nil {
			return nil, result.err
		}
		return result.response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("%w: control request timeout for %s", ErrTimeout, request["subtype"])
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *ControlRouter) nextRequestID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestCounter++
	randBytes := make([]byte, 4)
	rand.Read(randBytes)
	return fmt.Sprintf("req_%d_%s", r.requestCounter, hex.EncodeToString(randBytes))
}

// Interrupt sends an interrupt control request.
func (r *ControlRouter) Interrupt(ctx context.Context) error {
	_, err := r.sendControlRequest(ctx, map[string]any{"subtype": "interrupt"}, DefaultControlTimeout)
	return err
}

// SetPermissionMode changes the permission mode.
func (r *ControlRouter) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	_, err := r.sendControlRequest(ctx, map[string]any{
		"subtype": "set_permission_mode",
		"mode":    string(mode),
	}, DefaultControlTimeout)
	return err
}

// SetModel changes the model.
func (r *ControlRouter) SetModel(ctx context.Context, model string) error {
	request := map[string]any{"subtype": "set_model"}
	if model != "" {
		request["model"] = model
	}
	_, err := r.sendControlRequest(ctx, request, DefaultControlTimeout)
	return err
}

// GetMCPStatus returns the MCP server connection status.
func (r *ControlRouter) GetMCPStatus(ctx context.Context) (McpStatusResponse, error) {
	raw, err := r.sendControlRequest(ctx, map[string]any{"subtype": "mcp_status"}, DefaultControlTimeout)
	if err != nil {
		return McpStatusResponse{}, err
	}

	// Re-marshal the raw response map and decode into the typed struct.
	data, err := json.Marshal(raw)
	if err != nil {
		return McpStatusResponse{}, fmt.Errorf("marshal mcp_status response: %w", err)
	}

	var resp McpStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return McpStatusResponse{}, fmt.Errorf("decode mcp_status response: %w", err)
	}
	return resp, nil
}

// GetContextUsage returns a breakdown of current context window usage by category.
func (r *ControlRouter) GetContextUsage(ctx context.Context) (ContextUsageResponse, error) {
	raw, err := r.sendControlRequest(ctx, map[string]any{"subtype": "get_context_usage"}, DefaultControlTimeout)
	if err != nil {
		return ContextUsageResponse{}, err
	}

	// Re-marshal the raw response map and decode into the typed struct.
	data, err := json.Marshal(raw)
	if err != nil {
		return ContextUsageResponse{}, fmt.Errorf("marshal get_context_usage response: %w", err)
	}

	var resp ContextUsageResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return ContextUsageResponse{}, fmt.Errorf("decode get_context_usage response: %w", err)
	}
	return resp, nil
}

// ReconnectMCPServer sends a control request to reconnect a disconnected or
// failed MCP server.
func (r *ControlRouter) ReconnectMCPServer(ctx context.Context, serverName string) error {
	_, err := r.sendControlRequest(ctx, map[string]any{
		"subtype":    "mcp_reconnect",
		"serverName": serverName,
	}, DefaultControlTimeout)
	return err
}

// ToggleMCPServer sends a control request to enable or disable an MCP server.
func (r *ControlRouter) ToggleMCPServer(ctx context.Context, serverName string, enabled bool) error {
	_, err := r.sendControlRequest(ctx, map[string]any{
		"subtype":    "mcp_toggle",
		"serverName": serverName,
		"enabled":    enabled,
	}, DefaultControlTimeout)
	return err
}

// StopTask sends a control request to stop a running task.
func (r *ControlRouter) StopTask(ctx context.Context, taskID string) error {
	_, err := r.sendControlRequest(ctx, map[string]any{
		"subtype": "stop_task",
		"task_id": taskID,
	}, DefaultControlTimeout)
	return err
}

// RewindFiles rewinds tracked files to a specific user message.
func (r *ControlRouter) RewindFiles(ctx context.Context, userMessageUUID string) error {
	_, err := r.sendControlRequest(ctx, map[string]any{
		"subtype":         "rewind_files",
		"user_message_id": userMessageUUID,
	}, DefaultControlTimeout)
	return err
}

// GetServerInfo returns the cached initialization result.
func (r *ControlRouter) GetServerInfo() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.initializationResult
}

func (r *ControlRouter) popPendingResponse(id string) (chan controlResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.pendingResponses[id]
	if ok {
		delete(r.pendingResponses, id)
	}
	return ch, ok
}

func (r *ControlRouter) registerPendingResponse(id string, ch chan controlResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingResponses[id] = ch
}

func (r *ControlRouter) deletePendingResponse(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pendingResponses, id)
}

func (r *ControlRouter) getHookCallback(id string) (HookCallback, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cb, ok := r.hookCallbacks[id]
	return cb, ok
}

func (r *ControlRouter) getMCPServer(name string) (*MCPSDKConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	server, ok := r.sdkMCPServers[name]
	return server, ok
}

func getEnvDuration(key string) (time.Duration, error) {
	val := os.Getenv(key)
	if val == "" {
		return 0, nil
	}
	// Value is in milliseconds
	ms, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration from env %s: %w", key, err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// getEnvDurationWithDefault returns the env value if set, otherwise the default.
// If the environment variable is set but invalid, returns an error.
// Unlike the Python SDK, this function honors user-specified values even if
// they're smaller than the default.
func getEnvDurationWithDefault(key string, defaultVal time.Duration) (time.Duration, error) {
	d, err := getEnvDuration(key)
	if err != nil {
		return 0, err
	}
	if d == 0 {
		return defaultVal, nil
	}
	return d, nil
}
