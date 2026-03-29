//go:build linux || darwin

package client

import (
	"golang.org/x/sys/unix"
	"os"
)

// enableRawMode puts the terminal in raw mode and returns the original state.
func enableRawMode() (*unix.Termios, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}

	raw := *oldState
	// Disable echo, canonical mode, signals, and input processing
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Iflag &^= unix.IXON | unix.ICRNL | unix.BRKINT | unix.INPCK | unix.ISTRIP
	raw.Oflag &^= unix.OPOST
	// Read returns after each byte
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return nil, err
	}
	return oldState, nil
}

// disableRawMode restores the terminal to its original state.
func disableRawMode(state *unix.Termios) {
	if state != nil {
		unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETS, state)
	}
}
