package transfer

import (
	"bytes"
	"testing"
)

func TestCompress_Compressible(t *testing.T) {
	data := bytes.Repeat([]byte("hello world "), 100)
	compressed := Compress(data)
	if compressed == nil {
		t.Fatal("expected compression to succeed for compressible data")
	}
	if len(compressed) >= len(data) {
		t.Errorf("compressed (%d) should be smaller than original (%d)", len(compressed), len(data))
	}
	t.Logf("Compressed %d -> %d bytes (%.1f%%)", len(data), len(compressed), 100*float64(len(compressed))/float64(len(data)))
}

func TestCompress_Incompressible(t *testing.T) {
	// Pseudo-random data that doesn't compress well
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte((i*97 + 13) % 256)
	}
	compressed := Compress(data)
	if compressed != nil {
		t.Logf("Unexpectedly compressed: %d -> %d", len(data), len(compressed))
		// It's OK if it compressed, but the result must be smaller
		if len(compressed) >= len(data) {
			t.Errorf("compressed should be smaller or nil")
		}
	}
}

func TestCompress_Empty(t *testing.T) {
	compressed := Compress(nil)
	if compressed != nil {
		t.Error("expected nil for empty data")
	}

	compressed = Compress([]byte{})
	if compressed != nil {
		t.Error("expected nil for zero-length data")
	}
}

func TestDecompress_RoundTrip(t *testing.T) {
	original := bytes.Repeat([]byte("test data for round trip "), 50)
	compressed := Compress(original)
	if compressed == nil {
		t.Fatal("compression failed")
	}

	decompressed, err := Decompress(compressed, uint32(len(original)))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(decompressed, original) {
		t.Errorf("decompressed data mismatch: got %d bytes, want %d", len(decompressed), len(original))
	}
}

func TestDecompress_SizeMismatch(t *testing.T) {
	original := bytes.Repeat([]byte("data "), 100)
	compressed := Compress(original)
	if compressed == nil {
		t.Fatal("compression failed")
	}

	// Wrong size should error
	_, err := Decompress(compressed, uint32(len(original)+1))
	if err == nil {
		t.Fatal("expected size mismatch error")
	}
}

func TestDecompress_InvalidData(t *testing.T) {
	_, err := Decompress([]byte{0xFF, 0xFE, 0xFD}, 100)
	if err == nil {
		t.Fatal("expected error for invalid compressed data")
	}
}

func BenchmarkCompress_1KB(b *testing.B) {
	data := bytes.Repeat([]byte("benchmark data "), 68) // ~1KB
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compress(data)
	}
}

func BenchmarkDecompress_1KB(b *testing.B) {
	data := bytes.Repeat([]byte("benchmark data "), 68)
	compressed := Compress(data)
	origSize := uint32(len(data))
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decompress(compressed, origSize)
	}
}
