package client

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"blue-file-transfer/internal/bt"
	bftcrypto "blue-file-transfer/internal/crypto"
	"blue-file-transfer/internal/protocol"
	"blue-file-transfer/internal/transfer"
)

// Client manages a connection to a remote BFT server.
type Client struct {
	transport bt.Transport
	conn      bt.Conn
	rw        io.ReadWriter // encrypted or plain conn
	adapter   string
	Compress  bool
	Username  string
	Password  string
	Encrypted bool
}

// New creates a new Client.
func New(transport bt.Transport, adapter string) *Client {
	return &Client{
		transport: transport,
		adapter:   adapter,
	}
}

// Connect connects to a remote server. If Username is set, sends authentication
// and establishes AES-256-GCM encrypted channel.
func (c *Client) Connect(remoteAddr string, channel uint8) error {
	conn, err := c.transport.Connect(c.adapter, remoteAddr, channel)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	c.conn = conn
	c.rw = conn // default: unencrypted

	// Send auth if credentials are set
	if c.Username != "" {
		payload := protocol.EncodeTwoStrings(c.Username, c.Password)
		if err := protocol.WriteMessage(c.conn, protocol.MsgAuth, protocol.FlagNone, payload); err != nil {
			c.conn.Close()
			c.conn = nil
			return fmt.Errorf("send auth: %w", err)
		}
		if err := c.expectOKRaw(); err != nil {
			c.conn.Close()
			c.conn = nil
			return fmt.Errorf("auth: %w", err)
		}

		// Establish encrypted channel using password as shared secret
		encStream, err := bftcrypto.ClientHandshake(c.conn, c.Password)
		if err != nil {
			c.conn.Close()
			c.conn = nil
			return fmt.Errorf("encryption handshake: %w", err)
		}
		c.rw = encStream
		c.Encrypted = true
	}

	return nil
}

// ConnectWithConn uses an existing connection (for testing).
func (c *Client) ConnectWithConn(conn bt.Conn) {
	c.conn = conn
	c.rw = conn
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
	if err := protocol.WriteMessage(c.rw, protocol.MsgPwd, protocol.FlagNone, nil); err != nil {
		return "", err
	}

	msg, err := protocol.ReadMessage(c.rw)
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
	if err := protocol.WriteMessage(c.rw, protocol.MsgChDir, protocol.FlagNone, payload); err != nil {
		return err
	}

	msg, err := protocol.ReadMessage(c.rw)
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
	if err := protocol.WriteMessage(c.rw, protocol.MsgListDir, protocol.FlagNone, payload); err != nil {
		return nil, err
	}

	msg, err := protocol.ReadMessage(c.rw)
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
	if err := protocol.WriteMessage(c.rw, protocol.MsgGetInfo, protocol.FlagNone, payload); err != nil {
		return nil, err
	}

	msg, err := protocol.ReadMessage(c.rw)
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
	if err := protocol.WriteMessage(c.rw, protocol.MsgDelete, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Mkdir creates a directory on the server.
func (c *Client) Mkdir(path string) error {
	payload := protocol.EncodeString(path)
	if err := protocol.WriteMessage(c.rw, protocol.MsgMkdir, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Copy copies a file/directory on the server.
func (c *Client) Copy(src, dst string) error {
	payload := protocol.EncodeTwoStrings(src, dst)
	if err := protocol.WriteMessage(c.rw, protocol.MsgCopy, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Move moves/renames a file/directory on the server.
func (c *Client) Move(src, dst string) error {
	payload := protocol.EncodeTwoStrings(src, dst)
	if err := protocol.WriteMessage(c.rw, protocol.MsgMove, protocol.FlagNone, payload); err != nil {
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
	if err := protocol.WriteMessage(c.rw, protocol.MsgDownload, protocol.FlagNone, req.Encode()); err != nil {
		return "", err
	}

	// Read first response to determine if it's a file or directory
	msg, err := protocol.ReadMessage(c.rw)
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
			if err := transfer.ReceiveDir(c.rw, destDir, progressFn); err != nil {
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
		var recvErr error // set on first local error; triggers drain mode

		for {
			chunkMsg, err := protocol.ReadMessage(c.rw)
			if err != nil {
				if recvErr != nil {
					os.Remove(destPath)
					return "", recvErr
				}
				return "", err
			}

			switch chunkMsg.Header.Type {
			case protocol.MsgDataChunk:
				if recvErr != nil {
					continue // drain remaining chunks
				}
				data, _, err := transfer.RecvChunk(chunkMsg)
				if err != nil {
					recvErr = err
					continue
				}
				if _, err := f.Write(data); err != nil {
					recvErr = err
					continue
				}
				totalCRC = transfer.CRC32Update(totalCRC, data)
				bytesReceived += int64(len(data))
				if progressFn != nil {
					progressFn(bytesReceived, totalSize)
				}

			case protocol.MsgTransferEnd:
				if recvErr != nil {
					os.Remove(destPath)
					return "", recvErr
				}
				endPayload, err := protocol.DecodeTransferEndPayload(chunkMsg.Payload)
				if err != nil {
					os.Remove(destPath)
					return "", err
				}
				if endPayload.TotalCRC32 != totalCRC {
					os.Remove(destPath)
					return "", fmt.Errorf("total CRC mismatch")
				}
				return destPath, nil

			case protocol.MsgError:
				os.Remove(destPath)
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

	if err := protocol.WriteMessage(c.rw, protocol.MsgUpload, protocol.FlagNone, req.Encode()); err != nil {
		return err
	}

	// Wait for OK (server ready)
	if err := c.expectOK(); err != nil {
		return err
	}

	if isDir {
		if err := transfer.SendDir(c.rw, localPath, transfer.DefaultChunkSize, c.Compress, progressFn); err != nil {
			// Send TransferEnd (to close any incomplete file) + MsgOK (to end directory)
			// so the server's ReceiveDir can drain and respond with an error.
			endPayload := &protocol.TransferEndPayload{TotalCRC32: 0}
			protocol.WriteMessage(c.rw, protocol.MsgTransferEnd, protocol.FlagNone, endPayload.Encode())
			protocol.WriteMessage(c.rw, protocol.MsgOK, protocol.FlagNone, nil)
			c.expectOK() // read server's error response to resync
			return err
		}
		protocol.WriteMessage(c.rw, protocol.MsgOK, protocol.FlagNone, nil)
	} else {
		if err := transfer.SendFile(c.rw, localPath, transfer.DefaultChunkSize, c.Compress, progressFn); err != nil {
			// Send TransferEnd to unblock server's receive loop.
			// CRC will mismatch — server will respond with error, keeping stream in sync.
			endPayload := &protocol.TransferEndPayload{TotalCRC32: 0}
			protocol.WriteMessage(c.rw, protocol.MsgTransferEnd, protocol.FlagNone, endPayload.Encode())
			c.expectOK() // read server's error response to resync
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
	if err := protocol.WriteMessage(c.rw, protocol.MsgExec, protocol.FlagNone, payload); err != nil {
		return -1, err
	}

	for {
		msg, err := protocol.ReadMessage(c.rw)
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

// Shell starts an interactive shell session on the server.
// Stdin is forwarded to the remote shell, stdout/stderr are displayed locally.
// Returns the shell exit code.
func (c *Client) Shell(stdin io.Reader, stdout, stderr io.Writer) (int32, error) {
	if err := protocol.WriteMessage(c.rw, protocol.MsgShell, protocol.FlagNone, nil); err != nil {
		return -1, err
	}

	// Wait for OK (shell ready)
	if err := c.expectOK(); err != nil {
		return -1, err
	}

	done := make(chan int32, 1)

	// Goroutine: read server output and display
	go func() {
		for {
			msg, err := protocol.ReadMessage(c.rw)
			if err != nil {
				done <- -1
				return
			}

			switch msg.Header.Type {
			case protocol.MsgExecOutput:
				out, err := protocol.DecodeExecOutputPayload(msg.Payload)
				if err != nil {
					continue
				}
				switch out.Stream {
				case protocol.ExecStdout:
					stdout.Write(out.Data)
				case protocol.ExecStderr:
					stderr.Write(out.Data)
				}

			case protocol.MsgExecExit:
				exitPayload, _ := protocol.DecodeExecExitPayload(msg.Payload)
				if exitPayload != nil {
					done <- exitPayload.ExitCode
				} else {
					done <- 0
				}
				return

			case protocol.MsgError:
				ep, _ := protocol.DecodeErrorPayload(msg.Payload)
				if ep != nil {
					fmt.Fprintf(stderr, "Server error: %s\n", ep.Message)
				}
				done <- -1
				return
			}
		}
	}()

	// Goroutine: read local stdin and send to server
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdin.Read(buf)
			if n > 0 {
				protocol.WriteMessage(c.rw, protocol.MsgShellIn, protocol.FlagNone, buf[:n])
			}
			if err != nil {
				// Send exit signal
				protocol.WriteMessage(c.rw, protocol.MsgExecExit, protocol.FlagNone, nil)
				return
			}
		}
	}()

	exitCode := <-done
	return exitCode, nil
}

// Passwd changes the current user's password on the server.
func (c *Client) Passwd(oldPassword, newPassword string) error {
	payload := protocol.EncodeTwoStrings(oldPassword, newPassword)
	if err := protocol.WriteMessage(c.rw, protocol.MsgPasswd, protocol.FlagNone, payload); err != nil {
		return err
	}
	return c.expectOK()
}

// Scan scans for nearby Bluetooth devices.
func (c *Client) Scan(timeout int) ([]bt.Device, error) {
	return c.transport.Scan(c.adapter, time.Duration(timeout)*time.Second)
}

// expectOKRaw reads from raw conn (before encryption is established).
func (c *Client) expectOKRaw() error {
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

func (c *Client) expectOK() error {
	msg, err := protocol.ReadMessage(c.rw)
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

// Conn returns the read/write stream (encrypted if auth is active).
func (c *Client) Conn() io.ReadWriter {
	return c.rw
}
