package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"blue-file-transfer/internal/auth"
	"blue-file-transfer/internal/bt"
	"blue-file-transfer/internal/client"
	"blue-file-transfer/internal/server"
	"blue-file-transfer/internal/web"
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
	case "ls":
		runOneShot(args, "ls")
	case "download", "dl":
		runOneShot(args, "download")
	case "upload", "ul":
		runOneShot(args, "upload")
	case "rm":
		runOneShot(args, "rm")
	case "mkdir":
		runOneShot(args, "mkdir")
	case "cp":
		runOneShot(args, "cp")
	case "mv":
		runOneShot(args, "mv")
	case "exec", "!":
		runOneShot(args, "exec")
	case "shell":
		runOneShot(args, "shell")
	case "web":
		runWeb(args)
	case "pwd":
		runOneShot(args, "pwd")
	case "info":
		runOneShot(args, "info")
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
  bft version                Show version

One-shot commands (connect, run, exit):
  bft ls    --server <addr> [--path <dir>] [conn-options]
  bft download --server <addr> --path <remote> [--local <dir>] [conn-options]
  bft upload   --server <addr> --path <local> [--remote <path>] [conn-options]
  bft rm    --server <addr> --path <target> [conn-options]
  bft mkdir --server <addr> --path <dir> [conn-options]
  bft cp    --server <addr> --src <s> --dst <d> [conn-options]
  bft mv    --server <addr> --src <s> --dst <d> [conn-options]
  bft info  --server <addr> --path <target> [conn-options]
  bft pwd   --server <addr> [conn-options]
  bft exec  --server <addr> --cmd <command> [conn-options]
  bft shell --server <addr> [conn-options]
  bft web   --server <addr> [--port 8080] [--web-user admin] [--web-pass pass] [conn-options]

User management:
  bft useradd --users-file <path> --user <name> --pass <password>
  bft userdel --users-file <path> --user <name>
  bft userlist --users-file <path>

Server options:
  --adapter <hci>      Bluetooth adapter (default: hci0)
  --dir <path>         Root directory to serve (default: current dir)
  --channel <n>        RFCOMM channel 1-30 (default: 1)
  --rfcomm             Use RFCOMM transport (default: L2CAP)
  --no-exec            Disable remote command execution (enabled by default)
  --users-file <path>  Users file for authentication (default: none, no auth)

Connection options (client, one-shot, benchmark):
  --adapter <hci>      Bluetooth adapter (default: hci0)
  --server <addr>      Server Bluetooth address
  --channel <n>        Channel number (default: 1)
  --rfcomm             Use RFCOMM transport (default: L2CAP)
  --user <username>    Username for authentication
  --pass <password>    Password for authentication
  --no-compress        Disable compression (enabled by default)`)
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

func connectOneShot(flags map[string]string) *client.Client {
	serverAddr, ok := flags["server"]
	if !ok || serverAddr == "" {
		fmt.Fprintln(os.Stderr, "Error: --server <bt-address> is required")
		os.Exit(1)
	}

	adapter := "hci0"
	if v, ok := flags["adapter"]; ok {
		adapter = v
	}
	channel := uint8(1)
	if v, ok := flags["channel"]; ok {
		ch, _ := strconv.Atoi(v)
		if ch > 0 && ch < 31 {
			channel = uint8(ch)
		}
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

	transport, _ := getTransport(flags)
	c := client.New(transport, adapter)
	c.Compress = compress
	c.Username = username
	c.Password = password

	if err := c.Connect(serverAddr, channel); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return c
}

func runOneShot(args []string, command string) {
	flags := parseFlags(args)
	c := connectOneShot(flags)
	defer c.Disconnect()

	switch command {
	case "ls":
		path := ""
		if v, ok := flags["path"]; ok {
			path = v
		}
		listing, err := c.ListDir(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, e := range listing.Entries {
			typeStr := "FILE"
			if e.EntryType == 1 {
				typeStr = "DIR "
			}
			fmt.Printf("  %s %8d  %s\n", typeStr, e.Size, e.Name)
		}

	case "pwd":
		path, err := c.Pwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(path)

	case "info":
		target := flags["path"]
		if target == "" {
			fmt.Fprintln(os.Stderr, "Error: --path is required")
			os.Exit(1)
		}
		info, err := c.GetInfo(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Name: %s\nSize: %d\nType: %d\nMode: %o\n", info.Name, info.Size, info.EntryType, info.Mode)

	case "download":
		remotePath := flags["path"]
		if remotePath == "" {
			fmt.Fprintln(os.Stderr, "Error: --path is required")
			os.Exit(1)
		}
		localDir := "."
		if v, ok := flags["local"]; ok {
			localDir = v
		}
		result, err := c.Download(remotePath, localDir, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(result)

	case "upload":
		localPath := flags["path"]
		if localPath == "" {
			fmt.Fprintln(os.Stderr, "Error: --path is required")
			os.Exit(1)
		}
		remotePath := ""
		if v, ok := flags["remote"]; ok {
			remotePath = v
		}
		if remotePath == "" {
			remotePath = filepath.Base(localPath)
		}
		if err := c.Upload(localPath, remotePath, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("OK")

	case "rm":
		target := flags["path"]
		if target == "" {
			fmt.Fprintln(os.Stderr, "Error: --path is required")
			os.Exit(1)
		}
		if err := c.Delete(target); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "mkdir":
		target := flags["path"]
		if target == "" {
			fmt.Fprintln(os.Stderr, "Error: --path is required")
			os.Exit(1)
		}
		if err := c.Mkdir(target); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "cp":
		src, dst := flags["src"], flags["dst"]
		if src == "" || dst == "" {
			fmt.Fprintln(os.Stderr, "Error: --src and --dst are required")
			os.Exit(1)
		}
		if err := c.Copy(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "mv":
		src, dst := flags["src"], flags["dst"]
		if src == "" || dst == "" {
			fmt.Fprintln(os.Stderr, "Error: --src and --dst are required")
			os.Exit(1)
		}
		if err := c.Move(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "exec":
		cmdStr := flags["cmd"]
		if cmdStr == "" {
			fmt.Fprintln(os.Stderr, "Error: --cmd is required")
			os.Exit(1)
		}
		exitCode, err := c.Exec(cmdStr, os.Stdout, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(int(exitCode))

	case "shell":
		exitCode, err := c.Shell(os.Stdin, os.Stdout, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(int(exitCode))
	}
}

func runWeb(args []string) {
	flags := parseFlags(args)
	c := connectOneShot(flags)
	// Don't defer Disconnect — web server runs until killed

	port := "8080"
	if v, ok := flags["port"]; ok {
		port = v
	}
	webUser := "admin"
	if v, ok := flags["web-user"]; ok {
		webUser = v
	}
	webPass := "quansu1!"
	if v, ok := flags["web-pass"]; ok {
		webPass = v
	}

	addr := "0.0.0.0:" + port
	fmt.Printf("Web GUI: http://0.0.0.0:%s (user: %s)\n", port, webUser)

	webSrv := web.New(c, webUser, webPass)
	if err := webSrv.ListenAndServe(addr); err != nil {
		fmt.Fprintf(os.Stderr, "Web error: %v\n", err)
		os.Exit(1)
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
