package main

import (
	"fmt"
	"os"
	"strconv"

	"blue-file-transfer/internal/auth"
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
	case "useradd":
		runUserAdd(args)
	case "userdel":
		runUserDel(args)
	case "userlist":
		runUserList(args)
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
  bft server [options]       Start as server
  bft client [options]       Start interactive client
  bft scan [options]         Scan for Bluetooth devices
  bft useradd [options]      Add a user to the users file
  bft userdel [options]      Remove a user from the users file
  bft userlist [options]     List users in the users file
  bft version                Show version
  bft help                   Show this help

Server options:
  --adapter <hci>      Bluetooth adapter (default: hci0)
  --dir <path>         Root directory to serve (default: current dir)
  --channel <n>        RFCOMM channel 1-30 (default: 1)
  --rfcomm             Use RFCOMM transport (default: L2CAP)
  --no-exec            Disable remote command execution (enabled by default)
  --users-file <path>  Users file for authentication (default: none, no auth)

Client options:
  --adapter <hci>      Bluetooth adapter (default: hci0)
  --no-compress        Disable compression (enabled by default)
  --rfcomm             Use RFCOMM transport (default: L2CAP)
  --user <username>    Username for authentication
  --pass <password>    Password for authentication

Scan options:
  --adapter <hci>      Bluetooth adapter (default: hci0)
  --timeout <sec>      Scan timeout in seconds (default: 10)

User management:
  bft useradd --users-file <path> --user <name> --pass <password>
  bft userdel --users-file <path> --user <name>
  bft userlist --users-file <path>`)
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

	usersFile := ""
	if v, ok := flags["users-file"]; ok {
		usersFile = v
	}

	transport, proto := getTransport(flags)
	srv, err := server.New(transport, dir, adapter, channel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	srv.AllowExec = allowExec

	if usersFile != "" {
		users, err := auth.NewUserStore(usersFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading users: %v\n", err)
			os.Exit(1)
		}
		srv.Users = users
	}

	authStr := ""
	if srv.Users != nil && srv.Users.HasUsers() {
		authStr = fmt.Sprintf(", auth: %d user(s)", len(srv.Users.ListUsers()))
	}
	execStr := ""
	if allowExec {
		execStr = ", exec: ENABLED"
	}
	fmt.Printf("Starting BFT server [%s] on %s channel %d, serving: %s%s%s\n", proto, adapter, channel, dir, authStr, execStr)
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

	username := ""
	if v, ok := flags["user"]; ok {
		username = v
	}
	password := ""
	if v, ok := flags["pass"]; ok {
		password = v
	}

	transport, proto := getTransport(flags)
	c := client.New(transport, adapter)
	c.Compress = compress
	c.Username = username
	c.Password = password

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

func runUserAdd(args []string) {
	flags := parseFlags(args)

	usersFile, ok := flags["users-file"]
	if !ok || usersFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --users-file is required")
		os.Exit(1)
	}
	username, ok := flags["user"]
	if !ok || username == "" {
		fmt.Fprintln(os.Stderr, "Error: --user is required")
		os.Exit(1)
	}
	password, ok := flags["pass"]
	if !ok || password == "" {
		fmt.Fprintln(os.Stderr, "Error: --pass is required")
		os.Exit(1)
	}

	store, err := auth.NewUserStore(usersFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := store.AddUser(username, password); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("User '%s' added to %s\n", username, usersFile)
}

func runUserDel(args []string) {
	flags := parseFlags(args)

	usersFile, ok := flags["users-file"]
	if !ok || usersFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --users-file is required")
		os.Exit(1)
	}
	username, ok := flags["user"]
	if !ok || username == "" {
		fmt.Fprintln(os.Stderr, "Error: --user is required")
		os.Exit(1)
	}

	store, err := auth.NewUserStore(usersFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := store.RemoveUser(username); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("User '%s' removed from %s\n", username, usersFile)
}

func runUserList(args []string) {
	flags := parseFlags(args)

	usersFile, ok := flags["users-file"]
	if !ok || usersFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --users-file is required")
		os.Exit(1)
	}

	store, err := auth.NewUserStore(usersFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	users := store.ListUsers()
	if len(users) == 0 {
		fmt.Println("No users configured.")
		return
	}
	fmt.Printf("Users in %s:\n", usersFile)
	for _, u := range users {
		fmt.Printf("  %s\n", u)
	}
}
