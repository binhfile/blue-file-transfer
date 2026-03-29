package client

import (
	"os"
	"path/filepath"
	"testing"

	"blue-file-transfer/internal/bt"
	"blue-file-transfer/internal/server"
)

// setupClientServer creates a client connected to a server via mock transport.
func setupClientServer(t *testing.T) (*Client, *server.Server, string) {
	t.Helper()

	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello from server"), 0644)
	os.MkdirAll(filepath.Join(root, "docs"), 0755)
	os.WriteFile(filepath.Join(root, "docs", "readme.txt"), []byte("readme content"), 0644)

	clientConn, serverConn := bt.NewMockConnPair()
	transport := &bt.MockTransport{
		ListenerConn: serverConn,
		ClientConn:   clientConn,
	}

	srv, err := server.New(transport, root, "hci0", 1)
	if err != nil {
		t.Fatal(err)
	}

	// Run server in background
	go srv.ServeConn(serverConn)

	c := New(transport, "hci0")
	c.ConnectWithConn(clientConn)

	return c, srv, root
}

func TestClient_Pwd(t *testing.T) {
	c, _, _ := setupClientServer(t)
	defer c.Disconnect()

	path, err := c.Pwd()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/" {
		t.Errorf("pwd = %q, want %q", path, "/")
	}
}

func TestClient_ListDir(t *testing.T) {
	c, _, _ := setupClientServer(t)
	defer c.Disconnect()

	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}

	if len(listing.Entries) != 2 { // hello.txt + docs
		t.Errorf("entries = %d, want 2", len(listing.Entries))
	}
}

func TestClient_ChDir(t *testing.T) {
	c, _, _ := setupClientServer(t)
	defer c.Disconnect()

	if err := c.ChDir("docs"); err != nil {
		t.Fatal(err)
	}

	path, err := c.Pwd()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/docs" {
		t.Errorf("pwd = %q, want %q", path, "/docs")
	}
}

func TestClient_GetInfo(t *testing.T) {
	c, _, _ := setupClientServer(t)
	defer c.Disconnect()

	info, err := c.GetInfo("hello.txt")
	if err != nil {
		t.Fatal(err)
	}

	if info.Name != "hello.txt" {
		t.Errorf("name = %q", info.Name)
	}
	if info.Size != 17 { // "hello from server"
		t.Errorf("size = %d, want 17", info.Size)
	}
}

func TestClient_Mkdir(t *testing.T) {
	c, _, root := setupClientServer(t)
	defer c.Disconnect()

	if err := c.Mkdir("newdir"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(root, "newdir"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestClient_Delete(t *testing.T) {
	c, _, root := setupClientServer(t)
	defer c.Disconnect()

	if err := c.Delete("hello.txt"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "hello.txt")); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestClient_Copy(t *testing.T) {
	c, _, root := setupClientServer(t)
	defer c.Disconnect()

	if err := c.Copy("hello.txt", "hello_copy.txt"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(root, "hello_copy.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello from server" {
		t.Errorf("content = %q", content)
	}
}

func TestClient_Move(t *testing.T) {
	c, _, root := setupClientServer(t)
	defer c.Disconnect()

	if err := c.Move("hello.txt", "moved.txt"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "hello.txt")); !os.IsNotExist(err) {
		t.Error("original should not exist")
	}

	content, err := os.ReadFile(filepath.Join(root, "moved.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello from server" {
		t.Errorf("content = %q", content)
	}
}

func TestClient_Download_File(t *testing.T) {
	c, _, _ := setupClientServer(t)
	defer c.Disconnect()

	localDir := t.TempDir()
	result, err := c.Download("hello.txt", localDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello from server" {
		t.Errorf("content = %q, want %q", content, "hello from server")
	}
}

func TestClient_Upload_File(t *testing.T) {
	c, _, root := setupClientServer(t)
	defer c.Disconnect()

	// Create local file to upload
	localDir := t.TempDir()
	localFile := filepath.Join(localDir, "upload.txt")
	os.WriteFile(localFile, []byte("uploaded content"), 0644)

	if err := c.Upload(localFile, "upload.txt", nil); err != nil {
		t.Fatal(err)
	}

	// Verify on server
	content, err := os.ReadFile(filepath.Join(root, "upload.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "uploaded content" {
		t.Errorf("content = %q, want %q", content, "uploaded content")
	}
}

func TestClient_ChDir_PathTraversal(t *testing.T) {
	c, _, _ := setupClientServer(t)
	defer c.Disconnect()

	err := c.ChDir("../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestClient_GetInfo_NonExistent(t *testing.T) {
	c, _, _ := setupClientServer(t)
	defer c.Disconnect()

	_, err := c.GetInfo("nonexistent.txt")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"ls", []string{"ls"}},
		{"ls /path", []string{"ls", "/path"}},
		{`cp "file with spaces.txt" dst`, []string{"cp", "file with spaces.txt", "dst"}},
		{`upload 'my file.txt' remote`, []string{"upload", "my file.txt", "remote"}},
		{"  spaces  around  ", []string{"spaces", "around"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := splitArgs(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitArgs(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitArgs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
	}

	for _, tt := range tests {
		got := formatSize(tt.input)
		if got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
