# go-claudecode

A native Go SDK for programmatic interaction with the [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code).

This is a Go port of the official [claude-agent-sdk (Python)](https://github.com/anthropics/claude-agent-sdk), with built-in OS-level process sandboxing inspired by Anthropic's [sandbox-runtime](https://github.com/anthropics/sandbox-runtime).

## Requirements

- Go 1.25+
- Claude Code CLI (`claude`) installed and authenticated

## Installation

```bash
go get github.com/bpowers/go-claudecode
```

## Quick start

### One-shot query

```go
messages, err := claudecode.QuerySync(ctx, "What is 2+2?")
if err != nil {
    log.Fatal(err)
}
for _, msg := range messages {
    if am, ok := msg.(*claudecode.AssistantMessage); ok {
        fmt.Println(am.GetText())
    }
}
```

### Multi-turn conversation

```go
client := claudecode.NewClient(
    claudecode.WithModel("claude-sonnet-4-5"),
    claudecode.WithCWD("/path/to/project"),
)

if err := client.Connect(ctx); err != nil {
    log.Fatal(err)
}
defer client.Close(ctx)

if err := client.Query(ctx, "Help me understand this codebase", "default"); err != nil {
    log.Fatal(err)
}

for msg := range client.ReceiveResponse(ctx) {
    if msg.Err != nil {
        log.Printf("Error: %v", msg.Err)
        continue
    }
    switch m := msg.Message.(type) {
    case *claudecode.AssistantMessage:
        fmt.Println(m.GetText())
    case *claudecode.ResultMessage:
        fmt.Printf("Cost: $%.4f\n", *m.TotalCostUSD)
    }
}
```

### OS-level sandboxing

```go
policy := sandbox.DefaultPolicy()
policy.ReadWriteMounts = append(policy.ReadWriteMounts,
    sandbox.Mount{Source: "/my/project", Target: "/my/project"})
policy.AllowNetwork = false

cmd, err := policy.Command(ctx, "claude", "--print", "Hello")
if err != nil {
    log.Fatal(err)
}
output, err := cmd.CombinedOutput()
```

## Supported features

### SDK (package `claudecode`)

Feature parity with the Python `claude-agent-sdk`:

- **One-shot queries** -- `Query()`, `QuerySync()` for simple request/response
- **Streaming queries** -- `QueryWithInput()` with full bidirectional control protocol
- **Multi-turn client** -- `Client` for stateful, interactive conversations
- **Custom tools** -- In-process MCP servers via `chat.Tool` interface and `WithSDKMCPServer()`
- **External MCP servers** -- stdio, SSE, and HTTP MCP server connections
- **Hooks** -- PreToolUse, PostToolUse, UserPromptSubmit, Stop, SubagentStop, PreCompact
- **Tool permissions** -- `WithCanUseTool()` callback for allow/deny/modify decisions
- **Subagents** -- `WithAgent()` for custom agent definitions
- **Session management** -- Continue, resume, and fork sessions
- **Transcript parsing** -- Read Claude Code's JSONL transcripts back as typed messages
- **Full configuration** -- Model selection, system prompts, budget limits, permission modes, etc.

### Sandboxing (package `sandbox`)

A native Go reimplementation of Anthropic's sandbox-runtime:

- **macOS** -- Seatbelt (`sandbox-exec`) with dynamically generated profiles
- **Linux** -- bubblewrap (`bwrap`) with namespace isolation
- **Filesystem isolation** -- Read-only and read-write mount control
- **Network filtering** -- HTTP + SOCKS5 proxy with domain allow/deny lists
- **Environment control** -- Custom env vars, TMPDIR handling

### MCP server (package `mcp`)

A standalone MCP server implementation:

- **JSON-RPC 2.0** -- Protocol-compliant server over `io.Reader`/`io.Writer`
- **Tool registry** -- Thread-safe tool registration with `chat.Tool` interface
- **MCP 2025-11-25** -- Current protocol version

## Not supported (by design)

These features from the upstream reference implementations are intentionally excluded:

- **Violation monitoring** -- The sandbox-runtime's macOS log store tapping and Linux strace-based violation detection are not ported. The Go sandbox enforces policy but does not report violations after the fact.
- **Seccomp BPF** -- Unix socket restrictions via seccomp BPF filtering (from sandbox-runtime) are not implemented. Network filtering is handled entirely through the proxy.
- **Settings file configuration** -- The sandbox-runtime's JSON settings file format is not used. Sandbox policy is configured programmatically via the `sandbox.Policy` struct.
- **BYOP (Bring Your Own Proxy)** -- The sandbox-runtime's mode for plugging in an external proxy is not supported. The `sandbox` package runs its own built-in proxy.

## Architecture

```
claudecode/          SDK for interacting with Claude Code CLI
  subprocess.go      Spawns and manages the claude CLI process
  protocol.go        Bidirectional JSON control protocol
  mcp.go             Routes MCP messages to in-process tool servers
  hooks.go           Hook registration and callback dispatch
  client.go          Multi-turn conversation state machine
  query.go           One-shot query helpers

chat/                Shared types (Tool, Message, Content)

sandbox/             OS-level process sandboxing (independent of SDK)
  exec_darwin.go     macOS Seatbelt implementation
  exec_linux.go      Linux bubblewrap implementation
  proxy.go           HTTP + SOCKS5 network filtering proxy

mcp/                 Standalone MCP server implementation
```

The SDK works by spawning the `claude` CLI as a subprocess and communicating over stdin/stdout using Claude Code's JSON streaming protocol. The `sandbox` package can optionally wrap this subprocess in an OS-level sandbox to restrict filesystem and network access.

## Upstream tracking

This project tracks two upstream Anthropic repositories as git submodules in `third_party/`:

- `third_party/claude-agent-sdk-python` -- The official Python SDK
- `third_party/sandbox-runtime` -- The official sandbox runtime (TypeScript)

Updates are pulled on a **1-week delay** from upstream releases. This delay is intentional: it reduces the risk of incorporating a supply-chain compromise before the community has time to notice it. A cron job runs Claude Code to check for upstream changes, pull updates that are at least 1 week old, and port relevant concepts to the Go code.

## License

Apache License 2.0. See [LICENSE](LICENSE).
