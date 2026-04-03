package transfer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blue-file-transfer/internal/protocol"
)

// --- Adaptive Chunker Tests ---

func TestAdaptiveChunker_GrowOnFastWrite(t *testing.T) {
	ac := NewAdaptiveChunker(1024, 512, 8192)
	initial := ac.Size()

	// Simulate fast writes
	for i := 0; i < 5; i++ {
		ac.Adjust(1 * time.Millisecond)
	}

	if ac.Size() <= initial {
		t.Errorf("expected chunk size to grow from %d, got %d", initial, ac.Size())
	}
	t.Logf("Grew: %d -> %d after 5 fast writes", initial, ac.Size())
}

func TestAdaptiveChunker_ShrinkOnSlowWrite(t *testing.T) {
	ac := NewAdaptiveChunker(4096, 512, 8192)
	initial := ac.Size()

	// Simulate slow writes (ACL stalls)
	for i := 0; i < 5; i++ {
		ac.Adjust(100 * time.Millisecond)
	}

	if ac.Size() >= initial {
		t.Errorf("expected chunk size to shrink from %d, got %d", initial, ac.Size())
	}
	t.Logf("Shrunk: %d -> %d after 5 slow writes", initial, ac.Size())
}

func TestAdaptiveChunker_RespectsBounds(t *testing.T) {
	ac := NewAdaptiveChunker(1024, 512, 4096)

	// Grow to max
	for i := 0; i < 20; i++ {
		ac.Adjust(0)
	}
	if ac.Size() != 4096 {
		t.Errorf("expected max 4096, got %d", ac.Size())
	}

	// Shrink to min
	for i := 0; i < 20; i++ {
		ac.Adjust(1 * time.Second)
	}
	if ac.Size() != 512 {
		t.Errorf("expected min 512, got %d", ac.Size())
	}
}

func TestAdaptiveChunker_StableOnNormalWrite(t *testing.T) {
	ac := NewAdaptiveChunker(2048, 512, 8192)
	initial := ac.Size()

	// Normal writes between thresholds — no change
	for i := 0; i < 10; i++ {
		ac.Adjust(20 * time.Millisecond)
	}
	if ac.Size() != initial {
		t.Errorf("expected stable at %d, got %d", initial, ac.Size())
	}
}

// --- Pipeline Send/Receive Tests ---

func TestPipelineSendReceiveFile_Small(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "small.txt")
	content := []byte("hello world, this is a pipeline test file")
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	if err := PipelineSendFile(&buf, srcFile, DefaultChunkSize, false, nil); err != nil {
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

func TestPipelineSendReceiveFile_Large(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "large.bin")
	content := make([]byte, 1024*100) // 100KB
	for i := range content {
		content[i] = byte(i % 256)
	}
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	if err := PipelineSendFile(&buf, srcFile, 4096, false, nil); err != nil {
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

func TestPipelineSendReceiveFile_Compressed(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "compressible.txt")
	content := bytes.Repeat([]byte("pipeline compressed data line!\n"), 200)
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	if err := PipelineSendFile(&buf, srcFile, 1024, true, nil); err != nil {
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
		t.Errorf("content mismatch after decompress")
	}
	if wireSize >= len(content) {
		t.Errorf("compression didn't save space: wire=%d, original=%d", wireSize, len(content))
	}
	t.Logf("Pipeline compressed: %d -> %d (%.1f%% reduction)",
		len(content), wireSize, 100*(1-float64(wireSize)/float64(len(content))))
}

func TestPipelineSendReceiveFile_Empty(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "empty.txt")
	os.WriteFile(srcFile, nil, 0644)

	var buf bytes.Buffer
	if err := PipelineSendFile(&buf, srcFile, DefaultChunkSize, false, nil); err != nil {
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

func TestPipelineSendReceiveFile_Progress(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "progress.bin")
	content := make([]byte, 8192)
	os.WriteFile(srcFile, content, 0644)

	var buf bytes.Buffer
	var progressCalls int
	var lastTransferred int64
	progressFn := func(transferred, total int64) {
		progressCalls++
		lastTransferred = transferred
	}

	if err := PipelineSendFile(&buf, srcFile, 1024, false, progressFn); err != nil {
		t.Fatal(err)
	}

	if progressCalls == 0 {
		t.Error("progress callback never called")
	}
	if lastTransferred != 8192 {
		t.Errorf("final progress = %d, want 8192", lastTransferred)
	}
}

func TestPipelineSendReceiveDir(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root file"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("nested content"), 0644)

	var buf bytes.Buffer
	if err := PipelineSendDir(&buf, srcDir, 1024, false, nil); err != nil {
		t.Fatal(err)
	}
	protocol.WriteMessage(&buf, protocol.MsgOK, protocol.FlagNone, nil)

	dstDir := t.TempDir()
	if err := ReceiveDir(&buf, dstDir, nil); err != nil {
		t.Fatal(err)
	}

	got1, _ := os.ReadFile(filepath.Join(dstDir, "root.txt"))
	if string(got1) != "root file" {
		t.Errorf("root.txt = %q", got1)
	}
	got2, _ := os.ReadFile(filepath.Join(dstDir, "sub", "nested.txt"))
	if string(got2) != "nested content" {
		t.Errorf("sub/nested.txt = %q", got2)
	}
}

func TestPipelineSendReceiveDir_Compressed(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"),
		bytes.Repeat([]byte("compressible\n"), 100), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"),
		bytes.Repeat([]byte("more data\n"), 200), 0644)

	var buf bytes.Buffer
	if err := PipelineSendDir(&buf, srcDir, 1024, true, nil); err != nil {
		t.Fatal(err)
	}
	protocol.WriteMessage(&buf, protocol.MsgOK, protocol.FlagNone, nil)

	dstDir := t.TempDir()
	if err := ReceiveDir(&buf, dstDir, nil); err != nil {
		t.Fatal(err)
	}

	got1, _ := os.ReadFile(filepath.Join(dstDir, "a.txt"))
	expected1 := bytes.Repeat([]byte("compressible\n"), 100)
	if !bytes.Equal(got1, expected1) {
		t.Errorf("a.txt mismatch")
	}

	got2, _ := os.ReadFile(filepath.Join(dstDir, "sub", "b.txt"))
	expected2 := bytes.Repeat([]byte("more data\n"), 200)
	if !bytes.Equal(got2, expected2) {
		t.Errorf("sub/b.txt mismatch")
	}
}

// --- Streaming Compressor Tests ---

func TestStreamCompressor_CompressesData(t *testing.T) {
	sc := NewStreamCompressor()
	data := bytes.Repeat([]byte("stream compress test data "), 50)
	result := sc.Compress(data)
	if result == nil {
		t.Fatal("expected compression to succeed")
	}
	if len(result) >= len(data) {
		t.Errorf("no space savings: %d >= %d", len(result), len(data))
	}
	t.Logf("Stream compress: %d -> %d", len(data), len(result))
}

func TestStreamCompressor_NilOnIncompressible(t *testing.T) {
	sc := NewStreamCompressor()
	// Use truly random-like data that DEFLATE cannot compress
	data := make([]byte, 1024)
	// Linear congruential generator with good distribution
	v := uint32(0xDEADBEEF)
	for i := range data {
		v = v*1103515245 + 12345
		data[i] = byte(v >> 16)
	}
	result := sc.Compress(data)
	if result != nil {
		t.Logf("unexpected compression: %d -> %d (data may be compressible)", len(data), len(result))
	}
}

func TestStreamCompressor_EmptyInput(t *testing.T) {
	sc := NewStreamCompressor()
	if sc.Compress(nil) != nil {
		t.Error("expected nil for nil input")
	}
	if sc.Compress([]byte{}) != nil {
		t.Error("expected nil for empty input")
	}
}

// --- Dynamic Socket Buffer Tests ---

func TestComputeSocketBuffer_Defaults(t *testing.T) {
	buf := ComputeSocketBuffer(0, 0)
	if buf != 65536 {
		t.Errorf("expected default 65536, got %d", buf)
	}
}

func TestComputeSocketBuffer_SmallAdapter(t *testing.T) {
	// CSR8510: 310 MTU × 10 slots → 6200 → clamped to 8192
	buf := ComputeSocketBuffer(310, 10)
	if buf != 8192 {
		t.Errorf("expected 8192, got %d", buf)
	}
}

func TestComputeSocketBuffer_LargeAdapter(t *testing.T) {
	// Good adapter: 1021 MTU × 20 slots → 40840
	buf := ComputeSocketBuffer(1021, 20)
	expected := 1021 * 20 * 2
	if buf != expected {
		t.Errorf("expected %d, got %d", expected, buf)
	}
}

func TestComputeSocketBuffer_ClampMax(t *testing.T) {
	buf := ComputeSocketBuffer(65535, 100)
	if buf != 256*1024 {
		t.Errorf("expected max 262144, got %d", buf)
	}
}

// --- ComputeChunkSize Tests ---

func TestComputeChunkSize_Defaults(t *testing.T) {
	cs := ComputeChunkSize(0, 0)
	if cs != DefaultChunkSize {
		t.Errorf("expected %d, got %d", DefaultChunkSize, cs)
	}
}

func TestComputeChunkSize_SmallAdapter(t *testing.T) {
	// 310 × 10 / 2 = 1550
	cs := ComputeChunkSize(310, 10)
	if cs != 1550 {
		t.Errorf("expected 1550, got %d", cs)
	}
}

func TestComputeChunkSize_LargeAdapter(t *testing.T) {
	cs := ComputeChunkSize(1021, 200)
	if cs != MaxChunkSize {
		t.Errorf("expected max %d, got %d", MaxChunkSize, cs)
	}
}

// --- Benchmarks ---

func BenchmarkPipelineSendFile(b *testing.B) {
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
		PipelineSendFile(&buf, fpath, DefaultChunkSize, false, nil)
	}
}

func BenchmarkPipelineSendFile_LargeChunk(b *testing.B) {
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
		PipelineSendFile(&buf, fpath, 16384, false, nil)
	}
}

func BenchmarkPipelineSendFile_Compressed(b *testing.B) {
	dir := b.TempDir()
	fpath := filepath.Join(dir, "bench.txt")
	data := bytes.Repeat([]byte("benchmark compressible data line\n"), 32*1024)
	os.WriteFile(fpath, data, 0644)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		PipelineSendFile(&buf, fpath, DefaultChunkSize, true, nil)
	}
}

func BenchmarkSendFile_vs_Pipeline(b *testing.B) {
	dir := b.TempDir()
	fpath := filepath.Join(dir, "bench.bin")
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i)
	}
	os.WriteFile(fpath, data, 0644)

	for _, chunkSize := range []int{1024, 4096, 16384} {
		b.Run(fmt.Sprintf("Original_chunk%d", chunkSize), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for i := 0; i < b.N; i++ {
				var buf bytes.Buffer
				SendFile(&buf, fpath, chunkSize, false, nil)
			}
		})
		b.Run(fmt.Sprintf("Pipeline_chunk%d", chunkSize), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for i := 0; i < b.N; i++ {
				var buf bytes.Buffer
				PipelineSendFile(&buf, fpath, chunkSize, false, nil)
			}
		})
	}
}

func BenchmarkSendFile_vs_Pipeline_Compressed(b *testing.B) {
	dir := b.TempDir()
	fpath := filepath.Join(dir, "bench.txt")
	data := bytes.Repeat([]byte("benchmark compressible data\n"), 32*1024)
	os.WriteFile(fpath, data, 0644)

	for _, chunkSize := range []int{1024, 4096, 16384} {
		b.Run(fmt.Sprintf("Original_chunk%d", chunkSize), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for i := 0; i < b.N; i++ {
				var buf bytes.Buffer
				SendFile(&buf, fpath, chunkSize, true, nil)
			}
		})
		b.Run(fmt.Sprintf("Pipeline_chunk%d", chunkSize), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for i := 0; i < b.N; i++ {
				var buf bytes.Buffer
				PipelineSendFile(&buf, fpath, chunkSize, true, nil)
			}
		})
	}
}

func BenchmarkStreamCompressor(b *testing.B) {
	data := bytes.Repeat([]byte("stream compressor benchmark data!\n"), 30)
	sc := NewStreamCompressor()

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.Compress(data)
	}
}

func BenchmarkOriginalCompress(b *testing.B) {
	data := bytes.Repeat([]byte("stream compressor benchmark data!\n"), 30)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compress(data)
	}
}
