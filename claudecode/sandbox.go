package claudecode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bpowers/go-claudecode/sandbox"
)

// SandboxOptions configures optional features for the Claude Code sandbox.
type SandboxOptions struct {
	// VirtualEnvPath is the absolute path to a Python virtualenv to activate.
	// When set, the sandbox will:
	//   - Set VIRTUAL_ENV to this path
	//   - Prepend the virtualenv's bin directory to PATH
	//   - Mount the virtualenv directory as read-only
	// This enables Python scripts executed by Claude Code's Bash tool to use
	// packages installed in the virtualenv.
	VirtualEnvPath string

	// SessionDisplayDir is the host path to a session-specific directory for
	// generated images and visualizations. When set, the sandbox will mount
	// this directory as read-write at <workDir>/session-display/. Claude Code
	// can then save images to session-display/ which will be accessible via
	// the Display tool.
	SessionDisplayDir string
}

// NewClaudeCodeSandboxPolicy creates an OS-level sandbox policy for running
// the Claude Code CLI. Access is restricted to:
//   - Read-only: system dirs, Homebrew (/opt), Claude CLI binary (~/.local/share/claude, ~/.local/bin)
//   - Read-write: project directory (workDir), Claude config (~/.claude/)
//   - Network: allowed (required for Anthropic API)
//
// Optional SandboxOptions can be provided to configure additional features like
// virtualenv activation.
func NewClaudeCodeSandboxPolicy(workDir string, opts ...SandboxOptions) (*sandbox.Policy, error) {
	var options SandboxOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home directory: %w", err)
	}

	policy := sandbox.DefaultPolicy()
	policy.WorkDir = workDir
	policy.AllowNetwork = true
	policy.AllowAllReads = true // CLI needs to read system files, libraries, etc.

	// Add Homebrew paths (macOS)
	if sandbox.PathExists("/opt") {
		policy.ReadOnlyMounts = append(policy.ReadOnlyMounts, sandbox.Mount{
			Source: "/opt",
			Target: "/opt",
		})
	}

	// Add Claude CLI binary location
	claudeLocalShare := filepath.Join(home, ".local/share/claude")
	if sandbox.PathExists(claudeLocalShare) {
		policy.ReadOnlyMounts = append(policy.ReadOnlyMounts, sandbox.Mount{
			Source: claudeLocalShare,
			Target: claudeLocalShare,
		})
	}

	// Add ~/.local/bin for claude symlink
	localBin := filepath.Join(home, ".local/bin")
	if sandbox.PathExists(localBin) {
		policy.ReadOnlyMounts = append(policy.ReadOnlyMounts, sandbox.Mount{
			Source: localBin,
			Target: localBin,
		})
	}

	// Add Claude config directory (read-write for history, debug logs, config)
	// Create it if it doesn't exist so first-run works
	claudeConfig := filepath.Join(home, ".claude")
	if !sandbox.PathExists(claudeConfig) {
		if err := os.MkdirAll(claudeConfig, 0o700); err != nil {
			return nil, fmt.Errorf("create claude config directory: %w", err)
		}
	}
	policy.ReadWriteMounts = append(policy.ReadWriteMounts, sandbox.Mount{
		Source: claudeConfig,
		Target: claudeConfig,
	})

	// Claude Code also writes ~/.claude.json in the home directory (outside
	// ~/.claude/). Without this, the config save fails on the read-only root
	// bind, causing "Remote settings" and "Policy limits" to time out during
	// initialization (~60s total).
	claudeJSON := filepath.Join(home, ".claude.json")
	if !sandbox.PathExists(claudeJSON) {
		if err := os.WriteFile(claudeJSON, []byte("{}"), 0o600); err != nil {
			return nil, fmt.Errorf("create claude config file: %w", err)
		}
	}
	policy.ReadWriteMounts = append(policy.ReadWriteMounts, sandbox.Mount{
		Source: claudeJSON,
		Target: claudeJSON,
	})

	// Configure virtualenv if specified
	if options.VirtualEnvPath != "" {
		if err := configureVirtualEnv(policy, options.VirtualEnvPath); err != nil {
			return nil, fmt.Errorf("configure virtualenv: %w", err)
		}
	}

	// Mount session display directory if specified
	if options.SessionDisplayDir != "" {
		displayTarget := filepath.Join(workDir, "session-display")
		policy.ReadWriteMounts = append(policy.ReadWriteMounts, sandbox.Mount{
			Source: options.SessionDisplayDir,
			Target: displayTarget,
		})
	}

	return policy, nil
}

// configureVirtualEnv sets up the sandbox policy for Python virtualenv activation.
// This sets the VIRTUAL_ENV environment variable and prepends the virtualenv's
// bin directory to PATH, enabling Python scripts to use the virtualenv's packages.
func configureVirtualEnv(policy *sandbox.Policy, venvPath string) error {
	if !sandbox.PathExists(venvPath) {
		return fmt.Errorf("virtualenv path does not exist: %s", venvPath)
	}

	// Ensure we have an absolute path
	absVenvPath, err := filepath.Abs(venvPath)
	if err != nil {
		return fmt.Errorf("get absolute path for virtualenv: %w", err)
	}

	// Get the virtualenv's bin directory
	binDir := filepath.Join(absVenvPath, "bin")
	if !sandbox.PathExists(binDir) {
		return fmt.Errorf("virtualenv bin directory does not exist: %s", binDir)
	}

	// Mount the virtualenv directory as read-only
	policy.ReadOnlyMounts = append(policy.ReadOnlyMounts, sandbox.Mount{
		Source: absVenvPath,
		Target: absVenvPath,
	})

	// Set up environment variables for virtualenv activation
	if policy.Env == nil {
		policy.Env = make(map[string]string)
	}
	policy.Env["VIRTUAL_ENV"] = absVenvPath
	policy.Env["PATH"] = binDir + ":" + os.Getenv("PATH")

	return nil
}
