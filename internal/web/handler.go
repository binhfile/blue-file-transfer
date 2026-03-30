// Package web provides an embedded web GUI for browsing and managing
// files on a remote BFT server through a web browser.
package web

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blue-file-transfer/internal/protocol"
	"blue-file-transfer/internal/transfer"
)

// BFTClient defines the interface for BFT operations needed by the web GUI.
type BFTClient interface {
	ListDir(path string) (*protocol.DirListingPayload, error)
	ChDir(path string) error
	Download(remotePath, localDir string, progressFn transfer.ProgressFunc) (string, error)
	Upload(localPath, remotePath string, progressFn transfer.ProgressFunc) error
	Mkdir(path string) error
	Delete(path string) error
	Exec(command string, stdout, stderr io.Writer) (int32, error)
}

// Server serves the web GUI and proxies file operations to the BFT client.
type Server struct {
	client   BFTClient
	username string
	password string
	logger   *log.Logger
}

// New creates a new web server.
func New(c BFTClient, username, password string) *Server {
	return &Server{
		client:   c,
		username: username,
		password: password,
		logger:   log.New(os.Stderr, "[web] ", log.LstdFlags),
	}
}

// ListenAndServe starts the web server.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.auth(s.handleIndex))
	mux.HandleFunc("/api/ls", s.auth(s.handleLS))
	mux.HandleFunc("/api/download", s.auth(s.handleDownload))
	mux.HandleFunc("/api/upload", s.auth(s.handleUpload))
	mux.HandleFunc("/api/mkdir", s.auth(s.handleMkdir))
	mux.HandleFunc("/api/rm", s.auth(s.handleRm))
	mux.HandleFunc("/api/exec", s.auth(s.handleExec))

	s.logger.Printf("Web GUI listening on http://%s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.username || pass != s.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="BFT Web"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func (s *Server) handleLS(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}

	if path != "/" && path != "" {
		if err := s.client.ChDir(path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	listing, err := s.client.ListDir("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if path != "/" && path != "" {
		s.client.ChDir("/")
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"path":%q,"entries":[`, listing.Path)
	for i, e := range listing.Entries {
		if i > 0 {
			w.Write([]byte(","))
		}
		entryType := "file"
		if e.EntryType == protocol.EntryDir {
			entryType = "dir"
		}
		modTime := time.Unix(e.ModTime, 0).Format("2006-01-02 15:04:05")
		fmt.Fprintf(w, `{"name":%q,"type":%q,"size":%d,"mode":"%o","time":%q}`,
			e.Name, entryType, e.Size, e.Mode, modTime)
	}
	w.Write([]byte("]}"))
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}

	tmpDir, err := os.MkdirTemp("", "bft-web-")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	result, err := s.client.Download(path, tmpDir, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	info, err := os.Stat(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		http.Error(w, "directory download not supported via web", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(result)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	f, err := os.Open(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	io.Copy(w, f)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	remotePath := r.URL.Query().Get("path")
	if remotePath == "" {
		remotePath = "/"
	}

	r.ParseMultipartForm(64 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmpFile, err := os.CreateTemp("", "bft-upload-*-"+header.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	io.Copy(tmpFile, file)
	tmpFile.Close()

	remoteFile := header.Filename
	if remotePath != "/" {
		remoteFile = remotePath + "/" + header.Filename
	}

	if err := s.client.Upload(tmpFile.Name(), remoteFile, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"file":%q}`, remoteFile)
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	if err := s.client.Mkdir(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleRm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	if err := s.client.Delete(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	cmd := r.URL.Query().Get("cmd")
	if cmd == "" {
		http.Error(w, "cmd required", http.StatusBadRequest)
		return
	}

	var stdout, stderr strings.Builder
	exitCode, err := s.client.Exec(cmd, &stdout, &stderr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stdoutB64 := base64.StdEncoding.EncodeToString([]byte(stdout.String()))
	stderrB64 := base64.StdEncoding.EncodeToString([]byte(stderr.String()))

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"exit_code":%d,"stdout":"%s","stderr":"%s"}`, exitCode, stdoutB64, stderrB64)
}
