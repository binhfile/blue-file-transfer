package client

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blue-file-transfer/internal/protocol"
)

// RunInteractiveCLI starts the interactive command line interface.
func RunInteractiveCLI(c *Client) error {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Blue File Transfer Client")
	if c.Compress {
		fmt.Println("Compression: ON")
	}
	fmt.Println("Type 'help' for available commands")
	fmt.Println()

	for {
		if c.IsConnected() {
			fmt.Print("bft> ")
		} else {
			fmt.Print("bft (disconnected)> ")
		}

		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		args := splitArgs(line)
		if len(args) == 0 {
			continue
		}

		cmd := strings.ToLower(args[0])
		cmdArgs := args[1:]

		switch cmd {
		case "help":
			printHelp()
		case "exit", "quit":
			if c.IsConnected() {
				c.Disconnect()
			}
			fmt.Println("Goodbye!")
			return nil
		case "connect":
			handleConnect(c, cmdArgs)
		case "disconnect":
			handleDisconnect(c)
		case "scan":
			handleScan(c, cmdArgs)
		case "ls":
			handleLs(c, cmdArgs)
		case "cd":
			handleCd(c, cmdArgs)
		case "pwd":
			handlePwd(c)
		case "info":
			handleInfo(c, cmdArgs)
		case "download":
			handleDownload(c, cmdArgs)
		case "upload":
			handleUpload(c, cmdArgs)
		case "rm":
			handleRm(c, cmdArgs)
		case "mkdir":
			handleMkdir(c, cmdArgs)
		case "cp":
			handleCp(c, cmdArgs)
		case "mv":
			handleMv(c, cmdArgs)
		case "compress":
			handleCompress(c, cmdArgs)
		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", cmd)
		}
	}

	return scanner.Err()
}

func printHelp() {
	fmt.Println(`Available commands:
  connect <address> [channel]  Connect to a BFT server
  disconnect                   Disconnect from server
  scan                         Scan for Bluetooth devices
  ls [path]                    List remote directory
  cd <path>                    Change remote directory
  pwd                          Print remote working directory
  info <path>                  Get file/directory info
  download <remote> [local]    Download file/directory
  upload <local> [remote]      Upload file/directory
  rm <path>                    Delete file/directory on server
  mkdir <path>                 Create directory on server
  cp <src> <dst>               Copy on server
  mv <src> <dst>               Move/rename on server
  compress [on|off]             Toggle or set compression
  help                         Show this help
  exit                         Exit client`)
}

func requireConnected(c *Client) bool {
	if !c.IsConnected() {
		fmt.Println("Error: not connected. Use 'connect <address>' first.")
		return false
	}
	return true
}

func handleConnect(c *Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: connect <bt-address> [channel]")
		return
	}
	addr := args[0]
	channel := uint8(1)
	if len(args) > 1 {
		var ch int
		fmt.Sscanf(args[1], "%d", &ch)
		if ch > 0 && ch < 31 {
			channel = uint8(ch)
		}
	}

	fmt.Printf("Connecting to %s channel %d...\n", addr, channel)
	if err := c.Connect(addr, channel); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("Connected!")
}

func handleDisconnect(c *Client) {
	if !c.IsConnected() {
		fmt.Println("Not connected.")
		return
	}
	c.Disconnect()
	fmt.Println("Disconnected.")
}

func handleScan(c *Client, args []string) {
	timeout := 10
	if len(args) > 0 {
		fmt.Sscanf(args[0], "%d", &timeout)
	}

	fmt.Printf("Scanning for %d seconds...\n", timeout)
	devices, err := c.transport.Scan(c.adapter, time.Duration(timeout)*time.Second)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if len(devices) == 0 {
		fmt.Println("No devices found.")
		return
	}

	fmt.Printf("Found %d device(s):\n", len(devices))
	for _, d := range devices {
		fmt.Printf("  %s  %s\n", d.Address, d.Name)
	}
}

func handleLs(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}

	path := ""
	if len(args) > 0 {
		path = args[0]
	}

	listing, err := c.ListDir(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Directory: %s\n", listing.Path)
	if len(listing.Entries) == 0 {
		fmt.Println("  (empty)")
		return
	}

	for _, e := range listing.Entries {
		typeStr := "FILE"
		if e.EntryType == protocol.EntryDir {
			typeStr = "DIR "
		} else if e.EntryType == protocol.EntrySymlink {
			typeStr = "LINK"
		}

		sizeStr := formatSize(int64(e.Size))
		modTime := time.Unix(e.ModTime, 0).Format("2006-01-02 15:04")
		modeStr := os.FileMode(e.Mode).String()

		fmt.Printf("  %s %s %8s %s %s\n", typeStr, modeStr, sizeStr, modTime, e.Name)
	}
}

func handleCd(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: cd <path>")
		return
	}

	if err := c.ChDir(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func handlePwd(c *Client) {
	if !requireConnected(c) {
		return
	}

	path, err := c.Pwd()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println(path)
}

func handleInfo(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: info <path>")
		return
	}

	info, err := c.GetInfo(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	typeStr := "File"
	if info.EntryType == protocol.EntryDir {
		typeStr = "Directory"
	}

	fmt.Printf("  Name:     %s\n", info.Name)
	fmt.Printf("  Type:     %s\n", typeStr)
	fmt.Printf("  Size:     %s (%d bytes)\n", formatSize(int64(info.Size)), info.Size)
	fmt.Printf("  Mode:     %s\n", os.FileMode(info.Mode))
	fmt.Printf("  Modified: %s\n", time.Unix(info.ModTime, 0).Format("2006-01-02 15:04:05"))
}

func handleDownload(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: download <remote-path> [local-dir]")
		return
	}

	remotePath := args[0]
	localDir := "."
	if len(args) > 1 {
		localDir = args[1]
	}

	// Ensure local dir exists
	if err := os.MkdirAll(localDir, 0755); err != nil {
		fmt.Printf("Error creating local dir: %v\n", err)
		return
	}

	startTime := time.Now()
	var lastPrint time.Time

	progressFn := func(transferred, total int64) {
		now := time.Now()
		if now.Sub(lastPrint) < 200*time.Millisecond && transferred < total {
			return
		}
		lastPrint = now

		elapsed := now.Sub(startTime).Seconds()
		speed := float64(0)
		if elapsed > 0 {
			speed = float64(transferred) / elapsed
		}

		pct := float64(0)
		if total > 0 {
			pct = float64(transferred) / float64(total) * 100
		}

		fmt.Printf("\r  %s / %s (%.1f%%) %s    ",
			formatSize(transferred), formatSize(total), pct, formatBitSpeed(speed))

		if transferred >= total {
			fmt.Println()
		}
	}

	result, err := c.Download(remotePath, localDir, progressFn)
	if err != nil {
		fmt.Printf("\nError: %v\n", err)
		return
	}

	elapsed := time.Since(startTime)
	fmt.Printf("Downloaded to: %s (%.2fs)\n", result, elapsed.Seconds())
}

func handleUpload(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: upload <local-path> [remote-path]")
		return
	}

	localPath := args[0]
	remotePath := filepath.Base(localPath)
	if len(args) > 1 {
		remotePath = args[1]
	}

	// Verify local path exists
	if _, err := os.Stat(localPath); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	startTime := time.Now()
	var lastPrint time.Time

	progressFn := func(transferred, total int64) {
		now := time.Now()
		if now.Sub(lastPrint) < 200*time.Millisecond && transferred < total {
			return
		}
		lastPrint = now

		elapsed := now.Sub(startTime).Seconds()
		speed := float64(0)
		if elapsed > 0 {
			speed = float64(transferred) / elapsed
		}

		pct := float64(0)
		if total > 0 {
			pct = float64(transferred) / float64(total) * 100
		}

		fmt.Printf("\r  %s / %s (%.1f%%) %s    ",
			formatSize(transferred), formatSize(total), pct, formatBitSpeed(speed))

		if transferred >= total {
			fmt.Println()
		}
	}

	if err := c.Upload(localPath, remotePath, progressFn); err != nil {
		fmt.Printf("\nError: %v\n", err)
		return
	}

	elapsed := time.Since(startTime)
	fmt.Printf("Upload complete (%.2fs)\n", elapsed.Seconds())
}

func handleRm(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: rm <path>")
		return
	}

	if err := c.Delete(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("Deleted.")
}

func handleMkdir(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: mkdir <path>")
		return
	}

	if err := c.Mkdir(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("Directory created.")
}

func handleCp(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 2 {
		fmt.Println("Usage: cp <src> <dst>")
		return
	}

	if err := c.Copy(args[0], args[1]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("Copied.")
}

func handleMv(c *Client, args []string) {
	if !requireConnected(c) {
		return
	}
	if len(args) < 2 {
		fmt.Println("Usage: mv <src> <dst>")
		return
	}

	if err := c.Move(args[0], args[1]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("Moved.")
}

func handleCompress(c *Client, args []string) {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "on", "true", "1":
			c.Compress = true
		case "off", "false", "0":
			c.Compress = false
		default:
			fmt.Println("Usage: compress [on|off]")
			return
		}
	} else {
		c.Compress = !c.Compress
	}
	if c.Compress {
		fmt.Println("Compression: ON")
	} else {
		fmt.Println("Compression: OFF")
	}
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatBitSpeed formats bytes/sec as a bit-rate string (bps, Kbps, Mbps).
func formatBitSpeed(bytesPerSec float64) string {
	bps := bytesPerSec * 8
	switch {
	case bps >= 1000000:
		return fmt.Sprintf("%.2f Mbps", bps/1000000)
	case bps >= 1000:
		return fmt.Sprintf("%.1f Kbps", bps/1000)
	default:
		return fmt.Sprintf("%.0f bps", bps)
	}
}

func splitArgs(line string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(ch)
			}
		} else if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
		} else if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
