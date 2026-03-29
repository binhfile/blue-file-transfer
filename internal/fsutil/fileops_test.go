package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("world"), 0644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 {
		t.Fatalf("entries count = %d, want 3", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}

	for _, expected := range []string{"file1.txt", "file2.txt", "subdir"} {
		if !names[expected] {
			t.Errorf("missing entry: %s", expected)
		}
	}
}

func TestListDir_Empty(t *testing.T) {
	dir := t.TempDir()
	entries, err := ListDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries count = %d, want 0", len(entries))
	}
}

func TestListDir_NonExistent(t *testing.T) {
	_, err := ListDir("/nonexistent_path_12345")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetFileInfo(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.txt")
	os.WriteFile(fpath, []byte("content"), 0644)

	info, err := GetFileInfo(fpath)
	if err != nil {
		t.Fatal(err)
	}

	if info.Name != "test.txt" {
		t.Errorf("name = %q, want %q", info.Name, "test.txt")
	}
	if info.Size != 7 {
		t.Errorf("size = %d, want 7", info.Size)
	}
	if info.IsDir {
		t.Error("expected IsDir=false")
	}
}

func TestGetFileInfo_Directory(t *testing.T) {
	dir := t.TempDir()
	info, err := GetFileInfo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir {
		t.Error("expected IsDir=true")
	}
}

func TestGetFileInfo_NonExistent(t *testing.T) {
	_, err := GetFileInfo("/nonexistent_file_12345")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	content := []byte("copy me please")
	os.WriteFile(src, content, 0644)

	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "copy")

	// Create source structure
	os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "file2.txt"), []byte("world"), 0644)

	if err := CopyDir(srcDir, dstDir); err != nil {
		t.Fatal(err)
	}

	// Verify copy
	content1, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content1) != "hello" {
		t.Errorf("file1 content = %q, want %q", content1, "hello")
	}

	content2, err := os.ReadFile(filepath.Join(dstDir, "sub", "file2.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content2) != "world" {
		t.Errorf("file2 content = %q, want %q", content2, "world")
	}
}

func TestCopyDir_SingleFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("data"), 0644)

	// CopyDir with a file should fallback to CopyFile
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(dst)
	if string(got) != "data" {
		t.Errorf("content = %q, want %q", got, "data")
	}
}

func TestRemoveAll(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "todelete", "sub")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "file.txt"), []byte("x"), 0644)

	target := filepath.Join(dir, "todelete")
	if err := RemoveAll(target); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("directory should be deleted")
	}
}

func TestMkdirAll(t *testing.T) {
	dir := t.TempDir()
	newDir := filepath.Join(dir, "a", "b", "c")

	if err := MkdirAll(newDir); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestMoveFileOrDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	os.WriteFile(src, []byte("move me"), 0644)

	if err := MoveFileOrDir(src, dst); err != nil {
		t.Fatal(err)
	}

	// src should not exist
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should not exist after move")
	}

	// dst should have content
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "move me" {
		t.Errorf("content = %q, want %q", got, "move me")
	}
}
