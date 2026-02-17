package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
)

// Policy defines the security boundaries for sandboxed command execution via bubblewrap.
// The zero value provides maximum security isolation: no mounts, all namespaces unshared,
// child dies with parent, and new session created.
//
// Security model:
//   - PID namespace is isolated by default; network namespace is isolated when network
//     is blocked or proxy-filtered. IPC and UTS namespaces are shared for compatibility
//     with shared memory (PostgreSQL) and hostname-dependent programs.
//   - Only explicitly mounted paths are accessible inside the sandbox
//   - Read-only mounts prevent modification of system files
//   - Read-write mounts should be limited to working directories and necessary user data
//   - Child processes are terminated when the parent exits
//   - Processes cannot control the parent's terminal session
//
// To create a usable sandbox, at minimum add read-only system mounts and the working directory
// as a read-write mount. See DefaultPolicy() for a reasonable starting configuration.
//
// Concurrency: Policy instances must not be mutated while Command() calls are in progress.
// A single Policy can be reused across concurrent Command() calls provided that the
// caller does not modify its fields (ReadOnlyMounts, ReadWriteMounts, etc.) concurrently.
// This makes Policy ideal for use in HTTP handlers and other concurrent contexts where
// the same immutable sandbox configuration is reused across multiple requests.
type Policy struct {
	// ReadOnlyMounts are mounted read-only inside the sandbox (e.g., /usr, /bin, /lib).
	// These allow the sandboxed process to execute system binaries and load libraries.
	ReadOnlyMounts []Mount

	// ReadWriteMounts are mounted read-write inside the sandbox (e.g., working directory).
	// Limit these to only what the sandboxed process needs to write.
	ReadWriteMounts []Mount

	// DenyWritePaths are paths within writable mounts that should be denied write access.
	// This implements a "deny-within-allow" model: paths listed here are blocked from
	// writes even if they fall within a ReadWriteMount or the WorkDir.
	//
	// On Linux, these paths are re-bound as read-only over the writable mount.
	// If a path does not exist, /dev/null is mounted at the path to prevent creation.
	// On macOS, corresponding Seatbelt deny rules are generated using parameter
	// indirection. Non-existent paths are denied by pattern (the rule is harmless
	// if the path never exists).
	//
	// Common use case: blocking writes to dangerous files (.gitconfig, .bashrc,
	// .git/hooks) within an otherwise-writable working directory.
	//
	// Example:
	//   policy.DenyWritePaths = []string{
	//       "/home/user/project/.git/hooks",
	//       "/home/user/project/.bashrc",
	//   }
	DenyWritePaths []string

	// DenyReadPaths are paths that should be denied read access even when
	// AllowAllReads is true. This provides a "deny-within-allow" model for reads.
	//
	// On Linux, directories are hidden with --tmpfs (empty tmpfs overlay) and
	// files are hidden with --ro-bind /dev/null. Non-existent paths are skipped.
	// On macOS, corresponding Seatbelt deny file-read* rules are generated using
	// parameter indirection.
	DenyReadPaths []string

	// WorkDir specifies the working directory for the sandboxed command.
	// If empty, defaults to the current working directory (os.Getwd()).
	//
	// IMPORTANT: The working directory is automatically mounted read-write inside the sandbox.
	// You do NOT need to add it to ReadWriteMounts manually. The sandbox implementation
	// ensures WorkDir is accessible and sets it as the initial directory via --chdir (Linux)
	// or cmd.Dir (macOS).
	//
	// This design allows specifying the working directory without using os.Chdir(), which is
	// forbidden in library code as it affects global process state.
	WorkDir string

	// ProvideTmp controls whether /tmp is available inside the sandbox (default false = no /tmp).
	// - Linux: Creates isolated tmpfs mounted at /tmp (private to this sandbox, auto-cleaned on exit)
	// - macOS: Creates temporary directory on host and mounts it at /tmp inside sandbox.
	//   The temp directory is cleaned up via runtime.SetFinalizer when the *exec.Cmd is garbage
	//   collected. This is best-effort cleanup - finalizers are not guaranteed to run, but
	//   acceptable for temp directories in /tmp. Callers must hold the Cmd reference until
	//   after Wait() completes to ensure the temp directory exists during command execution.
	//   For explicit cleanup control, create your own temp directory and mount it with
	//   ReadWriteMounts instead of using ProvideTmp.
	ProvideTmp bool

	// AllowNetwork controls network access (default false = blocked).
	// - Linux: When false, --unshare-net isolates the network namespace
	// - macOS: When false, network-outbound/inbound Seatbelt rules are omitted
	//
	// Important: On macOS, AllowNetwork=true allows ALL network access including internet.
	// For localhost-only access (e.g., for Jupyter kernels), use AllowLocalhostOnly=true instead.
	AllowNetwork bool

	// AllowLocalhostOnly controls localhost-only network access (default false = blocked).
	// This is a safer alternative to AllowNetwork for applications that need IPC via TCP sockets
	// on localhost (127.0.0.1, ::1) but should not access external networks.
	//
	// - macOS: When true, Seatbelt rules allow network-outbound/inbound only for localhost
	// - Linux: When true, behaves the same as AllowNetwork=false (namespace isolation)
	//         Note: On Linux, localhost communication works even with network namespace isolation,
	//         so this flag has no additional effect beyond AllowNetwork=false.
	//
	// Typical use case: Jupyter notebook execution (kernel communication via localhost TCP)
	//
	// Security: This is the recommended setting for running untrusted code that needs IPC,
	// as it prevents the sandboxed process from accessing external internet while still
	// allowing local inter-process communication via TCP sockets.
	//
	// Note: If both AllowNetwork and AllowLocalhostOnly are true, AllowNetwork takes precedence
	// (full network access is granted).
	AllowLocalhostOnly bool

	// AllowAllReads controls whether file reads are unrestricted (default false = restricted).
	// When true, all file reads are allowed by default (matching sandbox-runtime behavior).
	// When false, only paths in ReadOnlyMounts and ReadWriteMounts are readable.
	//
	// This option is useful for sandboxing applications like Claude Code that need to read
	// system files, libraries, and other paths but should only write to specific directories.
	// For running untrusted code (like Python scripts), leave this false for maximum isolation.
	AllowAllReads bool

	// NetworkProxy enables filtered network access through HTTP and SOCKS5 proxies.
	// When set, the sandboxed process can only access network destinations allowed by
	// the proxy's filter. The proxy runs in the parent process and intercepts all
	// network connections.
	//
	// - macOS: Seatbelt restricts network access to only the proxy ports
	// - Linux: Full network namespace isolation (--unshare-net); proxy listens on
	//   localhost TCP sockets accessible within the isolated namespace
	//
	// The proxy must be explicitly created and closed by the caller:
	//   proxy, err := NewNetworkProxy(filter)
	//   if err != nil { return err }
	//   defer proxy.Close()
	//   policy.NetworkProxy = proxy
	//
	// Environment variables (HTTP_PROXY, HTTPS_PROXY, ALL_PROXY) are automatically
	// set in the sandboxed process to use the proxy.
	//
	// Note: If NetworkProxy is set, AllowNetwork and AllowLocalhostOnly are ignored.
	NetworkProxy *NetworkProxy

	// Env specifies additional environment variables to set in the sandboxed process.
	// These are applied after the base environment from os.Environ() and any
	// sandbox-generated variables (like TMPDIR). If a variable appears in both
	// os.Environ() and this map, the value in this map takes precedence.
	//
	// Common use cases:
	//   - Setting VIRTUAL_ENV and modifying PATH for Python virtualenvs
	//   - Configuring application-specific variables
	//   - Overriding system defaults for the sandboxed process
	//
	// Example (virtualenv):
	//   policy.Env = map[string]string{
	//       "VIRTUAL_ENV": "/path/to/venv",
	//       "PATH": "/path/to/venv/bin:" + os.Getenv("PATH"),
	//   }
	Env map[string]string

	// The following fields are Linux-specific and ignored on macOS:

	// AllowSharedNamespaces, when true, disables PID namespace isolation.
	// The default (false) isolates the PID namespace and conditionally isolates
	// the network namespace (based on AllowNetwork and NetworkProxy settings).
	// Only set to true if the sandboxed process must share PID namespace with the host.
	// Ignored on macOS (Seatbelt doesn't use Linux namespaces).
	AllowSharedNamespaces bool

	// UnshareIPC, when true, isolates the IPC namespace (adds --unshare-ipc).
	// The default (false) shares IPC with the host for compatibility with shared
	// memory (PostgreSQL, test frameworks using POSIX shared memory).
	// Only set to true if you need strict IPC isolation and don't use shared memory.
	// Ignored on macOS (Seatbelt doesn't use Linux namespaces).
	UnshareIPC bool

	// UnshareUTS, when true, isolates the UTS namespace (adds --unshare-uts).
	// The default (false) shares UTS with the host so hostname-dependent programs
	// (like hostname(1)) see the real hostname.
	// Only set to true if you need the sandbox to have its own hostname.
	// Ignored on macOS (Seatbelt doesn't use Linux namespaces).
	UnshareUTS bool

	// AllowParentSurvival, when true, allows the sandboxed process to outlive its parent
	// (skips --die-with-parent). The default (false) ensures child processes are cleaned up.
	// Only set to true if you need the sandbox to persist after the parent exits.
	// Ignored on macOS (Seatbelt doesn't have this concept).
	AllowParentSurvival bool

	// AllowSessionControl, when true, allows the sandboxed process to control the terminal
	// session (skips --new-session). The default (false) creates a new session.
	// Only set to true if the sandboxed process needs terminal control.
	// Ignored on macOS (Seatbelt doesn't have this concept).
	AllowSessionControl bool

	// EnableWeakerNestedSandbox, when true, skips mounting a fresh /proc filesystem.
	// This is required for running inside unprivileged Docker containers that lack
	// the CAP_SYS_ADMIN capability needed to mount /proc.
	// The default (false) mounts a fresh /proc for full PID namespace isolation.
	//
	// Note: When this is true and AllowSharedNamespaces is false, PID namespace
	// isolation is still applied but /proc shows host PIDs (from the parent mount
	// namespace). The sandbox can still not signal host processes, but they are
	// visible in /proc. This is an accepted trade-off for Docker compatibility.
	// Ignored on macOS (Seatbelt doesn't mount /proc).
	EnableWeakerNestedSandbox bool

	// EnableWeakerNetworkIsolation, when true, allows access to com.apple.trustd.agent
	// in the macOS Seatbelt profile. This is needed for Go programs (gh, gcloud,
	// terraform, kubectl, etc.) to verify TLS certificates when using the network proxy.
	// The default (false) blocks trustd.agent access for stronger isolation.
	//
	// WARNING: Enabling this opens a potential data exfiltration vector through
	// the trustd service. Only enable if you need Go TLS verification in the sandbox.
	// Ignored on Linux (bubblewrap doesn't use Mach IPC).
	EnableWeakerNetworkIsolation bool
}

// Mount represents a filesystem path binding into the sandbox.
// All mounts are required; if a mount source doesn't exist, the sandbox will fail to start.
// This ensures deterministic and predictable security boundaries.
type Mount struct {
	// Source is the absolute path on the host filesystem to mount.
	Source string

	// Target is the absolute path inside the sandbox where Source will appear.
	// Typically this is the same as Source to maintain path consistency.
	Target string
}

// DefaultPolicy returns a policy that provides a reasonable baseline for running
// basic commands in a sandbox. It includes minimal system directories mounted read-only
// and provides an isolated /tmp directory.
//
// System mounts (Linux):
//   - Always mounted: /usr, /bin, /lib, /etc
//   - Mounted if they exist: /sbin, /lib64, /run
//
// System mounts (macOS):
//   - Always mounted: /usr, /bin, /System, /Library, /etc
//   - Mounted if exists: /sbin
//
// Temp directory:
//   - ProvideTmp is enabled, providing isolated /tmp on both platforms
//
// Security settings:
//   - Network is blocked by default (AllowNetwork: false, AllowLocalhostOnly: false)
//   - For applications needing IPC via TCP (like Jupyter), use AllowLocalhostOnly: true
//     to allow localhost communication while blocking external internet
//   - All Linux isolation flags enabled (namespace isolation, die-with-parent, new session)
//   - Working directory is automatically mounted read-write at execution time
//
// Application-specific mounts (like /opt for Homebrew, virtualenv paths, user data directories)
// must be added explicitly by the caller. See package examples for common use cases.
func DefaultPolicy() *Policy {
	policy := &Policy{
		ReadOnlyMounts:  make([]Mount, 0, 10),
		ReadWriteMounts: make([]Mount, 0, 5),
		ProvideTmp:      true,
		// AllowNetwork defaults to false (network blocked)
		// All other security bools default to false (maximum security)
	}

	var required, optional []string

	if runtime.GOOS == "darwin" {
		// macOS system directories required for basic utilities
		required = []string{"/usr", "/bin", "/System", "/Library", "/etc"}
		optional = []string{"/sbin"}
	} else if runtime.GOOS == "linux" {
		// Linux system directories
		required = []string{"/usr", "/bin", "/lib", "/etc"}
		optional = []string{"/sbin", "/lib64", "/run"}
	}

	for _, path := range required {
		if PathExists(path) {
			policy.ReadOnlyMounts = append(policy.ReadOnlyMounts, Mount{
				Source: path,
				Target: path,
			})
		}
	}

	for _, path := range optional {
		if PathExists(path) {
			policy.ReadOnlyMounts = append(policy.ReadOnlyMounts, Mount{
				Source: path,
				Target: path,
			})
		}
	}

	return policy
}

// dangerousFiles lists files that should be protected from writes in sandboxed environments.
// These are configuration files that could be exploited to achieve code execution
// outside the sandbox (e.g., by installing git hooks or modifying shell profiles).
// Use DangerousFilesList() to get a copy safe for modification.
var dangerousFiles = []string{
	".gitconfig",
	".gitmodules",
	".gitattributes", // can define filter/diff drivers that execute arbitrary commands
	".bashrc",
	".bash_profile",
	".zshrc",
	".zprofile",
	".profile",
	".ripgreprc",
	".mcp.json",
}

// dangerousDirectories lists directories that should be protected from writes.
// These directories contain configuration that could be exploited to achieve
// code execution outside the sandbox.
// Use DangerousDirectoriesList() to get a copy safe for modification.
var dangerousDirectories = []string{
	".vscode",
	".idea",
	".claude/commands",
	".claude/agents",
}

// DangerousFilesList returns the list of files that should be protected from writes.
// Returns a new copy each time, safe for the caller to modify.
func DangerousFilesList() []string {
	result := make([]string, len(dangerousFiles))
	copy(result, dangerousFiles)
	return result
}

// DangerousDirectoriesList returns the list of directories that should be protected from writes.
// Returns a new copy each time, safe for the caller to modify.
func DangerousDirectoriesList() []string {
	result := make([]string, len(dangerousDirectories))
	copy(result, dangerousDirectories)
	return result
}

// DangerousGitPaths returns paths within a .git directory that should always
// be protected from writes. The hooks directory is always protected; config
// is protected unless allowGitConfig is true.
func DangerousGitPaths(allowGitConfig bool) []string {
	paths := []string{
		".git/hooks",
		".git/info", // contains exclude, attributes, and grafts files that alter git behavior
	}
	if !allowGitConfig {
		paths = append(paths, ".git/config")
	}
	return paths
}

// DangerousWriteDenyPaths returns all paths that should be denied write access
// relative to the given base directory. The returned paths are absolute.
// This is a convenience function that combines dangerousFiles, dangerousDirectories,
// and DangerousGitPaths into a single list of absolute paths.
func DangerousWriteDenyPaths(baseDir string, allowGitConfig bool) []string {
	var paths []string
	for _, f := range dangerousFiles {
		paths = append(paths, filepath.Join(baseDir, f))
	}
	for _, d := range dangerousDirectories {
		paths = append(paths, filepath.Join(baseDir, d))
	}
	for _, g := range DangerousGitPaths(allowGitConfig) {
		paths = append(paths, filepath.Join(baseDir, g))
	}
	return paths
}

// PathExists checks if a path exists. Returns false for any error, including permission denied.
func PathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
