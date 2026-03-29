package transfer

import (
	"hash/crc32"
	"io"
	"os"
)

// IEEE CRC32 table (hardware-accelerated on x86_64 via SSE4.2).
var crc32Table = crc32.MakeTable(crc32.IEEE)

// CRC32Chunk computes the CRC32 checksum of a byte slice.
func CRC32Chunk(data []byte) uint32 {
	return crc32.Update(0, crc32Table, data)
}

// CRC32Update incrementally updates a CRC32 checksum.
func CRC32Update(crc uint32, data []byte) uint32 {
	return crc32.Update(crc, crc32Table, data)
}

// CRC32File computes the CRC32 checksum of an entire file.
func CRC32File(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	hash := crc32.New(crc32Table)
	if _, err := io.Copy(hash, f); err != nil {
		return 0, err
	}
	return hash.Sum32(), nil
}
