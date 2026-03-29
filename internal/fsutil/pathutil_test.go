package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizePath_ValidRelative(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "subdir")
	os.MkdirAll(subdir, 0755)

	result, err := SanitizePath(root, "subdir")
	if err != nil {
		t.Fatal(err)
	}
	if result != subdir {
		t.Errorf("got %q, want %q", result, subdir)
	}
}

func TestSanitizePath_ValidAbsoluteWithinRoot(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "subdir")
	os.MkdirAll(subdir, 0755)

	result, err := SanitizePath(root, subdir)
	if err != nil {
		t.Fatal(err)
	}
	if result != subdir {
		t.Errorf("got %q, want %q", result, subdir)
	}
}

func TestSanitizePath_DotDotTraversal(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a"), 0755)

	_, err := SanitizePath(root, "../../etc/passwd")
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestSanitizePath_DotDotComplex(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "b"), 0755)

	_, err := SanitizePath(root, "a/b/../../../etc")
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestSanitizePath_AbsoluteOutsideRoot(t *testing.T) {
	root := t.TempDir()

	_, err := SanitizePath(root, "/etc/passwd")
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestSanitizePath_SymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	linkPath := filepath.Join(root, "evil_link")
	os.Symlink("/etc", linkPath)

	_, err := SanitizePath(root, "evil_link")
	if err == nil {
		t.Fatal("expected path traversal error for symlink outside root")
	}
}

func TestSanitizePath_SymlinkWithinRoot(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "target")
	os.MkdirAll(subdir, 0755)
	linkPath := filepath.Join(root, "link")
	os.Symlink(subdir, linkPath)

	result, err := SanitizePath(root, "link")
	if err != nil {
		t.Fatal(err)
	}
	if result != subdir {
		t.Errorf("got %q, want %q", result, subdir)
	}
}

func TestSanitizePath_NonExistentWithinRoot(t *testing.T) {
	root := t.TempDir()

	result, err := SanitizePath(root, "newfile.txt")
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(root, "newfile.txt")
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestSanitizePath_NonExistentOutsideRoot(t *testing.T) {
	root := t.TempDir()

	_, err := SanitizePath(root, "../newfile.txt")
	if err == nil {
		t.Fatal("expected error for non-existent path outside root")
	}
}

func TestSanitizePath_RootItself(t *testing.T) {
	root := t.TempDir()

	result, err := SanitizePath(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	absRoot, _ := filepath.Abs(root)
	if result != absRoot {
		t.Errorf("got %q, want %q", result, absRoot)
	}
}

func TestSanitizePath_DeepNested(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c", "d", "e")
	os.MkdirAll(deep, 0755)

	result, err := SanitizePath(root, "a/b/c/d/e")
	if err != nil {
		t.Fatal(err)
	}
	if result != deep {
		t.Errorf("got %q, want %q", result, deep)
	}
}

func TestResolveCwd_RelativeTarget(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "sub")
	os.MkdirAll(subdir, 0755)

	result, err := ResolveCwd(root, root, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if result != subdir {
		t.Errorf("got %q, want %q", result, subdir)
	}
}

func TestResolveCwd_DotDot(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "sub")
	os.MkdirAll(subdir, 0755)

	// Going up from sub should give root
	result, err := ResolveCwd(root, subdir, "..")
	if err != nil {
		t.Fatal(err)
	}
	absRoot, _ := filepath.Abs(root)
	if result != absRoot {
		t.Errorf("got %q, want %q", result, absRoot)
	}
}

func TestResolveCwd_DotDotBeyondRoot(t *testing.T) {
	root := t.TempDir()

	_, err := ResolveCwd(root, root, "..")
	if err == nil {
		t.Fatal("expected error for going beyond root")
	}
}
