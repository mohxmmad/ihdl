package internal

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
)

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
	state, err := exec.Command("stty", "-g").Output()
	if err != nil {
		return nil, err
	}
	if err := exec.Command("stty", "raw", "-echo").Run(); err != nil {
		return nil, err
	}
	return &terminalMode{state: strings.TrimSpace(string(state))}, nil
}

func (mode *terminalMode) restore() {
	if mode == nil || mode.state == "" {
		return
	}
	_ = exec.Command("stty", mode.state).Run()
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

func appendCommandByte(buf *bytes.Buffer, b byte) {
	buf.WriteByte(b)
}
