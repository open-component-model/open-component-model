package file

import (
	"fmt"
	"os"
	"strings"
)

// IsAbsolutePath checks if the provided path is an absolute path.
// It returns true if the path starts with a '/' (Unix-like systems) or has a drive letter followed by ':' (Windows).
// It returns false for relative paths or empty strings.
// Note: This function does not validate the path format, it only checks for absolute path characteristics.
func IsAbsolutePath(path string) bool {
	if len(path) == 0 {
		return false
	}
	if path[0] == '/' || (len(path) > 1 && path[1] == ':') { // Windows absolute path
		return true
	}
	return false
}

// EnsureAbsolutePath checks if the provided path is absolute. If it is not,
// it prepends the working directory to the path to make it absolute.
// If the working directory is not provided, it uses the current working directory.
// The function modifies the path in place and returns an error if it fails to get the current working directory.
func EnsureAbsolutePath(path *string, workingDir string) error {
	if IsAbsolutePath(*path) {
		return nil
	}

	if workingDir == "" {
		if dir, err := os.Getwd(); err != nil {
			return fmt.Errorf("error getting current working directory: %w", err)
		} else {
			workingDir = dir
		}
	}
	// make sure that we do not have two slashes in the path
	*path = fmt.Sprintf("%s%c%s", strings.TrimSuffix(workingDir, "/"), os.PathSeparator, strings.TrimPrefix(*path, "/"))

	return nil
}
