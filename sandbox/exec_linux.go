//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Linux-specific types for bubblewrap mount handling
type mount struct {
	flag   string
	source string
	target string
}

// commandContext implements Linux sandboxing using bubblewrap.
func (p *Policy) commandContext(ctx context.Context, name string, arg ...string) (*exec.Cmd, error) {
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("sandbox: bwrap not found: %w", err)
	}

	// Build full argv (name + args)
	argv := append([]string{name}, arg...)

	// Generate bubblewrap arguments
	bwrapArgs, err := bubblewrapArgs(p, name, argv)
	if err != nil {
		return nil, fmt.Errorf("sandbox: build bubblewrap args: %w", err)
	}

	// Create command: bwrap <bwrap-args> -- <command> <args>
	// bwrapArgs[0] is bwrapPath itself, skip it for exec.CommandContext
	cmd := exec.CommandContext(ctx, bwrapPath, bwrapArgs[1:]...)

	// Build environment with proper TMPDIR handling and custom env vars
	// On Linux with ProvideTmp, we mount a tmpfs at /tmp, so set TMPDIR=/tmp
	tmpDir := ""
	if p.ProvideTmp {
		tmpDir = "/tmp"
	}
	cmd.Env = buildEnv(p, tmpDir)

	// If network proxy is configured, filter existing proxy vars and add our own
	if p.NetworkProxy != nil {
		cmd.Env = filterProxyEnvVars(cmd.Env)
		cmd.Env = append(cmd.Env, p.NetworkProxy.Env()...)
	}

	return cmd, nil
}

// bubblewrapArgs builds the argument list for bwrap.
// Returns the full argv including bwrapPath at [0].
func bubblewrapArgs(policy *Policy, name string, argv []string) ([]string, error) {
	// Use Policy.WorkDir if specified, otherwise current directory
	wd := policy.WorkDir
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("lookpath bwrap: %w", err)
	}

	args := []string{bwrapPath}
	seen := newMountSet()

	// When AllowAllReads is true, bind-mount the entire root filesystem read-only.
	// This allows reading any file (like macOS's AllowAllReads behavior).
	// Later mounts (read-write paths, /proc, /dev, /tmp) will override specific paths.
	if policy.AllowAllReads {
		args = append(args, "--ro-bind", "/", "/")
		seen.add("--ro-bind", "/")
	}

	// Essential virtual filesystems
	// Mount fresh /proc unless in weaker nested sandbox mode (Docker without CAP_SYS_ADMIN)
	if !policy.EnableWeakerNestedSandbox {
		args = append(args, "--proc", "/proc")
	}
	args = append(args, "--dev", "/dev")

	// Temp directory (isolated tmpfs if requested)
	// Must be mounted BEFORE proxy sockets so they can be bind-mounted on top
	if policy.ProvideTmp {
		args = append(args, "--tmpfs", "/tmp")
	}

	// Mount read-only paths from policy (with canonicalization)
	for _, m := range policy.ReadOnlyMounts {
		canonSrc, err := canonicalPath(m.Source)
		if err != nil {
			return nil, fmt.Errorf("canonicalize readonly mount %s: %w", m.Source, err)
		}
		canonTgt, err := canonicalPath(m.Target)
		if err != nil {
			return nil, fmt.Errorf("canonicalize readonly target %s: %w", m.Target, err)
		}
		args, err = appendMount(args, seen, mount{flag: "--ro-bind", source: canonSrc, target: canonTgt})
		if err != nil {
			return nil, err
		}
	}

	// Mount read-write paths from policy (with canonicalization)
	for _, m := range policy.ReadWriteMounts {
		canonSrc, err := canonicalPath(m.Source)
		if err != nil {
			return nil, fmt.Errorf("canonicalize readwrite mount %s: %w", m.Source, err)
		}
		canonTgt, err := canonicalPath(m.Target)
		if err != nil {
			return nil, fmt.Errorf("canonicalize readwrite target %s: %w", m.Target, err)
		}
		args, err = appendMount(args, seen, mount{flag: "--bind", source: canonSrc, target: canonTgt})
		if err != nil {
			return nil, err
		}
	}

	// On modern Linux systems, /bin, /lib, /lib64, and /sbin are symlinks to /usr subdirectories.
	// We need to recreate these symlinks in the sandbox for executables and libraries to be found.
	// Skip this when AllowAllReads is true since the symlinks already exist from the root bind.
	if !policy.AllowAllReads {
		commonSymlinks := []struct {
			link   string
			target string
		}{
			{"/bin", "usr/bin"},
			{"/lib", "usr/lib"},
			{"/lib64", "usr/lib64"},
			{"/sbin", "usr/sbin"},
		}
		for _, sl := range commonSymlinks {
			if info, err := os.Lstat(sl.link); err == nil && info.Mode()&os.ModeSymlink != 0 {
				args = append(args, "--symlink", sl.target, sl.link)
			}
		}
	}

	// Namespace isolation: use selective unsharing (not --unshare-all).
	// PID and network are unshared by default; IPC and UTS are shared by default
	// for compatibility with shared memory and hostname-dependent programs.
	if !policy.AllowSharedNamespaces {
		args = append(args, "--unshare-pid")
	}
	// Network namespace: always unshare when proxy is configured (to force traffic
	// through proxy) or when network is blocked.
	if policy.NetworkProxy != nil || !policy.AllowNetwork {
		args = append(args, "--unshare-net")
	}
	if policy.UnshareIPC {
		args = append(args, "--unshare-ipc")
	}
	if policy.UnshareUTS {
		args = append(args, "--unshare-uts")
	}

	// Process lifecycle control
	if !policy.AllowParentSurvival {
		args = append(args, "--die-with-parent")
	}
	if !policy.AllowSessionControl {
		args = append(args, "--new-session")
	}

	// Mount working directory as read-write (with canonicalization)
	workdir, err := canonicalPath(wd)
	if err != nil {
		return nil, fmt.Errorf("canonicalize working directory: %w", err)
	}
	args, err = appendMount(args, seen, mount{flag: "--bind", source: workdir, target: workdir})
	if err != nil {
		return nil, fmt.Errorf("bind working directory: %w", err)
	}
	args = append(args, "--chdir", workdir)

	// Deny-within-allow: re-mount specific paths read-only within writable mounts.
	// This must come after all writable mounts so the read-only bind takes precedence.
	for _, denyPath := range policy.DenyWritePaths {
		canonDeny, err := canonicalPath(denyPath)
		if err != nil {
			// Path doesn't exist yet — mount /dev/null read-only at the raw path
			// to prevent creation. The path will appear as an empty regular file
			// inside the sandbox (not absent). We use the raw path because
			// canonicalization requires the path to exist; bubblewrap resolves
			// mount targets within its mount namespace.
			args = append(args, "--ro-bind", "/dev/null", denyPath)
			continue
		}
		// Re-bind as read-only to override the parent writable mount
		args = append(args, "--ro-bind", canonDeny, canonDeny)
	}

	// Deny-read: hide specific paths from read access.
	// Directories are overlaid with an empty tmpfs; files are replaced with /dev/null.
	for _, denyPath := range policy.DenyReadPaths {
		info, err := os.Stat(denyPath)
		if err != nil {
			continue // skip non-existent paths
		}
		canonDeny, err := canonicalPath(denyPath)
		if err != nil {
			// Fall back to raw path (same as DenyWritePaths behavior)
			canonDeny = denyPath
		}
		if info.IsDir() {
			args = append(args, "--tmpfs", canonDeny)
		} else {
			args = append(args, "--ro-bind", "/dev/null", canonDeny)
		}
	}

	// Append the separator and the actual command + arguments
	args = append(args, "--")
	args = append(args, argv...)

	return args, nil
}

// appendMount adds a mount entry to the bubblewrap args if not already present.
func appendMount(args []string, seen *mountSet, entry mount) ([]string, error) {
	if entry.source == "" || entry.target == "" {
		return nil, fmt.Errorf("sandbox: mount requires non-empty paths")
	}

	if seen.has(entry.flag, entry.target) {
		return args, nil
	}

	if err := ensurePath(entry.source); err != nil {
		return nil, fmt.Errorf("sandbox: stat %s: %w", entry.source, err)
	}

	seen.add(entry.flag, entry.target)
	args = append(args, entry.flag, entry.source, entry.target)
	return args, nil
}

// ensurePath verifies that a path exists.
func ensurePath(path string) error {
	if path == "" {
		return fmt.Errorf("sandbox: empty path")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}

