// Package claudecode provides a Go SDK for interacting with Claude Code CLI.
//
// This package enables programmatic interaction with Claude Code, including:
//   - One-shot queries via Query() and QuerySync()
//   - Multi-turn conversations via Client
//   - Hook registration for tool interception
//   - Custom tool permission callbacks
//   - In-process MCP server integration
//
// # Quick Start
//
// For simple one-shot queries:
//
//	messages, err := claudecode.QuerySync(ctx, "What is 2+2?")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, msg := range messages {
//	    if am, ok := msg.(*claudecode.AssistantMessage); ok {
//	        fmt.Println(am.GetText())
//	    }
//	}
//
// # Multi-Turn Conversations
//
// For interactive conversations with state:
//
//	client := claudecode.NewClient(
//	    claudecode.WithModel("claude-sonnet-4-5"),
//	    claudecode.WithCWD("/path/to/project"),
//	)
//
//	if err := client.Connect(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close(ctx)
//
//	// Send a query (sessionID "default" is used for single-session conversations)
//	if err := client.Query(ctx, "Help me understand this codebase", "default"); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Receive the response
//	for msg := range client.ReceiveResponse(ctx) {
//	    if msg.Err != nil {
//	        log.Printf("Error: %v", msg.Err)
//	        continue
//	    }
//	    switch m := msg.Message.(type) {
//	    case *claudecode.AssistantMessage:
//	        fmt.Println(m.GetText())
//	    case *claudecode.ResultMessage:
//	        fmt.Printf("Cost: $%.4f\n", *m.TotalCostUSD)
//	    }
//	}
//
// # Hooks
//
// Register hooks to intercept tool execution:
//
//	client := claudecode.NewClient(
//	    claudecode.WithHook(claudecode.HookPreToolUse, claudecode.HookMatcher{
//	        Matcher: "Bash",
//	        Hooks: []claudecode.HookCallback{
//	            func(ctx context.Context, input claudecode.HookInput, toolUseID *string) (claudecode.HookOutput, error) {
//	                // Inspect or modify the tool input
//	                if pi, ok := input.(claudecode.PreToolUseInput); ok {
//	                    fmt.Printf("Bash command: %v\n", pi.ToolInput["command"])
//	                }
//	                return claudecode.NewPreToolUseAllow(), nil
//	            },
//	        },
//	    }),
//	)
//
// # Tool Permissions
//
// Implement custom tool permission logic:
//
//	client := claudecode.NewClient(
//	    claudecode.WithCanUseTool(func(ctx context.Context, toolName string, input map[string]any, permCtx claudecode.ToolPermissionContext) (claudecode.PermissionResult, error) {
//	        if toolName == "Bash" {
//	            // Deny dangerous commands
//	            if cmd, ok := input["command"].(string); ok {
//	                if strings.Contains(cmd, "rm -rf") {
//	                    return claudecode.PermissionDeny{
//	                        Message: "Dangerous command not allowed",
//	                    }, nil
//	                }
//	            }
//	        }
//	        return claudecode.PermissionAllow{}, nil
//	    }),
//	)
//
// # MCP Servers
//
// Integrate in-process MCP servers with chat.Tool implementations:
//
//	client := claudecode.NewClient(
//	    claudecode.WithSDKMCPServer("calculator", myCalculatorTool),
//	    claudecode.WithAllowedTools("mcp__calculator__add"),
//	)
//
// # Configuration Options
//
// The package supports extensive configuration via functional options:
//
//   - WithModel: Set the AI model to use
//   - WithSystemPrompt: Custom system prompt
//   - WithTools: Explicit tool list
//   - WithAllowedTools: Restrict available tools
//   - WithPermissionMode: Set permission behavior
//   - WithMaxTurns: Limit conversation length
//   - WithMaxBudgetUSD: Set spending limit
//   - WithCWD: Set working directory
//   - WithHook: Register hook callbacks
//   - WithCanUseTool: Custom permission callback
//   - WithSDKMCPServer: Add in-process MCP server
//
// See the Option type documentation for the complete list.
package claudecode
