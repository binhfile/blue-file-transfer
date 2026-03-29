package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathTraversal is returned when a path escapes the root directory.
var ErrPathTraversal = errors.New("path traversal attempt detected")

// SanitizePath validates and resolves a requested path within a root directory.
// It prevents path traversal attacks by ensuring the resolved path stays within rootDir.
func SanitizePath(rootDir, requestedPath string) (string, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}

	// Ensure rootDir ends with separator for prefix check
	rootPrefix := filepath.Clean(rootDir) + string(filepath.Separator)

	// Clean the requested path
	cleaned := filepath.Clean(requestedPath)

	// If relative, join with root
	var fullPath string
	if filepath.IsAbs(cleaned) {
		fullPath = cleaned
	} else {
		fullPath = filepath.Join(rootDir, cleaned)
	}

	// Resolve symlinks if the path exists
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Path doesn't exist yet — check the parent directory
			parentDir := filepath.Dir(fullPath)
			resolvedParent, err2 := filepath.EvalSymlinks(parentDir)
			if err2 != nil {
				return "", fmt.Errorf("resolve parent: %w", err2)
			}
			if resolvedParent != filepath.Clean(rootDir) && !strings.HasPrefix(resolvedParent+string(filepath.Separator), rootPrefix) {
				return "", ErrPathTraversal
			}
			// Parent is within root, reconstruct with base name
			return filepath.Join(resolvedParent, filepath.Base(fullPath)), nil
		}
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// Verify resolved path is within root
	if resolved != filepath.Clean(rootDir) && !strings.HasPrefix(resolved+string(filepath.Separator), rootPrefix) && !strings.HasPrefix(resolved, rootPrefix) {
		return "", ErrPathTraversal
	}

	return resolved, nil
}

// ResolveCwd resolves a working directory change within the root.
// currentDir is the current working directory (absolute, within root).
// target is the requested new directory.
func ResolveCwd(rootDir, currentDir, target string) (string, error) {
	var newPath string
	if filepath.IsAbs(target) {
		newPath = target
	} else {
		newPath = filepath.Join(currentDir, target)
	}
	return SanitizePath(rootDir, newPath)
}
