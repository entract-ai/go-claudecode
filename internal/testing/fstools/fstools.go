// Package fstools provides filesystem tools for testing MCP server implementations.
//
// Tools operate on an *os.Root stored in the context, restricting all file
// operations to a directory tree. Use [WithRoot] to attach a root and
// [GetRoot] to retrieve it.
package fstools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

type contextKey struct{}

// WithRoot adds an *os.Root to the context for downstream tool calls.
func WithRoot(ctx context.Context, root *os.Root) context.Context {
	return context.WithValue(ctx, contextKey{}, root)
}

// GetRoot retrieves the *os.Root from the context.
func GetRoot(ctx context.Context) (*os.Root, error) {
	root, ok := ctx.Value(contextKey{}).(*os.Root)
	if !ok || root == nil {
		return nil, fmt.Errorf("no filesystem root found in context")
	}
	return root, nil
}

// cleanPath normalizes a file path for use with os.Root.
// It cleans the path and strips any leading slash.
func cleanPath(p string) string {
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		p = "."
	}
	return p
}

// ReadFileRequest is the input for ReadFile.
type ReadFileRequest struct {
	FileName string `json:"fileName"`
}

// ReadFileResult is the output of ReadFile.
type ReadFileResult struct {
	Content string `json:"content"`
}

// ReadFile reads a file from the rooted filesystem.
func ReadFile(ctx context.Context, req ReadFileRequest) (ReadFileResult, error) {
	root, err := GetRoot(ctx)
	if err != nil {
		return ReadFileResult{}, err
	}

	f, err := root.Open(cleanPath(req.FileName))
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to open file %s: %w", req.FileName, err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to read file %s: %w", req.FileName, err)
	}
	return ReadFileResult{Content: string(data)}, nil
}

// WriteFileRequest is the input for WriteFile.
type WriteFileRequest struct {
	FileName string `json:"fileName"`
	Content  string `json:"content"`
}

// WriteFileResult is the output of WriteFile.
type WriteFileResult struct {
	Success bool `json:"success"`
}

// WriteFile writes a file to the rooted filesystem.
func WriteFile(ctx context.Context, req WriteFileRequest) (WriteFileResult, error) {
	root, err := GetRoot(ctx)
	if err != nil {
		return WriteFileResult{}, err
	}

	name := cleanPath(req.FileName)

	// Create parent directories if needed.
	if dir := path.Dir(name); dir != "." && dir != "/" {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return WriteFileResult{}, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return WriteFileResult{}, fmt.Errorf("failed to create file %s: %w", name, err)
	}
	_, writeErr := f.Write([]byte(req.Content))
	closeErr := f.Close()
	if writeErr != nil {
		return WriteFileResult{}, fmt.Errorf("failed to write file %s: %w", name, writeErr)
	}
	if closeErr != nil {
		return WriteFileResult{}, fmt.Errorf("failed to close file %s: %w", name, closeErr)
	}
	return WriteFileResult{Success: true}, nil
}

// ReadDirRequest is the input for ReadDir.
type ReadDirRequest struct {
	Path string `json:"path,omitzero"`
}

// ReadDirResult is the output of ReadDir.
type ReadDirResult struct {
	Files []FileInfo `json:"files"`
}

// FileInfo contains information about a file.
type FileInfo struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// ReadDir reads a directory from the rooted filesystem.
func ReadDir(ctx context.Context, req ReadDirRequest) (ReadDirResult, error) {
	root, err := GetRoot(ctx)
	if err != nil {
		return ReadDirResult{}, err
	}

	dirPath := cleanPath(req.Path)

	f, err := root.Open(dirPath)
	if err != nil {
		return ReadDirResult{}, fmt.Errorf("failed to open directory %s: %w", dirPath, err)
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return ReadDirResult{}, fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}
	return ReadDirResult{Files: files}, nil
}
