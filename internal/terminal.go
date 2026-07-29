package internal

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

type ttyWriter struct {
	w io.Writer
}

func (tw *ttyWriter) Write(p []byte) (int, error) {
	var buf []byte
	needsCR := false
	for _, b := range p {
		if b == '\n' {
			if !needsCR {
				buf = append(buf, '\r', '\n')
				needsCR = true
			} else {
				buf = append(buf, '\n')
			}
		} else if b == '\r' {
			needsCR = true
			buf = append(buf, '\r')
		} else {
			needsCR = false
			buf = append(buf, b)
		}
	}
	return tw.w.Write(buf)
}

var _ io.Writer = (*ttyWriter)(nil)

func isTerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type terminalMode struct {
	state string
}

func newRawTerminalMode() (*terminalMode, error) {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = os.Stdin
	state, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	cmd = exec.Command("stty", "raw", "-echo")
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return &terminalMode{state: strings.TrimSpace(string(state))}, nil
}

func (mode *terminalMode) restore() {
	if mode == nil || mode.state == "" {
		return
	}
	cmd := exec.Command("stty", mode.state)
	cmd.Stdin = os.Stdin
	_ = cmd.Run()
}

type inputByte struct {
	b   byte
	err error
}

func readBytes(r io.Reader, out chan<- inputByte) {
	defer close(out)
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out <- inputByte{b: buf[0]}
		}
		if err != nil {
			out <- inputByte{err: err}
			return
		}
	}
}
