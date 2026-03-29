package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCRC32Chunk(t *testing.T) {
	data := []byte("hello world")
	crc := CRC32Chunk(data)
	if crc == 0 {
		t.Fatal("CRC should not be zero for non-empty data")
	}

	// Same data should produce same CRC
	crc2 := CRC32Chunk(data)
	if crc != crc2 {
		t.Errorf("CRC not deterministic: %08X != %08X", crc, crc2)
	}
}

func TestCRC32Chunk_Empty(t *testing.T) {
	crc := CRC32Chunk(nil)
	if crc != 0 {
		t.Errorf("CRC of empty data should be 0, got %08X", crc)
	}
}

func TestCRC32Chunk_DifferentData(t *testing.T) {
	crc1 := CRC32Chunk([]byte("hello"))
	crc2 := CRC32Chunk([]byte("world"))
	if crc1 == crc2 {
		t.Error("different data should produce different CRC")
	}
}

func TestCRC32Update_Incremental(t *testing.T) {
	data := []byte("hello world")
	fullCRC := CRC32Chunk(data)

	// Compute incrementally
	partial := CRC32Update(0, []byte("hello "))
	incremental := CRC32Update(partial, []byte("world"))

	if incremental != fullCRC {
		t.Errorf("incremental CRC %08X != full CRC %08X", incremental, fullCRC)
	}
}

func TestCRC32File(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.bin")
	data := []byte("test file content for CRC")
	os.WriteFile(fpath, data, 0644)

	fileCRC, err := CRC32File(fpath)
	if err != nil {
		t.Fatal(err)
	}

	memCRC := CRC32Chunk(data)
	if fileCRC != memCRC {
		t.Errorf("file CRC %08X != memory CRC %08X", fileCRC, memCRC)
	}
}

func TestCRC32File_NonExistent(t *testing.T) {
	_, err := CRC32File("/nonexistent_file_12345")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestCRC32File_Empty(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "empty.bin")
	os.WriteFile(fpath, nil, 0644)

	crc, err := CRC32File(fpath)
	if err != nil {
		t.Fatal(err)
	}
	if crc != 0 {
		t.Errorf("CRC of empty file should be 0, got %08X", crc)
	}
}

func BenchmarkCRC32_32KB(b *testing.B) {
	data := make([]byte, 32*1024)
	for i := range data {
		data[i] = byte(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CRC32Chunk(data)
	}
}
