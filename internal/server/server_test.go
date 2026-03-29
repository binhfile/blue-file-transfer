package server

import (
	"os"
	"path/filepath"
	"testing"

	"blue-file-transfer/internal/bt"
	"blue-file-transfer/internal/protocol"
)

// helper to set up a server with mock transport and temp root dir.
func setupTestServer(t *testing.T) (*Server, bt.Conn, bt.Conn) {
	t.Helper()

	root := t.TempDir()
	// Create test files
	os.WriteFile(filepath.Join(root, "test.txt"), []byte("hello world"), 0644)
	os.MkdirAll(filepath.Join(root, "subdir"), 0755)
	os.WriteFile(filepath.Join(root, "subdir", "nested.txt"), []byte("nested content"), 0644)

	clientConn, serverConn := bt.NewMockConnPair()
	transport := &bt.MockTransport{
		ListenerConn: serverConn,
		ClientConn:   clientConn,
	}

	srv, err := New(transport, root, "hci0", 1)
	if err != nil {
		t.Fatal(err)
	}

	return srv, clientConn, serverConn
}

func TestServer_Pwd(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	// Send pwd request
	protocol.WriteMessage(clientConn, protocol.MsgPwd, protocol.FlagNone, nil)

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected MsgOK, got 0x%02X", msg.Header.Type)
	}

	path, _, err := protocol.DecodeString(msg.Payload, 0)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/" {
		t.Errorf("pwd = %q, want %q", path, "/")
	}

	clientConn.Close()
}

func TestServer_ListDir(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	// Send ls request
	protocol.WriteMessage(clientConn, protocol.MsgListDir, protocol.FlagNone, nil)

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	if msg.Header.Type != protocol.MsgDirListing {
		t.Fatalf("expected MsgDirListing, got 0x%02X", msg.Header.Type)
	}

	listing, err := protocol.DecodeDirListingPayload(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}

	if len(listing.Entries) != 2 { // test.txt + subdir
		t.Errorf("entries count = %d, want 2", len(listing.Entries))
	}

	clientConn.Close()
}

func TestServer_ChDir(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	// cd subdir
	protocol.WriteMessage(clientConn, protocol.MsgChDir, protocol.FlagNone, protocol.EncodeString("subdir"))

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected MsgOK for cd, got 0x%02X", msg.Header.Type)
	}

	// pwd should be /subdir now
	protocol.WriteMessage(clientConn, protocol.MsgPwd, protocol.FlagNone, nil)

	msg, err = protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	path, _, _ := protocol.DecodeString(msg.Payload, 0)
	if path != "/subdir" {
		t.Errorf("pwd = %q, want %q", path, "/subdir")
	}

	clientConn.Close()
}

func TestServer_ChDir_PathTraversal(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	// Try to cd outside root
	protocol.WriteMessage(clientConn, protocol.MsgChDir, protocol.FlagNone, protocol.EncodeString("../../etc"))

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgError {
		t.Fatalf("expected MsgError, got 0x%02X", msg.Header.Type)
	}

	clientConn.Close()
}

func TestServer_GetInfo(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	protocol.WriteMessage(clientConn, protocol.MsgGetInfo, protocol.FlagNone, protocol.EncodeString("test.txt"))

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	if msg.Header.Type != protocol.MsgFileInfo {
		t.Fatalf("expected MsgFileInfo, got 0x%02X", msg.Header.Type)
	}

	info, _, err := protocol.DecodeFileInfoPayload(msg.Payload, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "test.txt" {
		t.Errorf("name = %q, want %q", info.Name, "test.txt")
	}
	if info.Size != 11 { // "hello world"
		t.Errorf("size = %d, want 11", info.Size)
	}

	clientConn.Close()
}

func TestServer_Mkdir(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	protocol.WriteMessage(clientConn, protocol.MsgMkdir, protocol.FlagNone, protocol.EncodeString("newdir"))

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected MsgOK, got 0x%02X", msg.Header.Type)
	}

	// Verify directory exists
	info, err := os.Stat(filepath.Join(srv.rootDir, "newdir"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}

	clientConn.Close()
}

func TestServer_Delete(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	protocol.WriteMessage(clientConn, protocol.MsgDelete, protocol.FlagNone, protocol.EncodeString("test.txt"))

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected MsgOK, got 0x%02X", msg.Header.Type)
	}

	// Verify file is deleted
	if _, err := os.Stat(filepath.Join(srv.rootDir, "test.txt")); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}

	clientConn.Close()
}

func TestServer_Delete_Root_Denied(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)
	_ = srv

	go srv.ServeConn(serverConn)

	// Try to delete root directory
	protocol.WriteMessage(clientConn, protocol.MsgDelete, protocol.FlagNone, protocol.EncodeString("."))

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgError {
		t.Fatalf("expected MsgError (cannot delete root), got 0x%02X", msg.Header.Type)
	}

	clientConn.Close()
}

func TestServer_Copy(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	payload := protocol.EncodeTwoStrings("test.txt", "test_copy.txt")
	protocol.WriteMessage(clientConn, protocol.MsgCopy, protocol.FlagNone, payload)

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected MsgOK, got 0x%02X", msg.Header.Type)
	}

	// Verify copy exists with same content
	content, err := os.ReadFile(filepath.Join(srv.rootDir, "test_copy.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello world" {
		t.Errorf("copy content = %q, want %q", content, "hello world")
	}

	clientConn.Close()
}

func TestServer_Move(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	payload := protocol.EncodeTwoStrings("test.txt", "renamed.txt")
	protocol.WriteMessage(clientConn, protocol.MsgMove, protocol.FlagNone, payload)

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected MsgOK, got 0x%02X", msg.Header.Type)
	}

	// Original should be gone
	if _, err := os.Stat(filepath.Join(srv.rootDir, "test.txt")); !os.IsNotExist(err) {
		t.Error("original should not exist after move")
	}

	// New should exist
	content, err := os.ReadFile(filepath.Join(srv.rootDir, "renamed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello world" {
		t.Errorf("content = %q, want %q", content, "hello world")
	}

	clientConn.Close()
}

func TestServer_GetInfo_NonExistent(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)
	_ = srv

	go srv.ServeConn(serverConn)

	protocol.WriteMessage(clientConn, protocol.MsgGetInfo, protocol.FlagNone, protocol.EncodeString("nonexistent.txt"))

	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgError {
		t.Fatalf("expected MsgError, got 0x%02X", msg.Header.Type)
	}

	clientConn.Close()
}

func TestServer_Download_File(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)
	_ = srv

	go srv.ServeConn(serverConn)

	// Request download
	protocol.WriteMessage(clientConn, protocol.MsgDownload, protocol.FlagNone, protocol.EncodeString("test.txt"))

	// Should receive FileInfo
	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgFileInfo {
		t.Fatalf("expected MsgFileInfo, got 0x%02X", msg.Header.Type)
	}

	info, _, _ := protocol.DecodeFileInfoPayload(msg.Payload, 0)
	if info.Name != "test.txt" {
		t.Errorf("name = %q", info.Name)
	}

	// Should receive DataChunk(s) + TransferEnd
	var receivedData []byte
	for {
		msg, err := protocol.ReadMessage(clientConn)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Header.Type == protocol.MsgDataChunk {
			chunk, _ := protocol.DecodeDataChunkPayload(msg.Payload)
			receivedData = append(receivedData, chunk.Data...)
		} else if msg.Header.Type == protocol.MsgTransferEnd {
			break
		} else {
			t.Fatalf("unexpected type 0x%02X", msg.Header.Type)
		}
	}

	if string(receivedData) != "hello world" {
		t.Errorf("received data = %q, want %q", receivedData, "hello world")
	}

	clientConn.Close()
}

func TestServer_Upload_File(t *testing.T) {
	srv, clientConn, serverConn := setupTestServer(t)

	go srv.ServeConn(serverConn)

	// Send upload request
	req := &protocol.UploadRequestPayload{
		Path:      "uploaded.txt",
		TotalSize: 13,
		ChunkSize: 32768,
		IsDir:     false,
	}
	protocol.WriteMessage(clientConn, protocol.MsgUpload, protocol.FlagNone, req.Encode())

	// Wait for OK
	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected MsgOK, got 0x%02X", msg.Header.Type)
	}

	// Send file info
	fileInfo := &protocol.FileInfoPayload{
		EntryType: protocol.EntryFile,
		Size:      13,
		ModTime:   1000,
		Mode:      0644,
		Name:      "uploaded.txt",
	}
	protocol.WriteMessage(clientConn, protocol.MsgFileInfo, protocol.FlagNone, fileInfo.Encode())

	// Send data chunk
	data := []byte("upload content")
	chunk := &protocol.DataChunkPayload{
		Offset: 0,
		CRC32:  crc32Chunk(data),
		Data:   data,
	}
	protocol.WriteMessage(clientConn, protocol.MsgDataChunk, protocol.FlagLastChunk, chunk.Encode())

	// Send transfer end
	endPayload := &protocol.TransferEndPayload{TotalCRC32: crc32Chunk(data)}
	protocol.WriteMessage(clientConn, protocol.MsgTransferEnd, protocol.FlagNone, endPayload.Encode())

	// Wait for final OK
	msg, err = protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Type != protocol.MsgOK {
		t.Fatalf("expected final MsgOK, got 0x%02X", msg.Header.Type)
	}

	// Verify file on server
	content, err := os.ReadFile(filepath.Join(srv.rootDir, "uploaded.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "upload content" {
		t.Errorf("content = %q, want %q", content, "upload content")
	}

	clientConn.Close()
}

func crc32Chunk(data []byte) uint32 {
	// Inline IEEE CRC32 for test independence
	var crc uint32 = 0xFFFFFFFF
	for _, b := range data {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xFFFFFFFF
}
