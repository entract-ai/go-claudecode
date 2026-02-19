//go:build !linux

package sandbox

// cleanupAfterCommand is a no-op on non-Linux platforms.
func cleanupAfterCommand() {}
