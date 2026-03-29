//go:build windows

package client

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode       = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode       = kernel32.NewProc("SetConsoleMode")
)

const (
	enableProcessedInput       = 0x0001
	enableLineInput            = 0x0002
	enableEchoInput            = 0x0004
	enableVirtualTerminalInput = 0x0200
)

type winTermState struct {
	mode uint32
}

func enableRawMode() (*winTermState, error) {
	handle := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	r1, _, e1 := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r1 == 0 {
		return nil, e1
	}

	state := &winTermState{mode: mode}

	// Disable line input and echo, enable virtual terminal input for arrow keys
	raw := mode &^ (enableLineInput | enableEchoInput | enableProcessedInput)
	raw |= enableVirtualTerminalInput

	r1, _, e1 = procSetConsoleMode.Call(uintptr(handle), uintptr(raw))
	if r1 == 0 {
		return nil, e1
	}
	return state, nil
}

func disableRawMode(state *winTermState) {
	if state != nil {
		handle := syscall.Handle(os.Stdin.Fd())
		procSetConsoleMode.Call(uintptr(handle), uintptr(state.mode))
	}
}
