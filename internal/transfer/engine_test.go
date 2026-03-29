package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"blue-file-transfer/internal/protocol"
)

func TestSendReceiveFile_Small(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "small.txt")
	content := []byte("hello world, this is a small test file")
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	if err := SendFile(&buf, srcFile, DefaultChunkSize, false, nil); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	result, err := ReceiveFile(&buf, dstDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestSendReceiveFile_Empty(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "empty.txt")
	os.WriteFile(srcFile, nil, 0644)

	var buf bytes.Buffer
	if err := SendFile(&buf, srcFile, DefaultChunkSize, false, nil); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	result, err := ReceiveFile(&buf, dstDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(result)
	if len(got) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(got))
	}
}

func TestSendReceiveFile_LargeMultiChunk(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "large.bin")

	chunkSize := 1024
	content := make([]byte, chunkSize*5+500)
	for i := range content {
		content[i] = byte(i % 256)
	}
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	if err := SendFile(&buf, srcFile, chunkSize, false, nil); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	result, err := ReceiveFile(&buf, dstDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(result)
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

func TestSendReceiveFile_Progress(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "progress.bin")
	content := make([]byte, 4096)
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	var sendProgress []int64
	sendFn := func(transferred, total int64) {
		sendProgress = append(sendProgress, transferred)
	}

	if err := SendFile(&buf, srcFile, 1024, false, sendFn); err != nil {
		t.Fatal(err)
	}

	if len(sendProgress) == 0 {
		t.Error("progress callback was never called")
	}
	if sendProgress[len(sendProgress)-1] != 4096 {
		t.Errorf("final progress = %d, want 4096", sendProgress[len(sendProgress)-1])
	}

	var recvProgress []int64
	recvFn := func(transferred, total int64) {
		recvProgress = append(recvProgress, transferred)
	}

	dstDir := t.TempDir()
	_, err := ReceiveFile(&buf, dstDir, recvFn)
	if err != nil {
		t.Fatal(err)
	}

	if len(recvProgress) == 0 {
		t.Error("receive progress callback was never called")
	}
}

func TestReceiveFile_CRCMismatch(t *testing.T) {
	var buf bytes.Buffer

	fileInfo := &protocol.FileInfoPayload{
		EntryType: protocol.EntryFile,
		Size:      5,
		ModTime:   1000,
		Mode:      0644,
		Name:      "bad.txt",
	}
	protocol.WriteMessage(&buf, protocol.MsgFileInfo, protocol.FlagNone, fileInfo.Encode())

	chunk := &protocol.DataChunkPayload{
		Offset: 0,
		CRC32:  0xDEADBEEF,
		Data:   []byte("hello"),
	}
	protocol.WriteMessage(&buf, protocol.MsgDataChunk, protocol.FlagLastChunk, chunk.Encode())

	dstDir := t.TempDir()
	_, err := ReceiveFile(&buf, dstDir, nil)
	if err == nil {
		t.Fatal("expected CRC mismatch error")
	}
}

func TestSendReceiveDir(t *testing.T) {
	srcDir := t.TempDir()

	os.MkdirAll(filepath.Join(srcDir, "sub1"), 0755)
	os.MkdirAll(filepath.Join(srcDir, "sub2"), 0755)
	os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root file"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub1", "a.txt"), []byte("file a"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub2", "b.txt"), []byte("file b content here"), 0644)

	var buf bytes.Buffer
	if err := SendDir(&buf, srcDir, 1024, false, nil); err != nil {
		t.Fatal(err)
	}

	protocol.WriteMessage(&buf, protocol.MsgOK, protocol.FlagNone, nil)

	dstDir := t.TempDir()
	if err := ReceiveDir(&buf, dstDir, nil); err != nil {
		t.Fatal(err)
	}

	got1, _ := os.ReadFile(filepath.Join(dstDir, "root.txt"))
	if string(got1) != "root file" {
		t.Errorf("root.txt = %q, want %q", got1, "root file")
	}

	got2, _ := os.ReadFile(filepath.Join(dstDir, "sub1", "a.txt"))
	if string(got2) != "file a" {
		t.Errorf("sub1/a.txt = %q, want %q", got2, "file a")
	}

	got3, _ := os.ReadFile(filepath.Join(dstDir, "sub2", "b.txt"))
	if string(got3) != "file b content here" {
		t.Errorf("sub2/b.txt = %q, want %q", got3, "file b content here")
	}
}

func TestSendReceiveDir_Empty(t *testing.T) {
	srcDir := t.TempDir()

	var buf bytes.Buffer
	if err := SendDir(&buf, srcDir, 1024, false, nil); err != nil {
		t.Fatal(err)
	}
	protocol.WriteMessage(&buf, protocol.MsgOK, protocol.FlagNone, nil)

	dstDir := t.TempDir()
	if err := ReceiveDir(&buf, dstDir, nil); err != nil {
		t.Fatal(err)
	}
}

// --- Compression tests ---

func TestSendReceiveFile_Compressed(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "compressible.txt")
	// Highly compressible: repeated text
	content := bytes.Repeat([]byte("hello world, this is compressible data! "), 100)
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	if err := SendFile(&buf, srcFile, 1024, true, nil); err != nil {
		t.Fatal(err)
	}

	wireSize := buf.Len()

	dstDir := t.TempDir()
	result, err := ReceiveFile(&buf, dstDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(result)
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch after decompress: got %d bytes, want %d bytes", len(got), len(content))
	}

	// Wire size should be smaller than original content
	if wireSize >= len(content) {
		t.Errorf("compression didn't reduce size: wire=%d, original=%d", wireSize, len(content))
	}
	t.Logf("Compression: %d -> %d bytes (%.1f%% reduction)", len(content), wireSize, 100*(1-float64(wireSize)/float64(len(content))))
}

func TestSendReceiveFile_CompressedIncompressible(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "random.bin")
	// Random-like data: not compressible
	content := make([]byte, 4096)
	for i := range content {
		content[i] = byte((i * 97 + 13) % 256)
	}
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	if err := SendFile(&buf, srcFile, 1024, true, nil); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	result, err := ReceiveFile(&buf, dstDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(result)
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

func TestSendReceiveDir_Compressed(t *testing.T) {
	srcDir := t.TempDir()

	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "text.txt"),
		bytes.Repeat([]byte("compressible text data\n"), 50), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "data.csv"),
		bytes.Repeat([]byte("col1,col2,col3,col4\n"), 100), 0644)

	var buf bytes.Buffer
	if err := SendDir(&buf, srcDir, 1024, true, nil); err != nil {
		t.Fatal(err)
	}
	protocol.WriteMessage(&buf, protocol.MsgOK, protocol.FlagNone, nil)

	dstDir := t.TempDir()
	if err := ReceiveDir(&buf, dstDir, nil); err != nil {
		t.Fatal(err)
	}

	got1, _ := os.ReadFile(filepath.Join(dstDir, "text.txt"))
	expected1 := bytes.Repeat([]byte("compressible text data\n"), 50)
	if !bytes.Equal(got1, expected1) {
		t.Errorf("text.txt mismatch: got %d bytes, want %d", len(got1), len(expected1))
	}

	got2, _ := os.ReadFile(filepath.Join(dstDir, "sub", "data.csv"))
	expected2 := bytes.Repeat([]byte("col1,col2,col3,col4\n"), 100)
	if !bytes.Equal(got2, expected2) {
		t.Errorf("sub/data.csv mismatch: got %d bytes, want %d", len(got2), len(expected2))
	}
}

func TestSendReceiveFile_CompressedEmpty(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "empty.txt")
	os.WriteFile(srcFile, nil, 0644)

	var buf bytes.Buffer
	if err := SendFile(&buf, srcFile, DefaultChunkSize, true, nil); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	result, err := ReceiveFile(&buf, dstDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(result)
	if len(got) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(got))
	}
}

func BenchmarkSendFile(b *testing.B) {
	dir := b.TempDir()
	fpath := filepath.Join(dir, "bench.bin")
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i)
	}
	os.WriteFile(fpath, data, 0644)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		SendFile(&buf, fpath, DefaultChunkSize, false, nil)
	}
}

func BenchmarkSendFile_Compressed(b *testing.B) {
	dir := b.TempDir()
	fpath := filepath.Join(dir, "bench.txt")
	data := bytes.Repeat([]byte("benchmark compressible data line\n"), 32*1024)
	os.WriteFile(fpath, data, 0644)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		SendFile(&buf, fpath, DefaultChunkSize, true, nil)
	}
}
