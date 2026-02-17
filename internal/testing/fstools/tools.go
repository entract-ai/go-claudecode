package fstools

import (
	"context"
	"encoding/json"

	"github.com/bpowers/go-claudecode/chat"
)

// ReadFileTool implements chat.Tool for reading files.
var ReadFileTool chat.Tool = readFileTool{}

type readFileTool struct{}

func (readFileTool) Name() string        { return "ReadFile" }
func (readFileTool) Description() string { return "Reads a file from the test filesystem" }
func (readFileTool) MCPJsonSchema() string {
	return `{"name":"ReadFile","description":"Reads a file from the test filesystem","inputSchema":{"type":"object","properties":{"fileName":{"type":"string"}},"required":["fileName"],"additionalProperties":false},"outputSchema":{"type":"object","properties":{"content":{"type":"string"},"error":{"type":["string","null"]}},"required":["content","error"],"additionalProperties":false}}`
}

func (readFileTool) Call(ctx context.Context, input string) string {
	var req ReadFileRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return errJSON("failed to parse input: " + err.Error())
	}
	result, err := ReadFile(ctx, req)
	return marshalResult(result, err)
}

// WriteFileTool implements chat.Tool for writing files.
var WriteFileTool chat.Tool = writeFileTool{}

type writeFileTool struct{}

func (writeFileTool) Name() string        { return "WriteFile" }
func (writeFileTool) Description() string { return "Writes a file to the test filesystem" }
func (writeFileTool) MCPJsonSchema() string {
	return `{"name":"WriteFile","description":"Writes a file to the test filesystem","inputSchema":{"type":"object","properties":{"content":{"type":"string"},"fileName":{"type":"string"}},"required":["fileName","content"],"additionalProperties":false},"outputSchema":{"type":"object","properties":{"error":{"type":["string","null"]},"success":{"type":"boolean"}},"required":["success","error"],"additionalProperties":false}}`
}

func (writeFileTool) Call(ctx context.Context, input string) string {
	var req WriteFileRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return errJSON("failed to parse input: " + err.Error())
	}
	result, err := WriteFile(ctx, req)
	return marshalResult(result, err)
}

// ReadDirTool implements chat.Tool for reading directories.
var ReadDirTool chat.Tool = readDirTool{}

type readDirTool struct{}

func (readDirTool) Name() string        { return "ReadDir" }
func (readDirTool) Description() string { return "Reads a directory from the test filesystem" }
func (readDirTool) MCPJsonSchema() string {
	return `{"name":"ReadDir","description":"Reads a directory from the test filesystem","inputSchema":{"type":"object","properties":{"path":{"type":"string","description":"Directory path to read (defaults to \".\" for root)"}},"additionalProperties":false},"outputSchema":{"type":"object","properties":{"error":{"type":["string","null"]},"files":{"type":"array","items":{"type":"object","properties":{"isDir":{"type":"boolean"},"name":{"type":"string"},"size":{"type":"integer"}},"required":["name","isDir","size"],"additionalProperties":false}}},"required":["files","error"],"additionalProperties":false}}`
}

func (readDirTool) Call(ctx context.Context, input string) string {
	var req ReadDirRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return errJSON("failed to parse input: " + err.Error())
	}
	result, err := ReadDir(ctx, req)
	return marshalResult(result, err)
}

// marshalResult wraps a result with an optional error and marshals to JSON.
func marshalResult(result any, err error) string {
	type wrapper struct {
		Result any     `json:"result,omitzero"`
		Error  *string `json:"error,omitzero"`
	}
	w := wrapper{Result: result}
	if err != nil {
		s := err.Error()
		w.Error = &s
	}

	// We need to inline the result fields alongside error.
	// Use a map to merge fields.
	resultBytes, _ := json.Marshal(result)
	var m map[string]any
	_ = json.Unmarshal(resultBytes, &m)
	if m == nil {
		m = make(map[string]any)
	}
	if err != nil {
		m["error"] = err.Error()
	} else {
		m["error"] = nil
	}
	out, _ := json.Marshal(m)
	return string(out)
}

// errJSON returns a JSON object with just an error field.
func errJSON(msg string) string {
	m := map[string]any{"error": msg}
	out, _ := json.Marshal(m)
	return string(out)
}
