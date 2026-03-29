package protocol

import (
	"bytes"
	"io"
	"testing"
)

func TestWriteReadMessage_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, MsgPwd, FlagNone, nil); err != nil {
		t.Fatal(err)
	}

	msg, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}

	if msg.Header.Type != MsgPwd {
		t.Errorf("type = 0x%02X, want 0x%02X", msg.Header.Type, MsgPwd)
	}
	if msg.Header.Flags != FlagNone {
		t.Errorf("flags = 0x%02X, want 0x%02X", msg.Header.Flags, FlagNone)
	}
	if msg.Header.Len != 0 {
		t.Errorf("len = %d, want 0", msg.Header.Len)
	}
	if len(msg.Payload) != 0 {
		t.Errorf("payload len = %d, want 0", len(msg.Payload))
	}
}

func TestWriteReadMessage_WithPayload(t *testing.T) {
	payload := []byte("hello world")
	var buf bytes.Buffer

	if err := WriteMessage(&buf, MsgOK, FlagLastChunk, payload); err != nil {
		t.Fatal(err)
	}

	msg, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}

	if msg.Header.Type != MsgOK {
		t.Errorf("type = 0x%02X, want 0x%02X", msg.Header.Type, MsgOK)
	}
	if msg.Header.Flags != FlagLastChunk {
		t.Errorf("flags = 0x%02X, want 0x%02X", msg.Header.Flags, FlagLastChunk)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Errorf("payload = %q, want %q", msg.Payload, payload)
	}
}

func TestWriteReadMessage_AllTypes(t *testing.T) {
	types := []uint8{
		MsgListDir, MsgGetInfo, MsgDownload, MsgUpload, MsgDelete,
		MsgMkdir, MsgCopy, MsgMove, MsgChDir, MsgPwd,
		MsgOK, MsgError, MsgDirListing, MsgFileInfo, MsgDataChunk, MsgTransferEnd,
	}

	for _, mt := range types {
		var buf bytes.Buffer
		payload := []byte{0x01, 0x02, 0x03}
		if err := WriteMessage(&buf, mt, FlagNone, payload); err != nil {
			t.Fatalf("type 0x%02X: write failed: %v", mt, err)
		}
		msg, err := ReadMessage(&buf)
		if err != nil {
			t.Fatalf("type 0x%02X: read failed: %v", mt, err)
		}
		if msg.Header.Type != mt {
			t.Errorf("type = 0x%02X, want 0x%02X", msg.Header.Type, mt)
		}
	}
}

func TestReadMessage_TruncatedHeader(t *testing.T) {
	buf := bytes.NewReader([]byte{0x01, 0x02}) // only 2 bytes
	_, err := ReadMessage(buf)
	if err == nil {
		t.Fatal("expected error for truncated header")
	}
}

func TestReadMessage_TruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	WriteMessage(&buf, MsgOK, FlagNone, []byte("hello"))
	// Truncate the payload
	data := buf.Bytes()[:HeaderSize+2]
	_, err := ReadMessage(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestReadMessage_EOF(t *testing.T) {
	_, err := ReadMessage(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error for empty reader")
	}
}

func TestReadMessage_PayloadTooLarge(t *testing.T) {
	// Craft a header with payload size > MaxPayloadSize
	var hdr [HeaderSize]byte
	hdr[0] = MsgOK
	hdr[2] = 0xFF // len = 0xFF_FF_FF_FF
	hdr[3] = 0xFF
	hdr[4] = 0xFF
	hdr[5] = 0xFF

	_, err := ReadMessage(bytes.NewReader(hdr[:]))
	if err == nil {
		t.Fatal("expected error for oversized payload")
	}
}

func TestEncodeDecodeString(t *testing.T) {
	tests := []string{"", "hello", "path/to/file.txt", "日本語"}
	for _, s := range tests {
		encoded := EncodeString(s)
		decoded, off, err := DecodeString(encoded, 0)
		if err != nil {
			t.Fatalf("DecodeString(%q): %v", s, err)
		}
		if decoded != s {
			t.Errorf("DecodeString = %q, want %q", decoded, s)
		}
		if off != len(encoded) {
			t.Errorf("offset = %d, want %d", off, len(encoded))
		}
	}
}

func TestDecodeString_Insufficient(t *testing.T) {
	_, _, err := DecodeString([]byte{0x05, 0x00, 'h'}, 0) // claims 5 bytes, only has 1
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeTwoStrings_RoundTrip(t *testing.T) {
	s1, s2 := "source/path", "dest/path"
	encoded := EncodeTwoStrings(s1, s2)
	d1, d2, err := DecodeTwoStrings(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != s1 || d2 != s2 {
		t.Errorf("got (%q, %q), want (%q, %q)", d1, d2, s1, s2)
	}
}

func TestFileInfoPayload_RoundTrip(t *testing.T) {
	original := &FileInfoPayload{
		EntryType: EntryFile,
		Size:      12345,
		ModTime:   1700000000,
		Mode:      0644,
		Name:      "test.txt",
	}

	encoded := original.Encode()
	decoded, off, err := DecodeFileInfoPayload(encoded, 0)
	if err != nil {
		t.Fatal(err)
	}
	if off != len(encoded) {
		t.Errorf("offset = %d, want %d", off, len(encoded))
	}
	if decoded.EntryType != original.EntryType ||
		decoded.Size != original.Size ||
		decoded.ModTime != original.ModTime ||
		decoded.Mode != original.Mode ||
		decoded.Name != original.Name {
		t.Errorf("decoded mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestFileInfoPayload_Directory(t *testing.T) {
	original := &FileInfoPayload{
		EntryType: EntryDir,
		Size:      0,
		ModTime:   1700000000,
		Mode:      0755,
		Name:      "subdir",
	}

	encoded := original.Encode()
	decoded, _, err := DecodeFileInfoPayload(encoded, 0)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.EntryType != EntryDir {
		t.Errorf("entry type = %d, want %d", decoded.EntryType, EntryDir)
	}
}

func TestDirListingPayload_RoundTrip(t *testing.T) {
	original := &DirListingPayload{
		Path: "/test/dir",
		Entries: []FileInfoPayload{
			{EntryType: EntryFile, Size: 100, ModTime: 1700000000, Mode: 0644, Name: "file1.txt"},
			{EntryType: EntryDir, Size: 0, ModTime: 1700000001, Mode: 0755, Name: "subdir"},
			{EntryType: EntryFile, Size: 999999, ModTime: 1700000002, Mode: 0600, Name: "big.bin"},
		},
	}

	encoded := original.Encode()
	decoded, err := DecodeDirListingPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Path != original.Path {
		t.Errorf("path = %q, want %q", decoded.Path, original.Path)
	}
	if len(decoded.Entries) != len(original.Entries) {
		t.Fatalf("entries count = %d, want %d", len(decoded.Entries), len(original.Entries))
	}
	for i, e := range decoded.Entries {
		if e.Name != original.Entries[i].Name || e.Size != original.Entries[i].Size {
			t.Errorf("entry[%d] mismatch: got %+v, want %+v", i, e, original.Entries[i])
		}
	}
}

func TestDirListingPayload_Empty(t *testing.T) {
	original := &DirListingPayload{
		Path:    "/empty",
		Entries: nil,
	}
	encoded := original.Encode()
	decoded, err := DecodeDirListingPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Entries) != 0 {
		t.Errorf("entries count = %d, want 0", len(decoded.Entries))
	}
}

func TestDataChunkPayload_RoundTrip(t *testing.T) {
	original := &DataChunkPayload{
		Offset: 32768,
		CRC32:  0xDEADBEEF,
		Data:   []byte("chunk data here with some content"),
	}

	encoded := original.Encode()
	decoded, err := DecodeDataChunkPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Offset != original.Offset {
		t.Errorf("offset = %d, want %d", decoded.Offset, original.Offset)
	}
	if decoded.CRC32 != original.CRC32 {
		t.Errorf("crc32 = 0x%08X, want 0x%08X", decoded.CRC32, original.CRC32)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("data mismatch")
	}
}

func TestDataChunkPayload_EmptyData(t *testing.T) {
	original := &DataChunkPayload{Offset: 0, CRC32: 0, Data: nil}
	encoded := original.Encode()
	decoded, err := DecodeDataChunkPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data) != 0 {
		t.Errorf("data len = %d, want 0", len(decoded.Data))
	}
}

func TestDataChunkPayload_TooShort(t *testing.T) {
	_, err := DecodeDataChunkPayload([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestTransferEndPayload_RoundTrip(t *testing.T) {
	original := &TransferEndPayload{TotalCRC32: 0xCAFEBABE}
	encoded := original.Encode()
	decoded, err := DecodeTransferEndPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.TotalCRC32 != original.TotalCRC32 {
		t.Errorf("crc = 0x%08X, want 0x%08X", decoded.TotalCRC32, original.TotalCRC32)
	}
}

func TestErrorPayload_RoundTrip(t *testing.T) {
	original := &ErrorPayload{Code: ErrCodeNotFound, Message: "file not found: test.txt"}
	encoded := original.Encode()
	decoded, err := DecodeErrorPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Code != original.Code || decoded.Message != original.Message {
		t.Errorf("got (%d, %q), want (%d, %q)", decoded.Code, decoded.Message, original.Code, original.Message)
	}
}

func TestErrorPayload_EmptyMessage(t *testing.T) {
	original := &ErrorPayload{Code: ErrCodeBusy, Message: ""}
	encoded := original.Encode()
	decoded, err := DecodeErrorPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Message != "" {
		t.Errorf("message = %q, want empty", decoded.Message)
	}
}

func TestUploadRequestPayload_RoundTrip(t *testing.T) {
	original := &UploadRequestPayload{
		Path:      "uploads/test.bin",
		TotalSize: 1024 * 1024,
		ChunkSize: 32768,
		IsDir:     false,
	}

	encoded := original.Encode()
	decoded, err := DecodeUploadRequestPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Path != original.Path ||
		decoded.TotalSize != original.TotalSize ||
		decoded.ChunkSize != original.ChunkSize ||
		decoded.IsDir != original.IsDir {
		t.Errorf("mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestUploadRequestPayload_Directory(t *testing.T) {
	original := &UploadRequestPayload{
		Path:      "mydir",
		TotalSize: 0,
		ChunkSize: 32768,
		IsDir:     true,
	}
	encoded := original.Encode()
	decoded, err := DecodeUploadRequestPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.IsDir {
		t.Error("expected IsDir=true")
	}
}

func TestMultipleMessages(t *testing.T) {
	var buf bytes.Buffer

	// Write multiple messages
	WriteMessage(&buf, MsgListDir, FlagNone, EncodeString("/test"))
	WriteMessage(&buf, MsgOK, FlagNone, nil)
	WriteMessage(&buf, MsgError, FlagNone, (&ErrorPayload{Code: 1, Message: "err"}).Encode())

	// Read them back
	msg1, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if msg1.Header.Type != MsgListDir {
		t.Errorf("msg1 type = 0x%02X", msg1.Header.Type)
	}

	msg2, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if msg2.Header.Type != MsgOK {
		t.Errorf("msg2 type = 0x%02X", msg2.Header.Type)
	}

	msg3, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if msg3.Header.Type != MsgError {
		t.Errorf("msg3 type = 0x%02X", msg3.Header.Type)
	}

	// Next read should fail
	_, err = ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected EOF")
	}
}

func BenchmarkWriteMessage(b *testing.B) {
	payload := make([]byte, 32*1024)
	w := io.Discard

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WriteMessage(w, MsgDataChunk, FlagNone, payload)
	}
}

func BenchmarkReadMessage(b *testing.B) {
	payload := make([]byte, 32*1024)
	var buf bytes.Buffer
	for i := 0; i < b.N; i++ {
		WriteMessage(&buf, MsgDataChunk, FlagNone, payload)
	}
	data := buf.Bytes()

	b.ResetTimer()
	r := bytes.NewReader(data)
	for i := 0; i < b.N; i++ {
		ReadMessage(r)
	}
}
