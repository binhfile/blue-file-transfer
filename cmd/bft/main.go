package main

import (
	"fmt"
	"os"
	"strconv"

	"blue-file-transfer/internal/bt"
	"blue-file-transfer/internal/client"
	"blue-file-transfer/internal/server"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "server":
		runServer(args)
	case "client":
		runClient(args)
	case "scan":
		runScan(args)
	case "version":
		fmt.Printf("bft version %s\n", version)
	case "benchmark", "bench":
		runBenchmark(args)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Blue File Transfer (bft) - Bluetooth file transfer tool

Usage:
  bft server [options]    Start as server
  bft client [options]    Start interactive client
  bft scan [options]      Scan for Bluetooth devices
  bft version             Show version
  bft help                Show this help

Server options:
  --adapter <hci>    Bluetooth adapter (default: hci0)
  --dir <path>       Root directory to serve (default: current dir)
  --channel <n>      RFCOMM channel 1-30 (default: 1)
  --rfcomm           Use RFCOMM transport (default: L2CAP)
  --no-exec          Disable remote command execution (enabled by default)

Client options:
  --adapter <hci>    Bluetooth adapter (default: hci0)
  --no-compress      Disable compression (enabled by default)
  --rfcomm           Use RFCOMM transport (default: L2CAP)

Scan options:
  --adapter <hci>    Bluetooth adapter (default: hci0)
  --timeout <sec>    Scan timeout in seconds (default: 10)`)
}

func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if len(args[i]) > 2 && args[i][:2] == "--" {
			key := args[i][2:]
			if i+1 < len(args) && (len(args[i+1]) < 2 || args[i+1][:2] != "--") {
				flags[key] = args[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		}
	}
	return flags
}

func getTransport(flags map[string]string) (bt.Transport, string) {
	proto := "l2cap"
	if _, ok := flags["rfcomm"]; ok {
		proto = "rfcomm"
	}
	return bt.NewTransportWithProtocol(proto), proto
}

func runServer(args []string) {
	flags := parseFlags(args)

	adapter := "hci0"
	if v, ok := flags["adapter"]; ok {
		adapter = v
	}

	dir := "."
	if v, ok := flags["dir"]; ok {
		dir = v
	}

	channel := uint8(1)
	if v, ok := flags["channel"]; ok {
		ch, err := strconv.Atoi(v)
		if err == nil && ch >= 1 && ch <= 30 {
			channel = uint8(ch)
		}
	}

	allowExec := true
	if _, ok := flags["no-exec"]; ok {
		allowExec = false
	}

	transport, proto := getTransport(flags)
	srv, err := server.New(transport, dir, adapter, channel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	srv.AllowExec = allowExec

	execStr := ""
	if allowExec {
		execStr = ", exec: ENABLED"
	}
	fmt.Printf("Starting BFT server [%s] on %s channel %d, serving: %s%s\n", proto, adapter, channel, dir, execStr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runClient(args []string) {
	flags := parseFlags(args)

	adapter := "hci0"
	if v, ok := flags["adapter"]; ok {
		adapter = v
	}

	compress := true
	if _, ok := flags["no-compress"]; ok {
		compress = false
	}

	transport, proto := getTransport(flags)
	c := client.New(transport, adapter)
	c.Compress = compress

	fmt.Printf("Transport: %s\n", proto)
	if err := client.RunInteractiveCLI(c); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runScan(args []string) {
	flags := parseFlags(args)

	adapter := "hci0"
	if v, ok := flags["adapter"]; ok {
		adapter = v
	}

	timeout := 10
	if v, ok := flags["timeout"]; ok {
		t, err := strconv.Atoi(v)
		if err == nil && t > 0 {
			timeout = t
		}
	}

	transport := bt.NewTransport()
	c := client.New(transport, adapter)

	fmt.Printf("Scanning on %s for %d seconds...\n", adapter, timeout)
	devices, err := c.Scan(timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
