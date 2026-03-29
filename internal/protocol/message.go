package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Message types (client -> server requests)
const (
	MsgListDir  uint8 = 0x01
	MsgGetInfo  uint8 = 0x02
	MsgDownload uint8 = 0x03
	MsgUpload   uint8 = 0x04
	MsgDelete   uint8 = 0x05
	MsgMkdir    uint8 = 0x06
	MsgCopy     uint8 = 0x07
	MsgMove     uint8 = 0x08
	MsgChDir    uint8 = 0x09
	MsgPwd      uint8 = 0x0A
	MsgExec     uint8 = 0x0B // Remote command execution

	// Response types (server -> client)
	MsgOK          uint8 = 0x80
	MsgError       uint8 = 0x81
	MsgDirListing  uint8 = 0x82
	MsgFileInfo    uint8 = 0x83
	MsgDataChunk   uint8 = 0x84
	MsgTransferEnd uint8 = 0x85
	MsgExecOutput  uint8 = 0x86 // Streaming command output
	MsgExecExit    uint8 = 0x87 // Command exit code
)

// Flags
const (
	FlagNone       uint8 = 0x00
	FlagLastChunk  uint8 = 0x01
	FlagCompressed uint8 = 0x02
)

// HeaderSize is the fixed size of a message header.
const HeaderSize = 6

// MaxPayloadSize is the maximum allowed payload size (64 MB).
const MaxPayloadSize = 64 * 1024 * 1024

// Header is the wire format header for all messages.
type Header struct {
	Type  uint8
	Flags uint8
	Len   uint32
}

// Message represents a complete protocol message.
type Message struct {
	Header  Header
	Payload []byte
}

// WriteMessage writes a message to the writer.
// Header and payload are combined into a single write to avoid Nagle delays
// on stream sockets (critical for RFCOMM throughput).
func WriteMessage(w io.Writer, msgType uint8, flags uint8, payload []byte) error {
	// Single allocation: header + payload in one buffer, one write syscall
	buf := make([]byte, HeaderSize+len(payload))
	buf[0] = msgType
	buf[1] = flags
	binary.LittleEndian.PutUint32(buf[2:6], uint32(len(payload)))
	if len(payload) > 0 {
		copy(buf[HeaderSize:], payload)
	}

	_, err := w.Write(buf)
	if err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

// ReadMessage reads a message from the reader.
func ReadMessage(r io.Reader) (*Message, error) {
	var hdr [HeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	msg := &Message{
		Header: Header{
			Type:  hdr[0],
			Flags: hdr[1],
			Len:   binary.LittleEndian.Uint32(hdr[2:6]),
		},
	}

	if msg.Header.Len > MaxPayloadSize {
		return nil, fmt.Errorf("payload too large: %d bytes (max %d)", msg.Header.Len, MaxPayloadSize)
	}

	if msg.Header.Len > 0 {
		msg.Payload = make([]byte, msg.Header.Len)
		if _, err := io.ReadFull(r, msg.Payload); err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
	}

	return msg, nil
}

// Error codes
const (
	ErrCodeNotFound       uint16 = 1
	ErrCodePermission     uint16 = 2
	ErrCodePathTraversal  uint16 = 3
	ErrCodeDiskFull       uint16 = 4
	ErrCodeInterrupted    uint16 = 5
	ErrCodeChecksum       uint16 = 6
	ErrCodeInvalidRequest uint16 = 7
	ErrCodeBusy           uint16 = 8
	ErrCodeExecDisabled   uint16 = 9
)

// ExecOutput stream type flags
const (
	ExecStdout uint8 = 0x01
	ExecStderr uint8 = 0x02
)

// ExecOutputPayload represents a chunk of command output.
// Wire: stream_type(1) + data(N)
type ExecOutputPayload struct {
	Stream uint8  // ExecStdout or ExecStderr
	Data   []byte
}

func (e *ExecOutputPayload) Encode() []byte {
	buf := make([]byte, 1+len(e.Data))
	buf[0] = e.Stream
	copy(buf[1:], e.Data)
	return buf
}

func DecodeExecOutputPayload(data []byte) (*ExecOutputPayload, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("insufficient data for ExecOutputPayload")
	}
	return &ExecOutputPayload{
		Stream: data[0],
		Data:   data[1:],
	}, nil
}

// ExecExitPayload represents a command exit code.
// Wire: exit_code(4, int32)
type ExecExitPayload struct {
	ExitCode int32
}

func (e *ExecExitPayload) Encode() []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(e.ExitCode))
	return buf
}

func DecodeExecExitPayload(data []byte) (*ExecExitPayload, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("insufficient data for ExecExitPayload")
	}
	return &ExecExitPayload{
		ExitCode: int32(binary.LittleEndian.Uint32(data[0:4])),
	}, nil
}

// FileEntryType constants
const (
	EntryFile    uint8 = 0
	EntryDir     uint8 = 1
	EntrySymlink uint8 = 2
)

// --- Payload encoding helpers ---

// EncodeString encodes a string as length-prefixed bytes (uint16 + data).
func EncodeString(s string) []byte {
	b := make([]byte, 2+len(s))
	binary.LittleEndian.PutUint16(b[0:2], uint16(len(s)))
	copy(b[2:], s)
	return b
}

// DecodeString decodes a length-prefixed string from data at offset.
// Returns the string and the new offset.
func DecodeString(data []byte, offset int) (string, int, error) {
	if offset+2 > len(data) {
		return "", 0, fmt.Errorf("insufficient data for string length at offset %d", offset)
	}
	slen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+slen > len(data) {
		return "", 0, fmt.Errorf("insufficient data for string body: need %d, have %d", slen, len(data)-offset)
	}
	s := string(data[offset : offset+slen])
	return s, offset + slen, nil
}

// EncodeTwoStrings encodes two strings (for copy, move operations).
func EncodeTwoStrings(s1, s2 string) []byte {
	b1 := EncodeString(s1)
	b2 := EncodeString(s2)
	result := make([]byte, len(b1)+len(b2))
	copy(result, b1)
	copy(result[len(b1):], b2)
	return result
}

// DecodeTwoStrings decodes two length-prefixed strings.
func DecodeTwoStrings(data []byte) (string, string, error) {
	s1, off, err := DecodeString(data, 0)
	if err != nil {
		return "", "", fmt.Errorf("decode first string: %w", err)
	}
	s2, _, err := DecodeString(data, off)
	if err != nil {
		return "", "", fmt.Errorf("decode second string: %w", err)
	}
	return s1, s2, nil
}

// FileInfoPayload represents file metadata.
type FileInfoPayload struct {
	EntryType uint8
	Size      uint64
	ModTime   int64
	Mode      uint32
	Name      string
}

// Encode serializes a FileInfoPayload.
func (f *FileInfoPayload) Encode() []byte {
	nameBytes := []byte(f.Name)
	buf := make([]byte, 1+8+8+4+2+len(nameBytes))
	buf[0] = f.EntryType
	binary.LittleEndian.PutUint64(buf[1:9], f.Size)
	binary.LittleEndian.PutUint64(buf[9:17], uint64(f.ModTime))
	binary.LittleEndian.PutUint32(buf[17:21], f.Mode)
	binary.LittleEndian.PutUint16(buf[21:23], uint16(len(nameBytes)))
	copy(buf[23:], nameBytes)
	return buf
}

// DecodeFileInfoPayload decodes a FileInfoPayload from data at offset.
func DecodeFileInfoPayload(data []byte, offset int) (*FileInfoPayload, int, error) {
	if offset+23 > len(data) {
		return nil, 0, fmt.Errorf("insufficient data for FileInfoPayload at offset %d", offset)
	}
	f := &FileInfoPayload{
		EntryType: data[offset],
		Size:      binary.LittleEndian.Uint64(data[offset+1 : offset+9]),
		ModTime:   int64(binary.LittleEndian.Uint64(data[offset+9 : offset+17])),
		Mode:      binary.LittleEndian.Uint32(data[offset+17 : offset+21]),
	}
	nameLen := int(binary.LittleEndian.Uint16(data[offset+21 : offset+23]))
	offset += 23
	if offset+nameLen > len(data) {
		return nil, 0, fmt.Errorf("insufficient data for file name")
	}
	f.Name = string(data[offset : offset+nameLen])
	return f, offset + nameLen, nil
}

// DirListingPayload represents a directory listing response.
type DirListingPayload struct {
	Path    string
	Entries []FileInfoPayload
}

// Encode serializes a DirListingPayload.
func (d *DirListingPayload) Encode() []byte {
	pathBytes := EncodeString(d.Path)
	countBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(countBytes, uint32(len(d.Entries)))

	total := len(pathBytes) + 4
	entryBufs := make([][]byte, len(d.Entries))
	for i, e := range d.Entries {
		entryBufs[i] = e.Encode()
		total += len(entryBufs[i])
	}

	buf := make([]byte, 0, total)
	buf = append(buf, pathBytes...)
	buf = append(buf, countBytes...)
	for _, eb := range entryBufs {
		buf = append(buf, eb...)
	}
	return buf
}

// DecodeDirListingPayload decodes a DirListingPayload.
func DecodeDirListingPayload(data []byte) (*DirListingPayload, error) {
	d := &DirListingPayload{}
	path, off, err := DecodeString(data, 0)
	if err != nil {
		return nil, fmt.Errorf("decode path: %w", err)
	}
	d.Path = path

	if off+4 > len(data) {
		return nil, fmt.Errorf("insufficient data for entry count")
	}
	count := binary.LittleEndian.Uint32(data[off : off+4])
	off += 4

	d.Entries = make([]FileInfoPayload, 0, count)
	for i := uint32(0); i < count; i++ {
		entry, newOff, err := DecodeFileInfoPayload(data, off)
		if err != nil {
			return nil, fmt.Errorf("decode entry %d: %w", i, err)
		}
		d.Entries = append(d.Entries, *entry)
		off = newOff
	}
	return d, nil
}

// DataChunkPayload represents a chunk of file data.
//
// Wire format (uncompressed): offset(8) + crc32(4) + data(N)
// Wire format (compressed):   offset(8) + crc32(4) + original_size(4) + compressed_data(N)
//
// CRC32 is always computed over the ORIGINAL (uncompressed) data.
// The FlagCompressed bit on the message header indicates which format is used.
type DataChunkPayload struct {
	Offset       uint64
	CRC32        uint32
	OriginalSize uint32 // only set when compressed
	Data         []byte // raw data (uncompressed) or compressed data
}

// Encode serializes a DataChunkPayload (uncompressed format).
func (c *DataChunkPayload) Encode() []byte {
	buf := make([]byte, 12+len(c.Data))
	binary.LittleEndian.PutUint64(buf[0:8], c.Offset)
	binary.LittleEndian.PutUint32(buf[8:12], c.CRC32)
	copy(buf[12:], c.Data)
	return buf
}

// EncodeCompressed serializes a DataChunkPayload with compressed format.
func (c *DataChunkPayload) EncodeCompressed() []byte {
	buf := make([]byte, 16+len(c.Data))
	binary.LittleEndian.PutUint64(buf[0:8], c.Offset)
	binary.LittleEndian.PutUint32(buf[8:12], c.CRC32)
	binary.LittleEndian.PutUint32(buf[12:16], c.OriginalSize)
	copy(buf[16:], c.Data)
	return buf
}

// DecodeDataChunkPayload decodes a DataChunkPayload (uncompressed format).
func DecodeDataChunkPayload(data []byte) (*DataChunkPayload, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("insufficient data for DataChunkPayload: %d bytes", len(data))
	}
	return &DataChunkPayload{
		Offset: binary.LittleEndian.Uint64(data[0:8]),
		CRC32:  binary.LittleEndian.Uint32(data[8:12]),
		Data:   data[12:],
	}, nil
}

// DecodeCompressedDataChunkPayload decodes a compressed DataChunkPayload.
func DecodeCompressedDataChunkPayload(data []byte) (*DataChunkPayload, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("insufficient data for compressed DataChunkPayload: %d bytes", len(data))
	}
	return &DataChunkPayload{
		Offset:       binary.LittleEndian.Uint64(data[0:8]),
		CRC32:        binary.LittleEndian.Uint32(data[8:12]),
		OriginalSize: binary.LittleEndian.Uint32(data[12:16]),
		Data:         data[16:],
	}, nil
}

// TransferEndPayload represents the end of a transfer.
type TransferEndPayload struct {
	TotalCRC32 uint32
}

// Encode serializes a TransferEndPayload.
func (t *TransferEndPayload) Encode() []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, t.TotalCRC32)
	return buf
}

// DecodeTransferEndPayload decodes a TransferEndPayload.
func DecodeTransferEndPayload(data []byte) (*TransferEndPayload, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("insufficient data for TransferEndPayload: %d bytes", len(data))
	}
	return &TransferEndPayload{
		TotalCRC32: binary.LittleEndian.Uint32(data[0:4]),
	}, nil
}

// ErrorPayload represents an error response.
type ErrorPayload struct {
	Code    uint16
	Message string
}

// Encode serializes an ErrorPayload.
func (e *ErrorPayload) Encode() []byte {
	msgBytes := []byte(e.Message)
	buf := make([]byte, 2+2+len(msgBytes))
	binary.LittleEndian.PutUint16(buf[0:2], e.Code)
	binary.LittleEndian.PutUint16(buf[2:4], uint16(len(msgBytes)))
	copy(buf[4:], msgBytes)
	return buf
}

// DecodeErrorPayload decodes an ErrorPayload.
func DecodeErrorPayload(data []byte) (*ErrorPayload, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("insufficient data for ErrorPayload: %d bytes", len(data))
	}
	code := binary.LittleEndian.Uint16(data[0:2])
	msgLen := int(binary.LittleEndian.Uint16(data[2:4]))
	if 4+msgLen > len(data) {
		return nil, fmt.Errorf("insufficient data for error message")
	}
	return &ErrorPayload{
		Code:    code,
		Message: string(data[4 : 4+msgLen]),
	}, nil
}

// UploadRequestPayload represents an upload initiation request.
type UploadRequestPayload struct {
	Path      string
	TotalSize uint64
	ChunkSize uint32
	IsDir     bool
	Compress  bool
}

// Encode serializes an UploadRequestPayload.
// Wire: path(2+N) + total_size(8) + chunk_size(4) + flags(1)
// flags: bit0=IsDir, bit1=Compress
func (u *UploadRequestPayload) Encode() []byte {
	pathBytes := EncodeString(u.Path)
	buf := make([]byte, len(pathBytes)+8+4+1)
	copy(buf, pathBytes)
	off := len(pathBytes)
	binary.LittleEndian.PutUint64(buf[off:off+8], u.TotalSize)
	binary.LittleEndian.PutUint32(buf[off+8:off+12], u.ChunkSize)
	var flags byte
	if u.IsDir {
		flags |= 0x01
	}
	if u.Compress {
		flags |= 0x02
	}
	buf[off+12] = flags
	return buf
}

// DecodeUploadRequestPayload decodes an UploadRequestPayload.
func DecodeUploadRequestPayload(data []byte) (*UploadRequestPayload, error) {
	path, off, err := DecodeString(data, 0)
	if err != nil {
		return nil, fmt.Errorf("decode path: %w", err)
	}
	if off+13 > len(data) {
		return nil, fmt.Errorf("insufficient data for upload request fields")
	}
	flags := data[off+12]
	return &UploadRequestPayload{
		Path:      path,
		TotalSize: binary.LittleEndian.Uint64(data[off : off+8]),
		ChunkSize: binary.LittleEndian.Uint32(data[off+8 : off+12]),
		IsDir:     flags&0x01 != 0,
		Compress:  flags&0x02 != 0,
	}, nil
}

// DownloadRequestPayload represents a download initiation request.
// Wire: path(2+N) + flags(1) — flags: bit0=Compress
type DownloadRequestPayload struct {
	Path     string
	Compress bool
}

// Encode serializes a DownloadRequestPayload.
func (d *DownloadRequestPayload) Encode() []byte {
	pathBytes := EncodeString(d.Path)
	buf := make([]byte, len(pathBytes)+1)
	copy(buf, pathBytes)
	if d.Compress {
		buf[len(pathBytes)] = 0x01
	}
	return buf
}

// DecodeDownloadRequestPayload decodes a DownloadRequestPayload.
func DecodeDownloadRequestPayload(data []byte) (*DownloadRequestPayload, error) {
	path, off, err := DecodeString(data, 0)
	if err != nil {
		return nil, fmt.Errorf("decode path: %w", err)
	}
	compress := false
	if off < len(data) {
		compress = data[off]&0x01 != 0
	}
	return &DownloadRequestPayload{
		Path:     path,
		Compress: compress,
	}, nil
}
