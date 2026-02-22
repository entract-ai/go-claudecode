# go-claudecode

Native Go SDK for the Claude Code CLI, with built-in OS-level sandboxing.

## Project structure

```
*.go            Main SDK (package claudecode) -- talks to the Claude Code CLI via subprocess
chat/           Shared types: Tool, Message, Content, Role
sandbox/        OS-level process sandboxing (Seatbelt on macOS, bwrap on Linux)
mcp/            Standalone MCP server (JSON-RPC 2.0 over io.Reader/io.Writer)
internal/       Test-only utilities (fstools for MCP integration tests)
third_party/    Git submodules (upstream references, not Go code)
```

## Upstream references

This project is a Go port of two Anthropic projects kept as git submodules:

- `third_party/claude-agent-sdk-python/` -- Official Python SDK (MIT). The root `claudecode` package ports this.
- `third_party/sandbox-runtime/` -- Official sandbox runtime in TypeScript (Apache 2.0). The `sandbox/` package ports this.

**These submodules are reference material only.** No Go code imports or depends on them. When porting features, read the upstream code to understand intent, then write idiomatic Go.

## Updating from upstream

Submodules are updated on a **1-week delay** from upstream. A cron job:

1. Checks for new commits in both submodules
2. Skips any commit less than 1 week old (supply-chain risk mitigation)
3. Pulls eligible updates
4. Reviews the diff and ports relevant changes to the Go code

When porting upstream changes:
- Port the concept, not the code. Python/TypeScript patterns do not translate directly to Go.
- Maintain the existing Go API style (functional options, interfaces, channels).
- If an upstream change adds a feature we intentionally don't support (see below), skip it.

## What we support

### SDK (root package)
Everything in the Python claude-agent-sdk:
- One-shot queries (Query, QuerySync)
- Streaming with bidirectional control protocol (QueryWithInput)
- Multi-turn conversations (Client)
- In-process MCP servers (WithSDKMCPServer + chat.Tool interface)
- External MCP servers (stdio, SSE, HTTP)
- All 6 hook types (PreToolUse, PostToolUse, UserPromptSubmit, Stop, SubagentStop, PreCompact)
- Tool permission callbacks (WithCanUseTool)
- Subagents, session forking, transcript parsing
- Full option set: model, system prompt, budget, permissions, env, etc.

### Sandboxing (sandbox/)
Core enforcement from sandbox-runtime:
- Filesystem isolation via read-only/read-write mounts
- Network domain filtering via HTTP + SOCKS5 proxy
- macOS: Seatbelt with dynamic profile generation
- Linux: bubblewrap with namespace isolation

### MCP server (mcp/)
- JSON-RPC 2.0 server
- Protocol version 2025-11-25
- Thread-safe tool registry

## What we do NOT support (by design)

These are intentional omissions from the upstream sandbox-runtime:

- **Violation monitoring** -- No macOS log store tapping or Linux strace-based violation detection. We enforce policy; we don't report violations after the fact.
- **Seccomp BPF** -- No Unix socket restrictions via seccomp. Network filtering is proxy-based.
- **Settings file configuration** -- No JSON settings file for sandbox config. Use `sandbox.Policy` struct directly.
- **BYOP** -- No "bring your own proxy" mode. The built-in proxy handles network filtering.

Do NOT add these features. If upstream changes relate to them, skip the port.

## Development

This project requires a GOEXPERIMENT flag for the build. Use `./with_api_keys.sh` as a wrapper, which sets the appropriate environment (including GOEXPERIMENT). If you run `go build`, `go test`, etc. directly, you must have the same GOEXPERIMENT in your environment.

```bash
# Run all tests
./with_api_keys.sh go test ./...

# Run tests for a specific package
./with_api_keys.sh go test .
./with_api_keys.sh go test ./sandbox/
./with_api_keys.sh go test ./mcp/

# Run with race detector
./with_api_keys.sh go test -race ./...
```

## Code style

- Follow standard Go conventions (gofmt, go vet, etc.)
- Use testify's require/assert in tests
- Functional options pattern for configuration (see `options.go`)
- Interfaces for extensibility (`chat.Tool`, `claudecode.Transport`, etc.)
- Channels and iterators for streaming results
- Keep the `sandbox/` package independent of `claudecode/` -- it is usable standalone
- No CGo. Pure Go only.
