package file

import (
	"fmt"
	"os"
	goPath "path"
	"strings"
)

// EnsureAbsolutePath checks if the provided path is absolute. If it is not,
// it prepends the working directory to the path to make it absolute.
// If the working directory is not provided, it uses the current working directory.
// The function modifies the path in place and returns an error if it fails to get the current working directory.
func EnsureAbsolutePath(path *string, workingDir string) error {
	if goPath.IsAbs(*path) {
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
