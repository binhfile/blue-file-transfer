package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FileEntry holds information about a file or directory.
type FileEntry struct {
	Name    string
	Size    int64
	Mode    os.FileMode
	ModTime int64
	IsDir   bool
}

// ListDir lists the contents of a directory.
func ListDir(path string) ([]FileEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	result := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // skip entries we can't stat
		}
		result = append(result, FileEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime().Unix(),
			IsDir:   e.IsDir(),
		})
	}
	return result, nil
}

// GetFileInfo returns info about a single file or directory.
func GetFileInfo(path string) (*FileEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &FileEntry{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
	}, nil
}

// MkdirAll creates a directory and all parents.
func MkdirAll(path string) error {
	return os.MkdirAll(path, 0755)
}

// RemoveAll removes a file or directory recursively.
func RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// CopyFile copies a single file.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}
	return nil
}

// CopyDir copies a directory recursively.
func CopyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if !srcInfo.IsDir() {
		return CopyFile(src, dst)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read source dir: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// MoveFileOrDir moves/renames a file or directory.
func MoveFileOrDir(src, dst string) error {
	return os.Rename(src, dst)
}
