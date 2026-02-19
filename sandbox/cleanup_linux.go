//go:build linux

package sandbox

import (
	"os"
	"sync"
)

var (
	bwrapMountPointsMu sync.Mutex
	bwrapMountPoints   = make(map[string]struct{})
)

// recordBwrapMountPoint tracks a host path that bubblewrap may create as a
// mount-point artifact when denying writes to a non-existent path.
func recordBwrapMountPoint(path string) {
	if path == "" {
		return
	}
	bwrapMountPointsMu.Lock()
	bwrapMountPoints[path] = struct{}{}
	bwrapMountPointsMu.Unlock()
}

// cleanupAfterCommand removes empty host files/directories that were created
// as bubblewrap mount points for non-existent deny-write paths.
//
// This is best-effort and safe to call repeatedly. It only removes tracked
// empty regular files and tracked empty directories.
func cleanupAfterCommand() {
	bwrapMountPointsMu.Lock()
	mountPoints := make([]string, 0, len(bwrapMountPoints))
	for p := range bwrapMountPoints {
		mountPoints = append(mountPoints, p)
	}
	bwrapMountPointsMu.Unlock()

	for _, mountPoint := range mountPoints {
		removeFromSet := false

		info, err := os.Lstat(mountPoint)
		if os.IsNotExist(err) {
			removeFromSet = true
		} else if err != nil {
			continue
		} else if info.Mode().IsRegular() {
			if info.Size() != 0 {
				continue
			}
			if err := os.Remove(mountPoint); err == nil || os.IsNotExist(err) {
				removeFromSet = true
			}
		} else if info.IsDir() {
			entries, err := os.ReadDir(mountPoint)
			if err != nil || len(entries) != 0 {
				continue
			}
			if err := os.Remove(mountPoint); err == nil || os.IsNotExist(err) {
				removeFromSet = true
			}
		} else {
			// Never remove other filesystem object types (symlink, socket, etc.).
			continue
		}

		if removeFromSet {
			bwrapMountPointsMu.Lock()
			delete(bwrapMountPoints, mountPoint)
			bwrapMountPointsMu.Unlock()
		}
	}
}
