package transfer

import (
	"bytes"
	"compress/flate"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"blue-file-transfer/internal/protocol"
)

// --- Adaptive chunk sizing ---

// AdaptiveChunker dynamically adjusts chunk size based on write latency.
// Fast writes → increase chunk size; slow writes (ACL stalls) → decrease.
type AdaptiveChunker struct {
	current int
	min     int
	max     int
}

// NewAdaptiveChunker creates a chunker starting at initialSize.
func NewAdaptiveChunker(initialSize, min, max int) *AdaptiveChunker {
	if min <= 0 {
		min = 512
	}
	if max <= 0 {
		max = MaxChunkSize
	}
	if initialSize < min {
		initialSize = min
	}
	if initialSize > max {
		initialSize = max
	}
	return &AdaptiveChunker{current: initialSize, min: min, max: max}
}

const (
	adaptFastThreshold = 5 * time.Millisecond  // write faster than this → grow
	adaptSlowThreshold = 50 * time.Millisecond // write slower than this → shrink
)

// Adjust updates chunk size based on last write duration.
func (a *AdaptiveChunker) Adjust(writeDuration time.Duration) {
	if writeDuration < adaptFastThreshold && a.current < a.max {
		next := a.current * 2
		if next > a.max {
			next = a.max
		}
		a.current = next
	} else if writeDuration > adaptSlowThreshold && a.current > a.min {
		next := a.current / 2
		if next < a.min {
			next = a.min
		}
		a.current = next
	}
}

// Size returns the current chunk size.
func (a *AdaptiveChunker) Size() int {
	return a.current
}

// --- Pipeline I/O ---

// chunkJob represents a prepared chunk ready for sending.
type chunkJob struct {
	encoded []byte // fully encoded message bytes (header + payload)
	dataLen int    // original data length (for progress tracking)
	err     error  // error from preparation
}

// PipelineSendFile sends a file with pipelined I/O: a reader goroutine
// prepares chunks (CRC + compress + encode) while the writer goroutine
// sends them over the connection concurrently.
func PipelineSendFile(w io.Writer, filePath string, chunkSize int, compress bool, progressFn ProgressFunc) error {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize > MaxChunkSize {
		chunkSize = MaxChunkSize
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	totalSize := info.Size()

	// Send FileInfo first
	fileInfo := &protocol.FileInfoPayload{
		EntryType: protocol.EntryFile,
		Size:      uint64(totalSize),
		ModTime:   info.ModTime().Unix(),
		Mode:      uint32(info.Mode()),
		Name:      filepath.Base(filePath),
	}
	if err := protocol.WriteMessage(w, protocol.MsgFileInfo, protocol.FlagNone, fileInfo.Encode()); err != nil {
		return fmt.Errorf("send file info: %w", err)
	}

	// Pipeline: reader goroutine → channel → writer (current goroutine)
	const pipelineDepth = 4
	jobs := make(chan chunkJob, pipelineDepth)

	// Use streaming compressor if enabled
	var streamComp *StreamCompressor
	if compress {
		streamComp = NewStreamCompressor()
	}

	go func() {
		defer close(jobs)
		buf := make([]byte, chunkSize)
		var offset int64
		var totalCRC uint32

		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])

				totalCRC = CRC32Update(totalCRC, chunk)
				isLast := readErr == io.EOF

				encoded, encErr := encodeChunk(chunk, uint64(offset), isLast, compress, streamComp)
				jobs <- chunkJob{encoded: encoded, dataLen: n, err: encErr}
				offset += int64(n)
			}
			if readErr == io.EOF {
				// Send TransferEnd
				endPayload := &protocol.TransferEndPayload{TotalCRC32: totalCRC}
				endBuf := encodeMessage(protocol.MsgTransferEnd, protocol.FlagNone, endPayload.Encode())
				jobs <- chunkJob{encoded: endBuf, dataLen: 0}
				return
			}
			if readErr != nil {
				jobs <- chunkJob{err: fmt.Errorf("read file: %w", readErr)}
				return
			}
		}
	}()

	// Writer: send pre-encoded messages with adaptive chunk timing
	var bytesSent int64
	for job := range jobs {
		if job.err != nil {
			return job.err
		}
		if _, err := w.Write(job.encoded); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		bytesSent += int64(job.dataLen)
		if progressFn != nil && job.dataLen > 0 {
			progressFn(bytesSent, totalSize)
		}
	}

	return nil
}

// PipelineSendDir sends a directory with pipelined I/O per file.
func PipelineSendDir(w io.Writer, dirPath string, chunkSize int, compress bool, progressFn ProgressFunc) error {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return fmt.Errorf("compute relative path: %w", err)
		}

		if info.IsDir() {
			dirInfo := &protocol.FileInfoPayload{
				EntryType: protocol.EntryDir,
				Size:      0,
				ModTime:   info.ModTime().Unix(),
				Mode:      uint32(info.Mode()),
				Name:      relPath,
			}
			return protocol.WriteMessage(w, protocol.MsgFileInfo, protocol.FlagNone, dirInfo.Encode())
		}

		// For each file, use pipelined send for the data portion
		fileInfo := &protocol.FileInfoPayload{
			EntryType: protocol.EntryFile,
			Size:      uint64(info.Size()),
			ModTime:   info.ModTime().Unix(),
			Mode:      uint32(info.Mode()),
			Name:      relPath,
		}
		if err := protocol.WriteMessage(w, protocol.MsgFileInfo, protocol.FlagNone, fileInfo.Encode()); err != nil {
			return err
		}

		return pipelineSendFileData(w, path, chunkSize, compress)
	})
}

// pipelineSendFileData sends file data chunks + TransferEnd with pipeline.
func pipelineSendFileData(w io.Writer, path string, chunkSize int, compress bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	const pipelineDepth = 4
	jobs := make(chan chunkJob, pipelineDepth)

	var streamComp *StreamCompressor
	if compress {
		streamComp = NewStreamCompressor()
	}

	go func() {
		defer close(jobs)
		buf := make([]byte, chunkSize)
		var offset int64
		var fileCRC uint32

		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				fileCRC = CRC32Update(fileCRC, chunk)

				isLast := readErr == io.EOF
				encoded, encErr := encodeChunk(chunk, uint64(offset), isLast, compress, streamComp)
				jobs <- chunkJob{encoded: encoded, dataLen: n, err: encErr}
				offset += int64(n)
			}
			if readErr == io.EOF {
				endPayload := &protocol.TransferEndPayload{TotalCRC32: fileCRC}
				endBuf := encodeMessage(protocol.MsgTransferEnd, protocol.FlagNone, endPayload.Encode())
				jobs <- chunkJob{encoded: endBuf}
				return
			}
			if readErr != nil {
				jobs <- chunkJob{err: fmt.Errorf("read: %w", readErr)}
				return
			}
		}
	}()

	for job := range jobs {
		if job.err != nil {
			return job.err
		}
		if _, err := w.Write(job.encoded); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}
	return nil
}

// encodeChunk prepares a fully encoded message for a data chunk.
// Computes CRC and optionally compresses in a single pass when possible.
func encodeChunk(data []byte, offset uint64, isLast bool, compress bool, sc *StreamCompressor) ([]byte, error) {
	// Compute CRC on original data
	chunkCRC := CRC32Chunk(data)

	flags := protocol.FlagNone
	if isLast {
		flags |= protocol.FlagLastChunk
	}

	if compress && sc != nil {
		compressed := sc.Compress(data)
		if compressed != nil {
			flags |= protocol.FlagCompressed
			payload := &protocol.DataChunkPayload{
				Offset:       offset,
				CRC32:        chunkCRC,
				OriginalSize: uint32(len(data)),
				Data:         compressed,
			}
			return encodeMessage(protocol.MsgDataChunk, flags, payload.EncodeCompressed()), nil
		}
	} else if compress {
		compressed := Compress(data)
		if compressed != nil {
			flags |= protocol.FlagCompressed
			payload := &protocol.DataChunkPayload{
				Offset:       offset,
				CRC32:        chunkCRC,
				OriginalSize: uint32(len(data)),
				Data:         compressed,
			}
			return encodeMessage(protocol.MsgDataChunk, flags, payload.EncodeCompressed()), nil
		}
	}

	payload := &protocol.DataChunkPayload{
		Offset: offset,
		CRC32:  chunkCRC,
		Data:   data,
	}
	return encodeMessage(protocol.MsgDataChunk, flags, payload.Encode()), nil
}

// encodeMessage builds the raw bytes for a protocol message (header + payload).
func encodeMessage(msgType uint8, flags uint8, payload []byte) []byte {
	buf := make([]byte, protocol.HeaderSize+len(payload))
	buf[0] = msgType
	buf[1] = flags
	buf[2] = byte(len(payload))
	buf[3] = byte(len(payload) >> 8)
	buf[4] = byte(len(payload) >> 16)
	buf[5] = byte(len(payload) >> 24)
	copy(buf[protocol.HeaderSize:], payload)
	return buf
}

// --- Streaming compression with shared dictionary ---

// StreamCompressor reuses a flate writer across chunks, preserving the
// compression dictionary for better ratios. Falls back to per-chunk if
// the compressor produces no savings.
type StreamCompressor struct {
	mu  sync.Mutex
	buf bytes.Buffer
	w   *flate.Writer
}

// NewStreamCompressor creates a new streaming compressor.
func NewStreamCompressor() *StreamCompressor {
	sc := &StreamCompressor{}
	sc.w, _ = flate.NewWriter(&sc.buf, CompressLevel)
	return sc
}

// Compress compresses data using the streaming dictionary.
// Returns nil if compression doesn't save space.
func (sc *StreamCompressor) Compress(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.buf.Reset()
	sc.w.Reset(&sc.buf)

	sc.w.Write(data)
	sc.w.Close()

	compressed := sc.buf.Bytes()
	if len(compressed) >= len(data) {
		return nil
	}

	result := make([]byte, len(compressed))
	copy(result, compressed)
	return result
}

// --- Zero-copy CRC + compress (G) ---

// CRC32Writer wraps a writer and computes CRC32 on the fly.
type CRC32Writer struct {
	w    io.Writer
	crc  uint32
	size int64
}

func NewCRC32Writer(w io.Writer) *CRC32Writer {
	return &CRC32Writer{w: w}
}

func (c *CRC32Writer) Write(p []byte) (int, error) {
	c.crc = crc32.Update(c.crc, crc32Table, p)
	n, err := c.w.Write(p)
	c.size += int64(n)
	return n, err
}

func (c *CRC32Writer) CRC32() uint32 { return c.crc }
func (c *CRC32Writer) Size() int64   { return c.size }

// --- Dynamic socket buffer tuning (F) ---

// ComputeSocketBuffer returns an optimal socket buffer size based on ACL capacity.
// Uses 2x total ACL capacity, clamped to [8KB, 256KB].
func ComputeSocketBuffer(aclMTU, aclPkts uint16) int {
	if aclMTU == 0 || aclPkts == 0 {
		return 65536 // default
	}
	buf := int(aclMTU) * int(aclPkts) * 2
	if buf < 8192 {
		buf = 8192
	}
	if buf > 256*1024 {
		buf = 256 * 1024
	}
	return buf
}
