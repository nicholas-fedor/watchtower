package util

import (
	"fmt"
	"os"
	"path/filepath"
)

// privateFilePermissions keeps saved state private to the Watchtower user.
const privateFilePermissions = 0o600

// UsableDir reports whether dir exists, is a directory, and can be written to.
//
// Parameters:
//   - dir: Directory path to probe. Empty is never usable.
//
// Returns:
//   - bool: True if a file can be created inside dir.
func UsableDir(dir string) bool {
	if dir == "" {
		return false
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	probe, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}

	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)

	return true
}

// WriteFileAtomic replaces the file at path with data by writing a temporary
// file in the same directory and renaming it into place, so a crash cannot
// leave a half-written file behind.
//
// Parameters:
//   - path: Destination file path.
//   - data: Complete new contents.
//
// Returns:
//   - error: Non-nil if the file could not be written or moved into place.
func WriteFileAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}

	tempName := temp.Name()

	_, err = temp.Write(data)
	if err == nil {
		err = temp.Chmod(privateFilePermissions)
	}

	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(tempName, path)
	}

	if err != nil {
		_ = os.Remove(tempName)

		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
