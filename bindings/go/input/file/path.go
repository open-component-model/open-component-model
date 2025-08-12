package file

import (
	"fmt"
	"os"
	"strings"
)

func IsAbsolutePath(path string) bool {
	if len(path) == 0 {
		return false
	}
	if path[0] == '/' || (len(path) > 1 && path[1] == ':') { // Windows absolute path
		return true
	}
	return false
}

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
