//go:build darwin

package sandbox

import "strings"

// matchFilename compares a filename to a pattern.
// On macOS (APFS default is case-insensitive), comparison is case-insensitive.
func matchFilename(name, pattern string) bool {
	return strings.EqualFold(name, pattern)
}
