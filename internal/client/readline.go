package client

import (
	"bufio"
	"fmt"
	"os"
)

// readline provides line editing with command history using arrow keys.
// Supports: Up/Down (history), Left/Right (cursor), Backspace, Home, End, Delete.
type readline struct {
	history         []string
	maxHist         int
	fallbackScanner *bufio.Scanner
}

func newReadline() *readline {
	return &readline{maxHist: 500}
}

func (r *readline) addHistory(line string) {
	if line == "" {
		return
	}
	// Don't add duplicate of last entry
	if len(r.history) > 0 && r.history[len(r.history)-1] == line {
		return
	}
	r.history = append(r.history, line)
	if len(r.history) > r.maxHist {
		r.history = r.history[len(r.history)-r.maxHist:]
	}
}

// readLine reads a line with history support. Returns the line and false on EOF.
func (r *readline) readLine(prompt string) (string, bool) {
	fmt.Print(prompt)

	buf := make([]byte, 0, 256)
	histIdx := len(r.history) // points past end = "current input"
	var savedInput []byte      // saved current input when browsing history
	cursor := 0                // cursor position in buf

	raw, err := enableRawMode()
	if err != nil {
		// Fallback: simple read without raw mode
		return r.readLineFallback(prompt)
	}
	defer disableRawMode(raw)

	for {
		var b [1]byte
		n, err := os.Stdin.Read(b[:])
		if err != nil || n == 0 {
			fmt.Println()
			return "", false
		}

		ch := b[0]

		switch {
		case ch == 3: // Ctrl+C
			fmt.Println("^C")
			return "", false

		case ch == 4: // Ctrl+D
			if len(buf) == 0 {
				fmt.Println()
				return "", false
			}

		case ch == 10 || ch == 13: // Enter
			fmt.Print("\r\n")
			line := string(buf)
			r.addHistory(line)
			return line, true

		case ch == 127 || ch == 8: // Backspace
			if cursor > 0 {
				// Remove char before cursor
				copy(buf[cursor-1:], buf[cursor:])
				buf = buf[:len(buf)-1]
				cursor--
				r.refreshLine(prompt, buf, cursor)
			}

		case ch == 27: // Escape sequence
			r.handleEscape(&buf, &cursor, &histIdx, &savedInput, prompt)

		case ch == 1: // Ctrl+A (Home)
			cursor = 0
			r.refreshLine(prompt, buf, cursor)

		case ch == 5: // Ctrl+E (End)
			cursor = len(buf)
			r.refreshLine(prompt, buf, cursor)

		case ch == 21: // Ctrl+U (clear line before cursor)
			buf = buf[cursor:]
			cursor = 0
			r.refreshLine(prompt, buf, cursor)

		case ch == 11: // Ctrl+K (clear line after cursor)
			buf = buf[:cursor]
			r.refreshLine(prompt, buf, cursor)

		case ch == 12: // Ctrl+L (clear screen)
			fmt.Print("\033[2J\033[H")
			r.refreshLine(prompt, buf, cursor)

		case ch >= 32: // Printable character
			// Insert at cursor position
			buf = append(buf, 0)
			copy(buf[cursor+1:], buf[cursor:])
			buf[cursor] = ch
			cursor++
			r.refreshLine(prompt, buf, cursor)
		}
	}
}

func (r *readline) handleEscape(buf *[]byte, cursor *int, histIdx *int, savedInput *[]byte, prompt string) {
	var seq [2]byte
	os.Stdin.Read(seq[:1])
	if seq[0] != '[' {
		return
	}
	os.Stdin.Read(seq[1:])

	switch seq[1] {
	case 'A': // Up arrow
		if *histIdx > 0 {
			if *histIdx == len(r.history) {
				// Save current input
				*savedInput = make([]byte, len(*buf))
				copy(*savedInput, *buf)
			}
			*histIdx--
			*buf = []byte(r.history[*histIdx])
			*cursor = len(*buf)
			r.refreshLine(prompt, *buf, *cursor)
		}

	case 'B': // Down arrow
		if *histIdx < len(r.history) {
			*histIdx++
			if *histIdx == len(r.history) {
				// Restore saved input
				*buf = *savedInput
			} else {
				*buf = []byte(r.history[*histIdx])
			}
			*cursor = len(*buf)
			r.refreshLine(prompt, *buf, *cursor)
		}

	case 'C': // Right arrow
		if *cursor < len(*buf) {
			*cursor++
			r.refreshLine(prompt, *buf, *cursor)
		}

	case 'D': // Left arrow
		if *cursor > 0 {
			*cursor--
			r.refreshLine(prompt, *buf, *cursor)
		}

	case 'H': // Home
		*cursor = 0
		r.refreshLine(prompt, *buf, *cursor)

	case 'F': // End
		*cursor = len(*buf)
		r.refreshLine(prompt, *buf, *cursor)

	case '3': // Delete key (ESC [ 3 ~)
		var tilde [1]byte
		os.Stdin.Read(tilde[:])
		if tilde[0] == '~' && *cursor < len(*buf) {
			copy((*buf)[*cursor:], (*buf)[*cursor+1:])
			*buf = (*buf)[:len(*buf)-1]
			r.refreshLine(prompt, *buf, *cursor)
		}
	}
}

func (r *readline) refreshLine(prompt string, buf []byte, cursor int) {
	// Move to start of line, clear it, rewrite prompt + buffer, position cursor
	fmt.Printf("\r\033[K%s%s", prompt, string(buf))
	// Move cursor to correct position
	if cursor < len(buf) {
		// Move cursor back from end
		fmt.Printf("\033[%dD", len(buf)-cursor)
	}
}

// readLineFallback is used when raw mode is not available (piped input).
func (r *readline) readLineFallback(prompt string) (string, bool) {
	if r.fallbackScanner == nil {
		r.fallbackScanner = bufio.NewScanner(os.Stdin)
	}
	if !r.fallbackScanner.Scan() {
		return "", false
	}
	line := r.fallbackScanner.Text()
	r.addHistory(line)
	return line, true
}
