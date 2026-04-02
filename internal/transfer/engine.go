package transfer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"blue-file-transfer/internal/protocol"
)

// DefaultChunkSize is the default transfer chunk size.
// Tuned for RFCOMM over Bluetooth Classic: larger chunks cause flow control
// stalls on controllers with small ACL buffers (e.g., CSR8510 with MTU=310, 10 slots).
// 1024 bytes achieves ~16 KB/s sustained; larger sizes cause periodic stalls
// that reduce average throughput significantly.
const DefaultChunkSize = 1024

// MaxChunkSize is the maximum allowed chunk size (64 KB).
// Useful for high-throughput controllers with large ACL buffers.
const MaxChunkSize = 64 * 1024

// ComputeChunkSize derives a transfer chunk size from adapter ACL parameters.
// Uses half the total ACL buffer capacity (acl_mtu * acl_pkts) to leave
// headroom for flow control. Returns DefaultChunkSize if ACL info is unknown.
func ComputeChunkSize(aclMTU, aclPkts uint16) int {
	if aclMTU == 0 || aclPkts == 0 {
		return DefaultChunkSize
	}
	capacity := int(aclMTU) * int(aclPkts)
	preferred := capacity / 2
	if preferred < DefaultChunkSize {
		preferred = DefaultChunkSize
	}
	if preferred > MaxChunkSize {
		preferred = MaxChunkSize
	}
	return preferred
}

// ProgressFunc is called during transfer to report progress.
// bytesTransferred is the cumulative bytes, totalBytes is the file size.
type ProgressFunc func(bytesTransferred, totalBytes int64)

// --- Chunk send/receive helpers (shared by all transfer functions) ---

// sendChunk sends a single data chunk, optionally compressing it.
// CRC32 is always computed over the original (uncompressed) data.
// Returns the flags used (for caller to detect last chunk).
func sendChunk(w io.Writer, data []byte, offset uint64, isLast bool, compress bool) error {
	chunkCRC := CRC32Chunk(data)

	flags := protocol.FlagNone
	if isLast {
		flags |= protocol.FlagLastChunk
	}

	if compress {
		compressed := Compress(data)
		if compressed != nil {
			// Compression reduced size — send compressed
			flags |= protocol.FlagCompressed
			payload := &protocol.DataChunkPayload{
				Offset:       offset,
				CRC32:        chunkCRC,
				OriginalSize: uint32(len(data)),
				Data:         compressed,
			}
			return protocol.WriteMessage(w, protocol.MsgDataChunk, flags, payload.EncodeCompressed())
		}
	}

	// Send uncompressed
	payload := &protocol.DataChunkPayload{
		Offset: offset,
		CRC32:  chunkCRC,
		Data:   data,
	}
	return protocol.WriteMessage(w, protocol.MsgDataChunk, flags, payload.Encode())
}

// RecvChunk decodes and decompresses a DataChunk message.
// Returns the original (uncompressed) data.
func RecvChunk(msg *protocol.Message) ([]byte, uint64, error) {
	isCompressed := msg.Header.Flags&protocol.FlagCompressed != 0

	if isCompressed {
		chunk, err := protocol.DecodeCompressedDataChunkPayload(msg.Payload)
		if err != nil {
			return nil, 0, fmt.Errorf("decode compressed chunk: %w", err)
		}
		original, err := Decompress(chunk.Data, chunk.OriginalSize)
		if err != nil {
			return nil, 0, fmt.Errorf("decompress chunk at offset %d: %w", chunk.Offset, err)
		}
		if CRC32Chunk(original) != chunk.CRC32 {
			return nil, 0, fmt.Errorf("chunk CRC mismatch at offset %d (after decompress)", chunk.Offset)
		}
		return original, chunk.Offset, nil
	}

	chunk, err := protocol.DecodeDataChunkPayload(msg.Payload)
	if err != nil {
		return nil, 0, fmt.Errorf("decode chunk: %w", err)
	}
	if CRC32Chunk(chunk.Data) != chunk.CRC32 {
		return nil, 0, fmt.Errorf("chunk CRC mismatch at offset %d", chunk.Offset)
	}
	return chunk.Data, chunk.Offset, nil
}

// drainFileChunks reads and discards messages until MsgTransferEnd, MsgError,
// or a read error. Used to resync the stream after a file receive error within
// a directory transfer, so the receiver can continue to the next file.
func drainFileChunks(r io.Reader) {
	for {
		msg, err := protocol.ReadMessage(r)
		if err != nil {
			return
		}
		switch msg.Header.Type {
		case protocol.MsgTransferEnd, protocol.MsgError:
			return
		}
	}
}

// --- File transfer ---

// SendFile sends a file over a connection using chunked transfer.
// If compress is true, each chunk is compressed before sending.
func SendFile(w io.Writer, filePath string, chunkSize int, compress bool, progressFn ProgressFunc) error {
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

	buf := make([]byte, chunkSize)
	var offset int64
	var totalCRC uint32

	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			totalCRC = CRC32Update(totalCRC, chunk)

			isLast := err == io.EOF
			if writeErr := sendChunk(w, chunk, uint64(offset), isLast, compress); writeErr != nil {
				return fmt.Errorf("send chunk at offset %d: %w", offset, writeErr)
			}

			offset += int64(n)
			if progressFn != nil {
				progressFn(offset, totalSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
	}

	// Send TransferEnd
	endPayload := &protocol.TransferEndPayload{TotalCRC32: totalCRC}
	if err := protocol.WriteMessage(w, protocol.MsgTransferEnd, protocol.FlagNone, endPayload.Encode()); err != nil {
		return fmt.Errorf("send transfer end: %w", err)
	}

	return nil
}

// ReceiveFile receives a file from a connection. It expects MsgFileInfo followed by MsgDataChunk messages.
// Automatically handles both compressed and uncompressed chunks.
// Returns the full path of the received file.
func ReceiveFile(r io.Reader, destDir string, progressFn ProgressFunc) (string, error) {
	msg, err := protocol.ReadMessage(r)
	if err != nil {
		return "", fmt.Errorf("read file info: %w", err)
	}
	if msg.Header.Type != protocol.MsgFileInfo {
		return "", fmt.Errorf("expected MsgFileInfo, got 0x%02X", msg.Header.Type)
	}

	fileInfo, _, err := protocol.DecodeFileInfoPayload(msg.Payload, 0)
	if err != nil {
		return "", fmt.Errorf("decode file info: %w", err)
	}

	destPath := filepath.Join(destDir, fileInfo.Name)
	totalSize := int64(fileInfo.Size)

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileInfo.Mode))
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	var totalCRC uint32
	var bytesReceived int64
	var recvErr error // set on first local error; triggers drain mode

	for {
		msg, err := protocol.ReadMessage(r)
		if err != nil {
			if recvErr != nil {
				return "", recvErr
			}
			return "", fmt.Errorf("read message: %w", err)
		}

		switch msg.Header.Type {
		case protocol.MsgDataChunk:
			if recvErr != nil {
				continue // drain remaining chunks
			}
			data, _, err := RecvChunk(msg)
			if err != nil {
				recvErr = err
				continue
			}
			if _, err := f.Write(data); err != nil {
				recvErr = fmt.Errorf("write chunk: %w", err)
				continue
			}
			totalCRC = CRC32Update(totalCRC, data)
			bytesReceived += int64(len(data))
			if progressFn != nil {
				progressFn(bytesReceived, totalSize)
			}

		case protocol.MsgTransferEnd:
			if recvErr != nil {
				return "", recvErr
			}
			endPayload, err := protocol.DecodeTransferEndPayload(msg.Payload)
			if err != nil {
				return "", fmt.Errorf("decode transfer end: %w", err)
			}
			if endPayload.TotalCRC32 != totalCRC {
				return "", fmt.Errorf("total CRC mismatch: expected %08X, got %08X", endPayload.TotalCRC32, totalCRC)
			}
			return destPath, nil

		case protocol.MsgError:
			errPayload, _ := protocol.DecodeErrorPayload(msg.Payload)
			if errPayload != nil {
				return "", fmt.Errorf("server error: [%d] %s", errPayload.Code, errPayload.Message)
			}
			return "", fmt.Errorf("server error (unparseable)")

		default:
			if recvErr != nil {
				continue // drain mode: skip unexpected messages
			}
			return "", fmt.Errorf("unexpected message type 0x%02X during transfer", msg.Header.Type)
		}
	}
}

// --- Directory transfer ---

// SendDir sends a directory recursively over a connection.
func SendDir(w io.Writer, dirPath string, chunkSize int, compress bool, progressFn ProgressFunc) error {
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

		// Send file entry
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

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", relPath, err)
		}
		defer f.Close()

		buf := make([]byte, chunkSize)
		var offset int64
		var fileCRC uint32

		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				fileCRC = CRC32Update(fileCRC, chunk)

				isLast := readErr == io.EOF
				if err := sendChunk(w, chunk, uint64(offset), isLast, compress); err != nil {
					return err
				}
				offset += int64(n)
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return fmt.Errorf("read %s: %w", relPath, readErr)
			}
		}

		endPayload := &protocol.TransferEndPayload{TotalCRC32: fileCRC}
		return protocol.WriteMessage(w, protocol.MsgTransferEnd, protocol.FlagNone, endPayload.Encode())
	})
}

// ReceiveDir receives a directory from a connection.
// Automatically handles both compressed and uncompressed chunks.
// The stream ends with MsgOK.
// On local errors (CRC mismatch, disk write), it drains remaining messages
// to keep the protocol stream synchronized for subsequent commands.
func ReceiveDir(r io.Reader, destDir string, progressFn ProgressFunc) error {
	var dirErr error // first error; triggers drain mode for remaining files

	for {
		msg, err := protocol.ReadMessage(r)
		if err != nil {
			if dirErr != nil {
				return dirErr
			}
			return fmt.Errorf("read message: %w", err)
		}

		switch msg.Header.Type {
		case protocol.MsgOK:
			return dirErr // nil on success

		case protocol.MsgFileInfo:
			fileInfo, _, err := protocol.DecodeFileInfoPayload(msg.Payload, 0)
			if err != nil {
				if dirErr == nil {
					dirErr = fmt.Errorf("decode file info: %w", err)
				}
				continue
			}

			fullPath := filepath.Join(destDir, fileInfo.Name)

			if fileInfo.EntryType == protocol.EntryDir {
				if dirErr == nil {
					if err := os.MkdirAll(fullPath, os.FileMode(fileInfo.Mode)); err != nil {
						dirErr = fmt.Errorf("create dir %s: %w", fileInfo.Name, err)
					}
				}
				continue
			}

			// File entry: receive chunks until TransferEnd
			if dirErr != nil {
				// Already failed — drain this file's chunks
				drainFileChunks(r)
				continue
			}

			parentDir := filepath.Dir(fullPath)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				dirErr = fmt.Errorf("create parent dir: %w", err)
				drainFileChunks(r)
				continue
			}

			f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileInfo.Mode))
			if err != nil {
				dirErr = fmt.Errorf("create file %s: %w", fileInfo.Name, err)
				drainFileChunks(r)
				continue
			}

			var totalCRC uint32
			var fileErr error
			fileComplete := false

			for !fileComplete {
				chunkMsg, err := protocol.ReadMessage(r)
				if err != nil {
					f.Close()
					if dirErr != nil {
						return dirErr
					}
					return fmt.Errorf("read chunk for %s: %w", fileInfo.Name, err)
				}

				switch chunkMsg.Header.Type {
				case protocol.MsgDataChunk:
					if fileErr != nil {
						continue // drain remaining chunks for this file
					}
					data, _, err := RecvChunk(chunkMsg)
					if err != nil {
						fileErr = err
						continue
					}
					if _, err := f.Write(data); err != nil {
						fileErr = fmt.Errorf("write %s: %w", fileInfo.Name, err)
						continue
					}
					totalCRC = CRC32Update(totalCRC, data)

				case protocol.MsgTransferEnd:
					if fileErr != nil {
						dirErr = fileErr
					} else {
						endPayload, err := protocol.DecodeTransferEndPayload(chunkMsg.Payload)
						if err != nil {
							dirErr = fmt.Errorf("decode transfer end: %w", err)
						} else if endPayload.TotalCRC32 != totalCRC {
							dirErr = fmt.Errorf("CRC mismatch for %s", fileInfo.Name)
						}
					}
					fileComplete = true

				case protocol.MsgError:
					f.Close()
					errPayload, _ := protocol.DecodeErrorPayload(chunkMsg.Payload)
					if errPayload != nil {
						return fmt.Errorf("remote error: [%d] %s", errPayload.Code, errPayload.Message)
					}
					return fmt.Errorf("remote error")
				}
			}
			f.Close()

		case protocol.MsgError:
			errPayload, _ := protocol.DecodeErrorPayload(msg.Payload)
			if errPayload != nil {
				return fmt.Errorf("server error: [%d] %s", errPayload.Code, errPayload.Message)
			}
			return fmt.Errorf("server error")

		default:
			if dirErr != nil {
				continue // drain mode: skip unexpected messages
			}
			return fmt.Errorf("unexpected message type 0x%02X", msg.Header.Type)
		}
	}
}
