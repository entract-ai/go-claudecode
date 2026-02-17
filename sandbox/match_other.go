//go:build !darwin

package sandbox

// matchFilename compares a filename to a pattern.
// On Linux (ext4, etc.), comparison is case-sensitive.
func matchFilename(name, pattern string) bool {
	return name == pattern
}
