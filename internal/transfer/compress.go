package transfer

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
)

// CompressLevel defines the compression level to use.
// flate.BestSpeed (1) gives the best throughput — minimal CPU cost
// while still achieving significant compression on compressible data.
const CompressLevel = flate.BestSpeed

// Compress compresses data using DEFLATE with BestSpeed.
// Returns compressed data. If compression doesn't reduce size,
// returns nil to signal the caller should send uncompressed.
func Compress(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}

	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, CompressLevel)
	if err != nil {
		return nil
	}
	w.Write(data)
	w.Close()

	compressed := buf.Bytes()
	// Only use compression if it actually saves space
	if len(compressed) >= len(data) {
		return nil
	}
	return compressed
}

// Decompress decompresses DEFLATE data.
func Decompress(compressed []byte, originalSize uint32) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(compressed))
	defer r.Close()

	// Pre-allocate output buffer
	out := make([]byte, 0, originalSize)
	buf := bytes.NewBuffer(out)

	if _, err := io.Copy(buf, r); err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	result := buf.Bytes()
	if uint32(len(result)) != originalSize {
		return nil, fmt.Errorf("decompress: size mismatch: got %d, want %d", len(result), originalSize)
	}
	return result, nil
}
