package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"blue-file-transfer/internal/client"
	"blue-file-transfer/internal/transfer"
)

func runBenchmark(args []string) {
	flags := parseFlags(args)

	clientAdapter := "hci1"
	if v, ok := flags["client-adapter"]; ok {
		clientAdapter = v
	}

	serverAddr := ""
	if v, ok := flags["server"]; ok {
		serverAddr = v
	}
	if serverAddr == "" {
		fmt.Fprintln(os.Stderr, "Error: --server <bt-address> is required")
		os.Exit(1)
	}

	channel := uint8(1)
	if v, ok := flags["channel"]; ok {
		ch, _ := strconv.Atoi(v)
		if ch > 0 && ch < 31 {
			channel = uint8(ch)
		}
	}

	chunkSize := transfer.DefaultChunkSize
	if v, ok := flags["chunk-size"]; ok {
		cs, _ := strconv.Atoi(v)
		if cs > 0 {
			chunkSize = cs
		}
	}

	localDir := "."
	if v, ok := flags["local-dir"]; ok {
		localDir = v
	}

	remotePath := ""
	if v, ok := flags["remote-file"]; ok {
		remotePath = v
	}

	uploadFile := ""
	if v, ok := flags["upload-file"]; ok {
		uploadFile = v
	}

	compress := true
	if _, ok := flags["no-compress"]; ok {
		compress = false
	}

	transport, proto := getTransport(flags)
	c := client.New(transport, clientAdapter)
	c.Compress = compress

	fmt.Printf("Connecting to %s on %s channel %d [%s]...\n", serverAddr, clientAdapter, channel, proto)
	if err := c.Connect(serverAddr, channel); err != nil {
		fmt.Fprintf(os.Stderr, "Connect error: %v\n", err)
		os.Exit(1)
	}
	defer c.Disconnect()
	fmt.Println("Connected!")

	compressStr := "OFF"
	if compress {
		compressStr = "ON"
	}

	// Run download benchmark if remote-file specified
	if remotePath != "" {
		fmt.Printf("\n=== DOWNLOAD BENCHMARK (chunk=%d, compress=%s) ===\n", chunkSize, compressStr)
		benchDownload(c, remotePath, localDir, chunkSize)
	}

	// Run upload benchmark if upload-file specified
	if uploadFile != "" {
		fmt.Printf("\n=== UPLOAD BENCHMARK (chunk=%d, compress=%s) ===\n", chunkSize, compressStr)
		benchUpload(c, uploadFile, chunkSize)
	}

	if remotePath == "" && uploadFile == "" {
		fmt.Println("No benchmark action specified. Use --remote-file and/or --upload-file")
	}
}

func benchDownload(c *client.Client, remotePath, localDir string, chunkSize int) {
	os.MkdirAll(localDir, 0755)

	startTime := time.Now()
	var lastBytes int64
	var lastTime time.Time

	progressFn := func(transferred, total int64) {
		now := time.Now()
		if now.Sub(lastTime) >= 500*time.Millisecond || transferred >= total {
			speed := float64(0)
			elapsed := now.Sub(startTime).Seconds()
			if elapsed > 0 {
				speed = float64(transferred) / elapsed
			}

			pct := float64(0)
			if total > 0 {
				pct = float64(transferred) / float64(total) * 100
			}

			instantSpeed := float64(0)
			if !lastTime.IsZero() {
				dt := now.Sub(lastTime).Seconds()
				if dt > 0 {
					instantSpeed = float64(transferred-lastBytes) / dt
				}
			}

			fmt.Printf("\r  %s / %s (%.1f%%) avg: %s  cur: %s      ",
				fmtSize(transferred), fmtSize(total), pct,
				fmtBitSpeed(speed), fmtBitSpeed(instantSpeed))

			lastBytes = transferred
			lastTime = now

			if transferred >= total {
				fmt.Println()
			}
		}
	}

	result, err := c.Download(remotePath, localDir, progressFn)
	elapsed := time.Since(startTime)

	if err != nil {
		fmt.Printf("Download error: %v\n", err)
		return
	}

	info, _ := os.Stat(result)
	size := info.Size()
	speed := float64(size) / elapsed.Seconds()

	fmt.Printf("  File: %s\n", filepath.Base(result))
	fmt.Printf("  Size: %s\n", fmtSize(size))
	fmt.Printf("  Time: %.3f s\n", elapsed.Seconds())
	fmt.Printf("  Speed: %s\n", fmtBitSpeed(speed))

	// Cleanup downloaded file
	os.Remove(result)
}

func benchUpload(c *client.Client, localPath string, chunkSize int) {
	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Printf("Upload error: %v\n", err)
		return
	}
	size := info.Size()

	remoteName := "benchmark_upload_" + filepath.Base(localPath)

	startTime := time.Now()
	var lastBytes int64
	var lastTime time.Time

	progressFn := func(transferred, total int64) {
		now := time.Now()
		if now.Sub(lastTime) >= 500*time.Millisecond || transferred >= total {
			speed := float64(0)
			elapsed := now.Sub(startTime).Seconds()
			if elapsed > 0 {
				speed = float64(transferred) / elapsed
			}

			pct := float64(0)
			if total > 0 {
				pct = float64(transferred) / float64(total) * 100
			}

			fmt.Printf("\r  %s / %s (%.1f%%) avg: %s      ",
				fmtSize(transferred), fmtSize(total), pct, fmtBitSpeed(speed))

			lastBytes = transferred
			lastTime = now
			_ = lastBytes

			if transferred >= total {
				fmt.Println()
			}
		}
	}

	err = c.Upload(localPath, remoteName, progressFn)
	elapsed := time.Since(startTime)

	if err != nil {
		fmt.Printf("Upload error: %v\n", err)
		return
	}

	speed := float64(size) / elapsed.Seconds()

	fmt.Printf("  File: %s\n", filepath.Base(localPath))
	fmt.Printf("  Size: %s\n", fmtSize(size))
	fmt.Printf("  Time: %.3f s\n", elapsed.Seconds())
	fmt.Printf("  Speed: %s\n", fmtBitSpeed(speed))

	// Cleanup uploaded file on server
	c.Delete(remoteName)
}

func fmtSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
	)
	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func fmtBitSpeed(bytesPerSec float64) string {
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
