package client

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"blue-file-transfer/internal/bt"
	"blue-file-transfer/internal/protocol"
	"blue-file-transfer/internal/transfer"
)

// Client manages a connection to a remote BFT server.
type Client struct {
	transport bt.Transport
	conn      bt.Conn
	adapter   string
	Compress  bool
}

// New creates a new Client.
func New(transport bt.Transport, adapter string) *Client {
	return &Client{
		transport: transport,
		adapter:   adapter,
	}
}

// Connect connects to a remote server.
func (c *Client) Connect(remoteAddr string, channel uint8) error {
	conn, err := c.transport.Connect(c.adapter, remoteAddr, channel)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	c.conn = conn
	return nil
}

// ConnectWithConn uses an existing connection (for testing).
func (c *Client) ConnectWithConn(conn bt.Conn) {
	c.conn = conn
}

// Disconnect closes the connection.
func (c *Client) Disconnect() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// IsConnected returns true if connected.
func (c *Client) IsConnected() bool {
	return c.conn != nil
}

// Pwd returns the current remote working directory.
func (c *Client) Pwd() (string, error) {
	if err := protocol.WriteMessage(c.conn, protocol.MsgPwd, protocol.FlagNone, nil); err != nil {
		return "", err
	}

	msg, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return "", err
	}

	if msg.Header.Type == protocol.MsgError {
		return "", decodeError(msg)
	}

	path, _, err := protocol.DecodeString(msg.Payload, 0)
	if err != nil {
		return "", err
	}
	return path, nil
}

// ChDir changes the remote working directory.
func (c *Client) ChDir(path string) error {
	payload := protocol.EncodeString(path)
	if err := protocol.WriteMessage(c.conn, protocol.MsgChDir, protocol.FlagNone, payload); err != nil {
		return err
	}

	msg, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return err
	}
	if msg.Header.Type == protocol.MsgError {
		return decodeError(msg)
	}
	return nil
}

// ListDir lists a remote directory.
func (c *Client) ListDir(path string) (*protocol.DirListingPayload, error) {
	var payload []byte
	if path != "" {
		payload = protocol.EncodeString(path)
	}
	if err := protocol.WriteMessage(c.conn, protocol.MsgListDir, protocol.FlagNone, payload); err != nil {
		return nil, err
	}

	msg, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return nil, err
	}
	if msg.Header.Type == protocol.MsgError {
		return nil, decodeError(msg)
	}
	if msg.Header.Type != protocol.MsgDirListing {
		return nil, fmt.Errorf("unexpected response: 0x%02X", msg.Header.Type)
	}

	return protocol.DecodeDirListingPayload(msg.Payload)
}

// GetInfo gets info about a remote file/directory.
func (c *Client) GetInfo(path string) (*protocol.FileInfoPayload, error) {
	payload := protocol.EncodeString(path)
	if err := protocol.WriteMessage(c.conn, protocol.MsgGetInfo, protocol.FlagNone, payload); err != nil {
		return nil, err
	}

	msg, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return nil, err
	}
	if msg.Header.Type == protocol.MsgError {
		return nil, decodeError(msg)
	}
	if msg.Header.Type != protocol.MsgFileInfo {
		return nil, fmt.Errorf("unexpected response: 0x%02X", msg.Header.Type)
	}

	info, _, err := protocol.DecodeFileInfoPayload(msg.Payload, 0)
	return info, err
}

// Delete removes a file or directory on the server.
func (c *Client) Delete(path string) error {
	payload := protocol.EncodeString(path)
	if err := protocol.WriteMessage(c.conn, protocol.MsgDelete, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Mkdir creates a directory on the server.
func (c *Client) Mkdir(path string) error {
	payload := protocol.EncodeString(path)
	if err := protocol.WriteMessage(c.conn, protocol.MsgMkdir, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Copy copies a file/directory on the server.
func (c *Client) Copy(src, dst string) error {
	payload := protocol.EncodeTwoStrings(src, dst)
	if err := protocol.WriteMessage(c.conn, protocol.MsgCopy, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Move moves/renames a file/directory on the server.
func (c *Client) Move(src, dst string) error {
	payload := protocol.EncodeTwoStrings(src, dst)
	if err := protocol.WriteMessage(c.conn, protocol.MsgMove, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Download downloads a file or directory from the server.
func (c *Client) Download(remotePath, localDir string, progressFn transfer.ProgressFunc) (string, error) {
	// Send download request with compress flag
	req := &protocol.DownloadRequestPayload{
		Path:     remotePath,
		Compress: c.Compress,
	}
	if err := protocol.WriteMessage(c.conn, protocol.MsgDownload, protocol.FlagNone, req.Encode()); err != nil {
		return "", err
	}

	// Read first response to determine if it's a file or directory
	msg, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return "", err
	}

	if msg.Header.Type == protocol.MsgError {
		return "", decodeError(msg)
	}

	if msg.Header.Type == protocol.MsgFileInfo {
		fileInfo, _, err := protocol.DecodeFileInfoPayload(msg.Payload, 0)
		if err != nil {
			return "", fmt.Errorf("decode file info: %w", err)
		}

		if fileInfo.EntryType == protocol.EntryDir {
			// Directory download — the first MsgFileInfo is the root dir "."
			// Use the remote path's basename as the local directory name.
			dirName := filepath.Base(remotePath)
			destDir := filepath.Join(localDir, dirName)
			if err := os.MkdirAll(destDir, os.FileMode(fileInfo.Mode)); err != nil {
				return "", fmt.Errorf("create dir: %w", err)
			}
			// ReceiveDir reads remaining entries (subdirs + files) until MsgOK.
			// Paths from SendDir are relative to the source dir root,
			// so we place them inside destDir.
			if err := transfer.ReceiveDir(c.conn, destDir, progressFn); err != nil {
				return "", err
			}
			return destDir, nil
		}

		// Single file download
		destPath := filepath.Join(localDir, fileInfo.Name)
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileInfo.Mode))
		if err != nil {
			return "", fmt.Errorf("create file: %w", err)
		}
		defer f.Close()

		totalSize := int64(fileInfo.Size)
		var totalCRC uint32
		var bytesReceived int64

		for {
			chunkMsg, err := protocol.ReadMessage(c.conn)
			if err != nil {
				return "", err
			}

			switch chunkMsg.Header.Type {
			case protocol.MsgDataChunk:
				data, _, err := transfer.RecvChunk(chunkMsg)
				if err != nil {
					return "", err
				}
				if _, err := f.Write(data); err != nil {
					return "", err
				}
				totalCRC = transfer.CRC32Update(totalCRC, data)
				bytesReceived += int64(len(data))
				if progressFn != nil {
					progressFn(bytesReceived, totalSize)
				}

			case protocol.MsgTransferEnd:
				endPayload, err := protocol.DecodeTransferEndPayload(chunkMsg.Payload)
				if err != nil {
					return "", err
				}
				if endPayload.TotalCRC32 != totalCRC {
					return "", fmt.Errorf("total CRC mismatch")
				}
				return destPath, nil

			case protocol.MsgError:
				return "", decodeError(chunkMsg)
			}
		}
	}

	return "", fmt.Errorf("unexpected response type: 0x%02X", msg.Header.Type)
}

// Upload uploads a file or directory to the server.
func (c *Client) Upload(localPath, remotePath string, progressFn transfer.ProgressFunc) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat local path: %w", err)
	}

	isDir := info.IsDir()
	var totalSize uint64
	if !isDir {
		totalSize = uint64(info.Size())
	}

	req := &protocol.UploadRequestPayload{
		Path:      remotePath,
		TotalSize: totalSize,
		ChunkSize: uint32(transfer.DefaultChunkSize),
		IsDir:     isDir,
		Compress:  c.Compress,
	}

	if err := protocol.WriteMessage(c.conn, protocol.MsgUpload, protocol.FlagNone, req.Encode()); err != nil {
		return err
	}

	// Wait for OK (server ready)
	if err := c.expectOK(); err != nil {
		return err
	}

	if isDir {
		if err := transfer.SendDir(c.conn, localPath, transfer.DefaultChunkSize, c.Compress, progressFn); err != nil {
			return err
		}
		protocol.WriteMessage(c.conn, protocol.MsgOK, protocol.FlagNone, nil)
	} else {
		if err := transfer.SendFile(c.conn, localPath, transfer.DefaultChunkSize, c.Compress, progressFn); err != nil {
			return err
		}
	}

	// Wait for server acknowledgment
	return c.expectOK()
}

// Exec executes a command on the remote server and streams output.
// stdoutWriter receives stdout, stderrWriter receives stderr.
// Returns the exit code.
func (c *Client) Exec(command string, stdoutWriter, stderrWriter io.Writer) (int32, error) {
	payload := protocol.EncodeString(command)
	if err := protocol.WriteMessage(c.conn, protocol.MsgExec, protocol.FlagNone, payload); err != nil {
		return -1, err
	}

	for {
		msg, err := protocol.ReadMessage(c.conn)
		if err != nil {
			return -1, fmt.Errorf("read exec response: %w", err)
		}

		switch msg.Header.Type {
		case protocol.MsgExecOutput:
			out, err := protocol.DecodeExecOutputPayload(msg.Payload)
			if err != nil {
				return -1, err
			}
			switch out.Stream {
			case protocol.ExecStdout:
				stdoutWriter.Write(out.Data)
			case protocol.ExecStderr:
				stderrWriter.Write(out.Data)
			}

		case protocol.MsgExecExit:
			exitPayload, err := protocol.DecodeExecExitPayload(msg.Payload)
			if err != nil {
				return -1, err
			}
			return exitPayload.ExitCode, nil

		case protocol.MsgError:
			return -1, decodeError(msg)

		default:
			return -1, fmt.Errorf("unexpected message type 0x%02X during exec", msg.Header.Type)
		}
	}
}

// Scan scans for nearby Bluetooth devices.
func (c *Client) Scan(timeout int) ([]bt.Device, error) {
	return c.transport.Scan(c.adapter, time.Duration(timeout)*time.Second)
}

func (c *Client) expectOK() error {
	msg, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return err
	}
	if msg.Header.Type == protocol.MsgError {
		return decodeError(msg)
	}
	if msg.Header.Type != protocol.MsgOK {
		return fmt.Errorf("expected OK, got 0x%02X", msg.Header.Type)
	}
	return nil
}

func decodeError(msg *protocol.Message) error {
	ep, err := protocol.DecodeErrorPayload(msg.Payload)
	if err != nil {
		return fmt.Errorf("server error (unparseable)")
	}
	return fmt.Errorf("server error [%d]: %s", ep.Code, ep.Message)
}

// Conn returns the underlying connection for advanced use.
func (c *Client) Conn() io.ReadWriter {
	return c.conn
}
