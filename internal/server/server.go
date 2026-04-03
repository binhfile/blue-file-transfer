package server

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"blue-file-transfer/internal/auth"
	"blue-file-transfer/internal/bt"
	bftcrypto "blue-file-transfer/internal/crypto"
	"blue-file-transfer/internal/fsutil"
	"blue-file-transfer/internal/protocol"
	"blue-file-transfer/internal/transfer"
)

// Server handles incoming Bluetooth file transfer connections.
type Server struct {
	transport  bt.Transport
	rootDir    string
	adapter    string
	channel    uint8
	AllowExec  bool
	Users      *auth.UserStore
	MaxClients int // max concurrent connections; 0 = unlimited
	FileUID    int // UID for created files (-1 = don't chown)
	FileGID    int // GID for created files (-1 = don't chown)
	logger     *log.Logger

	mu          sync.Mutex
	activeConns []trackedConn
}

type trackedConn struct {
	conn      bt.Conn
	startTime time.Time
}

// New creates a new Server.
func New(transport bt.Transport, rootDir, adapter string, channel uint8) (*Server, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root dir: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat root dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root path is not a directory: %s", absRoot)
	}

	return &Server{
		transport: transport,
		rootDir:   absRoot,
		adapter:   adapter,
		channel:   channel,
		FileUID:   -1,
		FileGID:   -1,
		logger:    log.New(os.Stderr, "[server] ", log.LstdFlags),
	}, nil
}

// ListenAndServe starts listening for Bluetooth connections.
func (s *Server) ListenAndServe() error {
	listener, err := s.transport.Listen(s.adapter, s.channel)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	maxStr := "unlimited"
	if s.MaxClients > 0 {
		maxStr = fmt.Sprintf("%d", s.MaxClients)
	}
	s.logger.Printf("Listening on %s channel %d, serving: %s (max clients: %s)", listener.Addr(), s.channel, s.rootDir, maxStr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.logger.Printf("Accept error: %v", err)
			continue
		}
		s.logger.Printf("Client connected: %s", conn.RemoteAddr())
		s.trackConn(conn)
		go func(c bt.Conn) {
			defer s.untrackConn(c)
			s.handleConnection(c)
			s.logger.Printf("Client disconnected: %s", c.RemoteAddr())
		}(conn)
	}
}

// ServeConn handles a single client connection. Exported for testing.
func (s *Server) ServeConn(conn bt.Conn) {
	s.handleConnection(conn)
}

func (s *Server) trackConn(conn bt.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.MaxClients > 0 && len(s.activeConns) >= s.MaxClients {
		oldest := s.activeConns[0]
		s.logger.Printf("Connection limit (%d) reached, dropping oldest: %s",
			s.MaxClients, oldest.conn.RemoteAddr())
		oldest.conn.Close()
		s.activeConns = s.activeConns[1:]
	}

	s.activeConns = append(s.activeConns, trackedConn{
		conn:      conn,
		startTime: time.Now(),
	})
}

func (s *Server) untrackConn(conn bt.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.activeConns {
		if c.conn == conn {
			s.activeConns = append(s.activeConns[:i], s.activeConns[i+1:]...)
			return
		}
	}
}

func (s *Server) handleConnection(conn bt.Conn) {
	defer conn.Close()

	chunkSize := transfer.DefaultChunkSize

	// Read first message — may be MsgMTU (negotiation) or MsgAuth or a command
	firstMsg, err := protocol.ReadMessage(conn)
	if err != nil {
		if err != io.EOF {
			s.logger.Printf("Read error: %v", err)
		}
		return
	}

	// Optional MTU negotiation (client sends MsgMTU as first message)
	if firstMsg.Header.Type == protocol.MsgMTU {
		chunkSize = s.negotiateMTU(conn, firstMsg)
		firstMsg = nil
	}

	// Authentication handshake (if users are configured)
	var authUser string
	var authPassword string
	if s.Users != nil && s.Users.HasUsers() {
		user, pass, ok := s.authenticateMsg(conn, firstMsg)
		if !ok {
			return
		}
		authUser = user
		authPassword = pass
		firstMsg = nil
		s.logger.Printf("Authenticated: %s", authUser)
	}

	// Establish encrypted channel after auth (password is the shared secret)
	var rw io.ReadWriter = conn
	if authPassword != "" {
		encStream, err := bftcrypto.ServerHandshake(conn, authPassword)
		if err != nil {
			s.logger.Printf("Encryption handshake failed: %v", err)
			return
		}
		rw = encStream
		s.logger.Printf("Encryption established (AES-256-GCM)")
	}

	currentDir := s.rootDir

	for {
		var msg *protocol.Message
		if firstMsg != nil {
			msg = firstMsg
			firstMsg = nil
		} else {
			msg, err = protocol.ReadMessage(rw)
			if err != nil {
				if err != io.EOF {
					s.logger.Printf("Read error: %v", err)
				}
				return
			}
		}

		switch msg.Header.Type {
		case protocol.MsgPwd:
			s.handlePwd(rw, currentDir)

		case protocol.MsgChDir:
			newDir, err := s.handleChDir(rw, currentDir, msg.Payload)
			if err == nil {
				currentDir = newDir
			}

		case protocol.MsgListDir:
			s.handleListDir(rw, currentDir, msg.Payload)

		case protocol.MsgGetInfo:
			s.handleGetInfo(rw, currentDir, msg.Payload)

		case protocol.MsgDelete:
			s.handleDelete(rw, currentDir, msg.Payload)

		case protocol.MsgMkdir:
			s.handleMkdir(rw, currentDir, msg.Payload)

		case protocol.MsgCopy:
			s.handleCopy(rw, currentDir, msg.Payload)

		case protocol.MsgMove:
			s.handleMove(rw, currentDir, msg.Payload)

		case protocol.MsgDownload:
			s.handleDownload(rw, currentDir, msg.Payload, chunkSize)

		case protocol.MsgUpload:
			s.handleUpload(rw, currentDir, msg.Payload)

		case protocol.MsgExec:
			s.handleExec(rw, currentDir, msg.Payload)

		case protocol.MsgPasswd:
			s.handlePasswd(rw, authUser, msg.Payload)

		case protocol.MsgShell:
			s.handleShell(rw, currentDir)
			return // shell takes over the connection until exit

		default:
			s.sendError(rw, protocol.ErrCodeInvalidRequest, fmt.Sprintf("unknown message type: 0x%02X", msg.Header.Type))
		}
	}
}

// negotiateMTU handles MsgMTU from the client and responds with server's ACL info.
// Returns the agreed chunk size.
func (s *Server) negotiateMTU(conn bt.Conn, clientMsg *protocol.Message) int {
	clientMTU, err := protocol.DecodeMTUPayload(clientMsg.Payload)
	if err != nil {
		s.logger.Printf("MTU decode error: %v (using default)", err)
		return transfer.DefaultChunkSize
	}

	aclMTU, aclPkts, _ := bt.ReadACLInfo(s.adapter)
	myChunk := transfer.ComputeChunkSize(aclMTU, aclPkts)

	resp := &protocol.MTUPayload{
		AclMTU:    aclMTU,
		AclPkts:   aclPkts,
		ChunkSize: uint32(myChunk),
	}
	protocol.WriteMessage(conn, protocol.MsgMTU, protocol.FlagNone, resp.Encode())

	agreed := myChunk
	if clientMTU.ChunkSize > 0 && int(clientMTU.ChunkSize) < agreed {
		agreed = int(clientMTU.ChunkSize)
	}

	s.logger.Printf("MTU agreed: chunk=%d (client ACL %d×%d, server ACL %d×%d)",
		agreed, clientMTU.AclMTU, clientMTU.AclPkts, aclMTU, aclPkts)
	return agreed
}

// authenticateMsg performs auth using a pre-read message or reads a new one.
func (s *Server) authenticateMsg(conn bt.Conn, preRead *protocol.Message) (string, string, bool) {
	var msg *protocol.Message
	var err error
	if preRead != nil {
		msg = preRead
	} else {
		msg, err = protocol.ReadMessage(conn)
		if err != nil {
			s.logger.Printf("Auth read error: %v", err)
			return "", "", false
		}
	}

	if msg.Header.Type != protocol.MsgAuth {
		s.sendError(conn, protocol.ErrCodeAuthRequired, "authentication required")
		return "", "", false
	}

	username, off, err := protocol.DecodeString(msg.Payload, 0)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeAuthFailed, "invalid auth payload")
		return "", "", false
	}
	password, _, err := protocol.DecodeString(msg.Payload, off)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeAuthFailed, "invalid auth payload")
		return "", "", false
	}

	if !s.Users.Authenticate(username, password) {
		s.sendError(conn, protocol.ErrCodeAuthFailed, "invalid username or password")
		s.logger.Printf("Auth failed: %s", username)
		return "", "", false
	}

	s.sendOK(conn)
	return username, password, true
}

func (s *Server) sendError(conn io.Writer, code uint16, message string) {
	payload := &protocol.ErrorPayload{Code: code, Message: message}
	protocol.WriteMessage(conn, protocol.MsgError, protocol.FlagNone, payload.Encode())
}

func (s *Server) sendOK(conn io.Writer) {
	protocol.WriteMessage(conn, protocol.MsgOK, protocol.FlagNone, nil)
}

// chownTree recursively sets ownership on a path if FileUID/FileGID are configured.
// Used when the server runs as root but files should be owned by a normal user.
func (s *Server) chownTree(path string) {
	if s.FileUID < 0 && s.FileGID < 0 {
		return
	}
	uid, gid := s.FileUID, s.FileGID
	if uid < 0 {
		uid = os.Getuid()
	}
	if gid < 0 {
		gid = os.Getgid()
	}
	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			os.Lchown(p, uid, gid)
		}
		return nil
	})
}

func (s *Server) handlePwd(conn io.Writer, currentDir string) {
	relPath, err := filepath.Rel(s.rootDir, currentDir)
	if err != nil {
		relPath = currentDir
	}
	if relPath == "." {
		relPath = "/"
	} else {
		relPath = "/" + relPath
	}
	protocol.WriteMessage(conn, protocol.MsgOK, protocol.FlagNone, protocol.EncodeString(relPath))
}

func (s *Server) handleChDir(conn io.Writer, currentDir string, payload []byte) (string, error) {
	target, _, err := protocol.DecodeString(payload, 0)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid path")
		return "", err
	}

	newDir, err := fsutil.ResolveCwd(s.rootDir, currentDir, target)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return "", err
	}

	info, err := os.Stat(newDir)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeNotFound, err.Error())
		return "", err
	}
	if !info.IsDir() {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "not a directory")
		return "", fmt.Errorf("not a directory")
	}

	s.sendOK(conn)
	return newDir, nil
}

func (s *Server) handleListDir(conn io.Writer, currentDir string, payload []byte) {
	target := "."
	if len(payload) > 0 {
		t, _, err := protocol.DecodeString(payload, 0)
		if err == nil && t != "" {
			target = t
		}
	}

	safePath, err := fsutil.ResolveCwd(s.rootDir, currentDir, target)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	entries, err := fsutil.ListDir(safePath)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeNotFound, err.Error())
		return
	}

	relPath, _ := filepath.Rel(s.rootDir, safePath)
	if relPath == "." {
		relPath = "/"
	} else {
		relPath = "/" + relPath
	}

	listing := &protocol.DirListingPayload{
		Path:    relPath,
		Entries: make([]protocol.FileInfoPayload, 0, len(entries)),
	}

	for _, e := range entries {
		entryType := protocol.EntryFile
		if e.IsDir {
			entryType = protocol.EntryDir
		}
		listing.Entries = append(listing.Entries, protocol.FileInfoPayload{
			EntryType: entryType,
			Size:      uint64(e.Size),
			ModTime:   e.ModTime,
			Mode:      uint32(e.Mode),
			Name:      e.Name,
		})
	}

	protocol.WriteMessage(conn, protocol.MsgDirListing, protocol.FlagNone, listing.Encode())
}

func (s *Server) handleGetInfo(conn io.Writer, currentDir string, payload []byte) {
	target, _, err := protocol.DecodeString(payload, 0)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid path")
		return
	}

	safePath, err := fsutil.ResolveCwd(s.rootDir, currentDir, target)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	entry, err := fsutil.GetFileInfo(safePath)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeNotFound, err.Error())
		return
	}

	entryType := protocol.EntryFile
	if entry.IsDir {
		entryType = protocol.EntryDir
	}

	info := &protocol.FileInfoPayload{
		EntryType: entryType,
		Size:      uint64(entry.Size),
		ModTime:   entry.ModTime,
		Mode:      uint32(entry.Mode),
		Name:      entry.Name,
	}
	protocol.WriteMessage(conn, protocol.MsgFileInfo, protocol.FlagNone, info.Encode())
}

func (s *Server) handleDelete(conn io.Writer, currentDir string, payload []byte) {
	target, _, err := protocol.DecodeString(payload, 0)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid path")
		return
	}

	safePath, err := fsutil.ResolveCwd(s.rootDir, currentDir, target)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	if safePath == s.rootDir {
		s.sendError(conn, protocol.ErrCodePermission, "cannot delete root directory")
		return
	}

	if err := fsutil.RemoveAll(safePath); err != nil {
		s.sendError(conn, protocol.ErrCodePermission, err.Error())
		return
	}

	s.sendOK(conn)
}

func (s *Server) handleMkdir(conn io.Writer, currentDir string, payload []byte) {
	target, _, err := protocol.DecodeString(payload, 0)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid path")
		return
	}

	safePath, err := fsutil.ResolveCwd(s.rootDir, currentDir, target)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	if err := fsutil.MkdirAll(safePath); err != nil {
		s.sendError(conn, protocol.ErrCodePermission, err.Error())
		return
	}
	s.chownTree(safePath)

	s.sendOK(conn)
}

func (s *Server) handleCopy(conn io.Writer, currentDir string, payload []byte) {
	src, dst, err := protocol.DecodeTwoStrings(payload)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid paths")
		return
	}

	safeSrc, err := fsutil.ResolveCwd(s.rootDir, currentDir, src)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	safeDst, err := fsutil.ResolveCwd(s.rootDir, currentDir, dst)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	if err := fsutil.CopyDir(safeSrc, safeDst); err != nil {
		s.sendError(conn, protocol.ErrCodePermission, err.Error())
		return
	}
	s.chownTree(safeDst)

	s.sendOK(conn)
}

func (s *Server) handleMove(conn io.Writer, currentDir string, payload []byte) {
	src, dst, err := protocol.DecodeTwoStrings(payload)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid paths")
		return
	}

	safeSrc, err := fsutil.ResolveCwd(s.rootDir, currentDir, src)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	safeDst, err := fsutil.ResolveCwd(s.rootDir, currentDir, dst)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	if err := fsutil.MoveFileOrDir(safeSrc, safeDst); err != nil {
		s.sendError(conn, protocol.ErrCodePermission, err.Error())
		return
	}
	s.chownTree(safeDst)

	s.sendOK(conn)
}

func (s *Server) handleDownload(conn io.Writer, currentDir string, payload []byte, chunkSize int) {
	// Parse download request — supports both legacy (string-only) and new (with compress flag)
	var target string
	var compress bool

	req, err := protocol.DecodeDownloadRequestPayload(payload)
	if err == nil {
		target = req.Path
		compress = req.Compress
	} else {
		// Fallback: legacy plain string path
		target, _, _ = protocol.DecodeString(payload, 0)
	}

	if target == "" {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "empty path")
		return
	}

	safePath, err := fsutil.ResolveCwd(s.rootDir, currentDir, target)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	info, err := os.Stat(safePath)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeNotFound, err.Error())
		return
	}

	if info.IsDir() {
		if err := transfer.PipelineSendDir(conn, safePath, chunkSize, compress, nil); err != nil {
			s.logger.Printf("SendDir error: %v", err)
			// Send error to unblock client's receive loop (may fail if connection is dead)
			s.sendError(conn, protocol.ErrCodeInterrupted, fmt.Sprintf("download failed: %v", err))
			return
		}
		s.sendOK(conn)
	} else {
		if err := transfer.PipelineSendFile(conn, safePath, chunkSize, compress, nil); err != nil {
			s.logger.Printf("SendFile error: %v", err)
			// Send error to unblock client's receive loop (may fail if connection is dead)
			s.sendError(conn, protocol.ErrCodeInterrupted, fmt.Sprintf("download failed: %v", err))
			return
		}
	}
}

func (s *Server) handleUpload(conn io.ReadWriter, currentDir string, payload []byte) {
	req, err := protocol.DecodeUploadRequestPayload(payload)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid upload request")
		return
	}

	safePath, err := fsutil.ResolveCwd(s.rootDir, currentDir, req.Path)
	if err != nil {
		s.sendError(conn, protocol.ErrCodePathTraversal, err.Error())
		return
	}

	// Signal ready
	s.sendOK(conn)

	if req.IsDir {
		if err := transfer.ReceiveDir(conn, safePath, nil); err != nil {
			s.logger.Printf("ReceiveDir error: %v", err)
			// ReceiveDir drains internally until MsgOK on error,
			// so the stream is synced. Send error instead of OK.
			s.sendError(conn, protocol.ErrCodeInterrupted, fmt.Sprintf("upload failed: %v", err))
			return
		}
	} else {
		parentDir := filepath.Dir(safePath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			s.sendError(conn, protocol.ErrCodePermission, err.Error())
			return
		}
		if err := receiveFileToPath(conn, safePath); err != nil {
			s.logger.Printf("ReceiveFile error: %v", err)
			// receiveFileToPath drains internally until TransferEnd on error,
			// so the stream is synced. Send error instead of OK.
			s.sendError(conn, protocol.ErrCodeInterrupted, fmt.Sprintf("upload failed: %v", err))
			// Clean up partial file
			os.Remove(safePath)
			return
		}
		// Log received file size
		if fi, err := os.Stat(safePath); err == nil {
			s.logger.Printf("Received: %s (%d bytes)", safePath, fi.Size())
		}
	}

	s.chownTree(safePath)
	s.sendOK(conn)
}

// authenticate performs the auth handshake (reads from conn). For backward compatibility with tests.
func (s *Server) authenticate(conn bt.Conn) (string, string, bool) {
	return s.authenticateMsg(conn, nil)
}

func (s *Server) handlePasswd(conn io.Writer, authUser string, payload []byte) {
	if s.Users == nil || !s.Users.HasUsers() {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "no user store configured")
		return
	}

	// Payload: old_password + new_password
	oldPass, off, err := protocol.DecodeString(payload, 0)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid passwd payload")
		return
	}
	newPass, _, err := protocol.DecodeString(payload, off)
	if err != nil {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid passwd payload")
		return
	}

	// Verify old password
	if !s.Users.Authenticate(authUser, oldPass) {
		s.sendError(conn, protocol.ErrCodeAuthFailed, "incorrect current password")
		return
	}

	if err := s.Users.ChangePassword(authUser, newPass); err != nil {
		s.sendError(conn, protocol.ErrCodePermission, err.Error())
		return
	}

	s.logger.Printf("Password changed: %s", authUser)
	s.sendOK(conn)
}

func (s *Server) handleExec(conn io.Writer, currentDir string, payload []byte) {
	if !s.AllowExec {
		s.sendError(conn, protocol.ErrCodeExecDisabled, "remote command execution is disabled (server needs --allow-exec)")
		return
	}

	cmdStr, _, err := protocol.DecodeString(payload, 0)
	if err != nil || cmdStr == "" {
		s.sendError(conn, protocol.ErrCodeInvalidRequest, "invalid command")
		return
	}

	s.logger.Printf("Exec: %s (cwd: %s)", cmdStr, currentDir)

	// Determine shell
	shell := "/bin/sh"
	shellArg := "-c"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
		shellArg = "/C"
	}

	cmd := exec.Command(shell, shellArg, cmdStr)
	cmd.Dir = currentDir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.sendError(conn, protocol.ErrCodePermission, fmt.Sprintf("stdout pipe: %v", err))
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		s.sendError(conn, protocol.ErrCodePermission, fmt.Sprintf("stderr pipe: %v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		s.sendError(conn, protocol.ErrCodePermission, fmt.Sprintf("start: %v", err))
		return
	}

	// Stream stdout and stderr concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	streamPipe := func(pipe io.ReadCloser, streamType uint8) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := pipe.Read(buf)
			if n > 0 {
				out := &protocol.ExecOutputPayload{
					Stream: streamType,
					Data:   buf[:n],
				}
				protocol.WriteMessage(conn, protocol.MsgExecOutput, protocol.FlagNone, out.Encode())
			}
			if err != nil {
				break
			}
		}
	}

	go streamPipe(stdoutPipe, protocol.ExecStdout)
	go streamPipe(stderrPipe, protocol.ExecStderr)

	wg.Wait()

	exitCode := int32(0)
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = -1
		}
	}

	exitPayload := &protocol.ExecExitPayload{ExitCode: exitCode}
	protocol.WriteMessage(conn, protocol.MsgExecExit, protocol.FlagNone, exitPayload.Encode())
}

func (s *Server) handleShell(rw io.ReadWriter, currentDir string) {
	if !s.AllowExec {
		s.sendError(rw, protocol.ErrCodeExecDisabled, "remote execution is disabled")
		return
	}

	// Determine shell
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}

	s.logger.Printf("Shell session started: %s (cwd: %s)", shell, currentDir)

	cmd := exec.Command(shell)
	cmd.Dir = currentDir
	cmd.Env = append(os.Environ(), "TERM=xterm", "PS1=\\u@\\h:\\w\\$ ")

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		s.sendError(rw, protocol.ErrCodePermission, fmt.Sprintf("stdin pipe: %v", err))
		return
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.sendError(rw, protocol.ErrCodePermission, fmt.Sprintf("stdout pipe: %v", err))
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		s.sendError(rw, protocol.ErrCodePermission, fmt.Sprintf("stderr pipe: %v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		s.sendError(rw, protocol.ErrCodePermission, fmt.Sprintf("start shell: %v", err))
		return
	}

	// Signal client that shell is ready
	s.sendOK(rw)

	// Goroutine: read stdout and send to client
	var wg sync.WaitGroup
	wg.Add(2)

	sendOutput := func(pipe io.ReadCloser, stream uint8) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := pipe.Read(buf)
			if n > 0 {
				out := &protocol.ExecOutputPayload{Stream: stream, Data: buf[:n]}
				if werr := protocol.WriteMessage(rw, protocol.MsgExecOutput, protocol.FlagNone, out.Encode()); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}

	go sendOutput(stdoutPipe, protocol.ExecStdout)
	go sendOutput(stderrPipe, protocol.ExecStderr)

	// Main loop: read client input (MsgShellIn) and write to shell stdin
	go func() {
		for {
			msg, err := protocol.ReadMessage(rw)
			if err != nil {
				stdinPipe.Close()
				cmd.Process.Kill()
				return
			}

			switch msg.Header.Type {
			case protocol.MsgShellIn:
				if _, err := stdinPipe.Write(msg.Payload); err != nil {
					return
				}
			case protocol.MsgExecExit:
				// Client requested exit
				stdinPipe.Close()
				cmd.Process.Kill()
				return
			}
		}
	}()

	// Wait for shell to exit
	wg.Wait()

	exitCode := int32(0)
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		}
	}

	exitPayload := &protocol.ExecExitPayload{ExitCode: exitCode}
	protocol.WriteMessage(rw, protocol.MsgExecExit, protocol.FlagNone, exitPayload.Encode())
	s.logger.Printf("Shell session ended (exit %d)", exitCode)
}

// receiveFileToPath receives a file to a specific path.
// Automatically handles compressed and uncompressed chunks.
// On local errors (CRC mismatch, disk write), it drains remaining chunks
// until TransferEnd to keep the protocol stream synchronized.
func receiveFileToPath(r io.Reader, destPath string) error {
	msg, err := protocol.ReadMessage(r)
	if err != nil {
		return fmt.Errorf("read file info: %w", err)
	}
	if msg.Header.Type != protocol.MsgFileInfo {
		return fmt.Errorf("expected MsgFileInfo, got 0x%02X", msg.Header.Type)
	}

	fileInfo, _, err := protocol.DecodeFileInfoPayload(msg.Payload, 0)
	if err != nil {
		return fmt.Errorf("decode file info: %w", err)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileInfo.Mode))
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	var totalCRC uint32
	var recvErr error // set on first local error; triggers drain mode

	for {
		msg, err := protocol.ReadMessage(r)
		if err != nil {
			if recvErr != nil {
				return recvErr
			}
			return fmt.Errorf("read message: %w", err)
		}

		switch msg.Header.Type {
		case protocol.MsgDataChunk:
			if recvErr != nil {
				continue // drain remaining chunks
			}
			data, _, err := transfer.RecvChunk(msg)
			if err != nil {
				recvErr = err
				continue
			}
			if _, err := f.Write(data); err != nil {
				recvErr = fmt.Errorf("write: %w", err)
				continue
			}
			totalCRC = transfer.CRC32Update(totalCRC, data)

		case protocol.MsgTransferEnd:
			if recvErr != nil {
				return recvErr
			}
			endPayload, err := protocol.DecodeTransferEndPayload(msg.Payload)
			if err != nil {
				return fmt.Errorf("decode transfer end: %w", err)
			}
			if endPayload.TotalCRC32 != totalCRC {
				return fmt.Errorf("total CRC mismatch")
			}
			return nil

		case protocol.MsgError:
			errPayload, _ := protocol.DecodeErrorPayload(msg.Payload)
			if errPayload != nil {
				return fmt.Errorf("remote error: [%d] %s", errPayload.Code, errPayload.Message)
			}
			return fmt.Errorf("remote error")
		}
	}
}
